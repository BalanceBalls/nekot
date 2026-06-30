package panes

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/BalanceBalls/nekot/components"
	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
	"github.com/atotto/clipboard"
	zone "github.com/lrstanley/bubblezone/v2"
)

const ResponseWaitingMsg = "Inference in progress • ctrl+s to stop"
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
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r", "clear prompt"),
	),
	exit: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "exit insert mode or editor mode"),
	),
	paste: key.NewBinding(
		key.WithKeys("ctrl+v"),
		key.WithHelp("ctrl+v", "insert text from clipboard"),
	),
	pasteCode: key.NewBinding(
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "insert code block from clipboard"),
	),
	attach: key.NewBinding(
		key.WithKeys("ctrl+a"),
		key.WithHelp("ctrl+a", "attach an image"),
	),
	enter: key.NewBinding(
		key.WithKeys("enter"),
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
	inputStyles := input.Styles()
	inputStyles.Focused.Prompt = lipgloss.NewStyle().Foreground(colors.ActiveTabBorderColor)
	inputStyles.Blurred.Prompt = inputStyles.Focused.Prompt
	input.SetStyles(inputStyles)
	input.CharLimit = 0
	input.SetWidth(20000)

	textEditor := textarea.New()
	textEditor.Placeholder = PlaceholderMsg
	textEditorStyles := textEditor.Styles()
	textEditorStyles.Focused.Prompt = lipgloss.NewStyle().Foreground(colors.ActiveTabBorderColor)
	textEditorStyles.Focused.CursorLine = textEditorStyles.Focused.CursorLine.Background(lipgloss.NoColor{})
	textEditorStyles.Focused.EndOfBuffer = lipgloss.NewStyle().
		Foreground(colors.ActiveTabBorderColor)
	textEditorStyles.Focused.LineNumber = lipgloss.NewStyle().Foreground(colors.AccentColor)
	textEditor.SetStyles(textEditorStyles)

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
	return nil
}

func (p PromptPane) Update(msg tea.Msg) (PromptPane, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	cmds = append(cmds, p.processTextInputUpdates(msg))
	cmds = append(cmds, p.processFilePickerUpdates(msg))

	p.handlePlaceholder()

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
		p.handleFocusEvent(msg)

	case tea.WindowSizeMsg:
		p.handleWindowSizeMsg(msg)

	case tea.MouseClickMsg:
		if !zone.Get("prompt_pane").InBounds(msg) || !p.isFocused {
			break
		}
		if msg.Button == tea.MouseLeft {
			cmds = append(cmds, p.keyInsert())
		}

	case tea.KeyPressMsg:
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
		return p.textEditor.Focus()
	default:
		return p.input.Focus()
	}
}

func (p *PromptPane) keyClear() tea.Cmd {
	p.attachments = []util.Attachment{}
	switch p.viewMode {
	case util.TextEditMode:
		p.textEditor.Reset()
	default:
		p.input.Reset()
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
		if !p.textEditor.Focused() {
			if p.operation == util.SystemMessageEditing {
				p.textEditor.SetValue("")
			}

			p.operation = util.NoOperaton

			return util.SendViewModeChangedMsg(util.NormalMode)
		}

		p.textEditor.Blur()

	case util.FilePickerMode:
		break

	default:
		if p.input.Focused() {
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
				util.SwitchToPane(util.SettingsPane),
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

func (p *PromptPane) handlePlaceholder() {
	if !p.ready {
		return
	}

	isInsertMode := p.inputMode == util.PromptInsertMode

	switch p.viewMode {
	case util.TextEditMode:
		if isInsertMode {
			p.textEditor.Placeholder = ""
			break
		}
		p.textEditor.Placeholder = PlaceholderMsg

	case util.FilePickerMode:
		break

	default:
		if isInsertMode {
			p.input.Placeholder = ""
			return
		}
		p.input.Placeholder = PlaceholderMsg
	}
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
	if !p.isFocused && p.viewMode == util.FilePickerMode {
		cmds = append(cmds, util.SendViewModeChangedMsg(util.NormalMode))
	}

	return tea.Batch(cmds...)
}

func (p *PromptPane) processTextInputUpdates(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	var cmds []tea.Cmd
	if p.isFocused && p.inputMode == util.PromptInsertMode && p.isSessionIdle {
		switch p.viewMode {
		case util.FilePickerMode:
			break
		case util.TextEditMode:
			p.textEditor, cmd = p.textEditor.Update(msg)
		default:
			// TODO: maybe there is a way to adjust heihgt for long inputs?
			// TODO: move to dimensions?
			if lipgloss.Width(p.input.Value()) > p.input.Width()-4 {
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

func (p *PromptPane) handleFocusEvent(msg util.FocusEvent) {
	p.isFocused = msg.IsFocused

	if p.isFocused {
		p.inputMode = util.PromptNormalMode
		p.inputContainer = p.inputContainer.BorderForeground(p.colors.ActiveTabBorderColor)
		styles := p.input.Styles()
		styles.Focused.Prompt = styles.Focused.Prompt.Foreground(p.colors.ActiveTabBorderColor)
		styles.Blurred.Prompt = styles.Blurred.Prompt.Foreground(p.colors.ActiveTabBorderColor)
		p.input.SetStyles(styles)
		return
	}

	p.inputMode = util.PromptNormalMode
	p.inputContainer = p.inputContainer.BorderForeground(p.colors.NormalTabBorderColor)
	styles := p.input.Styles()
	styles.Focused.Prompt = styles.Focused.Prompt.Foreground(p.colors.NormalTabBorderColor)
	styles.Blurred.Prompt = styles.Blurred.Prompt.Foreground(p.colors.NormalTabBorderColor)
	p.input.SetStyles(styles)
	p.input.Blur()
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
		p.input.SetWidth(w)
	}

	p.inputContainer = p.inputContainer.
		MaxWidth(p.terminalWidth).
		Width(w + p.inputContainer.GetHorizontalBorderSize())
	return nil
}

func (p *PromptPane) handleViewModeChange(msg util.ViewModeChanged) tea.Cmd {
	var cmd tea.Cmd

	prevMode := p.viewMode
	currentInput := p.getCurrentInput()

	p.viewMode = msg.Mode
	p.inputMode = util.PromptNormalMode

	w, h := util.CalcPromptPaneSize(p.terminalWidth, p.terminalHeight, p.viewMode)

	switch p.viewMode {
	case util.TextEditMode:
		cmd = p.openTextEditor(currentInput, p.operation, false)
		p.textEditor.SetHeight(h)
		p.textEditor.SetWidth(w)

	case util.FilePickerMode:
		cmd = p.openFilePicker(prevMode, currentInput)

	default:
		cmd = p.openInputField(prevMode, currentInput)
		p.input.SetWidth(w)
	}

	p.inputContainer = p.inputContainer.
		MaxWidth(p.terminalWidth).
		Width(w + p.inputContainer.GetHorizontalBorderSize())

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
		p.input.SetWidth(w - 2)
		p.textEditor.Blur()
		p.textEditor.Reset()

		currentInput = strings.ReplaceAll(currentInput, "\n\n", " ")
		currentInput = strings.ReplaceAll(currentInput, "\r\n", " ")
		currentInput = strings.ReplaceAll(currentInput, "\n", " ")

		currentInput = strings.TrimSpace(currentInput)

		p.input.SetValue(currentInput)
		return nil
	}

	inputLength := len(p.input.Value())
	p.input.SetCursor(inputLength)
	p.inputMode = util.PromptNormalMode
	return nil
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
		return p.textEditor.Focus()
	}

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
	p.textEditor.SetCursorColumn(0)
}

func (p PromptPane) AllowFocusChange(isMouseEvent bool) bool {
	if p.operation == util.SystemMessageEditing {
		return false
	}

	if isMouseEvent {
		return true
	}

	if p.isFocused && p.inputMode == util.PromptInsertMode {
		return false
	}

	if p.isFocused && p.viewMode == util.FilePickerMode {
		return false
	}

	return true
}

func (p PromptPane) Enable() PromptPane {
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

		return zone.Mark("prompt_pane", lipgloss.JoinVertical(lipgloss.Left,
			p.inputContainer.Render(content),
			infoBlockStyle.Render(infoBlockContent),
		))
	}

	p.input.Placeholder = ResponseWaitingMsg
	return zone.Mark("prompt_pane", p.inputContainer.Render(p.input.View()))
}
