package components

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/BalanceBalls/nekot/util"
	zone "github.com/lrstanley/bubblezone/v2"
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
	Id   string
	Text string
}

func (i ModelsListItem) FilterValue() string { return zone.Mark(i.Id, i.Text) }

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
	str = zone.Mark(i.Id, str)

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

func (l ModelsList) VisibleItems() []list.Item {
	return l.list.VisibleItems()
}

func (l ModelsList) IsFiltering() bool {
	return l.list.SettingFilter()
}

func (l ModelsList) Update(msg tea.Msg) (ModelsList, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {

	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			l.list.CursorUp()
			return l, nil
		}

		if msg.Button == tea.MouseWheelDown {
			l.list.CursorDown()
			return l, nil
		}
	}

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
	filterStyles := l.FilterInput.Styles()
	filterStyles.Focused.Prompt = filterStyles.Focused.Prompt.Foreground(colors.ActiveTabBorderColor).PaddingBottom(0).Margin(0)
	filterStyles.Blurred.Prompt = filterStyles.Focused.Prompt
	filterStyles.Cursor.Color = colors.NormalTabBorderColor
	l.FilterInput.SetStyles(filterStyles)

	return ModelsList{
		list: l,
	}
}
