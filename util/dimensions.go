package util

import "math"

// Defaults
const (
	DefaultTerminalWidth  = 120
	DefaultTerminalHeight = 80

	DefaultElementsPadding = 2
)

// Panes
const (
	PromptPaneHeight      = 6
	PromptPanePadding     = 2
	PromptPaneMarginTop   = 0
	StatusBarPaneHeight   = 5
	EditModeUIElementsSum = 4

	ChatPaneMarginRight = 1
	SidePaneLeftPadding = 5

	// A 'counterweight' is a sum of other elements' margins and paggings
	// The counterweight needs to be subtracted when calculating pane sizes
	// in order to properly align elements
	SettingsPaneHeightCounterweight = 3
	SessionsPaneHeightCounterweight = 4 // TODO: info pane hight
	ChatPaneVisualModeCounterweight = 1
)

// UI elements
const (
	ListRightShiftedItemPadding    = -2
	TextSelectorMaxWidthCorrection = 6
	InputContainerDelta            = 5

	ListItemMarginLeft  = 2
	ListItemPaddingLeft = 2

	WidthMinScalingLimit  = 120
	HeightMinScalingLimit = 46

	ListItemTrimThreshold  = 10
	ListItemTrimCharAmount = 14
)

/*
Pane sizes are calculated with proportions:
- Prompt pane:
  - Width: full termial witdh minus paddings
  - Height: a constant for height and a constant for top margin

- Chat pane:
  - Width: takes 2/3 of the terminal width
  - Height: full terminal height minus the prompt pane height

- Settings pane:
  - Width: takes 1/3 of the terminal width, minus paddings
  - Height: takes 1/3 of the chat pane height, minus paddings

- Sessions pane:
  - Width: takes 1/3 of the terminal width, minus paddings
  - Height: takes 2/3 of the chat pane height, minus paddings
*/

func twoThirds(reference int) int {
	return int(math.Round(float64(reference) * (2.0 / 3.0)))
}

func oneThird(reference int) int {
	return int(math.Round(float64(reference) / 3.0))
}

func ensureNonNegative(number int) int {
	if number < 0 {
		return 0
	}
	return number
}

func CalcPromptPaneSize(tw, th int, isTextEditMode bool) (w, h int) {
	if !isTextEditMode {
		return tw - PromptPanePadding, PromptPaneHeight
	}

	paneHeight := oneThird(th)
	return tw - PromptPanePadding, paneHeight
}

func CalcVisualModeViewSize(tw, th int) (w, h int) {
	chatPaneWidth, chatPaneHeight := CalcChatPaneSize(tw, th, NormalMode)

	return chatPaneWidth, chatPaneHeight - ChatPaneVisualModeCounterweight
}

func CalcChatPaneSize(tw, th int, mode ViewMode) (w, h int) {
	isSmallScale := tw < WidthMinScalingLimit

	var (
		paneWidth  int
		paneHeight int
	)

	switch mode {
	case NormalMode:
		paneHeight = th - PromptPaneHeight
		if isSmallScale {
			paneWidth = tw - DefaultElementsPadding
		} else {
			paneWidth = twoThirds(tw)
		}
	case ZenMode:
		paneHeight = th - PromptPaneHeight - 1
		paneWidth = tw - DefaultElementsPadding
	case TextEditMode:
		paneHeight = twoThirds(th) - EditModeUIElementsSum - 1
		paneWidth = tw - DefaultElementsPadding
	}

	return paneWidth, paneHeight
}

func CalcSettingsPaneSize(tw, th int) (w, h int) {
	_, chatPaneHeight := CalcChatPaneSize(tw, th, NormalMode)
	settingsPaneWidth := oneThird(tw) - SidePaneLeftPadding
	settingsPaneHeight := oneThird(chatPaneHeight) - SettingsPaneHeightCounterweight

	settingsPaneWidth = ensureNonNegative(settingsPaneWidth)
	settingsPaneHeight = ensureNonNegative(settingsPaneHeight)

	if tw < WidthMinScalingLimit {
		return 0, settingsPaneHeight
	}
	return settingsPaneWidth, settingsPaneHeight
}

func CalcModelsListSize(tw, th int) (w, h int) {
	settingsPaneWidth, settingsPaneHeight := CalcSettingsPaneSize(tw, th)
	modelsListWidth := settingsPaneWidth - DefaultElementsPadding
	modelsListHeight := settingsPaneHeight + 1

	modelsListWidth = ensureNonNegative(modelsListWidth)
	modelsListHeight = ensureNonNegative(modelsListHeight)

	if tw < WidthMinScalingLimit {
		return 0, modelsListHeight
	}
	return modelsListWidth, modelsListHeight
}

func CalcSessionsPaneSize(tw, th int) (w, h int) {
	_, chatPaneHeight := CalcChatPaneSize(tw, th, NormalMode)
	sessionsPaneWidth := oneThird(tw) - SidePaneLeftPadding
	sessionsPaneHeight := twoThirds(chatPaneHeight) - StatusBarPaneHeight - SessionsPaneHeightCounterweight

	sessionsPaneWidth = ensureNonNegative(sessionsPaneWidth)
	sessionsPaneHeight = ensureNonNegative(sessionsPaneHeight)

	if tw < WidthMinScalingLimit {
		return 0, sessionsPaneHeight
	}
	return sessionsPaneWidth, sessionsPaneHeight
}

func CalcSessionsListSize(tw, th, tipsOffset int) (w, h int) {
	_, chatPaneHeight := CalcChatPaneSize(tw, th, NormalMode)
	sessionsPaneListWidth := oneThird(tw) - SidePaneLeftPadding
	sessionsPaneListHeight := twoThirds(chatPaneHeight) - StatusBarPaneHeight - SessionsPaneHeightCounterweight - tipsOffset

	sessionsPaneListWidth = ensureNonNegative(sessionsPaneListWidth)
	sessionsPaneListHeight = ensureNonNegative(sessionsPaneListHeight)

	if tw < WidthMinScalingLimit {
		return 0, sessionsPaneListHeight
	}
	return sessionsPaneListWidth, sessionsPaneListHeight
}

func CalcMaxSettingItemWidth(containerWidth int) int {
	return containerWidth / 5 * 4
}

func TrimListItem(value string, listWidth int) string {
	threshold := ListItemTrimThreshold
	if listWidth-threshold > 0 {
		trimTo := listWidth - ListItemTrimCharAmount
		if listWidth-threshold < len(value) {
			value = value[0:trimTo] + "..."
		}
	}

	return value
}
