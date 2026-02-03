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
	"github.com/BalanceBalls/nekot/settings"
	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
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
	goUp          key.Binding
	goDown        key.Binding
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
	goUp: key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("g", "scroll to top"),
	),
	goDown: key.NewBinding(
		key.WithKeys("G"),
		key.WithHelp("G", "scroll to bottom"),
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
	currentSettings        util.Settings
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
	consumerCtx     context.Context
	consumerCancel  context.CancelFunc
}

var chatContainerStyle = lipgloss.NewStyle().
	Border(lipgloss.ThickBorder()).
	MarginRight(util.ChatPaneMarginRight)

var infoBarStyle = lipgloss.NewStyle().
	BorderTop(true).
	BorderStyle(lipgloss.HiddenBorder())

func NewChatPane(ctx context.Context, w, h int) ChatPane {
	chatView := viewport.New(w, h)
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
		consumerCtx:            context.Background(),
		keyMap:                 defaultChatPaneKeyMap,
		viewMode:               util.NormalMode,
		colors:                 colors,
		chatContainer:          chatContainerStyle,
		chatView:               chatView,
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

func waitForActivity(ctx context.Context, sub chan util.ProcessApiCompletionResponse) tea.Cmd {
	return func() tea.Msg {
		select {
		case someMessage := <-sub:
			return someMessage
		case <-ctx.Done():
			return nil
		}
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

	case settings.UpdateSettingsEvent:
		shouldReRender := msg.Settings.HideReasoning != p.currentSettings.HideReasoning
		p.currentSettings = msg.Settings
		if shouldReRender && len(p.sessionContent) != 0 {
			p = p.handleWindowResize(p.terminalWidth, p.terminalHeight)
		}

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
			}, paneWidth, p.colors, false, p.currentSettings)
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
			p.renderedHistory = util.GetMessagesAsPrettyString(
				msg.PreviousMsgArray,
				paneWidth,
				p.colors,
				p.quickChatActive,
				p.currentSettings)
			p.sessionContent = msg.PreviousMsgArray
			util.Slog.Debug("len(p.sessionContent) != len(msg.PreviousMsgArray)", "new length", len(msg.PreviousMsgArray))
		}

		p.chunksBuffer = append(p.chunksBuffer, msg.ChunkMessage)

		if !msg.IsComplete {
			cmds = append(cmds, waitForActivity(p.consumerCtx, p.msgChan))
		}

		return p, tea.Batch(cmds...)

	case tea.WindowSizeMsg:
		p = p.handleWindowResize(msg.Width, msg.Height)

	case tea.MouseMsg:
		if p.IsSelectionMode() && p.selectionView.IsCharSelecting() {
			if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
				return p, nil
			}
		}

		if msg.Button == tea.MouseButtonWheelUp && p.isChatContainerFocused {
			p.chatView.ScrollUp(3)
			return p, nil
		}

		if msg.Button == tea.MouseButtonWheelDown && p.isChatContainerFocused {
			p.chatView.ScrollDown(3)
			return p, nil
		}

		if !p.isChatContainerFocused {
			break
		}

		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if !p.IsSelectionMode() && len(p.sessionContent) > 0 {
				p.enterSelectionMode()
				enableUpdateOfViewport = false
				p.selectionView, cmd = p.selectionView.Update(msg)
				cmds = append(cmds, cmd)
				return p, tea.Batch(cmds...)
			}
		}

		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonRight {
			if !p.IsSelectionMode() && len(p.sessionContent) > 0 {
				p.enterSelectionMode()
				enableUpdateOfViewport = false
				p.selectionView, cmd = p.selectionView.Update(msg)
				cmds = append(cmds, cmd)
				return p, tea.Batch(cmds...)
			}
		}

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
		case key.Matches(msg, p.keyMap.goUp):
			if p.displayMode == normalMode && p.isChatContainerFocused {
				p.chatView.GotoTop()
			}

		case key.Matches(msg, p.keyMap.goDown):
			if p.displayMode == normalMode && p.isChatContainerFocused {
				p.chatView.GotoBottom()
			}

		case key.Matches(msg, p.keyMap.selectionMode):
			if !p.isChatContainerFocused || len(p.sessionContent) == 0 {
				break
			}
			p.enterSelectionMode()
			enableUpdateOfViewport = false

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

func (p *ChatPane) enterSelectionMode() {
	if len(p.sessionContent) == 0 {
		return
	}

	p.displayMode = selectionMode
	p.chatContainer = p.chatContainer.BorderForeground(p.colors.AccentColor)
	renderedContent := util.GetVisualModeView(
		p.sessionContent,
		p.chatView.Width,
		p.colors,
		p.currentSettings)
	mouseTopOffset := p.chatContainer.GetMarginTop() + p.chatContainer.GetBorderTopSize() + p.chatContainer.GetPaddingTop()
	mouseLeftOffset := p.chatContainer.GetMarginLeft() + p.chatContainer.GetBorderLeftSize() + p.chatContainer.GetPaddingLeft()
	p.selectionView = components.NewTextSelector(
		p.terminalWidth,
		p.terminalHeight,
		p.chatView.YOffset,
		mouseTopOffset,
		mouseLeftOffset,
		renderedContent,
		p.colors)
	p.selectionView.AdjustScroll()
}

func (p ChatPane) AllowFocusChange(isMouseEvent bool) bool {
	if isMouseEvent {
		return true
	}
	return !p.selectionView.IsSelecting()
}

func (p *ChatPane) DisplayCompletion(
	ctx context.Context,
	orchestrator *sessions.Orchestrator,
) tea.Cmd {
	if p.consumerCancel != nil {
		p.consumerCancel()
	}
	p.consumerCtx, p.consumerCancel = context.WithCancel(ctx)

	return tea.Batch(
		orchestrator.GetCompletion(p.consumerCtx, p.msgChan),
		waitForActivity(p.consumerCtx, p.msgChan),
	)
}

func (p *ChatPane) Cancel() {
	if p.consumerCancel != nil {
		p.consumerCancel()
	}
}

func (p *ChatPane) ResumeCompletion(
	ctx context.Context,
	orchestrator *sessions.Orchestrator,
) tea.Cmd {
	if p.consumerCancel != nil {
		p.consumerCancel()
	}
	p.consumerCtx, p.consumerCancel = context.WithCancel(ctx)

	return tea.Batch(
		orchestrator.ResumeCompletion(p.consumerCtx, p.msgChan),
		waitForActivity(p.consumerCtx, p.msgChan),
	)
}

func (p ChatPane) View() string {
	if p.IsSelectionMode() {
		infoRow := p.renderSelectionViewInfoRow()
		selectionView := p.selectionView.View()

		infoRowStyle := lipgloss.NewStyle().MarginTop(p.chatContainer.GetHeight() - lipgloss.Height(selectionView) - 2)
		content := lipgloss.JoinVertical(lipgloss.Left, selectionView, infoRowStyle.Render(infoRow))
		return zone.Mark("chat_pane", p.chatContainer.Render(content))
	}

	viewportContent := p.chatView.View()
	borderColor := p.colors.NormalTabBorderColor
	if p.isChatContainerFocused {
		borderColor = p.colors.ActiveTabBorderColor
	}

	if len(p.sessionContent) == 0 {
		return zone.Mark("chat_pane", p.chatContainer.BorderForeground(borderColor).Render(viewportContent))
	}

	infoRow := p.renderInfoRow()
	content := lipgloss.JoinVertical(lipgloss.Left, viewportContent, infoRow)
	return zone.Mark("chat_pane", p.chatContainer.BorderForeground(borderColor).Render(content))
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

	if p.currentSettings.WebSearchEnabled {
		info += " | [Web search]"
	}

	if p.currentSettings.HideReasoning {
		info += " | [Reasoning hidden]"
	}

	infoBar := infoBarStyle.Width(p.chatView.Width).Render(info)
	return infoBar
}

func (p ChatPane) renderSelectionViewInfoRow() string {
	info := ""
	if p.selectionView.IsCharSelecting() {
		charsSelected := p.selectionView.SelectedCharCount()
		charsWord := "chars"
		if charsSelected == 1 {
			charsWord = "char"
		}

		info += fmt.Sprintf("▐ Selected [%d %s]", charsSelected, charsWord)
		info += "  | `r` to copy raw • `y` to copy with formatting"

	} else if p.selectionView.IsSelecting() {
		linesSelected := p.selectionView.GetSelectedLines()
		linesWord := "lines"
		if len(linesSelected) == 1 {
			linesWord = "line"
		}

		info += fmt.Sprintf("▐ Selected [%d %s]", len(linesSelected), linesWord)
		info += "  | `r` to copy raw • `y` to copy with formatting"

	} else {
		info += "▐ Press 'space' to start selecting"
	}

	infoBar := infoBarStyle.Width(p.chatView.Width).Render(info)
	return infoBar
}

func (p ChatPane) initializePane(session sessions.Session) (ChatPane, tea.Cmd) {
	paneWidth, paneHeight := util.CalcChatPaneSize(p.terminalWidth, p.terminalHeight, p.viewMode)
	if !p.isChatPaneReady {
		p.chatView = viewport.New(paneWidth, paneHeight-2)
		p.chatView.MouseWheelEnabled = false

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
	oldContent := util.GetMessagesAsPrettyString(
		messages,
		paneWidth-1,
		p.colors,
		p.quickChatActive,
		p.currentSettings)
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
