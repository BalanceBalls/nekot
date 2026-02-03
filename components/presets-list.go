package components

import (
	"fmt"
	"io"
	"strings"

	"github.com/BalanceBalls/nekot/settings"
	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

type PresetsList struct {
	currentPresetId    int
	list               list.Model
	service            *settings.SettingsService
	confirmationActive bool
}

var presetItemSpan = lipgloss.NewStyle().
	PaddingLeft(util.ListItemPaddingLeft)

var presetItemSpanSelected = lipgloss.NewStyle().
	PaddingLeft(util.ListItemPaddingLeft)

type PresetsListItem struct {
	Id       string
	PresetId int
	Text     string
}

func (i PresetsListItem) FilterValue() string { return zone.Mark(i.Id, i.Text) }

type presetsItemDelegate struct{}

func (d presetsItemDelegate) Height() int                             { return 1 }
func (d presetsItemDelegate) Spacing() int                            { return 0 }
func (d presetsItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d presetsItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(PresetsListItem)
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

func (l *PresetsList) View() string {
	if l.list.FilterState() == list.Filtering {
		l.list.SetShowStatusBar(false)
	} else {
		l.list.SetShowStatusBar(true)
	}
	view := l.list.View()
	if l.confirmationActive {
		view += "\n Remove preset? y/n"
	} else {
		view += util.HelpStyle.Render("\n d delete" + util.TipsSeparator + "/ filter")
	}
	return view
}

func (l *PresetsList) GetSelectedItem() (PresetsListItem, bool) {
	item, ok := l.list.SelectedItem().(PresetsListItem)
	return item, ok
}

func (l PresetsList) VisibleItems() []list.Item {
	return l.list.VisibleItems()
}

func (l PresetsList) IsFiltering() bool {
	return l.list.SettingFilter()
}

func (l PresetsList) IsFirstPage() bool {
	return l.list.Paginator.Page == 0
}

func (l PresetsList) getCurrentPreset() (PresetsListItem, int) {
	presets := l.list.Items()
	currentIdx := l.list.Index()
	preset := presets[currentIdx].(PresetsListItem)
	return preset, currentIdx
}

func (l *PresetsList) hideConfirmation() {
	l.confirmationActive = false
}

func (l *PresetsList) showConfirmation() {
	l.confirmationActive = true
}

func (l *PresetsList) removePreset() {
	preset, idx := l.getCurrentPreset()
	err := l.service.RemovePreset(preset.PresetId)
	if err != nil {
		util.Slog.Error("failed to remove a preset", "error", err.Error())
		return
	}
	l.list.RemoveItem(idx)
}

func (l PresetsList) Update(msg tea.Msg) (PresetsList, tea.Cmd) {
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

	case tea.KeyMsg:
		key := msg.String()
		switch key {
		case "d":
			preset, _ := l.getCurrentPreset()
			if preset.PresetId != l.currentPresetId && preset.PresetId != util.DefaultSettingsId {
				l.showConfirmation()
			}
			return l, cmd
		case "y":
			if !l.confirmationActive {
				break
			}
			l.removePreset()
			l.hideConfirmation()
			return l, cmd
		case "n":
			if !l.confirmationActive {
				break
			}
			l.hideConfirmation()
			return l, cmd
		default:
			if l.confirmationActive {
				return l, cmd
			}
		}
	}
	l.list, cmd = l.list.Update(msg)
	return l, cmd
}

func NewPresetsList(
	items []list.Item,
	w, h int,
	currentId int,
	colors util.SchemeColors,
	service *settings.SettingsService,
) PresetsList {
	l := list.New(items, presetsItemDelegate{}, w, h-1)

	l.SetStatusBarItemName("found", "found")
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()

	l.Paginator.ActiveDot = lipgloss.NewStyle().
		Foreground(colors.HighlightColor).
		Render(util.ActiveDot)
	l.Paginator.InactiveDot = lipgloss.NewStyle().
		Foreground(colors.DefaultTextColor).
		Render(util.InactiveDot)
	listItemSpan = listItemSpan.Foreground(colors.DefaultTextColor)
	listItemSpanSelected = listItemSpanSelected.Foreground(colors.AccentColor)
	l.FilterInput.PromptStyle = l.FilterInput.PromptStyle.Foreground(colors.ActiveTabBorderColor).
		PaddingBottom(0).
		Margin(0)
	l.FilterInput.Cursor.Style = l.FilterInput.Cursor.Style.Foreground(colors.NormalTabBorderColor)

	return PresetsList{
		currentPresetId: currentId,
		list:            l,
		service:         service,
	}
}
