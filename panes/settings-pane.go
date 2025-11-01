package panes

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/BalanceBalls/nekot/clients"
	"github.com/BalanceBalls/nekot/components"
	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/settings"
	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type settingsViewMode int

const (
	defaultView settingsViewMode = iota
	modelsView
	presetsView
)

type settingsChangeMode int

const (
	inactive settingsChangeMode = iota
	presetChange
	maxTokensChange
	frequencyChange
	tempChange
	topPChange
	systemPromptChange
)

type settingsKeyMap struct {
	editTemp      key.Binding
	editFrequency key.Binding
	editTopP      key.Binding
	editSysPrompt key.Binding
	editMaxTokens key.Binding
	changeModel   key.Binding
	reset         key.Binding
	savePreset    key.Binding
	presetsMenu   key.Binding
	goBack        key.Binding
	choose        key.Binding
}

var defaultSettingsKeyMap = settingsKeyMap{
	editTemp:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "change temperature")),
	editFrequency: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "change frequency")),
	editTopP:      key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "change top_p")),
	editSysPrompt: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "s - edit sys prompt")),
	editMaxTokens: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "change max_tokens")),
	changeModel:   key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "change current model")),
	savePreset: key.NewBinding(
		key.WithKeys("ctrl+p"),
		key.WithHelp("ctrl+p", "ctrl+p - new preset"),
	),
	reset: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r", "ctrl+r - reset preset"),
	),
	presetsMenu: key.NewBinding(
		key.WithKeys("]", tea.KeyRight.String()),
		key.WithHelp("]", "presets menu"),
	),
	goBack: key.NewBinding(
		key.WithKeys(tea.KeyEsc.String(), "["),
		key.WithHelp("esc, [", "go back"),
	),
	choose: key.NewBinding(key.WithKeys(tea.KeyEnter.String())),
}

type SettingsPane struct {
	terminalWidth   int
	terminalHeight  int
	isFocused       bool
	viewMode        settingsViewMode
	changeMode      settingsChangeMode
	textInput       textinput.Model
	settingsService *settings.SettingsService
	spinner         spinner.Model
	loading         bool
	colors          util.SchemeColors
	keyMap          settingsKeyMap

	modelPicker  components.ModelsList
	presetPicker components.PresetsList

	container lipgloss.Style

	initMode  bool
	config    *config.Config
	llmClient util.LlmClient
	settings  util.Settings
	mainCtx   context.Context
}

var settingsService *settings.SettingsService

var activeHeader = lipgloss.NewStyle().
	BorderStyle(lipgloss.ThickBorder()).
	BorderBottom(true).
	Bold(true).
	MarginLeft(util.ListItemMarginLeft)

var inactiveHeader = list.DefaultStyles().
	NoItems.
	Bold(true).
	MarginLeft(util.ListItemMarginLeft)

var commandTips = lipgloss.NewStyle()
var listItemHeading = lipgloss.NewStyle().
	PaddingLeft(util.ListItemPaddingLeft)

var presetItemHeading = lipgloss.NewStyle().
	PaddingLeft(util.ListItemPaddingLeft).
	Bold(true)

var listItemSpan = lipgloss.NewStyle()
var spinnerStyle = lipgloss.NewStyle()

func (p SettingsPane) listItemRenderer(heading string, value string) string {
	headingEl := listItemHeading.Render
	spanEl := listItemSpan.Foreground(p.colors.DefaultTextColor).Render

	return headingEl(util.ListHeadingDot+" "+heading, spanEl(value))
}

func (p SettingsPane) presetItemRenderer(value string) string {
	headingEl := presetItemHeading.Render
	spanEl := listItemSpan.Bold(true).Foreground(p.colors.DefaultTextColor).Render

	return headingEl(util.ListHeadingDot+" Preset:", spanEl(value))
}

func initSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Points
	s.Style = spinnerStyle

	return s
}

func NewSettingsPane(db *sql.DB, ctx context.Context) SettingsPane {
	config, ok := config.FromContext(ctx)
	if !ok {
		fmt.Println("No config found")
		panic("No config found in context")
	}

	settingsService = settings.NewSettingsService(db)
	llmClient := clients.ResolveLlmClient(
		config.Provider,
		config.ProviderBaseUrl,
		config.SystemMessage,
	)

	colors := config.ColorScheme.GetColors()
	listItemSpan = listItemSpan.Foreground(colors.DefaultTextColor)
	listItemHeading = listItemHeading.Foreground(colors.MainColor)
	presetItemHeading = presetItemHeading.Foreground(colors.AccentColor)
	activeHeader = activeHeader.Foreground(colors.DefaultTextColor).
		BorderForeground(colors.DefaultTextColor)
	spinnerStyle = spinnerStyle.Foreground(colors.AccentColor)
	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), true).
		BorderForeground(colors.NormalTabBorderColor)

	spinner := initSpinner()

	return SettingsPane{
		keyMap:          defaultSettingsKeyMap,
		colors:          colors,
		terminalWidth:   util.DefaultTerminalWidth,
		viewMode:        defaultView,
		changeMode:      inactive,
		container:       containerStyle,
		config:          config,
		llmClient:       llmClient,
		settingsService: settingsService,
		spinner:         spinner,
		initMode:        true,
		loading:         true,
		mainCtx:         ctx,
	}
}

func (p *SettingsPane) Init() tea.Cmd {
	return p.spinner.Tick
}

func (p SettingsPane) Update(msg tea.Msg) (SettingsPane, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	if p.changeMode != inactive {
		beforeChange := p.textInput.Value()
		p.textInput, cmd = p.textInput.Update(msg)
		if p.textInput.Err != nil {
			p.textInput.SetValue(beforeChange)
		} else {
			cmds = append(cmds, cmd)
		}
	}

	switch msg := msg.(type) {

	case util.ErrorEvent:
		p.loading = false
		p.viewMode = defaultView
		p.changeMode = inactive

	case util.SystemPromptUpdatedMsg:
		p.settings.SystemPrompt = &msg.SystemPrompt
		var updErr error
		p.settings, updErr = p.settingsService.UpdateSettings(p.settings)
		if updErr != nil {
			cmds = append(cmds, util.MakeErrorMsg(updErr.Error()))
			break
		}
		cmds = append(cmds, settings.MakeSettingsUpdateMsg(p.settings, nil))
		cmds = append(cmds, util.SendNotificationMsg(util.SysPromptChangedNotification))

	case util.FocusEvent:
		p.isFocused = msg.IsFocused
		p.viewMode = defaultView

		return p, nil

	case tea.WindowSizeMsg:
		p.terminalWidth = msg.Width
		p.terminalHeight = msg.Height
		w, h := util.CalcSettingsPaneSize(p.terminalWidth, p.terminalHeight)
		p.container = p.container.Width(w).Height(h)

	case spinner.TickMsg:
		p.spinner, cmd = p.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case settings.UpdateSettingsEvent:
		if msg.Err != nil {
			return p, util.MakeErrorMsg(msg.Err.Error())
		}

		if p.initMode {
			p.settings = msg.Settings
			models := []list.Item{components.ModelsListItem{Text: msg.Settings.Model}}

			w, h := util.CalcModelsListSize(p.terminalWidth, p.terminalHeight)
			p.modelPicker = components.NewModelsList(models, w, h, p.colors)
			p.initMode = false
			p.loading = false

			cmds = append(cmds, util.SendAsyncDependencyReadyMsg(util.SettingsPaneModule))
		}

	case util.ModelsLoaded:
		p.loading = false
		p.viewMode = modelsView
		p.updateModelsList(msg.Models)

	case tea.KeyMsg:
		if p.initMode {
			break
		}

		if p.isFocused {
			if p.changeMode != inactive {
				cmd = p.handleSettingsUpdate(msg)
				cmds = append(cmds, cmd)
			} else {
				switch p.viewMode {
				case defaultView:
					cmd = p.handleViewMode(msg)
					cmds = append(cmds, cmd)
				case modelsView:
					cmd = p.handleModelMode(msg)
					cmds = append(cmds, cmd)
				case presetsView:
					cmd = p.handlePresetMode(msg)
					cmds = append(cmds, cmd)
				}
			}
		}
	}

	if !p.initMode && p.viewMode == modelsView {
		p.modelPicker, cmd = p.modelPicker.Update(msg)
		cmds = append(cmds, cmd)
	}

	if !p.initMode && p.viewMode == presetsView {
		p.presetPicker, cmd = p.presetPicker.Update(msg)
		cmds = append(cmds, cmd)
	}

	return p, tea.Batch(cmds...)
}

func (p SettingsPane) View() string {
	w, h := util.CalcSettingsPaneSize(p.terminalWidth, p.terminalHeight)
	defaultHeader := lipgloss.JoinHorizontal(
		lipgloss.Left,
		activeHeader.Render("[Settings]"),
		inactiveHeader.Render("Presets"),
	)
	if p.viewMode == modelsView {
		return p.container.Width(w).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				defaultHeader,
				p.modelPicker.View(),
			),
		)
	}

	if p.viewMode == presetsView {
		return p.container.Width(w).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				lipgloss.JoinHorizontal(
					lipgloss.Left,
					inactiveHeader.Render("Settings"),
					activeHeader.Render("[Presets]"),
				),
				p.presetPicker.View(),
			),
		)
	}

	editForm := ""
	tips := strings.Join([]string{
		"] [ - switch tabs",
		p.keyMap.savePreset.Help().Desc,
		p.keyMap.reset.Help().Desc,
		p.keyMap.editSysPrompt.Help().Desc}, "\n")

	if p.changeMode != inactive {
		tips = ""
		editForm = p.textInput.View()
	}

	if p.terminalHeight < util.HeightMinScalingLimit {
		tips = ""
	}

	modelName := util.TrimListItem(
		p.settings.Model,
		util.CalcMaxSettingItemWidth(p.container.GetWidth()))
	modelRowContent := p.listItemRenderer("(m) model", modelName)
	if p.loading {
		modelRowContent = p.listItemRenderer(p.spinner.View(), "")
	}

	var (
		temp      = "not set"
		top_p     = "not set"
		frequency = "not set"
	)

	if p.settings.Temperature != nil {
		temp = fmt.Sprint(*p.settings.Temperature)
	}
	if p.settings.TopP != nil {
		top_p = fmt.Sprint(*p.settings.TopP)
	}
	if p.settings.Frequency != nil {
		frequency = fmt.Sprint(*p.settings.Frequency)
	}

	tipsHeihgt := len(strings.Split(tips, "\n"))
	listItemsHeight := h - tipsHeihgt

	lowerRows := util.HelpStyle.Render(tips) + "\n" + editForm
	if p.terminalHeight < util.HeightMinScalingLimit || p.viewMode != defaultView || !p.isFocused {
		lowerRows = editForm
		listItemsHeight = h
	}

	borderColor := p.colors.NormalTabBorderColor
	if p.isFocused {
		borderColor = p.colors.ActiveTabBorderColor
	}

	return p.container.Width(w).BorderForeground(borderColor).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			defaultHeader,
			lipgloss.NewStyle().Height(listItemsHeight).Render(
				lipgloss.JoinVertical(lipgloss.Left,
					p.presetItemRenderer(p.settings.PresetName),
					modelRowContent,
					p.listItemRenderer("(t) max_tokens", fmt.Sprint(p.settings.MaxTokens)),
					p.listItemRenderer("(e) temperature", temp),
					p.listItemRenderer("(f) frequency", frequency),
					p.listItemRenderer("(p) top_p", top_p),
				),
			),
			lowerRows,
		),
	)
}

func (p SettingsPane) AllowFocusChange() bool {
	return p.viewMode == defaultView && p.changeMode == inactive
}
