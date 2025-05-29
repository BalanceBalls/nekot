package views

import (
	"context"
	"database/sql"
	"os"
	"runtime"
	"slices"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/BalanceBalls/nekot/clients"
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
	jumpToPane    key.Binding
	newSession    key.Binding
	quickChat     key.Binding
	saveQuickChat key.Binding
	quit          key.Binding
}

var defaultKeyMap = keyMap{
	cancel:        key.NewBinding(key.WithKeys("ctrl+s", "ctrl+b"), key.WithHelp("ctrl+b/ctrl+s", "stop inference")),
	zenMode:       key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "activate/deactivate zen mode")),
	editorMode:    key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "enter/exit editor mode")),
	quit:          key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit app")),
	quickChat:     key.NewBinding(key.WithKeys("ctrl+q"), key.WithHelp("ctrl+q", "start quick chat")),
	saveQuickChat: key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "save quick chat")),
	jumpToPane:    key.NewBinding(key.WithKeys("1", "2", "3", "4"), key.WithHelp("1,2,3,4", "jump to specific pane")),
	nextPane:      key.NewBinding(key.WithKeys(tea.KeyTab.String()), key.WithHelp("TAB", "move to next pane")),
	newSession:    key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "add new session")),
}

type MainView struct {
	viewReady        bool
	focused          util.Pane
	viewMode         util.ViewMode
	error            util.ErrorEvent
	currentSessionID string
	keys             keyMap

	chatPane     panes.ChatPane
	promptPane   panes.PromptPane
	sessionsPane panes.SessionsPane
	settingsPane panes.SettingsPane
	infoPane     panes.InfoPane
	loadedDeps   []util.AsyncDependency

	sessionOrchestrator sessions.Orchestrator
	context             context.Context
	completionContext   context.Context
	cancelInference     context.CancelFunc

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
	promptPane := panes.NewPromptPane(ctx)
	sessionsPane := panes.NewSessionsPane(db, ctx)
	settingsPane := panes.NewSettingsPane(db, ctx)
	statusBarPane := panes.NewInfoPane(db, ctx)

	w, h := util.CalcChatPaneSize(util.DefaultTerminalWidth, util.DefaultTerminalHeight, util.NormalMode)
	chatPane := panes.NewChatPane(ctx, w, h)

	orchestrator := sessions.NewOrchestrator(db, ctx)

	return MainView{
		keys:                defaultKeyMap,
		viewMode:            util.NormalMode,
		focused:             util.PromptPane,
		currentSessionID:    "",
		sessionOrchestrator: orchestrator,
		promptPane:          promptPane,
		sessionsPane:        sessionsPane,
		settingsPane:        settingsPane,
		infoPane:            statusBarPane,
		chatPane:            chatPane,
		context:             ctx,
	}
}

func (m MainView) Init() tea.Cmd {
	return tea.Batch(
		m.sessionOrchestrator.Init(),
		m.promptPane.Init(),
		m.sessionsPane.Init(),
		m.chatPane.Init(),
		m.settingsPane.Init(),
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

	if m.sessionOrchestrator.ProcessingMode == sessions.IDLE {
		m.sessionsPane, cmd = m.sessionsPane.Update(msg)
		cmds = append(cmds, cmd)
		m.settingsPane, cmd = m.settingsPane.Update(msg)
		cmds = append(cmds, cmd)
	}

	switch msg := msg.(type) {

	case util.ErrorEvent:
		m.sessionOrchestrator.ProcessingMode = sessions.IDLE
		m.error = msg
		m.viewReady = true
		cmds = append(cmds, util.SendProcessingStateChangedMsg(false))

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

	case util.AsyncDependencyReady:
		m.loadedDeps = append(m.loadedDeps, msg.Dependency)
		for _, dependency := range asyncDeps {
			if !slices.Contains(m.loadedDeps, dependency) {
				continue
			}
			m.viewReady = true
		}
		m.promptPane = m.promptPane.Enable()

	case util.PromptReady:
		m.error = util.ErrorEvent{}
		m.sessionOrchestrator.ArrayOfMessages = append(m.sessionOrchestrator.ArrayOfMessages, clients.ConstructUserMessage(msg.Prompt))
		m.sessionOrchestrator.ProcessingMode = sessions.PROCESSING
		m.viewMode = util.NormalMode

		completionContext, cancelInference := context.WithCancel(m.context)
		m.completionContext = completionContext
		m.cancelInference = cancelInference
		return m, tea.Batch(
			util.SendProcessingStateChangedMsg(true),
			m.chatPane.DisplayCompletion(m.completionContext, m.sessionOrchestrator),
			util.SendViewModeChangedMsg(m.viewMode))

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
			if m.cancelInference != nil {
				m.cancelInference()
				m.cancelInference = nil
				cmds = append(cmds, util.SendProcessingStateChangedMsg(false))
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
			if m.focused != util.PromptPane {
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
			if !m.isFocusChangeAllowed() {
				break
			}

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

			if util.IsFocusAllowed(m.viewMode, targetPane, m.terminalWidth) {
				m.focused = targetPane
				m.resetFocus()
			}

		case key.Matches(msg, m.keys.nextPane):
			if !m.isFocusChangeAllowed() {
				break
			}

			m.focused = util.GetNewFocusMode(m.viewMode, m.focused, m.terminalWidth)
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

	secondaryScreen := ""
	if m.viewMode == util.NormalMode {
		secondaryScreen = settingsAndSessionPanes
	}

	windowViews = lipgloss.NewStyle().
		Align(lipgloss.Right, lipgloss.Right).
		Render(
			lipgloss.JoinHorizontal(
				lipgloss.Top,
				mainView,
				secondaryScreen,
			),
		)

	promptView := m.promptPane.View()

	return lipgloss.NewStyle().Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			windowViews,
			promptView,
		),
	)
}

func (m *MainView) resetFocus() {
	m.sessionsPane, _ = m.sessionsPane.Update(util.MakeFocusMsg(m.focused == util.SessionsPane))
	m.settingsPane, _ = m.settingsPane.Update(util.MakeFocusMsg(m.focused == util.SettingsPane))
	m.chatPane, _ = m.chatPane.Update(util.MakeFocusMsg(m.focused == util.ChatPane))
	m.promptPane, _ = m.promptPane.Update(util.MakeFocusMsg(m.focused == util.PromptPane))
}

// TODO: use event to lock/unlock allowFocusChange flag
func (m MainView) isFocusChangeAllowed() bool {
	if !m.promptPane.AllowFocusChange() ||
		!m.chatPane.AllowFocusChange() ||
		!m.settingsPane.AllowFocusChange() ||
		!m.sessionsPane.AllowFocusChange() ||
		!m.viewReady ||
		m.sessionOrchestrator.ProcessingMode == sessions.PROCESSING {
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
