package components

import (
	"fmt"
	"io"
	"strings"

	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ModelsList struct {
	list list.Model
}

var tips = "/ filter"
var listItemSpan = lipgloss.NewStyle().
	PaddingLeft(util.ListItemPaddingLeft)

var listItemSpanSelected = lipgloss.NewStyle().
	PaddingLeft(util.ListItemPaddingLeft)

type ModelsListItem struct {
	Text string
}

func (i ModelsListItem) FilterValue() string { return i.Text }

type modelItemDelegate struct{}

func (d modelItemDelegate) Height() int                             { return 1 }
func (d modelItemDelegate) Spacing() int                            { return 0 }
func (d modelItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d modelItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(ModelsListItem)
	if !ok {
		return
	}

	str := fmt.Sprintf("%d. %s", index+1, i.Text)
	str = util.TrimListItem(str, m.Width())

	fn := listItemSpan.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			row := "> " + strings.Join(s, " ")
			return listItemSpanSelected.Render(row)
		}
	}

	fmt.Fprint(w, fn(str))
}

func (l *ModelsList) View() string {
	if l.list.FilterState() == list.Filtering {
		l.list.SetShowStatusBar(false)
	} else {
		l.list.SetShowStatusBar(true)
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		l.list.View(),
		util.HelpStyle.Render(tips))
}

func (l *ModelsList) GetSelectedItem() (ModelsListItem, bool) {
	item, ok := l.list.SelectedItem().(ModelsListItem)
	return item, ok
}

func (l ModelsList) IsFiltering() bool {
	return l.list.SettingFilter()
}

func (l ModelsList) Update(msg tea.Msg) (ModelsList, tea.Cmd) {
	var cmd tea.Cmd
	l.list, cmd = l.list.Update(msg)
	return l, cmd
}

func NewModelsList(items []list.Item, w, h int, colors util.SchemeColors) ModelsList {
	h = h - 1 // account for tips row
	l := list.New(items, modelItemDelegate{}, w, h)

	l.SetStatusBarItemName("fetched", "fetched")
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()

	l.Paginator.ActiveDot = lipgloss.NewStyle().Foreground(colors.HighlightColor).Render(util.ActiveDot)
	l.Paginator.InactiveDot = lipgloss.NewStyle().Foreground(colors.DefaultTextColor).Render(util.InactiveDot)
	listItemSpan = listItemSpan.Foreground(colors.DefaultTextColor)
	listItemSpanSelected = listItemSpanSelected.Foreground(colors.AccentColor)
	l.FilterInput.PromptStyle = l.FilterInput.PromptStyle.Foreground(colors.ActiveTabBorderColor).PaddingBottom(0).Margin(0)
	l.FilterInput.Cursor.Style = l.FilterInput.Cursor.Style.Foreground(colors.NormalTabBorderColor)

	return ModelsList{
		list: l,
	}
}
