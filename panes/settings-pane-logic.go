package panes

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/BalanceBalls/nekot/components"
	"github.com/BalanceBalls/nekot/settings"
	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

const floatPrescision = 32

func (p *SettingsPane) handlePresetModeMouse(msg tea.MouseMsg) tea.Cmd {
	if zone.Get("set_p_settings_tab").InBounds(msg) && p.viewMode == presetsView {
		p.viewMode = defaultView
	}

	if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft && p.viewMode == presetsView {
		for _, listItem := range p.presetPicker.VisibleItems() {
			v, _ := listItem.(components.PresetsListItem)
			if zone.Get(v.Id).InBounds(msg) {
				return p.selectPreset(v.PresetId)
			}
		}
	}

	return nil
}

func (p *SettingsPane) handlePresetMode(msg tea.KeyMsg) tea.Cmd {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	if p.presetPicker.IsFiltering() {
		return tea.Batch(cmds...)
	}

	switch {
	case key.Matches(msg, p.keyMap.goBack):
		if msg.String() == tea.KeyLeft.String() && !p.presetPicker.IsFirstPage() {
			return nil
		}

		p.viewMode = defaultView
		return cmd

	case key.Matches(msg, p.keyMap.choose):
		i, ok := p.presetPicker.GetSelectedItem()
		if ok {
			presetId := int(i.PresetId)
			cmd = p.selectPreset(presetId)
			cmds = append(cmds, cmd)
		}
	}

	return tea.Batch(cmds...)
}

func (p *SettingsPane) selectPreset(presetId int) tea.Cmd {
	preset, err := p.settingsService.GetPreset(presetId)

	if err != nil {
		return util.MakeErrorMsg(err.Error())
	}

	preset.Model = p.settings.Model
	p.viewMode = defaultView
	p.settings = preset

	return settings.MakeSettingsUpdateMsg(p.settings, nil)
}

func (p *SettingsPane) handleModelModeMouse(msg tea.MouseMsg) tea.Cmd {
	if zone.Get("set_p_presets_tab").InBounds(msg) && p.viewMode == modelsView {
		return p.switchToPresets()
	}

	if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft && p.viewMode == modelsView {
		for _, listItem := range p.modelPicker.VisibleItems() {
			v, _ := listItem.(components.ModelsListItem)
			if zone.Get(v.Id).InBounds(msg) {
				return p.selectModel(string(v.Text))
			}
		}
	}

	return nil
}

func (p *SettingsPane) handleModelMode(msg tea.KeyMsg) tea.Cmd {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	if p.modelPicker.IsFiltering() {
		return tea.Batch(cmds...)
	}

	switch msg.Type {
	case tea.KeyEsc:
		p.viewMode = defaultView
		return cmd

	case tea.KeyEnter:
		i, ok := p.modelPicker.GetSelectedItem()
		if ok {
			cmd = p.selectModel(string(i.Text))
			cmds = append(cmds, cmd)
		}
	}

	return tea.Batch(cmds...)
}

func (p *SettingsPane) selectModel(model string) tea.Cmd {
	p.settings.Model = string(model)
	p.viewMode = defaultView

	var updateError error
	p.settings, updateError = settingsService.UpdateSettings(p.settings)
	if updateError != nil {
		return util.MakeErrorMsg(updateError.Error())
	}

	return settings.MakeSettingsUpdateMsg(p.settings, nil)
}

func (p *SettingsPane) handleViewModeMouse(msg tea.MouseMsg) tea.Cmd {
	if zone.Get("set_p_presets_tab").InBounds(msg) && p.viewMode == defaultView {
		return p.switchToPresets()
	}

	if zone.Get("set_p_preset_item").InBounds(msg) && p.viewMode == defaultView {
		return p.switchToPresets()
	}

	if zone.Get("models_list").InBounds(msg) {
		return p.switchToModelsList()
	}

	if zone.Get("max_tokens").InBounds(msg) {
		return p.configureInput("Enter Max Tokens", util.MaxTokensValidator, maxTokensChange)
	}

	if zone.Get("temperature").InBounds(msg) {
		return p.configureInput("Enter Temperature "+util.TemperatureRange, util.TemperatureValidator, tempChange)
	}

	if zone.Get("frequency").InBounds(msg) {
		return p.configureInput("Enter Frequency "+util.FrequencyRange, util.FrequencyValidator, frequencyChange)
	}

	if zone.Get("top_p").InBounds(msg) {
		return p.configureInput("Enter TopP "+util.TopPRange, util.TopPValidator, topPChange)
	}

	return nil
}

func (p *SettingsPane) handleViewMode(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, p.keyMap.presetsMenu):
		return p.switchToPresets()

	case key.Matches(msg, p.keyMap.changeModel):
		return p.switchToModelsList()

	case key.Matches(msg, p.keyMap.savePreset):
		cmd = p.configureInput(
			"Enter name for a preset",
			util.EmptyValidator,
			presetChange)

	case key.Matches(msg, p.keyMap.reset):
		var updErr error
		p.settings, updErr = p.settingsService.ResetToDefault(p.settings)
		if updErr != nil {
			return util.MakeErrorMsg(updErr.Error())
		}
		cmd = settings.MakeSettingsUpdateMsg(p.settings, nil)

	case key.Matches(msg, p.keyMap.editSysPrompt):
		content := ""
		if p.settings.SystemPrompt != nil {
			content = *p.settings.SystemPrompt
		}
		cmd = util.SwitchToEditor(content, util.SystemMessageEditing, false)

	case key.Matches(msg, p.keyMap.editFrequency):
		cmd = p.configureInput("Enter Frequency "+util.FrequencyRange, util.FrequencyValidator, frequencyChange)
	case key.Matches(msg, p.keyMap.editTemp):
		cmd = p.configureInput("Enter Temperature "+util.TemperatureRange, util.TemperatureValidator, tempChange)
	case key.Matches(msg, p.keyMap.editTopP):
		cmd = p.configureInput("Enter TopP "+util.TopPRange, util.TopPValidator, topPChange)
	case key.Matches(msg, p.keyMap.editMaxTokens):
		cmd = p.configureInput("Enter Max Tokens", util.MaxTokensValidator, maxTokensChange)
	}

	return cmd
}

func (p *SettingsPane) switchToPresets() tea.Cmd {
	p.viewMode = presetsView
	presets, err := p.loadPresets()
	if err != nil {
		return util.MakeErrorMsg(err.Error())
	}
	p.updatePresetsList(presets)
	return nil
}

func (p *SettingsPane) switchToModelsList() tea.Cmd {
	p.loading = true
	p.changeMode = inactive
	return tea.Batch(
		func() tea.Msg { return p.loadModels(p.config.Provider, p.config.ProviderBaseUrl) },
		p.spinner.Tick)
}

func (p *SettingsPane) configureInput(title string, validator func(str string) error, mode settingsChangeMode) tea.Cmd {
	ti := textinput.New()
	ti.PromptStyle = lipgloss.NewStyle().PaddingLeft(util.DefaultElementsPadding)
	p.textInput = ti
	p.textInput.Placeholder = title
	p.textInput.Width = p.container.GetWidth() - util.InputContainerDelta
	p.changeMode = mode
	p.textInput.Focus()
	p.textInput.Validate = validator
	return p.textInput.Cursor.BlinkCmd()
}

func (p *SettingsPane) handleSettingsUpdate(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg.Type {

	case tea.KeyEsc:
		p.viewMode = defaultView
		p.changeMode = inactive
		return cmd

	case tea.KeyEnter:
		inputValue := p.textInput.Value()
		if inputValue == "" {
			return cmd
		}

		switch p.changeMode {
		case presetChange:
			err := p.updatePresetName(inputValue)
			if err != nil {
				return util.MakeErrorMsg(err.Error())
			}
			cmds = append(cmds, util.SendNotificationMsg(util.PresetSavedNotification))
			cmds = append(cmds, settings.MakeSettingsUpdateMsg(p.settings, nil))
			return tea.Batch(cmds...)

		case frequencyChange:
			err := p.updateFrequency(inputValue)
			if err != nil {
				return util.MakeErrorMsg(err.Error())
			}

		case maxTokensChange:
			err := p.updateMaxTokens(inputValue)
			if err != nil {
				return util.MakeErrorMsg(err.Error())
			}

		case tempChange:
			err := p.updateTemperature(inputValue)
			if err != nil {
				return util.MakeErrorMsg(err.Error())
			}

		case topPChange:
			err := p.updateTopP(inputValue)
			if err != nil {
				return util.MakeErrorMsg(err.Error())
			}
		}

		newSettings, err := settingsService.UpdateSettings(p.settings)
		if err != nil {
			return util.MakeErrorMsg(err.Error())
		}

		p.settings = newSettings
		p.viewMode = defaultView
		p.changeMode = inactive
		cmds = append(cmds, settings.MakeSettingsUpdateMsg(p.settings, nil))
	}

	cmds = append(cmds, p.textInput.Cursor.BlinkCmd())
	return tea.Batch(cmds...)
}

func (p SettingsPane) loadModels(providerType string, apiUrl string) tea.Msg {
	ctx, cancel := context.
		WithTimeout(p.mainCtx, time.Duration(util.DefaultRequestTimeOutSec*time.Second))
	defer cancel()

	availableModels, err := p.settingsService.GetProviderModels(ctx, providerType, apiUrl)

	if err != nil {
		return util.MakeErrorMsg(err.Error())
	}

	return util.ModelsLoaded{Models: availableModels}
}

func (p SettingsPane) loadPresets() ([]util.Settings, error) {
	availablePresets, err := p.settingsService.GetPresetsList()

	if err != nil {
		return availablePresets, err
	}

	return availablePresets, nil
}

func (p *SettingsPane) updateModelsList(models []string) {
	var modelsList []list.Item
	for i, model := range models {
		modelsList = append(modelsList, components.ModelsListItem{
			Id:   "model_list_" + fmt.Sprint(i),
			Text: model,
		})
	}

	w, h := util.CalcModelsListSize(p.terminalWidth, p.terminalHeight)
	p.modelPicker = components.NewModelsList(modelsList, w, h, p.colors)
}

func (p *SettingsPane) updatePresetsList(presets []util.Settings) {
	var presetsList []list.Item
	for i, preset := range presets {
		presetsList = append(presetsList, components.PresetsListItem{
			Id:       "presets_list_" + fmt.Sprint(i),
			PresetId: preset.ID,
			Text:     preset.PresetName,
		})
	}

	w, h := util.CalcModelsListSize(p.terminalWidth, p.terminalHeight)
	p.presetPicker = components.NewPresetsList(presetsList, w, h, p.settings.ID, p.colors, p.settingsService)
}

func (p *SettingsPane) updatePresetName(inputValue string) error {
	newPreset := util.Settings{
		Model:        p.settings.Model,
		MaxTokens:    p.settings.MaxTokens,
		Frequency:    p.settings.Frequency,
		SystemPrompt: p.settings.SystemPrompt,
		TopP:         p.settings.TopP,
		Temperature:  p.settings.Temperature,
		PresetName:   inputValue,
	}
	newId, err := p.settingsService.SavePreset(newPreset)
	if err != nil {
		return err
	}
	newPreset.ID = newId
	p.settings = newPreset
	p.viewMode = defaultView
	p.changeMode = inactive
	return nil
}

func (p *SettingsPane) updateFrequency(inputValue string) error {
	value, err := strconv.ParseFloat(inputValue, floatPrescision)
	if err != nil {
		return err
	}
	newFreq := float32(value)
	p.settings.Frequency = &newFreq
	p.changeMode = inactive
	return nil
}

func (p *SettingsPane) updateMaxTokens(inputValue string) error {
	newTokens, err := strconv.Atoi(inputValue)
	if err != nil {
		return err
	}
	p.settings.MaxTokens = newTokens
	p.changeMode = inactive
	return nil
}

func (p *SettingsPane) updateTemperature(inputValue string) error {
	value, err := strconv.ParseFloat(inputValue, floatPrescision)
	if err != nil {
		return err
	}
	temp := float32(value)
	p.settings.Temperature = &temp
	p.changeMode = inactive
	return nil
}

func (p *SettingsPane) updateTopP(inputValue string) error {
	value, err := strconv.ParseFloat(inputValue, floatPrescision)
	if err != nil {
		return err
	}
	topp := float32(value)
	p.settings.TopP = &topp
	p.changeMode = inactive
	return nil
}
