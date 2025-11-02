package sessions

import (
	"database/sql"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/BalanceBalls/nekot/clients"
	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/settings"
	"github.com/BalanceBalls/nekot/user"
	"github.com/BalanceBalls/nekot/util"
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/net/context"
)

const (
	IDLE       = "idle"
	PROCESSING = "processing"
	ERROR      = "error"
)

type Orchestrator struct {
	sessionService  *SessionService
	userService     *user.UserService
	settingsService *settings.SettingsService
	config          config.Config

	mu                        *sync.RWMutex
	InferenceClient           util.LlmClient
	Settings                  util.Settings
	CurrentSessionID          int
	CurrentSessionName        string
	CurrentSessionIsTemporary bool
	ArrayOfProcessResult      []util.ProcessApiCompletionResponse
	ArrayOfMessages           []util.MessageToSend
	CurrentAnswer             string
	ResponseBuffer            string
	AllSessions               []Session
	ProcessingMode            string

	settingsReady bool
	dataLoaded    bool
	initialized   bool
	mainCtx       context.Context
}

func NewOrchestrator(db *sql.DB, ctx context.Context) Orchestrator {
	ss := NewSessionService(db)
	us := user.NewUserService(db)

	config, ok := config.FromContext(ctx)
	if !ok {
		fmt.Println("No config found")
		panic("No config found in context")
	}

	settingsService := settings.NewSettingsService(db)
	llmClient := clients.ResolveLlmClient(
		config.Provider,
		config.ProviderBaseUrl,
		config.SystemMessage,
	)

	return Orchestrator{
		mainCtx:              ctx,
		config:               *config,
		ArrayOfProcessResult: []util.ProcessApiCompletionResponse{},
		sessionService:       ss,
		userService:          us,
		settingsService:      settingsService,
		InferenceClient:      llmClient,
		ProcessingMode:       IDLE,
		mu:                   &sync.RWMutex{},
	}
}

func (m Orchestrator) Init() tea.Cmd {
	// Need to load the latest session as the current session  (select recently created)
	// OR we need to create a brand new session for the user with a random name (insert new and return)

	initCtx, cancel := context.
		WithTimeout(m.mainCtx, time.Duration(util.DefaultRequestTimeOutSec*time.Second))

	settingsData := func() tea.Msg {
		defer cancel()
		return m.settingsService.GetSettings(initCtx, util.DefaultSettingsId, m.config)
	}

	dbData := func() tea.Msg {
		mostRecentSession, err := m.sessionService.GetMostRecessionSessionOrCreateOne()
		if err != nil {
			return util.MakeErrorMsg(err.Error())
		}

		user, err := m.userService.GetUser(1)
		if err != nil {
			if err == sql.ErrNoRows {
				user, err = m.userService.InsertNewUser(mostRecentSession.ID)
			} else {
				return util.MakeErrorMsg(err.Error())
			}
		}

		mostRecentSession, err = m.sessionService.GetSession(user.CurrentActiveSessionID)
		if err != nil {
			return util.MakeErrorMsg(err.Error())
		}

		allSessions, err := m.sessionService.GetAllSessions()
		if err != nil {
			return util.MakeErrorMsg(err.Error())
		}

		dbLoadEvent := LoadDataFromDB{
			Session:                mostRecentSession,
			AllSessions:            allSessions,
			CurrentActiveSessionID: user.CurrentActiveSessionID,
		}
		return dbLoadEvent
	}

	return tea.Batch(settingsData, dbData)
}

func (m Orchestrator) Update(msg tea.Msg) (Orchestrator, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case util.CopyLastMsg:
		latestBotMessage, err := m.GetLatestBotMessage()
		if err == nil {
			clipboard.WriteAll(latestBotMessage)
			cmds = append(cmds, util.SendNotificationMsg(util.CopiedNotification))
		}

	case util.CopyAllMsgs:
		clipboard.WriteAll(m.GetMessagesAsString())
		cmds = append(cmds, util.SendNotificationMsg(util.CopiedNotification))

	case SaveQuickChat:
		log.Println("Save quick chat received. IsTemporary: ", m.CurrentSessionIsTemporary)
		if m.CurrentSessionIsTemporary {
			m.sessionService.SaveQuickChat(m.CurrentSessionID)
			updatedSession, _ := m.sessionService.GetSession(m.CurrentSessionID)
			cmds = append(cmds, SendUpdateCurrentSessionMsg(updatedSession))
			cmds = append(cmds, SendRefreshSessionsListMsg())
			cmds = append(cmds, util.SendNotificationMsg(util.SessionSavedNotification))
		}

	case UpdateCurrentSession:
		if !msg.Session.IsTemporary {
			m.sessionService.SweepTemporarySessions()
			m.userService.UpdateUserCurrentActiveSession(1, msg.Session.ID)
		}

		m.setCurrentSessionData(msg.Session)

	case LoadDataFromDB:
		log.Println("Orchestrator loaded data from db. Session name:", msg.Session.SessionName)

		m.setCurrentSessionData(msg.Session)
		m.AllSessions = msg.AllSessions
		m.dataLoaded = true

	case settings.UpdateSettingsEvent:
		if msg.Err != nil {
			return m, util.MakeErrorMsg(msg.Err.Error())
		}
		m.Settings = msg.Settings
		m.settingsReady = true

	case util.ProcessApiCompletionResponse:
		log.Println(msg)
		// add the latest message to the array of messages
		cmds = append(cmds, m.hanldeProcessAPICompletionResponse(msg))

		if !msg.Final {
			cmds = append(cmds, SendResponseChunkProcessedMsg(m.CurrentAnswer, m.ArrayOfMessages))
		}
	}

	if m.dataLoaded && m.settingsReady && !m.initialized {
		cmds = append(cmds, util.SendAsyncDependencyReadyMsg(util.Orchestrator))
		m.initialized = true
	}

	return m, tea.Batch(cmds...)
}

func (m Orchestrator) GetCompletion(
	ctx context.Context,
	resp chan util.ProcessApiCompletionResponse,
) tea.Cmd {
	return m.InferenceClient.RequestCompletion(ctx, m.ArrayOfMessages, m.Settings, resp)
}

func (m Orchestrator) GetLatestBotMessage() (string, error) {
	// the last bot in the array is actually the blank message (the stop command)
	lastIndex := len(m.ArrayOfMessages) - 2
	// Check if lastIndex is within the bounds of the slice
	if lastIndex >= 0 && lastIndex < len(m.ArrayOfMessages) {
		return m.ArrayOfMessages[lastIndex].Content, nil
	}
	return "", fmt.Errorf(
		"no messages found in array of messages. Length: %v",
		len(m.ArrayOfMessages),
	)
}

func (m Orchestrator) GetMessagesAsString() string {
	var messages string
	for _, message := range m.ArrayOfMessages {
		messageToUse := message.Content

		if messages == "" {
			messages = messageToUse
			continue
		}

		messages = messages + "\n" + messageToUse
	}

	return messages
}

func (m *Orchestrator) setCurrentSessionData(session Session) {
	m.CurrentSessionIsTemporary = session.IsTemporary
	m.CurrentSessionID = session.ID
	m.CurrentSessionName = session.SessionName
	m.ArrayOfMessages = session.Messages
}

func (m *Orchestrator) hanldeProcessAPICompletionResponse(
	msg util.ProcessApiCompletionResponse,
) tea.Cmd {

	m.mu.Lock()
	p := NewMessageProcessor(m.ArrayOfProcessResult, m.ResponseBuffer, m.Settings)
	result, err := p.Process(msg)

	if err != nil {
		log.Printf(
			"error occured on processing the following chunk (%s):\n%s",
			err.Error(),
			msg,
		)
		m.mu.Unlock()
		return m.resetStateAndCreateError(err.Error())
	}

	m.appendAndOrderProcessResults(result)
	m.sessionService.UpdateSessionTokens(
		m.CurrentSessionID,
		result.PromptTokens,
		result.CompletionTokens,
	)

	m.mu.Unlock()

	if result.IsSkipped {
		return nil
	}

	if result.IsCancelled {
		return tea.Batch(
			util.SendNotificationMsg(util.CancelledNotification),
			m.finishResponseProcessing(result.JSONResponse))
	}

	m.ProcessingMode = PROCESSING
	m.CurrentAnswer = result.CurrentResponse

	if result.IsFinished {
		return m.finishResponseProcessing(result.JSONResponse)
	}

	return nil
}

func (m *Orchestrator) finishResponseProcessing(response util.MessageToSend) tea.Cmd {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Println("finishResponse triggered")

	m.ArrayOfMessages = append(
		m.ArrayOfMessages,
		response,
	)

	err := m.sessionService.UpdateSessionMessages(m.CurrentSessionID, m.ArrayOfMessages)
	m.ProcessingMode = IDLE
	m.CurrentAnswer = ""
	m.ResponseBuffer = ""
	m.ArrayOfProcessResult = []util.ProcessApiCompletionResponse{}

	if err != nil {
		util.Log("Error updating session messages", err)
		return m.resetStateAndCreateError(err.Error())
	}

	return util.SendProcessingStateChangedMsg(false)
}

func (m *Orchestrator) appendAndOrderProcessResults(processingResult ParsingResult) {
	m.ResponseBuffer = processingResult.CurrentResponse
	m.ArrayOfProcessResult = processingResult.CurrentResponseDataChunks

	sort.SliceStable(m.ArrayOfProcessResult, func(i, j int) bool {
		return m.ArrayOfProcessResult[i].ID < m.ArrayOfProcessResult[j].ID
	})
}

func (m *Orchestrator) resetStateAndCreateError(errMsg string) tea.Cmd {
	m.ProcessingMode = ERROR
	m.ArrayOfProcessResult = []util.ProcessApiCompletionResponse{}
	m.CurrentAnswer = ""
	return tea.Batch(util.MakeErrorMsg(errMsg), util.SendProcessingStateChangedMsg(false))
}
