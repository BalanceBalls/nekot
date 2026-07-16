package panes

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/BalanceBalls/nekot/components"
	"github.com/BalanceBalls/nekot/settings"
	"github.com/BalanceBalls/nekot/util"
	zone "github.com/lrstanley/bubblezone/v2"
)

const floatPrescision = 32

func (p *SettingsPane) handlePresetModeMouse(msg tea.MouseReleaseMsg) tea.Cmd {
	if zone.Get("set_p_settings_tab").InBounds(msg) && p.viewMode == presetsView {
		p.viewMode = defaultView
	}

	if msg.Button == tea.MouseLeft && p.viewMode == presetsView {
		for _, listItem := range p.presetPicker.VisibleItems() {
			v, _ := listItem.(components.PresetsListItem)
			if zone.Get(v.Id).InBounds(msg) {
				return p.selectPreset(v.PresetId)
			}
		}
	}

	return nil
}

func (p *SettingsPane) handlePresetMode(msg tea.KeyPressMsg) tea.Cmd {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	if p.presetPicker.IsFiltering() {
		return tea.Batch(cmds...)
	}

	switch {
	case msg.String() == "]" || msg.String() == "right":
		p.switchToMCP()
		return nil

	case key.Matches(msg, p.keyMap.goBack):
		if msg.String() == "left" && !p.presetPicker.IsFirstPage() {
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

func (p *SettingsPane) switchToMCP() {
	p.viewMode = mcpView
	p.changeMode = inactive
	p.mcpInspect = false
	p.mcpError = ""
	p.clampMCPSelection()
}

func (p *SettingsPane) clampMCPSelection() {
	if p.mcpManager == nil {
		p.mcpSelected = 0
		return
	}
	statuses := p.mcpManager.Statuses()
	if len(statuses) == 0 {
		p.mcpSelected = 0
		return
	}
	if p.mcpSelected >= len(statuses) {
		p.mcpSelected = len(statuses) - 1
	}
	if p.mcpSelected < 0 {
		p.mcpSelected = 0
	}
}

func (p *SettingsPane) handleMCPMode(msg tea.KeyPressMsg) tea.Cmd {
	if p.mcpManager == nil {
		if msg.String() == "esc" || msg.String() == "[" || msg.String() == "left" {
			p.viewMode = defaultView
		}
		return nil
	}
	statuses := p.mcpManager.Statuses()
	p.clampMCPSelection()
	if p.mcpInspect {
		if msg.String() == "esc" || msg.String() == "enter" {
			p.mcpInspect = false
		}
		return nil
	}

	switch msg.String() {
	case "esc":
		p.viewMode = defaultView
		return nil
	case "[", "left":
		return p.switchToPresets()
	case "]", "right":
		p.viewMode = defaultView
		return nil
	case "up", "k":
		if p.mcpSelected > 0 {
			p.mcpSelected--
		}
		return nil
	case "down", "j":
		if p.mcpSelected+1 < len(statuses) {
			p.mcpSelected++
		}
		return nil
	case "enter":
		if len(statuses) > 0 {
			p.mcpInspect = true
		}
		return nil
	case "ctrl+r":
		return runMCPAction(p.mcpManager.Reload)
	}

	if len(statuses) == 0 {
		return nil
	}
	selected := statuses[p.mcpSelected]
	switch msg.String() {
	case " ":
		return runMCPAction(func() error {
			return p.mcpManager.SetEnabled(selected.ID, !selected.Enabled)
		})
	case "r":
		return runMCPAction(func() error { return p.mcpManager.Reconnect(selected.ID) })
	case "a":
		p.mcpInspect = true
		return runMCPAction(func() error { return p.mcpManager.Authorize(selected.ID) })
	case "x":
		return runMCPAction(func() error { return p.mcpManager.Logout(selected.ID) })
	}
	return nil
}

func (p SettingsPane) renderMCPView(width, height int) string {
	if p.mcpManager == nil {
		return lipgloss.NewStyle().Width(width).Height(height).Render("MCP manager unavailable")
	}
	statuses := p.mcpManager.Statuses()
	if len(statuses) == 0 {
		return lipgloss.NewStyle().Width(width).Height(height).Render(
			"No MCP servers configured\n\nctrl+r reload config",
		)
	}
	selectedIndex := p.mcpSelected
	if selectedIndex >= len(statuses) {
		selectedIndex = len(statuses) - 1
	}
	selected := statuses[selectedIndex]
	if p.mcpInspect {
		lines := []string{
			fmt.Sprintf("%s (%s)", selected.ID, selected.State),
			fmt.Sprintf("Transport: %s", selected.Transport),
			fmt.Sprintf("Tools: %d exposed / %d discovered", selected.ExposedTools, selected.DiscoveredTools),
		}
		if selected.LastError != "" {
			lines = append(lines, "Error: "+selected.LastError)
		}
		if selected.AuthorizationURL != "" {
			lines = append(lines, "Authorization URL: "+selected.AuthorizationURL)
		}
		lines = append(lines, "", "Exposed tools")
		tools := p.mcpManager.ToolsForServer(selected.ID)
		if len(tools) == 0 {
			lines = append(lines, "  none")
		}
		for _, tool := range tools {
			approval := "approval"
			if !tool.RequiresApproval {
				approval = "trusted"
			}
			lines = append(lines, fmt.Sprintf("  %s  [%s]", tool.OriginalName, approval))
		}
		lines = append(lines, "", "enter / esc  Back")
		return lipgloss.NewStyle().Width(width).Height(height).Render(strings.Join(lines, "\n"))
	}

	lines := make([]string, 0, len(statuses)+8)
	for index, status := range statuses {
		cursor := " "
		if index == selectedIndex {
			cursor = ">"
		}
		toggle := "[ ]"
		if status.Enabled {
			toggle = "[x]"
		}
		line := fmt.Sprintf(
			"%s %s %s  %s  %d/%d tools",
			cursor,
			toggle,
			status.ID,
			status.State,
			status.ExposedTools,
			status.DiscoveredTools,
		)
		lines = append(lines, util.TrimListItem(line, width))
	}
	lines = append(lines, "")
	if selected.LastError != "" {
		lines = append(lines, util.TrimListItem("Error: "+selected.LastError, width))
	}
	if p.mcpError != "" {
		lines = append(lines, util.TrimListItem("Action: "+p.mcpError, width))
	}
	lines = append(lines,
		"space toggle   enter inspect   r reconnect",
		"a authorize    x logout        ctrl+r reload",
	)
	return lipgloss.NewStyle().Width(width).Height(height).Render(strings.Join(lines, "\n"))
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

func (p *SettingsPane) handleModelModeMouse(msg tea.MouseReleaseMsg) tea.Cmd {
	if zone.Get("set_p_presets_tab").InBounds(msg) && p.viewMode == modelsView {
		return p.switchToPresets()
	}

	if msg.Button == tea.MouseLeft && p.viewMode == modelsView {
		for _, listItem := range p.modelPicker.VisibleItems() {
			v, _ := listItem.(components.ModelsListItem)
			if zone.Get(v.Id).InBounds(msg) {
				return p.selectModel(string(v.Text))
			}
		}
	}

	return nil
}

func (p *SettingsPane) handleModelMode(msg tea.KeyPressMsg) tea.Cmd {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	if p.modelPicker.IsFiltering() {
		return tea.Batch(cmds...)
	}

	switch msg.Code {
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

func (p *SettingsPane) handleViewModeMouse(msg tea.MouseReleaseMsg) tea.Cmd {
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

func (p *SettingsPane) handleViewMode(msg tea.KeyPressMsg) tea.Cmd {
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
	styles := ti.Styles()
	styles.Focused.Prompt = lipgloss.NewStyle().PaddingLeft(util.DefaultElementsPadding)
	styles.Blurred.Prompt = styles.Focused.Prompt
	ti.SetStyles(styles)
	p.textInput = ti
	p.textInput.Placeholder = title
	p.textInput.SetWidth(
		p.container.GetWidth() -
			p.container.GetHorizontalBorderSize() -
			util.InputContainerDelta,
	)
	p.changeMode = mode
	p.textInput.Validate = validator
	return p.textInput.Focus()
}

func (p *SettingsPane) handleSettingsUpdate(msg tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg.Code {

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
