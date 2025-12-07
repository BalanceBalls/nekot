package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BalanceBalls/nekot/clients"
	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/settings"
	"github.com/BalanceBalls/nekot/user"
	"github.com/BalanceBalls/nekot/util"
	"github.com/PuerkitoBio/goquery"
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
		ResponseProcessingState: util.Idle,
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
		// util.Slog.Debug("response chunk recieved", "data", msg)
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

func (m Orchestrator) ResumeCompletion(
	ctx context.Context,
	resp chan util.ProcessApiCompletionResponse,
	messages []util.LocalStoreMessage,
) tea.Cmd {
	return m.InferenceClient.RequestCompletion(ctx, messages, m.Settings, resp)
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
	util.Slog.Debug("processing state before new chunk",
		"state", m.ResponseProcessingState,
		"chunks ready", len(m.ArrayOfProcessResult),
		"data", msg)

	p := NewMessageProcessor(m.ArrayOfProcessResult, m.ResponseBuffer, m.ResponseProcessingState, m.Settings)
	result, err := p.Process(msg)

	// util.Slog.Debug("processed chunk",
	// 	"id", msg.ID,
	// 	"chunks ready", len(result.CurrentResponseDataChunks))

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
			m.finishResponseProcessing(result.JSONResponse, false))
	}

	if len(result.ToolCalls) > 0 {
		var cmds []tea.Cmd
		cmds = append(cmds, m.finishResponseProcessing(result.JSONResponse, true))

		for _, tc := range result.ToolCalls {
			switch tc.Name {
			case "web_search":
				cmds = append(cmds, doWebSearch(tc.Args))
			}
		}

		return tea.Batch(cmds...)
	}

	m.CurrentAnswer = result.CurrentResponse

	if result.State == util.Finalized {
		return m.finishResponseProcessing(result.JSONResponse, false)
	}

	return nil
}

func doWebSearch(args map[string]string) tea.Cmd {
	query := args["query"]
	toolName := "web_search"

	return func() tea.Msg {
		util.Slog.Debug("executing a web search for query", "query", query)
		time.Sleep(time.Second * 2)
		result, err := performDuckDuckGoSearch(query)
		isSuccess := true
		if err != nil {
			util.Slog.Error("web search failed", "error", err.Error())
			isSuccess = false
		}

		toolCallResult := ""
		jsonData, err := json.Marshal(result)
		if err != nil {
			util.Slog.Error("failed to serialize web_search result data", "error", err.Error())
			isSuccess = false
		}

		if isSuccess {
			toolCallResult = string(jsonData)
		}
		return ToolCallComplete{
			IsSuccess: isSuccess,
			Name:      toolName,
			Result:    toolCallResult,
		}
	}
}

type WebSearchResult struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Link    string `json:"link"`
}

func performDuckDuckGoSearch(query string) ([]WebSearchResult, error) {
	baseURL := "https://html.duckduckgo.com/html/?"
	params := url.Values{}
	params.Add("q", query)
	requestURL := baseURL + params.Encode()

	client := &http.Client{}
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)AppleWebKit/537.36(KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var results []WebSearchResult

	doc.Find(".result.results_links.results_links_deep.web-result").
		EachWithBreak(func(i int, s *goquery.Selection) bool {
			if i >= 5 {
				return false
			}

			title := strings.TrimSpace(s.Find("h2.result__title a.result__a").Text())
			linkHref, _ := s.Find("h2.result__title a.result__a").Attr("href")
			link := ""
			if strings.Contains(linkHref, "/l/?uddg=") {
				unescapedURL, err := url.Parse(linkHref)
				if err == nil {
					link = unescapedURL.Query().Get("uddg")
				} else {
					link = linkHref
				}

			} else {
				link = linkHref
			}

			snippet := strings.TrimSpace(s.Find("a.result__snippet").Text())

			if title != "" && link != "" {
				results = append(results, WebSearchResult{
					Title:   title,
					Snippet: snippet,
					Link:    link,
				})
			}
			return true
		})

	return results, nil
}

func (m *Orchestrator) finishResponseProcessing(response util.LocalStoreMessage, isToolCall bool) tea.Cmd {
	m.mu.Lock()
	defer m.mu.Unlock()

	util.Slog.Info("response received in full, finishing response processing now")
	util.Slog.Info("final response message", "content", response)

	m.ArrayOfMessages = append(
		m.ArrayOfMessages,
		response,
	)

	err := m.sessionService.UpdateSessionMessages(m.CurrentSessionID, m.ArrayOfMessages)
	if err != nil {
		return m.resetStateAndCreateError(err.Error())
	}

	if isToolCall {
		m.ResponseProcessingState = util.AwaitingToolCallResult
		return util.SendProcessingStateChangedMsg(util.AwaitingToolCallResult)
	}

	m.ResponseProcessingState = util.Idle
	m.CurrentAnswer = ""
	m.ResponseBuffer = ""
	m.ArrayOfProcessResult = []util.ProcessApiCompletionResponse{}

	return util.SendProcessingStateChangedMsg(util.Idle)
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
	m.ResponseProcessingState = util.Idle
	return tea.Batch(util.MakeErrorMsg(errMsg), util.SendProcessingStateChangedMsg(util.Idle))
}
