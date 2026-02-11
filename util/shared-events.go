package util

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"
)

type PrompInputMode int

const (
	PromptInsertMode PrompInputMode = iota
	PromptNormalMode
)

type Pane int
type AsyncDependency int
type Notification int

// fake enum to keep tab of the currently focused pane
const (
	SettingsPane Pane = iota
	SessionsPane
	PromptPane
	ChatPane
)

const (
	SettingsPaneModule AsyncDependency = iota
	Orchestrator
)

const (
	CopiedNotification Notification = iota
	CancelledNotification
	SysPromptChangedNotification
	PresetSavedNotification
	SessionSavedNotification
	SessionExportedNotification
)

type ViewMode int

const (
	ZenMode ViewMode = iota
	TextEditMode
	NormalMode
	FilePickerMode
	ContextPickerMode
)

type Operation int

const (
	NoOperaton Operation = iota
	SystemMessageEditing
)

var (
	NormalFocusPanes = []Pane{SettingsPane, SessionsPane, PromptPane, ChatPane}
	ZenFocusPanes    = []Pane{PromptPane, ChatPane}
)

func IsFocusAllowed(mode ViewMode, pane Pane, tw int) bool {
	focusPanes := getFocuesPanes(mode, pane, tw)

	return slices.Contains(focusPanes, pane)
}

func GetNewFocusMode(mode ViewMode, currentFocus Pane, tw int, isBackward bool) Pane {
	focusPanes := getFocuesPanes(mode, currentFocus, tw)

	direction := 1
	if isBackward {
		direction = -1
	}

	for i, v := range focusPanes {
		if v == currentFocus {
			n := len(focusPanes)
			newIndex := (i + direction%n + n) % n
			return focusPanes[newIndex]
		}
	}

	Slog.Debug("current focus not found in mode", "pane", currentFocus)
	return currentFocus
}

func getFocuesPanes(mode ViewMode, pane Pane, tw int) []Pane {
	var focusPanes []Pane

	switch mode {
	case NormalMode:
		focusPanes = NormalFocusPanes
		if tw < WidthMinScalingLimit {
			focusPanes = ZenFocusPanes
		}
	case ZenMode:
		focusPanes = ZenFocusPanes
	case TextEditMode:
		focusPanes = ZenFocusPanes
	default:
		focusPanes = []Pane{pane}
	}

	return focusPanes
}

type ModelsLoaded struct {
	Models []string
}

type ProcessingStateChanged struct {
	State ProcessingState
}

func SendProcessingStateChangedMsg(processingState ProcessingState) tea.Cmd {
	return func() tea.Msg {
		return ProcessingStateChanged{State: processingState}
	}
}

type PromptReady struct {
	Prompt       string
	Attachments  []Attachment
	ContextChips []FileContextChip
}

func SendPromptReadyMsg(prompt string, attachments []Attachment) tea.Cmd {
	return func() tea.Msg {
		return PromptReady{
			Prompt:      prompt,
			Attachments: attachments,
		}
	}
}

func SendPromptReadyWithContextMsg(prompt string, attachments []Attachment, contextChips []FileContextChip) tea.Cmd {
	return func() tea.Msg {
		return PromptReady{
			Prompt:       prompt,
			Attachments:  attachments,
			ContextChips: contextChips,
		}
	}
}

type ContextChipsProcessed struct {
	Prompt         string
	Attachments    []Attachment
	ContextContent  string
	Errors         []string // Errors that occurred during processing
}

type AsyncDependencyReady struct {
	Dependency AsyncDependency
}

func SendAsyncDependencyReadyMsg(dependency AsyncDependency) tea.Cmd {
	return func() tea.Msg {
		return AsyncDependencyReady{Dependency: dependency}
	}
}

type FocusEvent struct {
	IsFocused bool
}

func MakeFocusMsg(v bool) tea.Msg {
	return FocusEvent{IsFocused: v}
}

type ErrorEvent struct {
	Message string
}

func MakeErrorMsg(v string) tea.Cmd {
	Slog.Error(v)
	return func() tea.Msg {
		return ErrorEvent{Message: v}
	}
}

type NotificationMsg struct {
	Notification Notification
}

func SendNotificationMsg(notification Notification) tea.Cmd {
	return func() tea.Msg {
		return NotificationMsg{Notification: notification}
	}
}

type CopyLastMsg struct{}

func SendCopyLastMsg() tea.Msg {
	return CopyLastMsg{}
}

type CopyAllMsgs struct{}

func SendCopyAllMsgs() tea.Msg {
	return CopyAllMsgs{}
}

type ViewModeChanged struct {
	Mode ViewMode
}

func SendViewModeChangedMsg(mode ViewMode) tea.Cmd {
	return func() tea.Msg {
		return ViewModeChanged{Mode: mode}
	}
}

type SwitchToPaneMsg struct {
	Target Pane
}

func SwitchToPane(target Pane) tea.Cmd {
	return func() tea.Msg {
		return SwitchToPaneMsg{Target: target}
	}
}

type OpenTextEditorMsg struct {
	Content   string
	Operation Operation
	IsFocused bool
}

type SystemPromptUpdatedMsg struct {
	SystemPrompt string
}

func UpdateSystemPrompt(prompt string) tea.Cmd {
	return func() tea.Msg {
		return SystemPromptUpdatedMsg{SystemPrompt: prompt}
	}
}

func SwitchToEditor(content string, op Operation, isFocused bool) tea.Cmd {
	openEditorMsg := func() tea.Msg {
		return OpenTextEditorMsg{Content: content, Operation: op, IsFocused: isFocused}
	}

	switchFocus := func() tea.Msg {
		return SwitchToPaneMsg{Target: PromptPane}
	}

	switchMode := func() tea.Msg {
		return ViewModeChanged{Mode: TextEditMode}
	}

	// order matters, messages are queued sequentially
	return tea.Batch(switchFocus, switchMode, openEditorMsg)
}

type AddNewSessionMsg struct {
	IsTemporary bool
}

func AddNewSession(isTemporary bool) tea.Cmd {
	return func() tea.Msg {
		return AddNewSessionMsg{
			IsTemporary: isTemporary,
		}
	}
}
