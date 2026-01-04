package panes

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/BalanceBalls/nekot/components"
	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/sessions"
	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	exit: key.NewBinding(
		key.WithKeys(tea.KeyEsc.String()),
		key.WithHelp("esc", "exit insert mode or editor mode"),
	),
	copyLast: key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "copy last message from chat to clipboard"),
	),
	copyAll: key.NewBinding(
		key.WithKeys("Y"),
		key.WithHelp("Y", "copy all chat to clipboard"),
	),
	selectionMode: key.NewBinding(
		key.WithKeys(tea.KeySpace.String(), "v", "V"),
		key.WithHelp("<space>, v, V", "enter selection mode"),
	),
}

const pulsarIntervalMs = 100

type renderContentMsg int

func renderingPulsar() tea.Msg {
	time.Sleep(time.Millisecond * pulsarIntervalMs)
	return renderContentMsg(1)
}

type ChatPane struct {
	isChatPaneReady        bool
	chatViewReady          bool
	displayMode            displayMode
	chatContent            string
	isChatContainerFocused bool
	msgChan                chan util.ProcessApiCompletionResponse
	viewMode               util.ViewMode
	sessionContent         []util.LocalStoreMessage
	chunksBuffer           []string
	responseBuffer         string
	renderedResponseBuffer string
	renderedHistory        string
	idleCyclesCount        int
	processingState        util.ProcessingState
	mu                     *sync.RWMutex

	terminalWidth  int
	terminalHeight int

	quickChatActive bool
	keyMap          chatPaneKeyMap
	colors          util.SchemeColors
	chatContainer   lipgloss.Style
	chatView        viewport.Model
	selectionView   components.TextSelector
	mainCtx         context.Context
}

var chatContainerStyle = lipgloss.NewStyle().
	Border(lipgloss.ThickBorder()).
	MarginRight(util.ChatPaneMarginRight)

var infoBarStyle = lipgloss.NewStyle().
	BorderTop(true).
	BorderStyle(lipgloss.HiddenBorder())

func NewChatPane(ctx context.Context, w, h int) ChatPane {
	chatView := viewport.New(w, h)
	chatView.HighPerformanceRendering = true
	msgChan := make(chan util.ProcessApiCompletionResponse)

	config, ok := config.FromContext(ctx)
	if !ok {
		util.Slog.Error("failed to extract config from context")
		panic("No config found in context")
	}
	colors := config.ColorScheme.GetColors()

	defaultChatContent := util.GetManual(w, colors)
	chatView.SetContent(defaultChatContent)
	chatContainerStyle = chatContainerStyle.
		Width(w).
		Height(h).
		BorderForeground(colors.NormalTabBorderColor)

	infoBarStyle = infoBarStyle.
		Width(w).
		BorderForeground(colors.MainColor).
		Foreground(colors.HighlightColor)

	return ChatPane{
		mainCtx:                ctx,
		keyMap:                 defaultChatPaneKeyMap,
		viewMode:               util.NormalMode,
		colors:                 colors,
		chatContainer:          chatContainerStyle,
		chatView:               chatView,
		chatViewReady:          false,
		chatContent:            defaultChatContent,
		renderedHistory:        defaultChatContent,
		isChatContainerFocused: false,
		msgChan:                msgChan,
		terminalWidth:          util.DefaultTerminalWidth,
		terminalHeight:         util.DefaultTerminalHeight,
		displayMode:            normalMode,
		chunksBuffer:           []string{},
		mu:                     &sync.RWMutex{},
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

	case util.ProcessingStateChanged:
		p.mu.Lock()
		defer p.mu.Unlock()

		p.processingState = msg.State
		switch msg.State {
		case util.AwaitingToolCallResult:
			p.responseBuffer = ""
			p.chunksBuffer = []string{}
			cmds = append(cmds, renderingPulsar)
		case util.ProcessingChunks:
			cmds = append(cmds, renderingPulsar)
		case util.Finalized:
			cmds = append(cmds, renderingPulsar)
		}

	case sessions.LoadDataFromDB:
		// util.Slog.Debug("case LoadDataFromDB: ", "message", msg)
		return p.initializePane(msg.Session)

	case sessions.UpdateCurrentSession:
		return p.initializePane(msg.Session)

	case renderContentMsg:
		p.mu.Lock()
		defer p.mu.Unlock()

		if p.processingState == util.AwaitingToolCallResult {
			return p, renderingPulsar
		}

		if p.processingState == util.Idle {
			p.chunksBuffer = []string{}
			return p, nil
		}

		if len(p.chunksBuffer) == 0 {
			return p, renderingPulsar
		}

		paneWidth := p.chatContainer.GetWidth()
		newContent := p.chunksBuffer[len(p.chunksBuffer)-1]

		p.chunksBuffer = []string{}

		diff := getStringsDiff(p.responseBuffer, newContent)
		p.responseBuffer += diff

		renderWindow := p.responseBuffer

		chatHeightDelta := p.chatView.Height + 20 // arbitrary , just my emperical guess
		bufferLines := strings.Split(renderWindow, "\n")

		showOldMessages := true

		if chatHeightDelta < len(bufferLines) {
			showOldMessages = false
			to := len(bufferLines) - 1
			from := to - chatHeightDelta
			renderWindow = strings.Join(bufferLines[from:to], "\n")
		}

		if diff != "" {
			p.renderedResponseBuffer = util.RenderBotMessage(util.LocalStoreMessage{
				Content: renderWindow,
				Role:    "assistant",
			}, paneWidth, p.colors, false)
		}

		result := p.renderedResponseBuffer
		if showOldMessages {
			result = p.renderedHistory + "\n" + p.renderedResponseBuffer
		}

		p.chatView.SetContent(result)
		p.chatView.GotoBottom()

		return p, renderingPulsar

	case sessions.ResponseChunkProcessed:
		if len(p.sessionContent) != len(msg.PreviousMsgArray) {
			paneWidth := p.chatContainer.GetWidth()
			p.renderedHistory = util.GetMessagesAsPrettyString(msg.PreviousMsgArray, paneWidth, p.colors, p.quickChatActive)
			p.sessionContent = msg.PreviousMsgArray
			util.Slog.Debug("len(p.sessionContent) != len(msg.PreviousMsgArray)", "new length", len(msg.PreviousMsgArray))
		}

		p.chunksBuffer = append(p.chunksBuffer, msg.ChunkMessage)

		if !msg.IsComplete {
			cmds = append(cmds, waitForActivity(p.msgChan))
		}

		return p, tea.Batch(cmds...)

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

func getStringsDiff(oldStr, newStr string) string {
	i := 0

	for i < len(oldStr) && i < len(newStr) && oldStr[i] == newStr[i] {
		i++
	}

	return newStr[i:]
}

func (p ChatPane) IsSelectionMode() bool {
	return p.displayMode == selectionMode
}

func (p ChatPane) AllowFocusChange() bool {
	return !p.selectionView.IsSelecting()
}

func (p *ChatPane) DisplayCompletion(
	ctx context.Context,
	orchestrator *sessions.Orchestrator,
) tea.Cmd {
	return tea.Batch(
		orchestrator.GetCompletion(ctx, p.msgChan),
		waitForActivity(p.msgChan),
	)
}

func (p *ChatPane) ResumeCompletion(
	ctx context.Context,
	orchestrator *sessions.Orchestrator,
	toolResults []util.ToolCall,
) tea.Cmd {
	return tea.Batch(
		orchestrator.ResumeCompletion(ctx, p.msgChan, toolResults),
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

	if len(p.sessionContent) == 0 {
		return p.chatContainer.BorderForeground(borderColor).Render(viewportContent)
	}

	infoRow := p.renderInfoRow()
	content := lipgloss.JoinVertical(lipgloss.Left, viewportContent, infoRow)
	return p.chatContainer.BorderForeground(borderColor).Render(content)
}

func (p ChatPane) DisplayError(error string) string {
	return p.chatContainer.Render(
		util.RenderErrorMessage(error, p.chatContainer.GetWidth(), p.colors),
	)
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

func (p ChatPane) renderInfoRow() string {
	percent := p.chatView.ScrollPercent()

	info := fmt.Sprintf("▐ [%.f%%]", percent*100)
	if percent == 0 {
		info = "▐ [Top]"
	}
	if percent == 1 {
		info = "▐ [Bottom]"
	}

	if p.quickChatActive {
		info += " | [Quick chat]"
	}

	infoBar := infoBarStyle.Width(p.chatView.Width).Render(info)
	return infoBar
}

func (p ChatPane) initializePane(session sessions.Session) (ChatPane, tea.Cmd) {
	paneWidth, paneHeight := util.CalcChatPaneSize(p.terminalWidth, p.terminalHeight, p.viewMode)
	if !p.isChatPaneReady {
		p.chatView = viewport.New(paneWidth, paneHeight-2)
		p.isChatPaneReady = true
	}

	p.quickChatActive = session.IsTemporary
	if len(session.Messages) == 0 && !session.IsTemporary {
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
	p.sessionContent = []util.LocalStoreMessage{}
	p.renderedHistory = manual
	return p
}

func (p ChatPane) displaySession(
	messages []util.LocalStoreMessage,
	paneWidth int,
	useScroll bool,
) ChatPane {
	oldContent := util.GetMessagesAsPrettyString(messages, paneWidth, p.colors, p.quickChatActive)
	p.chatView.SetContent(oldContent)
	if useScroll {
		p.chatView.GotoBottom()
	}
	p.sessionContent = messages
	p.renderedHistory = oldContent

	p.chunksBuffer = []string{}

	p.responseBuffer = ""
	p.renderedResponseBuffer = ""
	return p
}

func (p ChatPane) handleWindowResize(width int, height int) ChatPane {
	p.terminalWidth = width
	p.terminalHeight = height

	w, h := util.CalcChatPaneSize(p.terminalWidth, p.terminalHeight, p.viewMode)
	p.chatView.Height = h - 2
	p.chatView.Width = w
	p.chatContainer = p.chatContainer.Width(w).Height(h)

	if p.viewMode == util.NormalMode {
		p = p.displaySession(p.sessionContent, w, false)
	}

	if len(p.sessionContent) == 0 && !p.quickChatActive {
		p = p.displayManual()
	}

	return p
}
