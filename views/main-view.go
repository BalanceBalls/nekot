package views

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
	"golang.org/x/term"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/panes"
	"github.com/BalanceBalls/nekot/sessions"
	"github.com/BalanceBalls/nekot/util"
)

const pulsarIntervalMs = 300

var asyncDeps = []util.AsyncDependency{util.SettingsPaneModule, util.Orchestrator}

type keyMap struct {
	cancel        key.Binding
	zenMode       key.Binding
	editorMode    key.Binding
	nextPane      key.Binding
	previousPane  key.Binding
	jumpToPane    key.Binding
	newSession    key.Binding
	quickChat     key.Binding
	saveQuickChat key.Binding
	quit          key.Binding
}

var defaultKeyMap = keyMap{
	cancel: key.NewBinding(
		key.WithKeys("ctrl+s", "ctrl+b"),
		key.WithHelp("ctrl+b/ctrl+s", "stop inference"),
	),
	zenMode: key.NewBinding(
		key.WithKeys("ctrl+o"),
		key.WithHelp("ctrl+o", "activate/deactivate zen mode"),
	),
	editorMode: key.NewBinding(
		key.WithKeys("ctrl+e"),
		key.WithHelp("ctrl+e", "enter/exit editor mode"),
	),
	quit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit app")),
	quickChat: key.NewBinding(
		key.WithKeys("ctrl+q"),
		key.WithHelp("ctrl+q", "start quick chat"),
	),
	saveQuickChat: key.NewBinding(
		key.WithKeys("ctrl+x"),
		key.WithHelp("ctrl+x", "save quick chat"),
	),
	jumpToPane: key.NewBinding(
		key.WithKeys("1", "2", "3", "4"),
		key.WithHelp("1,2,3,4", "jump to specific pane"),
	),
	nextPane: key.NewBinding(
		key.WithKeys(tea.KeyTab.String()),
		key.WithHelp("TAB", "move to next pane"),
	),
	previousPane: key.NewBinding(
		key.WithKeys(tea.KeyShiftTab.String()),
		key.WithHelp("SHIFT+TAB", "move to previous pane"),
	),
	newSession: key.NewBinding(
		key.WithKeys("ctrl+n"),
		key.WithHelp("ctrl+n", "add new session"),
	),
}

type MainView struct {
	viewReady        bool
	controlsLocked   bool
	focused          util.Pane
	viewMode         util.ViewMode
	error            util.ErrorEvent
	currentSessionID string
	keys             keyMap

	chatPane         panes.ChatPane
	promptPane       panes.PromptPane
	sessionsPane     panes.SessionsPane
	settingsPane     panes.SettingsPane
	infoPane         panes.InfoPane
	loadedDeps       []util.AsyncDependency
	pendingToolCalls []util.ToolCall
	initialPrompt    string

	flags               config.StartupFlags
	config              config.Config
	sessionOrchestrator sessions.Orchestrator
	sessionService      sessions.SessionService
	context             context.Context
	processingCtx       context.Context
	processingCancel    context.CancelFunc

	terminalWidth  int
	terminalHeight int
}

// Windows terminal is not able to work with tea.WindowSizeMsg directly
// Wrokaround is to constatly check if the terminal windows size changed
// and manually triggering tea.WindowSizeMsg
type checkDimensionsMsg int

func dimensionsPulsar() tea.Msg {
	time.Sleep(time.Millisecond * pulsarIntervalMs)
	return checkDimensionsMsg(1)
}

func NewMainView(db *sql.DB, ctx context.Context) MainView {
	util.Slog.Debug("initializing main view")
	promptPane := panes.NewPromptPane(ctx)
	sessionsPane := panes.NewSessionsPane(db, ctx)
	settingsPane := panes.NewSettingsPane(db, ctx)
	statusBarPane := panes.NewInfoPane(db, ctx)
	sessionsService := sessions.NewSessionService(db)

	w, h := util.CalcChatPaneSize(
		util.DefaultTerminalWidth,
		util.DefaultTerminalHeight,
		util.NormalMode,
	)
	chatPane := panes.NewChatPane(ctx, w, h)
	orchestrator := sessions.NewOrchestrator(db, ctx)

	flags, ok := config.FlagsFromContext(ctx)
	if !ok {
		util.Slog.Error("failed to extract startup flags from context")
		flags = &config.StartupFlags{}
	}

	config, ok := config.FromContext(ctx)
	if !ok {
		util.Slog.Error("failed to extract config from context")
		panic("No config found in context")
	}

	util.Slog.Debug("config loaded", "values", config)
	return MainView{
		keys:                defaultKeyMap,
		viewMode:            util.NormalMode,
		focused:             util.PromptPane,
		currentSessionID:    "",
		sessionOrchestrator: orchestrator,
		sessionService:      *sessionsService,
		promptPane:          promptPane,
		sessionsPane:        sessionsPane,
		settingsPane:        settingsPane,
		infoPane:            statusBarPane,
		chatPane:            chatPane,
		config:              *config,
		flags:               *flags,
		context:             ctx,
		initialPrompt:       flags.InitialPrompt,
	}
}

func (m MainView) Init() tea.Cmd {
	return tea.Sequence(
		m.sessionOrchestrator.Init(),
		m.sessionsPane.Init(),
		m.settingsPane.Init(),
		m.promptPane.Init(),
		m.chatPane.Init(),
		func() tea.Msg { return dimensionsPulsar() },
	)
}

func (m MainView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	m.sessionOrchestrator, cmd = m.sessionOrchestrator.Update(msg)
	cmds = append(cmds, cmd)

	m.infoPane, cmd = m.infoPane.Update(msg)
	cmds = append(cmds, cmd)

	m.promptPane, cmd = m.promptPane.Update(msg)
	cmds = append(cmds, cmd)

	if m.sessionOrchestrator.ResponseProcessingState == util.Idle {
		m.sessionsPane, cmd = m.sessionsPane.Update(msg)
		cmds = append(cmds, cmd)
		m.settingsPane, cmd = m.settingsPane.Update(msg)
		cmds = append(cmds, cmd)
	}

	switch msg := msg.(type) {

	case util.ErrorEvent:
		m.sessionOrchestrator.ResponseProcessingState = util.Idle
		m.error = msg
		m.viewReady = true
		m.controlsLocked = false
		cmds = append(cmds, util.SendProcessingStateChangedMsg(util.Idle))

	case checkDimensionsMsg:
		if runtime.GOOS == "windows" {
			w, h, _ := term.GetSize(int(os.Stdout.Fd()))
			if m.terminalWidth != w || m.terminalHeight != h {
				cmds = append(cmds, func() tea.Msg { return tea.WindowSizeMsg{Width: w, Height: h} })
			}
			cmds = append(cmds, dimensionsPulsar)
		}

	case util.ViewModeChanged:
		m.viewMode = msg.Mode

	case util.SwitchToPaneMsg:
		if util.IsFocusAllowed(m.viewMode, msg.Target, m.terminalWidth) {
			m.focused = msg.Target
			m.resetFocus()
		}

	case sessions.UpdateCurrentSession:
		if m.initialPrompt != "" && m.flags.StartNewSession {
			cmds = append(cmds, util.SendPromptReadyMsg(m.initialPrompt, []util.Attachment{}))
			m.initialPrompt = ""
		}

	case util.ProcessingStateChanged:
		if msg.State == util.Idle {
			m.controlsLocked = false
		}

	case util.AsyncDependencyReady:
		if !slices.Contains(m.loadedDeps, msg.Dependency) {
			m.loadedDeps = append(m.loadedDeps, msg.Dependency)
		}

		if len(m.loadedDeps) == len(asyncDeps) {
			allLoaded := true
			for _, required := range asyncDeps {
				if !slices.Contains(m.loadedDeps, required) {
					allLoaded = false
					break
				}
			}

			if allLoaded {
				m.viewReady = true
				m.promptPane = m.promptPane.Enable()

				// if there is also a 'new session' flag - need to do it differently
				if m.initialPrompt != "" && !m.flags.StartNewSession {
					cmds = append(cmds, util.SendPromptReadyMsg(m.initialPrompt, []util.Attachment{}))
					m.initialPrompt = ""
				}
			}
		}

		if m.viewReady && m.flags.StartNewSession {
			cmds = append(cmds, util.AddNewSession(false))
		}

	case sessions.ToolCallComplete:
		util.Slog.Debug("ToolCallComplete event received")
		if m.sessionOrchestrator.ResponseProcessingState == util.Idle {
			return m, nil
		}

		if m.sessionOrchestrator.ResponseProcessingState != util.AwaitingToolCallResult {
			return m, util.MakeErrorMsg("did not expect a tool call result")
		}

		if len(m.sessionOrchestrator.ArrayOfMessages) == 0 {
			return m, tea.Batch(
				util.MakeErrorMsg("tool call result received but session has no messages"),
				util.SendProcessingStateChangedMsg(util.Idle),
			)
		}

		if !msg.IsSuccess {
			return m, tea.Batch(
				util.MakeErrorMsg("tool call failed: "+msg.Name),
				util.SendProcessingStateChangedMsg(util.Idle),
			)
		}

		lastIdx := len(m.sessionOrchestrator.ArrayOfMessages) - 1
		lastTurn := m.sessionOrchestrator.ArrayOfMessages[lastIdx]

		if len(lastTurn.ToolCalls) > 0 {
			for _, tc := range lastTurn.ToolCalls {
				if tc.Function.Name == msg.Name && tc.Id == msg.Id && msg.IsSuccess {
					m.pendingToolCalls = append(m.pendingToolCalls, util.ToolCall{
						Id: msg.Id,
						Function: util.ToolFunction{
							Args: tc.Function.Args,
							Name: tc.Function.Name,
						},
						Result: &msg.Result,
					})
				}
			}
		}

		if len(m.pendingToolCalls) == len(lastTurn.ToolCalls) {
			updatedArray := append(m.sessionOrchestrator.ArrayOfMessages, util.LocalStoreMessage{
				Model:       lastTurn.Model,
				Role:        "tool",
				Attachments: []util.Attachment{},
				ToolCalls:   m.pendingToolCalls,
			})

			err := m.sessionService.UpdateSessionMessages(m.sessionOrchestrator.GetCurrentSessionId(), updatedArray)
			if err != nil {
				return m, tea.Batch(util.MakeErrorMsg(err.Error()), util.SendProcessingStateChangedMsg(util.Idle))
			}
			util.Slog.Debug("consturcted tool call results for continuation", "amount", len(m.pendingToolCalls))

			m.pendingToolCalls = []util.ToolCall{}
			m.setProcessingContext()
			cmds = append(cmds, m.chatPane.ResumeCompletion(m.processingCtx, &m.sessionOrchestrator))
			return m, tea.Batch(cmds...)
		}

	case util.PromptReady:
		m.error = util.ErrorEvent{}

		util.Slog.Debug("prompt ready message received", "msg", msg)

		loadedAttachments := []util.Attachment{}
		if len(msg.Attachments) != 0 {

			util.Slog.Debug("preparing attachments")

			for _, attachment := range msg.Attachments {
				b64, err := m.fileToBase64(attachment.Path)
				if err != nil {
					util.Slog.Error("failed to convert attachment to base64", "error", err.Error())
					return m, util.MakeErrorMsg(err.Error())
				}

				t := util.Attachment{
					Path:    attachment.Path,
					Content: b64,
					Type:    mapAttachmentType(attachment.Type),
				}
				loadedAttachments = append(loadedAttachments, t)
			}
		}

		// Process context chips asynchronously
		if len(msg.ContextChips) > 0 {
			util.Slog.Debug("dispatching async context chips processing", "count", len(msg.ContextChips))
			// Capture maxDepth at command creation time to avoid race condition
			maxDepth := m.config.GetContextMaxDepth()
			return m, m.makeProcessContextChipsCmd(msg.Prompt, loadedAttachments, msg.ContextChips, maxDepth)
		}

		// No context chips, proceed directly
		m.sessionOrchestrator.ArrayOfMessages = append(
			m.sessionOrchestrator.ArrayOfMessages,
			util.LocalStoreMessage{
				Role:        "user",
				Content:     msg.Prompt,
				Attachments: loadedAttachments,
			})
		m.viewMode = util.NormalMode
		m.controlsLocked = true

		m.setProcessingContext()
		return m, tea.Sequence(
			util.SendProcessingStateChangedMsg(util.ProcessingChunks),
			util.SendViewModeChangedMsg(m.viewMode),
			m.chatPane.DisplayCompletion(m.processingCtx, &m.sessionOrchestrator))

	case util.ContextChipsProcessed:
		// Context chips have been processed asynchronously
		contextContent := msg.ContextContent
		if contextContent != "" {
			contextContent = "\n\n" + contextContent
		}

		m.sessionOrchestrator.ArrayOfMessages = append(
			m.sessionOrchestrator.ArrayOfMessages,
			util.LocalStoreMessage{
				Role:        "user",
				Content:     msg.Prompt + contextContent,
				Attachments: msg.Attachments,
			})
		m.viewMode = util.NormalMode
		m.controlsLocked = true

		m.setProcessingContext()
		
		// Build the sequence of commands
		sequence := []tea.Cmd{
			util.SendProcessingStateChangedMsg(util.ProcessingChunks),
			util.SendViewModeChangedMsg(m.viewMode),
			m.chatPane.DisplayCompletion(m.processingCtx, &m.sessionOrchestrator),
		}
		
		// Show notification if there were errors during processing (non-blocking)
		if len(msg.Errors) > 0 {
			errorMsg := fmt.Sprintf("Some context files failed to load:\n%s", strings.Join(msg.Errors, "\n"))
			sequence = append(sequence, util.MakeErrorMsg(errorMsg))
		}
		
		return m, tea.Sequence(sequence...)

	case tea.MouseMsg:
		targetPane := m.focused

		if m.controlsLocked {
			break
		}

		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			util.Slog.Debug("MainView.MouseMsg: checking zones", "viewMode", m.viewMode, "currentFocused", m.focused)
			switch {
			case zone.Get("chat_pane").InBounds(msg):
				util.Slog.Debug("MainView.MouseMsg: chat_pane clicked", "viewMode", m.viewMode)
				targetPane = util.ChatPane
			case zone.Get("prompt_pane").InBounds(msg):
				targetPane = util.PromptPane
			case zone.Get("settings_pane").InBounds(msg):
				targetPane = util.SettingsPane
			case zone.Get("sessions_pane").InBounds(msg):
				targetPane = util.SessionsPane
			}

			if targetPane != m.focused {
				util.Slog.Debug("MainView.MouseMsg: focus change requested", "from", m.focused, "to", targetPane, "viewMode", m.viewMode)
				m.handleFocusChange(targetPane, true)
				return m, nil
			}
		}

	case tea.KeyMsg:
		if key.Matches(msg, m.keys.quit) {
			return m, tea.Quit
		}

		if !m.viewReady {
			break
		}

		switch {

		case key.Matches(msg, m.keys.saveQuickChat):
			cmds = append(cmds, sessions.SendSaveQuickChatMsg())

		case key.Matches(msg, m.keys.quickChat):
			cmds = append(cmds, m.InitiateNewSession(true))

		case key.Matches(msg, m.keys.newSession):
			cmds = append(cmds, m.InitiateNewSession(false))

		case key.Matches(msg, m.keys.cancel):
			cancelCmd := m.CancelProcessing()

			if cancelCmd != nil {
				cmds = append(cmds, cancelCmd)
				return m, tea.Batch(cmds...)
			}

		case key.Matches(msg, m.keys.zenMode):
			m.focused = util.PromptPane
			m.sessionsPane, _ = m.sessionsPane.Update(util.MakeFocusMsg(m.focused == util.SessionsPane))
			m.settingsPane, _ = m.settingsPane.Update(util.MakeFocusMsg(m.focused == util.SettingsPane))

			cmds = append(cmds, cmd)

			switch m.viewMode {
			case util.NormalMode:
				m.viewMode = util.ZenMode
			case util.ZenMode:
				m.viewMode = util.NormalMode
			}

			cmds = append(cmds, util.SendViewModeChangedMsg(m.viewMode))

		case key.Matches(msg, m.keys.editorMode):
			if m.focused != util.PromptPane || !m.promptPane.AllowFocusChange(false) {
				break
			}

			switch m.viewMode {
			case util.NormalMode:
				m.viewMode = util.TextEditMode
			case util.ZenMode:
				m.viewMode = util.TextEditMode
			case util.TextEditMode:
				m.viewMode = util.NormalMode
			}
			cmds = append(cmds, util.SendViewModeChangedMsg(m.viewMode))

		case key.Matches(msg, m.keys.jumpToPane):
			var targetPane util.Pane
			switch msg.String() {
			case "1":
				targetPane = util.PromptPane
			case "2":
				targetPane = util.ChatPane
			case "3":
				targetPane = util.SettingsPane
			case "4":
				targetPane = util.SessionsPane
			}
			m.handleFocusChange(targetPane, false)

		case key.Matches(msg, m.keys.nextPane):
			if !m.isFocusChangeAllowed(false) {
				break
			}

			m.focused = util.GetNewFocusMode(m.viewMode, m.focused, m.terminalWidth, false)
			m.resetFocus()

		case key.Matches(msg, m.keys.previousPane):
			if !m.isFocusChangeAllowed(false) {
				break
			}

			m.focused = util.GetNewFocusMode(m.viewMode, m.focused, m.terminalWidth, true)
			m.resetFocus()
		}

	case tea.WindowSizeMsg:
		m.terminalWidth = msg.Width
		m.terminalHeight = msg.Height

		m.chatPane, cmd = m.chatPane.Update(msg)
		cmds = append(cmds, cmd)
		m.settingsPane, cmd = m.settingsPane.Update(msg)
		cmds = append(cmds, cmd)
		m.sessionsPane, cmd = m.sessionsPane.Update(msg)
		cmds = append(cmds, cmd)
	}

	m.chatPane, cmd = m.chatPane.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *MainView) handleFocusChange(targetPane util.Pane, isMouseEvent bool) {
	if !m.isFocusChangeAllowed(isMouseEvent) {
		return
	}

	if util.IsFocusAllowed(m.viewMode, targetPane, m.terminalWidth) {
		m.focused = targetPane
		m.resetFocus()
	}
}

func (m MainView) View() string {
	var windowViews string

	settingsAndSessionPanes := lipgloss.JoinVertical(
		lipgloss.Left,
		m.settingsPane.View(),
		m.sessionsPane.View(),
		m.infoPane.View(),
	)

	mainView := m.chatPane.View()
	if m.error.Message != "" {
		mainView = m.chatPane.DisplayError(m.error.Message)
	}

	// Conditionally render windowViews based on view mode
	// In ContextPickerMode or FilePickerMode, don't render chat pane to avoid phantom pane
	if m.viewMode == util.NormalMode {
		windowViews = lipgloss.NewStyle().
			Align(lipgloss.Right, lipgloss.Right).
			Render(
				lipgloss.JoinHorizontal(
					lipgloss.Top,
					mainView,
					settingsAndSessionPanes,
				),
			)
	} else if m.viewMode == util.ContextPickerMode || m.viewMode == util.FilePickerMode {
		// In ContextPickerMode or FilePickerMode, render empty windowViews
		// This prevents the chat pane from being rendered and creating a clickable zone
		windowViews = ""
	} else {
		// In TextEditMode, ZenMode, etc.
		// Only render mainView without secondary screen
		windowViews = lipgloss.NewStyle().
			Align(lipgloss.Right, lipgloss.Right).
			Render(mainView)
	}

	util.Slog.Debug("MainView.View: rendering", "viewMode", m.viewMode)

	promptView := m.promptPane.View()

	return zone.Scan(lipgloss.NewStyle().Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			windowViews,
			promptView,
		),
	))
}

func (m *MainView) setProcessingContext() {
	if m.processingCancel != nil {
		m.processingCancel()
	}
	m.processingCtx, m.processingCancel = context.WithCancel(m.context)
}

func (m *MainView) resetFocus() {
	m.sessionsPane, _ = m.sessionsPane.Update(util.MakeFocusMsg(m.focused == util.SessionsPane))
	m.settingsPane, _ = m.settingsPane.Update(util.MakeFocusMsg(m.focused == util.SettingsPane))
	m.chatPane, _ = m.chatPane.Update(util.MakeFocusMsg(m.focused == util.ChatPane))
	m.promptPane, _ = m.promptPane.Update(util.MakeFocusMsg(m.focused == util.PromptPane))
}

func (m MainView) fileToBase64(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		util.Slog.Error("failed to read file", "path", filePath, "error", err.Error())
		return "", err
	}

	maxSize := 1024 * 1024 * m.config.MaxAttachmentSizeMb
	if len(data) > maxSize {
		util.Slog.Error("attchment exceeds allowed size limit", "path", filePath, "size (kb)", len(data)*1024)
		return "", fmt.Errorf("attchment exceeds allowed size limit of %d MB \n Attachment: %s",
			m.config.MaxAttachmentSizeMb,
			filePath)
	}

	base64Str := base64.StdEncoding.EncodeToString(data)
	return base64Str, nil
}

func mapAttachmentType(attachmentType string) string {
	switch attachmentType {
	case "img":
		return "image_url"
	case "file":
		// https: //platform.openai.com/docs/guides/pdf-files?api-mode=chat#base64-encoded-files
		return "input_file"
	}
	return ""
}

func (m MainView) makeProcessContextChipsCmd(prompt string, attachments []util.Attachment, chips []util.FileContextChip, maxDepth int) tea.Cmd {
	return func() tea.Msg {
		var contextContent strings.Builder
		var errors []string

		for _, chip := range chips {
			if chip.IsFolder {
				// Read folder contents
				util.Slog.Debug("reading folder context", "path", chip.Path)
				contents, filePaths, err := util.ReadFolderContents(chip.Path, maxDepth)
				if err != nil {
					errMsg := fmt.Sprintf("Failed to read folder %s: %s", chip.Path, err.Error())
					util.Slog.Error("failed to read folder", "path", chip.Path, "error", err.Error())
					errors = append(errors, errMsg)
					continue
				}

				// Format folder contents
				formatted := util.FormatFolderContents(contents, filePaths)
				contextContent.WriteString(formatted)
			} else {
				// Read single file
				util.Slog.Debug("reading file context", "path", chip.Path)
				content, err := util.ReadFileContent(chip.Path)
				if err != nil {
					errMsg := fmt.Sprintf("Failed to read file %s: %s", chip.Path, err.Error())
					util.Slog.Error("failed to read file", "path", chip.Path, "error", err.Error())
					errors = append(errors, errMsg)
					continue
				}

				// Format file content
				formatted := util.FormatFileContent(chip.Path, content)
				contextContent.WriteString(formatted)
			}
		}

		return util.ContextChipsProcessed{
			Prompt:         prompt,
			Attachments:    attachments,
			ContextContent: contextContent.String(),
			Errors:         errors,
		}
	}
}

func (m MainView) processContextChips(chips []util.FileContextChip) string {
	var contextContent strings.Builder
	maxDepth := m.config.GetContextMaxDepth()

	for _, chip := range chips {
		if chip.IsFolder {
			// Read folder contents
			util.Slog.Debug("reading folder context", "path", chip.Path)
			contents, filePaths, err := util.ReadFolderContents(chip.Path, maxDepth)
			if err != nil {
				util.Slog.Error("failed to read folder", "path", chip.Path, "error", err.Error())
				continue
			}

			// Format folder contents
			formatted := util.FormatFolderContents(contents, filePaths)
			contextContent.WriteString(formatted)
		} else {
			// Read single file
			util.Slog.Debug("reading file context", "path", chip.Path)
			content, err := util.ReadFileContent(chip.Path)
			if err != nil {
				util.Slog.Error("failed to read file", "path", chip.Path, "error", err.Error())
				continue
			}

			// Format file content
			formatted := util.FormatFileContent(chip.Path, content)
			contextContent.WriteString(formatted)
		}
	}

	return contextContent.String()
}

// TODO: use event to lock/unlock allowFocusChange flag?
func (m MainView) isFocusChangeAllowed(isMouseEvent bool) bool {
	if !m.promptPane.AllowFocusChange(isMouseEvent) ||
		!m.chatPane.AllowFocusChange(isMouseEvent) ||
		!m.settingsPane.AllowFocusChange(isMouseEvent) ||
		!m.sessionsPane.AllowFocusChange(isMouseEvent) ||
		!m.viewReady ||
		m.sessionOrchestrator.IsProcessing() {
		util.Slog.Warn(
			"focus change not allowed.",
			"processing mode",
			m.sessionOrchestrator.ResponseProcessingState,
		)
		return false
	}

	return true
}

func (m *MainView) InitiateNewSession(isTemporary bool) tea.Cmd {
	if util.IsFocusAllowed(m.viewMode, util.PromptPane, m.terminalWidth) {
		if m.focused != util.SessionsPane {
			m.focused = util.PromptPane
			m.resetFocus()
		}
	}
	return util.AddNewSession(isTemporary)
}

func (m *MainView) CancelProcessing() tea.Cmd {
	var cmds []tea.Cmd

	if !m.sessionOrchestrator.IsProcessing() {
		return nil
	}

	m.sessionOrchestrator.Cancel()
	m.chatPane.Cancel()
	m.processingCancel()

	finalizeCmd := m.sessionOrchestrator.FinalizeResponseOnCancel()
	if finalizeCmd != nil {
		cmds = append(cmds, finalizeCmd)
	} else {
		cmds = append(cmds, util.SendProcessingStateChangedMsg(util.Idle))
	}

	cmds = append(cmds, util.SendNotificationMsg(util.CancelledNotification))
	return tea.Batch(cmds...)
}
