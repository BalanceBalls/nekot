package panes

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BalanceBalls/nekot/components"
	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const ResponseWaitingMsg = "> Please wait ..."
const InitializingMsg = "Components initializing ..."
const PlaceholderMsg = "Press i to type • ctrl+e expand/collapse editor • ctrl+r clear"

type keyMap struct {
	insert    key.Binding
	clear     key.Binding
	exit      key.Binding
	paste     key.Binding
	pasteCode key.Binding
	attach    key.Binding
	enter     key.Binding
}

var defaultKeyMap = keyMap{
	insert: key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "enter insert mode")),
	clear: key.NewBinding(
		key.WithKeys(tea.KeyCtrlR.String()),
		key.WithHelp("ctrl+r", "clear prompt"),
	),
	exit: key.NewBinding(
		key.WithKeys(tea.KeyEsc.String()),
		key.WithHelp("esc", "exit insert mode or editor mode"),
	),
	paste: key.NewBinding(
		key.WithKeys(tea.KeyCtrlV.String()),
		key.WithHelp("ctrl+v", "insert text from clipboard"),
	),
	pasteCode: key.NewBinding(
		key.WithKeys(tea.KeyCtrlS.String()),
		key.WithHelp("ctrl+s", "insert code block from clipboard"),
	),
	attach: key.NewBinding(
		key.WithKeys(tea.KeyCtrlA.String()),
		key.WithHelp("ctrl+a", "attach an image"),
	),
	enter: key.NewBinding(
		key.WithKeys(tea.KeyEnter.String()),
		key.WithHelp("enter", "send prompt"),
	),
}

var infoBlockStyle = lipgloss.NewStyle()
var infoPrefix = lipgloss.NewStyle().Bold(true)

var infoLabel = lipgloss.NewStyle().
	BorderLeft(true).
	BorderStyle(lipgloss.InnerHalfBlockBorder()).
	Bold(true).
	MarginRight(1).
	PaddingRight(1).
	PaddingLeft(1)

type PromptPane struct {
	input          textinput.Model
	textEditor     textarea.Model
	filePicker     components.FilePicker
	inputContainer lipgloss.Style
	inputMode      util.PrompInputMode
	colors         util.SchemeColors
	keys           keyMap

	pendingInsert  string
	attachments    []util.Attachment
	operation      util.Operation
	viewMode       util.ViewMode
	isSessionIdle  bool
	isFocused      bool
	terminalWidth  int
	terminalHeight int
	ready          bool
	mainCtx        context.Context
}

func NewPromptPane(ctx context.Context) PromptPane {
	config, ok := config.FromContext(ctx)
	if !ok {
		util.Slog.Error("failed to extract config from context")
		panic("No config found in context")
	}

	colors := config.ColorScheme.GetColors()

	input := textinput.New()
	input.Placeholder = InitializingMsg
	input.PromptStyle = lipgloss.NewStyle().Foreground(colors.ActiveTabBorderColor)
	input.CharLimit = 0
	input.Width = 20000

	textEditor := textarea.New()
	textEditor.Placeholder = PlaceholderMsg
	textEditor.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(colors.ActiveTabBorderColor)
	textEditor.FocusedStyle.CursorLine.Background(lipgloss.NoColor{})
	textEditor.FocusedStyle.EndOfBuffer = lipgloss.NewStyle().
		Foreground(colors.ActiveTabBorderColor)
	textEditor.FocusedStyle.LineNumber = lipgloss.NewStyle().Foreground(colors.AccentColor)

	textEditor.EndOfBufferCharacter = rune(' ')
	textEditor.ShowLineNumbers = true
	textEditor.CharLimit = 0
	textEditor.MaxHeight = 0
	textEditor.Blur()

	container := lipgloss.NewStyle().
		AlignVertical(lipgloss.Bottom).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(colors.ActiveTabBorderColor).
		MarginTop(util.PromptPaneMarginTop)

	infoLabel = infoLabel.
		BorderLeftForeground(colors.ActiveTabBorderColor).
		Foreground(colors.NormalTabBorderColor)

	infoPrefix = infoPrefix.
		Foreground(colors.HighlightColor)

	return PromptPane{
		mainCtx:        ctx,
		operation:      util.NoOperaton,
		keys:           defaultKeyMap,
		viewMode:       util.NormalMode,
		colors:         colors,
		input:          input,
		textEditor:     textEditor,
		inputContainer: container,
		inputMode:      util.PromptNormalMode,
		isSessionIdle:  true,
		isFocused:      true,
		terminalWidth:  util.DefaultTerminalWidth,
		terminalHeight: util.DefaultTerminalHeight,
	}
}

func (p PromptPane) Init() tea.Cmd {
	return p.input.Cursor.BlinkCmd()
}

func (p PromptPane) Update(msg tea.Msg) (PromptPane, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	cmds = append(cmds, p.processTextInputUpdates(msg))
	cmds = append(cmds, p.processFilePickerUpdates(msg))

	switch msg := msg.(type) {

	case util.OpenTextEditorMsg:
		cmd = p.openTextEditor(msg.Content, msg.Operation, msg.IsFocused)
		cmds = append(cmds, cmd)

	case util.ViewModeChanged:
		cmd = p.handleViewModeChange(msg)
		cmds = append(cmds, cmd)

	case util.ProcessingStateChanged:
		p.isSessionIdle = !util.IsProcessingActive(msg.State)

	case util.FocusEvent:
		return p, p.handleFocusEvent(msg)

	case tea.WindowSizeMsg:
		p.handleWindowSizeMsg(msg)

	case tea.KeyMsg:
		if !p.ready {
			break
		}

		switch {
		case key.Matches(msg, p.keys.attach):
			cmds = append(cmds, p.keyAttach())

		case key.Matches(msg, p.keys.insert):
			cmds = append(cmds, p.keyInsert())

		case key.Matches(msg, p.keys.clear):
			cmds = append(cmds, p.keyClear())

		case key.Matches(msg, p.keys.exit):
			cmds = append(cmds, p.keyExit())

		case key.Matches(msg, p.keys.enter):
			cmds = append(cmds, p.keyEnter())

		case key.Matches(msg, p.keys.paste):
			cmds = append(cmds, p.keyPaste())

		case key.Matches(msg, p.keys.pasteCode):
			cmds = append(cmds, p.keyPasteCode())
		}
	}

	return p, tea.Batch(cmds...)
}

func (p *PromptPane) keyAttach() tea.Cmd {
	if p.isFocused && p.operation == util.NoOperaton && p.viewMode != util.FilePickerMode {
		return util.SendViewModeChangedMsg(util.FilePickerMode)
	} else {
		return util.SendViewModeChangedMsg(util.NormalMode)
	}
}

func (p *PromptPane) keyInsert() tea.Cmd {
	if !p.isFocused || p.inputMode != util.PromptNormalMode {
		return nil
	}

	p.inputMode = util.PromptInsertMode
	switch p.viewMode {
	case util.TextEditMode:
		p.textEditor.Focus()
		p.textEditor.Placeholder = ""
		return p.textEditor.Cursor.BlinkCmd()
	default:
		p.input.Focus()
		p.input.Placeholder = ""
		return p.input.Cursor.BlinkCmd()
	}
}

func (p *PromptPane) keyClear() tea.Cmd {
	p.attachments = []util.Attachment{}
	switch p.viewMode {
	case util.TextEditMode:
		p.textEditor.Reset()
		if p.inputMode != util.PromptInsertMode {
			p.textEditor.Placeholder = PlaceholderMsg
		}
	default:
		p.input.Reset()
		if p.inputMode != util.PromptInsertMode {
			p.input.Placeholder = PlaceholderMsg
		}
	}

	return nil
}

func (p *PromptPane) keyExit() tea.Cmd {
	if !p.isFocused {
		return nil
	}
	p.inputMode = util.PromptNormalMode

	switch p.viewMode {
	case util.TextEditMode:
		p.textEditor.Placeholder = PlaceholderMsg
		if !p.textEditor.Focused() {
			p.textEditor.Reset()

			p.operation = util.NoOperaton
			return util.SendViewModeChangedMsg(util.NormalMode)
		}

		p.textEditor.Blur()

	case util.FilePickerMode:
		break

	default:
		p.input.Placeholder = PlaceholderMsg
		if !p.input.Focused() {
			p.input.Reset()
		} else {
			p.input.Blur()
		}
	}

	return nil
}

func (p *PromptPane) keyEnter() tea.Cmd {
	if !p.isFocused || !p.isSessionIdle {
		return nil
	}

	if p.viewMode == util.FilePickerMode {
		return nil
	}

	attachments := p.attachments

	switch p.viewMode {
	case util.TextEditMode:
		if strings.TrimSpace(p.textEditor.Value()) == "" {
			break
		}

		if p.textEditor.Focused() {
			break
		}

		promptText := p.textEditor.Value()
		p.textEditor.SetValue("")
		p.textEditor.Blur()

		if p.operation == util.SystemMessageEditing {
			p.operation = util.NoOperaton

			return tea.Batch(
				util.UpdateSystemPrompt(promptText),
				util.SendViewModeChangedMsg(util.NormalMode),
				func() tea.Msg {
					return util.SwitchToPaneMsg{Target: util.SettingsPane}
				},
			)
		}

		p.attachments = []util.Attachment{}
		return tea.Batch(
			util.SendPromptReadyMsg(promptText, attachments),
			util.SendViewModeChangedMsg(util.NormalMode))

	default:
		if strings.TrimSpace(p.input.Value()) == "" {
			break
		}

		promptText := p.input.Value()
		p.input.SetValue("")
		p.input.Blur()

		p.inputMode = util.PromptNormalMode

		p.attachments = []util.Attachment{}
		return util.SendPromptReadyMsg(promptText, attachments)
	}

	return nil
}

func (p *PromptPane) keyPaste() tea.Cmd {
	var cmd tea.Cmd
	if p.isFocused {
		buffer, _ := clipboard.ReadAll()
		content := strings.TrimSpace(buffer)

		if p.viewMode != util.TextEditMode && strings.Contains(content, "\n") {
			cmd = util.SwitchToEditor(content, util.NoOperaton, true)
			p.pendingInsert = ""
		}

		clipboard.WriteAll(content)
	}
	return cmd
}

func (p *PromptPane) keyPasteCode() tea.Cmd {
	if p.isFocused && p.viewMode == util.TextEditMode && p.textEditor.Focused() {
		p.insertBufferContentAsCodeBlock()
	}
	return nil
}

func (p *PromptPane) processFilePickerUpdates(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	if p.isFocused && p.viewMode == util.FilePickerMode {
		if p.filePicker.SelectedFile != "" {
			attachmentPath := p.filePicker.SelectedFile
			attachmentPath = filepath.Clean(attachmentPath)
			attachmentPath = strings.ReplaceAll(attachmentPath, `\ `, " ")
			p.attachments = append(p.attachments, util.Attachment{
				Type: "img",
				Path: attachmentPath,
			})

			cmds = append(cmds, util.SendViewModeChangedMsg(p.filePicker.PrevView))
			p.filePicker.SelectedFile = ""
		} else {
			p.filePicker, cmd = p.filePicker.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return tea.Batch(cmds...)
}

func (p *PromptPane) processTextInputUpdates(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	var cmds []tea.Cmd
	if p.isFocused && p.inputMode == util.PromptInsertMode && p.isSessionIdle {
		switch p.viewMode {
		case util.TextEditMode:
			p.textEditor, cmd = p.textEditor.Update(msg)
		default:
			// TODO: maybe there is a way to adjust heihgt for long inputs?
			// TODO: move to dimensions?
			if lipgloss.Width(p.input.Value()) > p.input.Width-4 {
				p.input, cmd = p.input.Update(msg)
				cmds = append(cmds, util.SwitchToEditor(p.input.Value(), util.NoOperaton, true))
			} else {
				p.input, cmd = p.input.Update(msg)
			}
		}
		cmds = append(cmds, cmd)

		if p.operation == util.NoOperaton {
			p.parseAttachments()
		}
	}

	return tea.Batch(cmds...)
}

func (p *PromptPane) handleFocusEvent(msg util.FocusEvent) tea.Cmd {
	p.isFocused = msg.IsFocused

	if p.isFocused {
		p.inputMode = util.PromptNormalMode
		p.inputContainer = p.inputContainer.BorderForeground(p.colors.ActiveTabBorderColor)
		p.input.PromptStyle = p.input.PromptStyle.Foreground(p.colors.ActiveTabBorderColor)
	} else {
		p.inputMode = util.PromptNormalMode
		p.inputContainer = p.inputContainer.BorderForeground(p.colors.NormalTabBorderColor)
		p.input.PromptStyle = p.input.PromptStyle.Foreground(p.colors.NormalTabBorderColor)
		p.input.Blur()
	}

	return nil
}

func (p *PromptPane) handleWindowSizeMsg(msg tea.WindowSizeMsg) tea.Cmd {
	p.terminalWidth = msg.Width
	p.terminalHeight = msg.Height

	w, h := util.CalcPromptPaneSize(p.terminalWidth, p.terminalHeight, p.viewMode)
	switch p.viewMode {

	case util.FilePickerMode:
		p.filePicker.SetSize(w, h)
	case util.TextEditMode:
		p.textEditor.SetHeight(h)
		p.textEditor.SetWidth(w)
	default:
		p.input.Width = w
	}

	p.inputContainer = p.inputContainer.MaxWidth(p.terminalWidth).Width(w)
	return nil
}

func (p *PromptPane) handleViewModeChange(msg util.ViewModeChanged) tea.Cmd {
	var cmd tea.Cmd

	prevMode := p.viewMode
	currentInput := p.getCurrentInput()

	p.viewMode = msg.Mode
	p.inputMode = util.PromptNormalMode

	w, _ := util.CalcPromptPaneSize(p.terminalWidth, p.terminalHeight, p.viewMode)

	switch p.viewMode {
	case util.TextEditMode:
		cmd = p.openTextEditor(currentInput, util.NoOperaton, false)

	case util.FilePickerMode:
		cmd = p.openFilePicker(prevMode, currentInput)

	default:
		cmd = p.openInputField(prevMode, currentInput)
	}

	p.inputContainer = p.inputContainer.MaxWidth(p.terminalWidth).Width(w)

	return cmd
}

func (p *PromptPane) getCurrentInput() string {

	if p.textEditor.Value() != "" {
		return p.textEditor.Value()
	}

	if p.input.Value() != "" {
		return p.input.Value()
	}

	return ""
}

func (p *PromptPane) openInputField(previousViewMode util.ViewMode, currentInput string) tea.Cmd {
	w, _ := util.CalcPromptPaneSize(p.terminalWidth, p.terminalHeight, p.viewMode)
	if previousViewMode == util.TextEditMode {
		p.input.Width = w
		p.textEditor.Blur()
		p.textEditor.Reset()

		p.input.SetValue(currentInput)
		return nil
	}

	inputLength := len(p.input.Value())
	p.input.Focus()
	p.input.SetCursor(inputLength)
	p.input.Placeholder = ""
	p.inputMode = util.PromptInsertMode
	return p.input.Cursor.BlinkCmd()
}

func (p *PromptPane) openFilePicker(previousViewMode util.ViewMode, currentInput string) tea.Cmd {
	w, h := util.CalcPromptPaneSize(p.terminalWidth, p.terminalHeight, p.viewMode)
	p.filePicker = components.NewFilePicker(previousViewMode, currentInput, p.colors)
	p.filePicker.SetSize(w, h)
	return p.filePicker.Init()
}

func (p *PromptPane) openTextEditor(content string, op util.Operation, isFocused bool) tea.Cmd {
	p.operation = op

	p.input.Blur()
	p.input.Reset()

	if p.pendingInsert != "" {
		content += "\n" + p.pendingInsert
		p.pendingInsert = ""
	}

	p.textEditor.SetValue(content)

	if isFocused {
		p.inputMode = util.PromptInsertMode
		p.textEditor.Focus()
		return p.textEditor.Cursor.BlinkCmd()
	}
	p.textEditor.Placeholder = PlaceholderMsg

	return nil
}

func (p *PromptPane) parseAttachments() []util.Attachment {
	imgTagRegex := regexp.MustCompile(`\[img=[^\]]+\]`)
	fileTagRegex := regexp.MustCompile(`\[file=[^\]]+\]`)

	content := ""
	if p.viewMode == util.TextEditMode {
		content = p.textEditor.Value()
	} else {
		content = p.input.Value()
	}

	re := regexp.MustCompile(`\[(img|file)=([^\]]+)\]`)
	matches := re.FindAllStringSubmatch(content, -1)

	attachments := p.attachments
	for _, match := range matches {
		attachmentType := match[1]
		attachmentPath := match[2]

		attachmentPath = filepath.Clean(attachmentPath)
		attachmentPath = strings.ReplaceAll(attachmentPath, `\ `, " ")
		attachments = append(attachments, util.Attachment{
			Type: attachmentType,
			Path: attachmentPath,
		})

		switch attachmentType {
		case "img":
			content = imgTagRegex.ReplaceAllString(content, "")
		case "file":
			content = fileTagRegex.ReplaceAllString(content, "")
		}
	}

	if len(attachments) == 0 {
		return attachments
	}

	p.attachments = attachments

	if p.viewMode == util.TextEditMode {
		p.textEditor.SetValue(content)
	} else {
		p.input.SetValue(content)
	}

	return attachments
}

func (p *PromptPane) insertBufferContentAsCodeBlock() {
	buffer, _ := clipboard.ReadAll()
	currentInput := p.textEditor.Value()

	lines := strings.Split(currentInput, "\n")
	lang := lines[len(lines)-1]
	currentInput = strings.Join(lines[0:len(lines)-1], "\n")
	bufferContent := strings.Trim(string(buffer), "\n")
	codeBlock := "\n```" + lang + "\n" + bufferContent + "\n```\n"

	p.textEditor.SetValue(currentInput + codeBlock)
	p.textEditor.SetCursor(0)
}

func (p PromptPane) AllowFocusChange() bool {
	if p.isFocused && p.inputMode == util.PromptInsertMode {
		return false
	}

	if p.isFocused && p.viewMode == util.FilePickerMode {
		return false
	}

	if p.operation == util.SystemMessageEditing {
		return false
	}

	return true
}

func (p PromptPane) Enable() PromptPane {
	p.input.Placeholder = PlaceholderMsg
	p.ready = true
	return p
}

func (p PromptPane) View() string {
	if p.isSessionIdle {
		content := ""

		switch p.viewMode {
		case util.FilePickerMode:
			content = p.filePicker.View()
		case util.TextEditMode:
			content = p.textEditor.View()
		default:
			content = p.input.View()
		}

		infoBlockContent := infoLabel.Render("Use ctrl+a to attach an image")

		if len(p.attachments) != 0 {
			imageBlocks := []string{infoPrefix.Render("Attachments: ")}
			for _, image := range p.attachments {
				fileName := filepath.Base(image.Path)
				imageBlocks = append(imageBlocks, infoLabel.Render(fileName))
			}

			infoBlockContent = lipgloss.JoinHorizontal(lipgloss.Left, imageBlocks...)
		}

		if p.operation == util.SystemMessageEditing {
			infoBlockContent = infoLabel.Render("Editing system prompt")
		}

		return lipgloss.JoinVertical(lipgloss.Left,
			p.inputContainer.Render(content),
			infoBlockStyle.Render(infoBlockContent),
		)
	}

	return p.inputContainer.Render(ResponseWaitingMsg)
}
