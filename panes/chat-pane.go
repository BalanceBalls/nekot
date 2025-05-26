package panes

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tearingItUp786/nekot/components"
	"github.com/tearingItUp786/nekot/config"
	"github.com/tearingItUp786/nekot/sessions"
	"github.com/tearingItUp786/nekot/util"
)

type displayMode int

const (
	normalMode displayMode = iota
	selectionMode
)

type chatPaneKeyMap struct {
	selectionMode key.Binding
	exit          key.Binding
	copyLast      key.Binding
	copyAll       key.Binding
}

var defaultChatPaneKeyMap = chatPaneKeyMap{
	exit:          key.NewBinding(key.WithKeys(tea.KeyEsc.String()), key.WithHelp("esc", "exit insert mode or editor mode")),
	copyLast:      key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy last message from chat to clipboard")),
	copyAll:       key.NewBinding(key.WithKeys("Y"), key.WithHelp("Y", "copy all chat to clipboard")),
	selectionMode: key.NewBinding(key.WithKeys(tea.KeySpace.String(), "v", "V"), key.WithHelp("<space>, v, V", "enter selection mode")),
}

type ChatPane struct {
	isChatPaneReady        bool
	chatViewReady          bool
	displayMode            displayMode
	chatContent            string
	isChatContainerFocused bool
	msgChan                chan util.ProcessApiCompletionResponse
	viewMode               util.ViewMode
	sessionContent         []util.MessageToSend

	terminalWidth  int
	terminalHeight int

	keyMap        chatPaneKeyMap
	colors        util.SchemeColors
	chatContainer lipgloss.Style
	chatView      viewport.Model
	selectionView components.TextSelector
	mainCtx       context.Context
}

var chatContainerStyle = lipgloss.NewStyle().
	Border(lipgloss.ThickBorder()).
	MarginRight(util.ChatPaneMarginRight)

func NewChatPane(ctx context.Context, w, h int) ChatPane {
	chatView := viewport.New(w, h)
	msgChan := make(chan util.ProcessApiCompletionResponse)

	config, ok := config.FromContext(ctx)
	if !ok {
		fmt.Println("No config found")
		panic("No config found in context")
	}
	colors := config.ColorScheme.GetColors()

	defaultChatContent := util.GetManual(w, colors)
	chatView.SetContent(defaultChatContent)
	chatContainerStyle = chatContainerStyle.
		Width(w).
		Height(h).
		BorderForeground(colors.NormalTabBorderColor)

	return ChatPane{
		mainCtx:                ctx,
		keyMap:                 defaultChatPaneKeyMap,
		viewMode:               util.NormalMode,
		colors:                 colors,
		chatContainer:          chatContainerStyle,
		chatView:               chatView,
		chatViewReady:          false,
		chatContent:            defaultChatContent,
		isChatContainerFocused: false,
		msgChan:                msgChan,
		terminalWidth:          util.DefaultTerminalWidth,
		terminalHeight:         util.DefaultTerminalHeight,
		displayMode:            normalMode,
	}
}

func waitForActivity(sub chan util.ProcessApiCompletionResponse) tea.Cmd {
	return func() tea.Msg {
		someMessage := <-sub
		return someMessage
	}
}

func (p ChatPane) Init() tea.Cmd {
	return nil
}

func (p ChatPane) Update(msg tea.Msg) (ChatPane, tea.Cmd) {
	var (
		cmd                    tea.Cmd
		cmds                   []tea.Cmd
		enableUpdateOfViewport = true
	)

	if p.IsSelectionMode() {
		p.selectionView, cmd = p.selectionView.Update(msg)
		cmds = append(cmds, cmd)
	}

	switch msg := msg.(type) {
	case util.ViewModeChanged:
		p.viewMode = msg.Mode
		return p, func() tea.Msg {
			return tea.WindowSizeMsg{
				Width:  p.terminalWidth,
				Height: p.terminalHeight,
			}
		}

	case util.FocusEvent:
		p.isChatContainerFocused = msg.IsFocused
		p.displayMode = normalMode

		return p, nil

	case sessions.LoadDataFromDB:
		return p.initializePane(msg.Session)

	case sessions.UpdateCurrentSession:
		return p.initializePane(msg.Session)

	case sessions.ResponseChunkProcessed:
		paneWidth := p.chatContainer.GetWidth()

		oldContent := util.GetMessagesAsPrettyString(msg.PreviousMsgArray, paneWidth, p.colors)
		styledBufferMessage := util.RenderBotMessage(msg.ChunkMessage, paneWidth, p.colors, false)

		if styledBufferMessage != "" {
			styledBufferMessage = "\n" + styledBufferMessage
		}

		rendered := oldContent + styledBufferMessage
		p.chatView.SetContent(rendered)
		p.chatView.GotoBottom()

		cmds = append(cmds, waitForActivity(p.msgChan))

	case tea.WindowSizeMsg:
		p = p.handleWindowResize(msg.Width, msg.Height)

	case tea.KeyMsg:
		if !p.isChatContainerFocused {
			enableUpdateOfViewport = false
		}

		if p.IsSelectionMode() {
			switch {
			case key.Matches(msg, p.keyMap.exit):
				p.displayMode = normalMode
				p.chatContainer.BorderForeground(p.colors.ActiveTabBorderColor)
				p.selectionView.Reset()
			}
		}

		if p.IsSelectionMode() {
			break
		}

		switch {
		case key.Matches(msg, p.keyMap.selectionMode):
			if !p.isChatContainerFocused || len(p.sessionContent) == 0 {
				break
			}
			p.displayMode = selectionMode
			enableUpdateOfViewport = false
			p.chatContainer.BorderForeground(p.colors.AccentColor)
			renderedContent := util.GetVisualModeView(p.sessionContent, p.chatView.Width, p.colors)
			p.selectionView = components.NewTextSelector(
				p.terminalWidth,
				p.terminalHeight,
				p.chatView.YOffset,
				renderedContent,
				p.colors)
			p.selectionView.AdjustScroll()

		case key.Matches(msg, p.keyMap.copyLast):
			if p.isChatContainerFocused {
				copyLast := func() tea.Msg {
					return util.SendCopyLastMsg()
				}
				cmds = append(cmds, copyLast)
			}

		case key.Matches(msg, p.keyMap.copyAll):
			if p.isChatContainerFocused {
				copyAll := func() tea.Msg {
					return util.SendCopyAllMsgs()
				}
				cmds = append(cmds, copyAll)
			}
		}
	}

	if enableUpdateOfViewport {
		p.chatView, cmd = p.chatView.Update(msg)
		cmds = append(cmds, cmd)
	}

	return p, tea.Batch(cmds...)
}

func (p ChatPane) IsSelectionMode() bool {
	return p.displayMode == selectionMode
}

func (p ChatPane) AllowFocusChange() bool {
	return !p.selectionView.IsSelecting()
}

func (p ChatPane) DisplayCompletion(ctx context.Context, orchestrator sessions.Orchestrator) tea.Cmd {
	return tea.Batch(
		orchestrator.GetCompletion(ctx, p.msgChan),
		waitForActivity(p.msgChan),
	)
}

func (p ChatPane) View() string {
	if p.IsSelectionMode() {
		return p.chatContainer.Render(p.selectionView.View())
	}

	viewportContent := p.chatView.View()
	borderColor := p.colors.NormalTabBorderColor
	if p.isChatContainerFocused {
		borderColor = p.colors.ActiveTabBorderColor
	}
	return p.chatContainer.BorderForeground(borderColor).Render(viewportContent)
}

func (p ChatPane) DisplayError(error string) string {
	return p.chatContainer.Render(util.RenderErrorMessage(error, p.chatContainer.GetWidth(), p.colors))
}

func (p ChatPane) SetPaneWitdth(w int) {
	p.chatContainer.Width(w)
}

func (p ChatPane) SetPaneHeight(h int) {
	p.chatContainer.Height(h)
}

func (p ChatPane) GetWidth() int {
	return p.chatContainer.GetWidth()
}

func (p ChatPane) initializePane(session sessions.Session) (ChatPane, tea.Cmd) {
	paneWidth, paneHeight := util.CalcChatPaneSize(p.terminalWidth, p.terminalHeight, p.viewMode)
	if !p.isChatPaneReady {
		p.chatView = viewport.New(paneWidth, paneHeight)
		p.isChatPaneReady = true
	}

	if len(session.Messages) == 0 {
		p = p.displayManual()
	} else {
		p = p.displaySession(session.Messages, paneWidth, true)
	}

	return p, nil
}

func (p ChatPane) displayManual() ChatPane {
	manual := util.GetManual(p.terminalWidth, p.colors)
	p.chatView.SetContent(manual)
	p.chatView.GotoTop()
	p.sessionContent = []util.MessageToSend{}
	return p
}

func (p ChatPane) displaySession(messages []util.MessageToSend, paneWidth int, useScroll bool) ChatPane {
	oldContent := util.GetMessagesAsPrettyString(messages, paneWidth, p.colors)
	p.chatView.SetContent(oldContent)
	if useScroll {
		p.chatView.GotoBottom()
	}
	p.sessionContent = messages
	return p
}

func (p ChatPane) handleWindowResize(width int, height int) ChatPane {
	p.terminalWidth = width
	p.terminalHeight = height

	w, h := util.CalcChatPaneSize(p.terminalWidth, p.terminalHeight, p.viewMode)
	p.chatView.Height = h
	p.chatView.Width = w
	p.chatContainer = p.chatContainer.Width(w).Height(h)

	if p.viewMode == util.NormalMode {
		p = p.displaySession(p.sessionContent, w, false)
	}

	if len(p.sessionContent) == 0 {
		p = p.displayManual()
	}

	return p
}
