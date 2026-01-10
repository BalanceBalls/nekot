package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/BalanceBalls/nekot/clients"
	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/extensions/websearch"
	"github.com/BalanceBalls/nekot/settings"
	"github.com/BalanceBalls/nekot/user"
	"github.com/BalanceBalls/nekot/util"
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
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
	ArrayOfMessages           []util.LocalStoreMessage
	CurrentAnswer             string
	ResponseBuffer            string
	ResponseProcessingState   util.ProcessingState
	AllSessions               []Session
	ProcessingMode            string

	settingsReady    bool
	dataLoaded       bool
	initialized      bool
	mainCtx          context.Context
	processingCtx    context.Context
	processingCancel context.CancelFunc
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
		ResponseProcessingState: util.Idle,
		mu:                      &sync.RWMutex{},
	}
}

func (m Orchestrator) Init() tea.Cmd {

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

	case util.InterruptProcessingMsg:
		if m.processingCancel != nil {
			util.Slog.Debug("cancellation func is not null")
			m.processingCancel()
			cmds = append(cmds, util.SendProcessingStateChangedMsg(util.Idle))
			cmds = append(cmds, util.SendNotificationMsg(util.CancelledNotification))
		}

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
		cmds = append(cmds, m.hanldeProcessAPICompletionResponse(msg))
		cmds = append(cmds, SendResponseChunkProcessedMsg(m.CurrentAnswer, m.ArrayOfMessages, false))

	case ToolCallRequest:
		tc := msg.ToolCall
		switch tc.Function.Name {
		case "web_search":
			webSearchCtx := m.setProcessingContext(m.processingCtx)
			return m, m.doWebSearch(webSearchCtx, tc.Id, tc.Function.Args)
		}

	case InferenceFinalized:
		return m, m.finishResponseProcessing(msg.Response, msg.IsToolCall)
	}

	if m.dataLoaded && m.settingsReady && !m.initialized {
		cmds = append(cmds, util.SendAsyncDependencyReadyMsg(util.Orchestrator))
		m.initialized = true
	}

	return m, tea.Batch(cmds...)
}

func (m *Orchestrator) GetCompletion(
	ctx context.Context,
	resp chan util.ProcessApiCompletionResponse,
) tea.Cmd {
	m.processingCancel = nil
	m.processingCtx = nil
	return m.InferenceClient.RequestCompletion(m.setProcessingContext(ctx), m.ArrayOfMessages, m.Settings, resp)
}

func (m *Orchestrator) ResumeCompletion(
	ctx context.Context,
	resp chan util.ProcessApiCompletionResponse,
) tea.Cmd {
	m.mu.Lock()
	defer m.mu.Unlock()

	updatedSession, _ := m.sessionService.GetSession(m.CurrentSessionID)
	m.setCurrentSessionData(updatedSession)
	return m.InferenceClient.RequestCompletion(m.setProcessingContext(ctx), updatedSession.Messages, m.Settings, resp)
}

func (m *Orchestrator) setProcessingContext(ctx context.Context) context.Context {
	if m.processingCtx != nil {
		return m.processingCtx
	}

	processingCtx, processingCancel := context.WithCancel(ctx)
	m.processingCancel = processingCancel
	m.processingCtx = processingCtx
	return processingCtx
}

func (m Orchestrator) GetCurrentSessionId() int {
	return m.CurrentSessionID
}

func (m Orchestrator) IsIdle() bool {
	return m.ResponseProcessingState == util.Idle
}

func (m Orchestrator) IsProcessing() bool {
	return util.IsProcessingActive(m.ResponseProcessingState)
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
	defer m.mu.Unlock()

	util.Slog.Debug("processing state before new chunk",
		"state", m.ResponseProcessingState,
		"chunks ready", len(m.ArrayOfProcessResult),
		"data", msg.Result,
		"isFinal", msg.Final)

	prevProcessingState := m.ResponseProcessingState
	p := NewMessageProcessor(m.ArrayOfProcessResult, m.ResponseBuffer, m.ResponseProcessingState, m.Settings)
	result, err := p.Process(msg)

	util.Slog.Debug("processed chunk",
		"id", msg.ID,
		"chunks ready", len(result.CurrentResponseDataChunks),
		"state", result.State)

	if err != nil {
		util.Slog.Error("error occured on processing a chunk", "chunk", msg, "error", err.Error())
		return m.resetStateAndCreateError(err.Error())
	}

	m.handleTokenStatsUpdate(result)
	m.appendAndOrderProcessResults(result)

	if result.IsSkipped {
		util.Slog.Info("result skipped", "data", msg.Result)
		return nil
	}

	if result.IsCancelled {
		util.Slog.Info("result cancelled", "json result", result.JSONResponse.Content)
		return tea.Batch(
			FinalizeResponse(result.JSONResponse, true),
			util.SendNotificationMsg(util.CancelledNotification),
		)
	}

	if len(result.ToolCalls) > 0 {
		var cmds []tea.Cmd
		util.Slog.Debug("processed chunk with a tool call",
			"chunk", msg.Result.Choices,
			"tools", result.ToolCalls)

		cmds = append(cmds, util.SendProcessingStateChangedMsg(result.State))
		cmds = append(cmds, FinalizeResponse(result.JSONResponse, true))

		for _, tc := range result.ToolCalls {
			cmds = append(cmds, ExecuteToolCallRequest(tc))
		}

		return tea.Sequence(cmds...)
	}

	m.CurrentAnswer = result.CurrentResponse

	if result.State == util.Finalized {
		util.Slog.Debug("result finalized", "json result", result.JSONResponse.Content)
		return FinalizeResponse(result.JSONResponse, false)
	}

	if prevProcessingState != result.State {
		return util.SendProcessingStateChangedMsg(result.State)
	}

	return nil
}

func (m *Orchestrator) doWebSearch(ctx context.Context, id string, args map[string]string) tea.Cmd {
	return func() tea.Msg {
		toolName := "web_search"
		result, err := websearch.PrepareContextFromWebSearch(ctx, args["query"])
		if err != nil {
			util.Slog.Error("web search failed", "error", err.Error())
			return util.ErrorEvent{Message: err.Error()}
		}

		jsonData, err := json.Marshal(result)
		if err != nil {
			util.Slog.Error("failed to serialize web_search result data", "error", err.Error())
			return ToolCallComplete{
				Id:        id,
				IsSuccess: false,
				Name:      toolName,
				Result:    "",
			}
		}

		util.Slog.Debug("retrieved context from a web search")
		return ToolCallComplete{
			Id:        id,
			IsSuccess: true,
			Name:      toolName,
			Result:    string(jsonData),
		}
	}
}

func (m *Orchestrator) finishResponseProcessing(response util.LocalStoreMessage, isToolCall bool) tea.Cmd {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ArrayOfMessages = append(
		m.ArrayOfMessages,
		response,
	)

	err := m.sessionService.UpdateSessionMessages(m.CurrentSessionID, m.ArrayOfMessages)
	if err != nil {
		return m.resetStateAndCreateError(err.Error())
	}

	nextProcessingState := util.Idle
	if isToolCall {
		nextProcessingState = util.AwaitingToolCallResult
	}

	util.Slog.Info("response received in full, finishing response processing now",
		"prev state", m.ResponseProcessingState,
		"next state", nextProcessingState)
	m.ResponseProcessingState = nextProcessingState
	m.CurrentAnswer = ""
	m.ResponseBuffer = ""
	m.ArrayOfProcessResult = []util.ProcessApiCompletionResponse{}

	return tea.Batch(
		util.SendProcessingStateChangedMsg(nextProcessingState),
		SendResponseChunkProcessedMsg(m.CurrentAnswer, m.ArrayOfMessages, true),
	)
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
}

func (m *Orchestrator) resetStateAndCreateError(errMsg string) tea.Cmd {
	m.ArrayOfProcessResult = []util.ProcessApiCompletionResponse{}
	m.CurrentAnswer = ""
	m.ResponseProcessingState = util.Idle
	return tea.Batch(util.MakeErrorMsg(errMsg), util.SendProcessingStateChangedMsg(util.Idle))
}
