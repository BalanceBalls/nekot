package components

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/BalanceBalls/nekot/util"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

const (
	HighlightPrefix     = " +"
	CharHighlightPrefix = "ch"
	CursorSymbol        = "~"
)

type keyMap struct {
	visualLineMode key.Binding
	up             key.Binding
	down           key.Binding
	pageUp         key.Binding
	pageDown       key.Binding
	copy           key.Binding
	copyRaw        key.Binding
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
	copy:    key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy selection")),
	copyRaw: key.NewBinding(key.WithKeys("c", "r"), key.WithHelp("c/r", "raw copy selection")),
	bottom:  key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "go to bottom")),
	top:     key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "go to top")),
}

type cursor struct {
	line int
}

type selection struct {
	Active bool
	anchor cursor
}

type charSelection struct {
	Active    bool
	line      int
	anchorCol int
	cursorCol int
}

type TextSelector struct {
	Selection          selection
	CharSelection      charSelection
	lines              []string
	cursor             cursor
	scrollOffset       int
	paneHeight         int
	paneWidth          int
	keys               keyMap
	renderedText       string
	colors             util.SchemeColors
	mouseSelecting     bool
	mouseSelectingChar bool
	mouseTopOffset     int
	mouseLeftOffset    int

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
	case tea.MouseMsg:
		s = s.handleMouseSelection(msg)
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
			s.CharSelection.Active = false
			s.mouseSelectingChar = false

		case key.Matches(msg, s.keys.copy):
			if s.IsSelecting() {
				if s.CharSelection.Active {
					s.copySelectedCharsToClipboard(false)
				} else {
					s.copySelectedLinesToClipboard(false)
				}
				s.Selection.Active = false
				s.CharSelection.Active = false
				s.mouseSelecting = false
				s.mouseSelectingChar = false
				cmds = append(cmds, util.SendNotificationMsg(util.CopiedNotification))
			}

		case key.Matches(msg, s.keys.copyRaw):
			if s.IsSelecting() {
				if s.CharSelection.Active {
					s.copySelectedCharsToClipboard(true)
				} else {
					s.copySelectedLinesToClipboard(true)
				}
				s.Selection.Active = false
				s.CharSelection.Active = false
				s.mouseSelecting = false
				s.mouseSelectingChar = false
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
	textColor := lipgloss.AdaptiveColor{Dark: "#000000", Light: "#ffffff"}
	highlightStyle := lipgloss.NewStyle().
		Foreground(textColor).
		Background(s.colors.HighlightColor)

	cursorStyle := lipgloss.NewStyle().
		Foreground(textColor).
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

	var charLine, charStartCol, charEndCol int
	if s.CharSelection.Active {
		charLine = s.CharSelection.line
		charStartCol = s.CharSelection.anchorCol
		charEndCol = s.CharSelection.cursorCol
		if charStartCol > charEndCol {
			charStartCol, charEndCol = charEndCol, charStartCol
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
		case s.CharSelection.Active && i == charLine:
			sb.WriteString(highlightStyle.Render(CharHighlightPrefix))
			sb.WriteString(s.highlightLineSegment(line, charStartCol, charEndCol, highlightStyle))
			sb.WriteString("\n")

		case s.Selection.Active && i >= startLine && i <= endLine:
			sb.WriteString(highlightStyle.Render(HighlightPrefix))
			sb.WriteString(line)
			sb.WriteString("\n")

		case !s.Selection.Active && !s.CharSelection.Active && i == s.cursor.line:
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

func (s TextSelector) handleMouseSelection(msg tea.MouseMsg) TextSelector {
	if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft {
		s.mouseSelecting = false
		return s
	}

	if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonRight {
		s.mouseSelectingChar = false
		return s
	}

	if !zone.Get("chat_pane").InBounds(msg) {
		return s
	}

	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		delta := 3
		if msg.Button == tea.MouseButtonWheelUp {
			delta = -3
		}

		s.scrollOffset = min(max(s.scrollOffset+delta, 0), s.maxScrollOffset())
		if !s.Selection.Active && !s.CharSelection.Active && !s.mouseSelecting && !s.mouseSelectingChar {
			visibleStart := s.scrollOffset
			visibleEnd := s.scrollOffset + s.paneHeight - 1
			if s.cursor.line < visibleStart {
				s.cursor.line = visibleStart
			} else if s.cursor.line > visibleEnd {
				s.cursor.line = visibleEnd
			}
			if s.cursor.line < s.firstLinePosition() {
				s.cursor.line = s.firstLinePosition()
			} else if s.cursor.line > s.lastLinePosition() {
				s.cursor.line = s.lastLinePosition()
			}
		}

		return s
	}

	switch {

	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		line, scrollOffset := s.lineFromMouse(msg)
		s.scrollOffset = scrollOffset
		s.cursor.line = line
		s.Selection.Active = true
		s.Selection.anchor = s.cursor
		s.CharSelection.Active = false
		s.mouseSelecting = true
		s.mouseSelectingChar = false

	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonRight:
		line, scrollOffset := s.lineFromMouse(msg)
		s.scrollOffset = scrollOffset
		s.cursor.line = line
		s.Selection.Active = false
		s.mouseSelecting = false
		s.CharSelection.Active = true
		s.CharSelection.line = line
		prefixWidth := lipgloss.Width(CharHighlightPrefix)
		s.CharSelection.anchorCol = s.columnFromMouse(msg, s.lines[line], prefixWidth)
		s.CharSelection.cursorCol = s.CharSelection.anchorCol
		s.mouseSelectingChar = true

	case msg.Action == tea.MouseActionMotion && s.mouseSelecting:
		line, scrollOffset := s.lineFromMouse(msg)
		s.scrollOffset = scrollOffset
		s.cursor.line = line

	case msg.Action == tea.MouseActionMotion && s.mouseSelectingChar:
		prefixWidth := lipgloss.Width(CharHighlightPrefix)
		s.CharSelection.cursorCol = s.columnFromMouse(msg, s.lines[s.CharSelection.line], prefixWidth)
	}

	return s
}

func (s TextSelector) lineFromMouse(msg tea.MouseMsg) (int, int) {
	_, mouseY := zone.Get("chat_pane").Pos(msg)
	if mouseY < 0 || s.paneHeight <= 0 {
		return s.cursor.line, s.scrollOffset
	}

	contentY := mouseY - s.mouseTopOffset
	scrollOffset := s.scrollOffset
	maxScrollOffset := s.maxScrollOffset()

	if contentY < 0 {
		scrollOffset = max(scrollOffset-1, 0)
		contentY = 0
	} else if contentY >= s.paneHeight {
		scrollOffset = min(scrollOffset+1, maxScrollOffset)
		contentY = max(s.paneHeight-1, 0)
	}

	scrollOffset = min(max(scrollOffset, 0), maxScrollOffset)
	line := scrollOffset + contentY
	line = max(line, s.firstLinePosition())
	line = min(line, s.lastLinePosition())

	return line, scrollOffset
}

func (s TextSelector) columnFromMouse(msg tea.MouseMsg, line string, prefixWidth int) int {
	mouseX, _ := zone.Get("chat_pane").Pos(msg)
	contentX := max(mouseX-s.mouseLeftOffset-prefixWidth, 0)

	visibleLine := util.StripAnsiCodes(line)
	lineRunes := []rune(visibleLine)
	if len(lineRunes) == 0 {
		return 0
	}

	if contentX >= len(lineRunes) {
		return len(lineRunes) - 1
	}

	return contentX
}

func (s TextSelector) maxScrollOffset() int {
	maxOffset := s.lastLinePosition() - s.paneHeight + 1
	if maxOffset < 0 {
		return 0
	}
	return maxOffset
}

func (s TextSelector) highlightLineSegment(line string, startCol int, endCol int, highlightStyle lipgloss.Style) string {
	visibleLine := util.StripAnsiCodes(line)
	lineRunes := []rune(visibleLine)
	if len(lineRunes) == 0 {
		return visibleLine
	}

	if startCol > endCol {
		startCol, endCol = endCol, startCol
	}

	startCol = max(startCol, 0)
	endCol = max(endCol, 0)
	startCol = min(startCol, len(lineRunes)-1)
	endCol = min(endCol, len(lineRunes)-1)

	before := string(lineRunes[:startCol])
	selected := string(lineRunes[startCol : endCol+1])
	after := string(lineRunes[endCol+1:])

	return before + highlightStyle.Render(selected) + after
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

func (s TextSelector) copySelectedLinesToClipboard(isRawCopy bool) {
	if !s.Selection.Active {
		return
	}

	selectedLines := s.GetSelectedLines()
	ansiFreeText := util.StripAnsiCodes(strings.Join(selectedLines, "\n"))

	ansiFreeLines := strings.Split(ansiFreeText, "\n")
	var linesToCopy = make([]string, len(ansiFreeLines))

	for i, line := range ansiFreeLines {
		if isRawCopy {
			linesToCopy[i] = strings.TrimRightFunc(strings.TrimLeftFunc(line, unicode.IsSpace), unicode.IsSpace)
		} else {
			linesToCopy[i] = strings.TrimRight(line, " ")
		}
	}

	joinSeparator := "\n"
	if isRawCopy {
		joinSeparator = " "
	}

	clipboard.WriteAll(strings.Join(linesToCopy, joinSeparator))
}

func (s TextSelector) copySelectedCharsToClipboard(isRawCopy bool) {
	if !s.CharSelection.Active {
		return
	}

	selectedText := s.GetSelectedChars()
	if isRawCopy {
		selectedText = strings.TrimRightFunc(strings.TrimLeftFunc(selectedText, unicode.IsSpace), unicode.IsSpace)
	} else {
		selectedText = strings.TrimRight(selectedText, " ")
	}

	clipboard.WriteAll(selectedText)
}

func (s TextSelector) GetSelectedLines() []string {
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
	return selectedLines
}

func (s TextSelector) GetSelectedChars() string {
	if !s.CharSelection.Active {
		return ""
	}

	if s.CharSelection.line < 0 || s.CharSelection.line >= len(s.lines) {
		return ""
	}

	visibleLine := util.StripAnsiCodes(s.lines[s.CharSelection.line])
	lineRunes := []rune(visibleLine)
	if len(lineRunes) == 0 {
		return ""
	}

	startCol, endCol := s.charSelectionRange(len(lineRunes))
	selected := string(lineRunes[startCol:endCol])

	return filterLine(selected)
}

func (s TextSelector) charSelectionRange(lineLength int) (int, int) {
	if lineLength <= 0 {
		return 0, 0
	}

	startCol := s.CharSelection.anchorCol
	endCol := s.CharSelection.cursorCol
	if startCol > endCol {
		startCol, endCol = endCol, startCol
	}

	startCol = max(startCol, 0)
	endCol = max(endCol, 0)
	startCol = min(startCol, lineLength-1)
	endCol = min(endCol, lineLength-1)

	return startCol, endCol + 1
}

func filterLine(line string) string {
	line = strings.ReplaceAll(line, "🤖", "")
	line = strings.ReplaceAll(line, "💁", "")
	return line
}

func (s *TextSelector) Reset() {
	s.Selection.Active = false
	s.CharSelection.Active = false
	s.mouseSelecting = false
	s.mouseSelectingChar = false
}

func (s TextSelector) IsSelecting() bool {
	return s.Selection.Active || s.CharSelection.Active
}

func (s TextSelector) IsCharSelecting() bool {
	return s.CharSelection.Active
}

func (s TextSelector) LinesSelected() int {
	if s.CharSelection.Active {
		return 1
	}
	return len(s.GetSelectedLines())
}

func (s TextSelector) SelectedCharCount() int {
	if !s.CharSelection.Active {
		return 0
	}

	return len([]rune(s.GetSelectedChars()))
}

func NewTextSelector(
	w, h int,
	scrollPos int,
	mouseTopOffset int,
	mouseLeftOffset int,
	sessionData string,
	colors util.SchemeColors,
) TextSelector {

	lines := strings.Split(sessionData, "\n")

	viewWidth, viewHeight := util.CalcVisualModeViewSize(w, h)

	viewHeight = viewHeight - 1
	pos := scrollPos + viewHeight/2
	pos = max(pos, 1)

	if pos > len(lines) {
		pos = len(lines) - 1
	}

	state := TextSelector{
		lines:              lines,
		cursor:             cursor{line: pos},
		Selection:          selection{Active: false},
		CharSelection:      charSelection{Active: false},
		scrollOffset:       scrollPos,
		paneHeight:         viewHeight,
		paneWidth:          viewWidth,
		keys:               defaultKeyMap,
		renderedText:       sessionData,
		numberLines:        0,
		colors:             colors,
		mouseTopOffset:     mouseTopOffset,
		mouseLeftOffset:    mouseLeftOffset,
		mouseSelecting:     false,
		mouseSelectingChar: false,
	}

	return state
}
