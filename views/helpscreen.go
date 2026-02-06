package views

import (
	"github.com/charmbracelet/lipgloss"
)

func (m MainView) renderHelpView() string {
	colors := m.config.ColorScheme.GetColors()

	// titleStyle := lipgloss.NewStyle().
	// 	Bold(true).
	// 	BorderForeground(colors.NormalTabBorderColor).
	// 	Foreground(colors.ActiveTabBorderColor).
	// 	MarginBottom(1).
	// 	Align(lipgloss.Center)

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(colors.MainColor).
		MarginTop(1).
		MarginBottom(0)

	keyStyle := lipgloss.NewStyle().
		Foreground(colors.AccentColor).
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(colors.DefaultTextColor)

	helpContainer := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colors.NormalTabBorderColor).
		Padding(2).
		AlignHorizontal(lipgloss.Center).
		Width(m.terminalWidth - 4).
		Height(m.terminalHeight - 2)

	binding := func(key, desc string) string {
		return lipgloss.JoinHorizontal(lipgloss.Left,
			keyStyle.Render(key),
			" ",
			descStyle.Render(desc),
		)
	}

	section := func(name string, bindings ...string) string {
		content := lipgloss.JoinVertical(lipgloss.Left, bindings...)
		return lipgloss.JoinVertical(lipgloss.Left,
			sectionStyle.Render(name),
			content,
		)
	}

	globalSection := section("Global",
		binding("ctrl+c", "quit"),
		binding("ctrl+o", "toggle zen mode"),
		binding("ctrl+n", "new session"),
		binding("ctrl+q", "quick chat"),
		binding("ctrl+x", "save quick chat"),
		binding("ctrl+w", "toggle web search"),
		binding("ctrl+h", "hide/show reasoning"),
		binding("ctrl+b/s", "stop inference"),
		binding("tab/shift+tab", "navigate panes"),
		binding("1/2/3/4", "jump to pane"),
		binding("?", "show help"),
	)

	promptSection := section("Prompt",
		binding("i", "enter insert mode"),
		binding("ctrl+e", "toggle editor mode"),
		binding("esc", "exit insert/editor mode"),
		binding("ctrl+r", "clear prompt"),
		binding("ctrl+v", "paste from clipboard"),
		binding("ctrl+s", "paste code block"),
		binding("ctrl+a", "attach image/file"),
		binding("enter", "send message"),
	)

	chatSection := section("Chat",
		binding("space/v/V", "enter selection mode"),
		binding("y", "copy last message"),
		binding("Y", "copy all messages"),
		binding("g", "scroll to top"),
		binding("G", "scroll to bottom"),
		binding("esc", "exit selection mode"),
	)

	sessionsSection := section("Sessions",
		binding("d", "delete session"),
		binding("e", "rename session"),
		binding("shift+x", "export session"),
		binding("/", "filter sessions"),
		binding("esc", "cancel action"),
	)

	settingsSection := section("Settings",
		binding("e", "change temperature"),
		binding("f", "change frequency"),
		binding("p", "change top_p"),
		binding("t", "change max_tokens"),
		binding("s", "edit system prompt"),
		binding("m", "change model"),
		binding("]/[", "switch tabs"),
		binding("ctrl+p", "save preset"),
		binding("ctrl+r", "reset preset"),
	)

	exitHint := lipgloss.NewStyle().
		Foreground(colors.HighlightColor).
		MarginTop(1).
		Align(lipgloss.Center).
		Render("Press ESC to close help")

	leftColumn := lipgloss.JoinVertical(lipgloss.Left,
		globalSection,
		settingsSection,
	)

	rightColumn := lipgloss.JoinVertical(lipgloss.Left,
		sessionsSection,
		promptSection,
		chatSection,
	)

	columns := lipgloss.NewStyle()
	// Padding(2).
	// Border(lipgloss.NormalBorder()).
	// BorderForeground(colors.ActiveTabBorderColor)

	content := lipgloss.JoinVertical(lipgloss.Center,
		// titleStyle.Render("__________________________________________________"),
		columns.Render(lipgloss.JoinHorizontal(lipgloss.Top, leftColumn, "    ", rightColumn)),
		exitHint,
	)

	return lipgloss.Place(m.terminalWidth, m.terminalHeight,
		lipgloss.Center, lipgloss.Center,
		helpContainer.Render(content),
	)
}
