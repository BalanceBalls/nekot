package panes

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/sessions"
	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const notificationDisplayDurationSec = 2

const (
	copiedLabelText           = "Copied to clipboard"
	cancelledLabelText        = "Inference interrupted"
	sysPromptChangedLabelText = "System prompt updated"
	presetSavedLabelText      = "Preset saved"
	sessionSavedLableText     = "Session saved"
	idleLabelText             = "IDLE"
	processingLabelText       = "Processing"
)

var infoSpinnerStyle = lipgloss.NewStyle()
var defaultLabelStyle = lipgloss.NewStyle().
	BorderLeft(true).
	BorderStyle(lipgloss.InnerHalfBlockBorder()).
	Bold(true).
	MarginRight(1).
	PaddingRight(1).
	PaddingLeft(1)

type InfoPane struct {
	sessionService *sessions.SessionService
	currentSession sessions.Session
	colors         util.SchemeColors
	spinner        spinner.Model

	processingIdleLabel   lipgloss.Style
	processingActiveLabel lipgloss.Style
	promptTokensLablel    lipgloss.Style
	completionTokensLabel lipgloss.Style
	notificationLabel     lipgloss.Style
	quickChatLabel        lipgloss.Style

	showNotification bool
	notification     util.Notification
	isProcessing     bool
	terminalWidth    int
	terminalHeight   int
}

func NewInfoPane(db *sql.DB, ctx context.Context) InfoPane {
	ss := sessions.NewSessionService(db)

	config, ok := config.FromContext(ctx)
	if !ok {
		util.Slog.Error("failed to extract config from context")
		panic("No config found in context")
	}
	colors := config.ColorScheme.GetColors()
	spinner := initInfoSpinner()

	infoSpinnerStyle = infoSpinnerStyle.Foreground(colors.HighlightColor)
	processingIdleLabel := defaultLabelStyle.
		BorderLeftForeground(colors.HighlightColor).
		Foreground(colors.DefaultTextColor)
	processingActiveLabel := defaultLabelStyle.
		BorderLeftForeground(colors.AccentColor).
		Foreground(colors.DefaultTextColor)
	promptTokensLablel := defaultLabelStyle.
		BorderLeftForeground(colors.ActiveTabBorderColor).
		Foreground(colors.DefaultTextColor)
	completionTokensLabel := defaultLabelStyle.
		BorderLeftForeground(colors.ActiveTabBorderColor).
		Foreground(colors.DefaultTextColor)
	notificationLabel := defaultLabelStyle.
		Background(colors.NormalTabBorderColor).
		BorderLeftForeground(colors.HighlightColor).
		Align(lipgloss.Left).
		Foreground(lipgloss.Color(colors.DefaultTextColor.Dark))
	quickChatLabel := defaultLabelStyle.Background(colors.HighlightColor)

	return InfoPane{
		processingIdleLabel:   processingIdleLabel,
		processingActiveLabel: processingActiveLabel,
		promptTokensLablel:    promptTokensLablel,
		completionTokensLabel: completionTokensLabel,
		notificationLabel:     notificationLabel,
		quickChatLabel:        quickChatLabel,

		spinner:        spinner,
		colors:         colors,
		sessionService: ss,
		terminalWidth:  util.DefaultTerminalWidth,
		terminalHeight: util.DefaultTerminalHeight,
	}
}

func initInfoSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Ellipsis
	s.Style = infoSpinnerStyle

	return s
}

type tickMsg struct{}

func (p InfoPane) Init() tea.Cmd {
	return nil
}

func (p InfoPane) Update(msg tea.Msg) (InfoPane, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.terminalWidth = msg.Width
		p.terminalHeight = msg.Height

	case sessions.LoadDataFromDB:
		p.currentSession = msg.Session

	case sessions.UpdateCurrentSession:
		p.currentSession = msg.Session

	case spinner.TickMsg:
		p.spinner, cmd = p.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case util.NotificationMsg:
		p.notification = msg.Notification
		p.showNotification = true
		cmds = append(cmds, tickAfter(notificationDisplayDurationSec))

	case tickMsg:
		p.showNotification = false

	case util.ProcessingStateChanged:
		p.isProcessing = msg.IsProcessing
		if !msg.IsProcessing {
			session, err := p.sessionService.GetSession(p.currentSession.ID)
			if err != nil {
				util.MakeErrorMsg(err.Error())
			}
			p.currentSession = session
		} else {
			cmds = append(cmds, p.spinner.Tick)
		}
	}

	return p, tea.Batch(cmds...)
}

func (p InfoPane) View() string {
	paneWidth, _ := util.CalcSettingsPaneSize(p.terminalWidth, p.terminalHeight)
	var processingLabel string
	if p.isProcessing {
		processingLabel = p.processingActiveLabel.Render(processingLabelText + p.spinner.View())
	} else {
		processingLabel = p.processingIdleLabel.Render(idleLabelText)
	}

	promptTokensLablel := p.promptTokensLablel.Render(
		fmt.Sprintf("IN: %d", p.currentSession.PromptTokens),
	)
	completionTokensLabel := p.completionTokensLabel.Render(
		fmt.Sprintf("OUT: %d", p.currentSession.CompletionTokens),
	)

	quickChatLabel := ""
	if p.currentSession.IsTemporary {
		quickChatLabel = p.quickChatLabel.Render("Q")
	}

	firstRow := processingLabel
	secondRow := lipgloss.JoinHorizontal(
		lipgloss.Left,
		promptTokensLablel,
		completionTokensLabel,
		quickChatLabel,
	)

	if p.showNotification {
		notificationLabel := lipgloss.NewStyle()
		notificationText := ""

		switch p.notification {
		case util.SessionSavedNotification:
			notificationText = sessionSavedLableText
			notificationLabel = p.notificationLabel.
				Background(p.colors.AccentColor).
				Width(paneWidth - 1)
		case util.PresetSavedNotification:
			notificationText = presetSavedLabelText
			notificationLabel = p.notificationLabel.
				Background(p.colors.AccentColor).
				Width(paneWidth - 1)
		case util.SysPromptChangedNotification:
			notificationText = sysPromptChangedLabelText
			notificationLabel = p.notificationLabel.
				Background(p.colors.AccentColor).
				Width(paneWidth - 1)
		case util.CopiedNotification:
			notificationText = copiedLabelText
			notificationLabel = p.notificationLabel.
				Background(p.colors.NormalTabBorderColor).
				Width(paneWidth - 1)
		case util.CancelledNotification:
			notificationText = cancelledLabelText
			notificationLabel = p.notificationLabel.
				Background(p.colors.ErrorColor).
				Width(paneWidth - 1)
		}

		firstRow = lipgloss.JoinHorizontal(
			lipgloss.Left,
			notificationLabel.Render(notificationText),
		)

		secondRow = ""
	}

	return lipgloss.NewStyle().
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(p.colors.NormalTabBorderColor).
		Width(paneWidth).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Left,
				firstRow,
				secondRow,
			),
		)
}

func tickAfter(seconds int) tea.Cmd {
	return tea.Tick(time.Second*time.Duration(seconds), func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}
