package components

import (
	"fmt"
	"io"
	"strings"

	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

var (
	itemStyle         = lipgloss.NewStyle().PaddingLeft(util.ListItemPaddingLeft)
	selectedItemStyle = lipgloss.
				NewStyle().
				PaddingLeft(util.ListRightShiftedItemPadding)
	activeItemStyle = itemStyle
)

type SessionListItem struct {
	Id        string
	SessionId int
	Text      string
	IsActive  bool
}

type SessionsList struct {
	list list.Model
}

func (i SessionListItem) FilterValue() string { return zone.Mark(i.Id, i.Text) }

type sessionItemDelegate struct{}

func (d sessionItemDelegate) Height() int  { return 1 }
func (d sessionItemDelegate) Spacing() int { return 0 }
func (d sessionItemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	var cmds []tea.Cmd

	return tea.Batch(cmds...)
}

func (d sessionItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(SessionListItem)
	if !ok {
		util.Slog.Warn("failed to cast to SessionListItem")
		return
	}

	str := zone.Mark(i.Id, fmt.Sprintf("%s", i.Text))
	str = util.TrimListItem(str, m.Width())

	fn := itemStyle.Render
	selectedRender := selectedItemStyle.Render

	if i.IsActive {
		fn = activeItemStyle.Render
		selectedRender = activeItemStyle.Render
	}

	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedRender("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

func (l *SessionsList) GetSelectedItem() (SessionListItem, bool) {
	item, ok := l.list.SelectedItem().(SessionListItem)
	return item, ok
}

func (l *SessionsList) SetSelectedItem(id int) {
	l.list.Select(id)
}

func (l *SessionsList) SetItems(items []list.Item) {
	l.list.ResetFilter()
	l.list.SetItems(items)
}

func (l *SessionsList) SetShowStatusBar(show bool) {
	l.list.SetShowStatusBar(show)
}

func (l *SessionsList) SetSize(w, h int) {
	if w > 2 && h > 2 {
		l.list.SetWidth(w)
		l.list.SetHeight(h)
	}
}

func (l SessionsList) VisibleItems() []list.Item {
	return l.list.VisibleItems()
}

func (l SessionsList) IsFiltering() bool {
	return l.list.SettingFilter()
}

func (l SessionsList) GetWidth() int {
	return l.list.Width()
}

func (l SessionsList) Update(msg tea.Msg) (SessionsList, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonWheelUp {
			l.list.CursorUp()
			return l, nil
		}

		if msg.Button == tea.MouseButtonWheelDown {
			l.list.CursorDown()
			return l, nil
		}
	}

	l.list, cmd = l.list.Update(msg)
	return l, cmd
}

func NewSessionsList(items []list.Item, w, h int, colors util.SchemeColors) SessionsList {
	l := list.New(items, sessionItemDelegate{}, w, h)

	l.SetStatusBarItemName("session", "sessions")
	l.SetShowTitle(false)
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowFilter(true)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()

	l.Paginator.ActiveDot = lipgloss.NewStyle().
		Foreground(colors.HighlightColor).
		Render(util.ActiveDot)
	l.Paginator.InactiveDot = lipgloss.NewStyle().
		Foreground(colors.DefaultTextColor).
		Render(util.InactiveDot)
	selectedItemStyle = selectedItemStyle.Foreground(colors.AccentColor)
	activeItemStyle = activeItemStyle.Foreground(colors.HighlightColor)
	itemStyle = itemStyle.Foreground(colors.DefaultTextColor)
	l.FilterInput.PromptStyle = l.FilterInput.PromptStyle.Foreground(colors.ActiveTabBorderColor).
		PaddingBottom(0).
		Margin(0)
	l.FilterInput.Cursor.Style = l.FilterInput.Cursor.Style.Foreground(colors.NormalTabBorderColor)

	return SessionsList{
		list: l,
	}
}

func (l *SessionsList) EditListView() string {
	return lipgloss.
		NewStyle().
		PaddingLeft(util.DefaultElementsPadding).
		Render(l.list.View())
}
