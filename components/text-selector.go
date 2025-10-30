package components

import (
	"strconv"
	"strings"

	"github.com/BalanceBalls/nekot/util"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	HighlightPrefix = " +"
	CursorSymbol    = "~"
)

type keyMap struct {
	visualLineMode key.Binding
	up             key.Binding
	down           key.Binding
	pageUp         key.Binding
	pageDown       key.Binding
	copy           key.Binding
	bottom         key.Binding
	top            key.Binding
}

var defaultKeyMap = keyMap{
	visualLineMode: key.NewBinding(
		key.WithKeys("V", "v", tea.KeySpace.String()),
		key.WithHelp("V, v, <space>", "visual line mode"),
	),
	up:   key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "move up")),
	down: key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "move down")),
	pageUp: key.NewBinding(
		key.WithKeys("ctrl+u", "u"),
		key.WithHelp("ctrl+u", "move up a page"),
	),
	pageDown: key.NewBinding(
		key.WithKeys("ctrl+d", "d"),
		key.WithHelp("ctrl+d", "move down a page"),
	),
	copy:   key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy selection")),
	bottom: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "go to bottom")),
	top:    key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "go to top")),
}

type cursor struct {
	line int
}

type selection struct {
	Active bool
	anchor cursor
}

type TextSelector struct {
	Selection    selection
	lines        []string
	cursor       cursor
	scrollOffset int
	paneHeight   int
	paneWidth    int
	keys         keyMap
	renderedText string
	colors       util.SchemeColors

	numberLines int
}

func (s TextSelector) Init() tea.Cmd {
	return nil
}

func (s TextSelector) Update(msg tea.Msg) (TextSelector, tea.Cmd) {
	var (
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		paneWidth, paneHeight := util.CalcVisualModeViewSize(msg.Width, msg.Height)
		s.paneHeight = paneHeight
		s.paneWidth = paneWidth
		s.AdjustScroll()
	case tea.KeyMsg:

		keypress := msg.String()
		if number, err := strconv.Atoi(keypress); err == nil {
			return s.handleLineJumps(keypress, number), nil
		}

		switch {

		case key.Matches(msg, s.keys.pageUp):
			upLines := s.paneHeight / 2
			s.cursor.line = max(s.cursor.line-upLines, s.firstLinePosition())
			s.AdjustScroll()

		case key.Matches(msg, s.keys.up):
			s = s.handleKeyUp()

		case key.Matches(msg, s.keys.down):
			s = s.handleKeyDown()

		case key.Matches(msg, s.keys.pageDown):
			downLines := s.paneHeight / 2
			s.cursor.line = min(s.cursor.line+downLines, s.lastLinePosition())
			s.AdjustScroll()

		case key.Matches(msg, s.keys.bottom):
			s.cursor.line = s.lastLinePosition()
			s.AdjustScroll()

		case key.Matches(msg, s.keys.top):
			s.cursor.line = s.firstLinePosition()
			s.AdjustScroll()

		case key.Matches(msg, s.keys.visualLineMode):
			if s.Selection.Active {
				s.Selection.Active = false
			} else {
				s.Selection.Active = true
				s.Selection.anchor = s.cursor
			}

		case key.Matches(msg, s.keys.copy):
			if s.Selection.Active {
				s.copySelectedLinesToClipboard()
				s.Selection.Active = false
				cmds = append(cmds, util.SendNotificationMsg(util.CopiedNotification))
			}
		}
	}

	return s, tea.Batch(cmds...)
}

func (s *TextSelector) AdjustScroll() {
	if s.cursor.line < s.scrollOffset {
		s.scrollOffset = s.cursor.line - 1
	} else if s.cursor.line >= s.scrollOffset+s.paneHeight {
		s.scrollOffset = s.cursor.line - s.paneHeight + 1
	}
}

func (s TextSelector) View() string {
	return s.renderLines()
}

func (s TextSelector) renderLines() string {
	highlightStyle := lipgloss.NewStyle().
		Foreground(s.colors.DefaultTextColor).
		Background(s.colors.HighlightColor)

	cursorStyle := lipgloss.NewStyle().
		Foreground(s.colors.DefaultTextColor).
		Background(s.colors.AccentColor)

	// Pre-compute selection range if active
	var startLine, endLine int
	if s.Selection.Active {
		startLine = s.Selection.anchor.line
		endLine = s.cursor.line
		if startLine > endLine {
			startLine, endLine = endLine, startLine
		}
	}

	// Use string builder for better performance
	// Might need to look into this for other functions as well
	var sb strings.Builder

	// Calculate visible range
	start := s.scrollOffset
	end := min(start+s.paneHeight, len(s.lines))

	// Determine the average line length so we can pre-allocate memory for the string builder
	var totalLen int
	for i := start; i < end; i++ {
		totalLen += len(s.lines[i])
	}
	avgLineLen := totalLen/(end-start) + 2 // +2 for newline and prefix/cursor
	sb.Grow((end - start) * avgLineLen)

	// Render each line
	for i := start; i < end; i++ {
		line := s.lines[i]

		switch {
		case s.Selection.Active && i >= startLine && i <= endLine:
			sb.WriteString(highlightStyle.Render(HighlightPrefix))
			sb.WriteString(line)
			sb.WriteString("\n")

		case !s.Selection.Active && i == s.cursor.line:
			sb.WriteString(cursorStyle.Render(CursorSymbol))
			sb.WriteString(line)
			sb.WriteString("\n")

		default:
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func (s TextSelector) lastLinePosition() int {
	return len(s.lines) - 1
}

func (s TextSelector) firstLinePosition() int {
	return 1
}

func (s TextSelector) handleKeyUp() TextSelector {
	firstLinePosition := s.firstLinePosition()
	if s.cursor.line > firstLinePosition {
		projectedPosition := s.cursor.line - s.numberLines
		projectedPosition = max(projectedPosition, firstLinePosition)

		if s.numberLines > 0 {
			s.cursor.line = projectedPosition
			s.numberLines = 0
		} else {
			s.cursor.line--
		}
	}
	s.AdjustScroll()
	return s
}

func (s TextSelector) handleKeyDown() TextSelector {
	lastLinePosition := s.lastLinePosition()
	if s.cursor.line < lastLinePosition {
		projectedPosition := s.cursor.line + s.numberLines
		projectedPosition = min(projectedPosition, lastLinePosition)

		if s.numberLines > 0 {
			s.cursor.line = projectedPosition
			s.numberLines = 0
		} else {
			s.cursor.line++
		}
	}
	s.AdjustScroll()
	return s
}

func (s TextSelector) handleLineJumps(keypress string, parsedNumber int) TextSelector {
	if s.numberLines > 0 {
		prevNumber := strconv.Itoa(s.numberLines)
		combinedNumber, err := strconv.Atoi(prevNumber + keypress)
		if err == nil {
			s.numberLines = combinedNumber
		}
	} else {
		s.numberLines = parsedNumber
	}
	return s
}

func (s TextSelector) copySelectedLinesToClipboard() {
	if !s.Selection.Active {
		return
	}

	var selectedLines []string
	startLine := s.Selection.anchor.line
	endLine := s.cursor.line
	if startLine > endLine {
		startLine, endLine = endLine, startLine
	}
	for i := startLine; i <= endLine; i++ {
		filteredLine := filterLine(s.lines[i])
		selectedLines = append(selectedLines, filteredLine)
	}

	ansiFreeText := util.StripAnsiCodes(strings.Join(selectedLines, "\n"))

	ansiFreeLines := strings.Split(ansiFreeText, "\n")
	var linesToCopy = make([]string, len(ansiFreeLines))

	for i, line := range ansiFreeLines {
		linesToCopy[i] = strings.TrimRight(line, " ")
	}

	clipboard.WriteAll(strings.Join(linesToCopy, "\n"))
}

func filterLine(line string) string {
	line = strings.ReplaceAll(line, "🤖", "")
	line = strings.ReplaceAll(line, "💁", "")
	return line
}

func (s *TextSelector) Reset() {
	s.Selection.Active = false
}

func (s TextSelector) IsSelecting() bool {
	return s.Selection.Active
}

func NewTextSelector(
	w, h int,
	scrollPos int,
	sessionData string,
	colors util.SchemeColors,
) TextSelector {

	lines := strings.Split(sessionData, "\n")

	viewWidth, viewHeight := util.CalcVisualModeViewSize(w, h)
	pos := scrollPos + viewHeight/2
	pos = max(pos, 1)

	if pos > len(lines) {
		pos = len(lines) - 1
	}

	state := TextSelector{
		lines:        lines,
		cursor:       cursor{line: pos},
		Selection:    selection{Active: false},
		scrollOffset: scrollPos,
		paneHeight:   viewHeight,
		paneWidth:    viewWidth,
		keys:         defaultKeyMap,
		renderedText: sessionData,
		numberLines:  0,
		colors:       colors,
	}

	return state
}
