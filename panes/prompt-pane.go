package panes

import (
	"context"
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tearingItUp786/nekot/config"
	"github.com/tearingItUp786/nekot/util"
)

const ResponseWaitingMsg = "> Please wait ..."
const InitializingMsg = "Components initializing ..."
const PlaceholderMsg = "Press i to type. Use ctrl+e to expand/collapse editor"

type keyMap struct {
	insert    key.Binding
	clear     key.Binding
	exit      key.Binding
	paste     key.Binding
	pasteCode key.Binding
	enter     key.Binding
}

var defaultKeyMap = keyMap{
	insert:    key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "enter insert mode")),
	clear:     key.NewBinding(key.WithKeys(tea.KeyCtrlR.String()), key.WithHelp("ctrl+r", "clear prompt")),
	exit:      key.NewBinding(key.WithKeys(tea.KeyEsc.String()), key.WithHelp("esc", "exit insert mode or editor mode")),
	paste:     key.NewBinding(key.WithKeys(tea.KeyCtrlV.String()), key.WithHelp("ctrl+v", "insert text from clipboard")),
	pasteCode: key.NewBinding(key.WithKeys(tea.KeyCtrlS.String()), key.WithHelp("ctrl+s", "insert code block from clipboard")),
	enter:     key.NewBinding(key.WithKeys(tea.KeyEnter.String()), key.WithHelp("enter", "send prompt")),
}

type PromptPane struct {
	input      textinput.Model
	textEditor textarea.Model
	container  lipgloss.Style
	inputMode  util.PrompInputMode
	colors     util.SchemeColors
	keys       keyMap

	pendingInsert  string
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
		fmt.Println("No config found")
		panic("No config found in context")
	}

	colors := config.ColorScheme.GetColors()

	input := textinput.New()
	input.Placeholder = InitializingMsg
	input.PromptStyle = lipgloss.NewStyle().Foreground(colors.ActiveTabBorderColor)
	input.CharLimit = 0
	input.Width = 0

	textEditor := textarea.New()
	textEditor.Placeholder = PlaceholderMsg
	textEditor.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(colors.ActiveTabBorderColor)
	textEditor.FocusedStyle.CursorLine.Background(lipgloss.NoColor{})
	textEditor.FocusedStyle.EndOfBuffer = lipgloss.NewStyle().Foreground(colors.ActiveTabBorderColor)
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

	return PromptPane{
		mainCtx:        ctx,
		operation:      util.NoOperaton,
		keys:           defaultKeyMap,
		viewMode:       util.NormalMode,
		colors:         colors,
		input:          input,
		textEditor:     textEditor,
		container:      container,
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

	if p.isFocused && p.inputMode == util.PromptInsertMode && p.isSessionIdle {
		switch p.viewMode {
		case util.TextEditMode:
			p.textEditor, cmd = p.textEditor.Update(msg)
		default:
			p.input, cmd = p.input.Update(msg)
		}
		cmds = append(cmds, cmd)
	}

	switch msg := msg.(type) {

	case util.OpenTextEditorMsg:
		p.textEditor.SetValue(msg.Content)
		p.operation = msg.Operation

	case util.ViewModeChanged:
		p.viewMode = msg.Mode
		p.inputMode = util.PromptNormalMode

		isTextEditMode := p.viewMode == util.TextEditMode
		w, h := util.CalcPromptPaneSize(p.terminalWidth, p.terminalHeight, isTextEditMode)
		if isTextEditMode {
			p.textEditor.SetHeight(h)
			p.textEditor.SetWidth(w)

			currentInput := p.input.Value()
			p.input.Blur()
			p.input.Reset()

			if p.pendingInsert != "" {
				currentInput += "\n" + p.pendingInsert
				p.pendingInsert = ""
			}

			p.textEditor.SetValue(currentInput)
		} else {
			p.input.Width = w
			currentInput := p.textEditor.Value()
			p.textEditor.Blur()
			p.textEditor.Reset()

			p.input.SetValue(currentInput)
		}
		p.container = p.container.MaxWidth(p.terminalWidth).Width(w)

	case util.ProcessingStateChanged:
		p.isSessionIdle = msg.IsProcessing == false

	case util.FocusEvent:
		p.isFocused = msg.IsFocused

		if p.isFocused {
			p.inputMode = util.PromptNormalMode
			p.container = p.container.BorderForeground(p.colors.ActiveTabBorderColor)
			p.input.PromptStyle = p.input.PromptStyle.Foreground(p.colors.ActiveTabBorderColor)
		} else {
			p.inputMode = util.PromptNormalMode
			p.container = p.container.BorderForeground(p.colors.NormalTabBorderColor)
			p.input.PromptStyle = p.input.PromptStyle.Foreground(p.colors.NormalTabBorderColor)
			p.input.Blur()
		}
		return p, nil

	case tea.WindowSizeMsg:
		p.terminalWidth = msg.Width
		p.terminalHeight = msg.Height

		isTextEditMode := p.viewMode == util.TextEditMode
		w, h := util.CalcPromptPaneSize(p.terminalWidth, p.terminalHeight, isTextEditMode)
		if isTextEditMode {
			p.textEditor.SetHeight(h)
			p.textEditor.SetWidth(w)
		} else {
			p.input.Width = w
		}
		p.container = p.container.MaxWidth(p.terminalWidth).Width(w)

	case tea.KeyMsg:
		if !p.ready {
			break
		}

		switch {

		case key.Matches(msg, p.keys.insert):
			if p.isFocused && p.inputMode == util.PromptNormalMode {
				p.inputMode = util.PromptInsertMode
				switch p.viewMode {
				case util.TextEditMode:
					p.textEditor.Focus()
					cmds = append(cmds, p.textEditor.Cursor.BlinkCmd())
				default:
					p.input.Focus()
					cmds = append(cmds, p.input.Cursor.BlinkCmd())
				}
			}

		case key.Matches(msg, p.keys.clear):
			switch p.viewMode {
			case util.TextEditMode:
				p.textEditor.Reset()
			default:
				p.input.Reset()
			}

		case key.Matches(msg, p.keys.exit):
			if p.isFocused {
				p.inputMode = util.PromptNormalMode

				switch p.viewMode {
				case util.TextEditMode:
					if !p.textEditor.Focused() {
						p.textEditor.Reset()
						p.operation = util.NoOperaton
						cmds = append(cmds, util.SendViewModeChangedMsg(util.NormalMode))
					} else {
						p.textEditor.Blur()
					}
				default:
					if !p.input.Focused() {
						p.input.Reset()
					} else {
						p.input.Blur()
					}
				}
			}

		case key.Matches(msg, p.keys.enter):
			if p.isFocused && p.isSessionIdle {

				switch p.viewMode {
				case util.TextEditMode:
					if !p.textEditor.Focused() {
						promptText := p.textEditor.Value()
						p.textEditor.SetValue("")
						p.textEditor.Blur()

						if p.operation == util.SystemMessageEditing {
							p.operation = util.NoOperaton

							return p, tea.Batch(
								util.UpdateSystemPrompt(promptText),
								util.SendViewModeChangedMsg(util.NormalMode),
								func() tea.Msg {
									return util.SwitchToPaneMsg{Target: util.SettingsPane}
								},
							)
						}

						return p, tea.Batch(
							util.SendPromptReadyMsg(promptText),
							util.SendViewModeChangedMsg(util.NormalMode))
					}
				default:
					promptText := p.input.Value()
					p.input.SetValue("")
					p.input.Blur()

					p.inputMode = util.PromptNormalMode

					return p, util.SendPromptReadyMsg(promptText)
				}
			}

		case key.Matches(msg, p.keys.paste):
			if p.isFocused {
				buffer, _ := clipboard.ReadAll()
				content := strings.TrimSpace(buffer)

				if p.viewMode != util.TextEditMode && strings.Contains(content, "\n") {
					cmds = append(cmds, util.SendViewModeChangedMsg(util.TextEditMode))
					p.pendingInsert = content
				}

				clipboard.WriteAll(content)
			}

		case key.Matches(msg, p.keys.pasteCode):
			if p.isFocused && p.viewMode == util.TextEditMode && p.textEditor.Focused() {
				p.insertBufferContentAsCodeBlock()
			}
		}
	}

	return p, tea.Batch(cmds...)
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
		content := p.input.View()
		if p.viewMode == util.TextEditMode {
			content = p.textEditor.View()
		}
		return p.container.Render(content)
	}

	return p.container.Render(ResponseWaitingMsg)
}
