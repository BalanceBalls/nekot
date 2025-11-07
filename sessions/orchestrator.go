package sessions

import (
	"database/sql"
	"fmt"
	"slices"
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
	ResponseProcessingState   ProcessingState
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
		util.Slog.Error("failed to extract config from context")
		panic("No config found in context")
	}

	settingsService := settings.NewSettingsService(db)
	llmClient := clients.ResolveLlmClient(
		config.Provider,
		config.ProviderBaseUrl,
		config.SystemMessage,
	)

	return Orchestrator{
		mainCtx:                 ctx,
		config:                  *config,
		ArrayOfProcessResult:    []util.ProcessApiCompletionResponse{},
		sessionService:          ss,
		userService:             us,
		settingsService:         settingsService,
		InferenceClient:         llmClient,
		ResponseProcessingState: Idle,
		mu:                      &sync.RWMutex{},
	}
}

func (m Orchestrator) Init() tea.Cmd {
	// Need to load the latest session as the current session  (select recently created)
	// OR we need to create a brand new session for the user with a random name (insert new and return)

	initCtx, cancel := context.
		WithTimeout(m.mainCtx, time.Duration(util.DefaultRequestTimeOutSec*time.Second))

	settingsData := func() tea.Msg {
		defer cancel()
		util.Slog.Debug("orchestrator.Init(): settings loaded from db")
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

		util.Slog.Debug("orchestrator.Init(): settings loaded from db")

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
		util.Slog.Debug("orchestrator loaded data from db", "Session name:", msg.Session.SessionName)
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
		util.Slog.Debug("response chunk recieved", "data", msg)
		cmds = append(cmds, m.hanldeProcessAPICompletionResponse(msg))
		cmds = append(cmds, SendResponseChunkProcessedMsg(m.CurrentAnswer, m.ArrayOfMessages))
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

func (m Orchestrator) IsIdle() bool {
	return m.ResponseProcessingState == Idle
}

func (m Orchestrator) IsProcessing() bool {
	processingStates := []ProcessingState{
		ProcessingChunks,
		AwaitingFinalization,
		Finalized,
	}
	return slices.Contains(processingStates, m.ResponseProcessingState)
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
	util.Slog.Debug("processing state before new chunk",
		"state", m.ResponseProcessingState,
		"chunks ready", len(m.ArrayOfProcessResult))

	p := NewMessageProcessor(m.ArrayOfProcessResult, m.ResponseBuffer, m.ResponseProcessingState, m.Settings)
	result, err := p.Process(msg)

	util.Slog.Debug("processed chunk",
		"id", msg.ID,
		"chunks ready", len(result.CurrentResponseDataChunks))

	if err != nil {
		util.Slog.Error("error occured on processing a chunk", "chunk", msg, "error", err.Error())
		m.mu.Unlock()
		return m.resetStateAndCreateError(err.Error())
	}

	m.handleTokenStatsUpdate(result)
	m.appendAndOrderProcessResults(result)

	m.mu.Unlock()

	if result.IsSkipped {
		return nil
	}

	if result.IsCancelled {
		return tea.Batch(
			util.SendNotificationMsg(util.CancelledNotification),
			m.finishResponseProcessing(result.JSONResponse))
	}

	m.CurrentAnswer = result.CurrentResponse

	if result.State == Finalized {
		return m.finishResponseProcessing(result.JSONResponse)
	}

	return nil
}

func (m *Orchestrator) finishResponseProcessing(response util.MessageToSend) tea.Cmd {
	m.mu.Lock()
	defer m.mu.Unlock()

	util.Slog.Info("response received in full, finishing response processing now")
	util.Slog.Info("final response message", "content", response)

	m.ArrayOfMessages = append(
		m.ArrayOfMessages,
		response,
	)

	err := m.sessionService.UpdateSessionMessages(m.CurrentSessionID, m.ArrayOfMessages)
	m.CurrentAnswer = ""
	m.ResponseBuffer = ""
	m.ResponseProcessingState = Idle
	m.ArrayOfProcessResult = []util.ProcessApiCompletionResponse{}

	if err != nil {
		return m.resetStateAndCreateError(err.Error())
	}

	return util.SendProcessingStateChangedMsg(false)
}

func (m *Orchestrator) handleTokenStatsUpdate(processingResult ProcessingResult) {
	if processingResult.PromptTokens > 0 && processingResult.CompletionTokens > 0 {
		m.sessionService.UpdateSessionTokens(
			m.CurrentSessionID,
			processingResult.PromptTokens,
			processingResult.CompletionTokens,
		)
	}
}

func (m *Orchestrator) appendAndOrderProcessResults(processingResult ProcessingResult) {
	m.ResponseBuffer = processingResult.CurrentResponse
	m.ArrayOfProcessResult = processingResult.CurrentResponseDataChunks
	m.ResponseProcessingState = processingResult.State

	sort.SliceStable(m.ArrayOfProcessResult, func(i, j int) bool {
		return m.ArrayOfProcessResult[i].ID < m.ArrayOfProcessResult[j].ID
	})
}

func (m *Orchestrator) resetStateAndCreateError(errMsg string) tea.Cmd {
	m.ArrayOfProcessResult = []util.ProcessApiCompletionResponse{}
	m.CurrentAnswer = ""
	m.ResponseProcessingState = Idle
	return tea.Batch(util.MakeErrorMsg(errMsg), util.SendProcessingStateChangedMsg(false))
}
