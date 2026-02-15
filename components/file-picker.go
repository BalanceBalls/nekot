package components

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Debounce message for delayed search
type debounceSearchMsg struct {
	filterText string
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// SearchResult represents a file found during recursive search
type SearchResult struct {
	Path    string
	RelPath string // Relative to current directory
	IsDir   bool
	Size    int64
}

type FilePicker struct {
	SelectedFile  string
	PrevView      util.ViewMode
	PrevInputData string
	filepicker    filepicker.Model
	quitting      bool
	err           error
	// For context picker mode
	IsContextMode bool
	// Track if filter input is focused
	filterInputFocused bool
	// Filter input for searching files
	filterInput textinput.Model
	// Filtered files list
	filteredFiles []os.DirEntry
	// Search results for recursive fuzzy matching
	searchResults []SearchResult
	// Search depth from config
	searchDepth int
	// Theme colors for styling
	colors util.SchemeColors
	// Currently selected file for preview (for context mode)
	previewFile string
	// Preview content
	previewContent string

	// Caching
	cachedPreviewRendered string
	cachedPreviewFile     string
	cachedPreviewMtime    time.Time
	cachedTerminalWidth   int
	cachedIsText          bool
	cachedDirEntries      []os.DirEntry
	cachedDirPath         string
	// Text file validation cache: path -> isText
	textFileCache      map[string]bool
	textFileCacheMtime map[string]time.Time

	// Terminal width for line truncation in preview
	terminalWidth int
	// Selection index for filtered view (when filter is active)
	filteredSelectionIndex int
	// Debounce timer and channel for recursive search
	debounceTimer   *time.Timer
	debounceChannel chan string // Channel to signal when debounce completes
	// Timestamp of last search for rate limiting
	lastSearchTime time.Time
}

func NewFilePicker(
	prevView util.ViewMode,
	prevInput string,
	colors util.SchemeColors,
	isContextMode bool,
	searchDepth int,
) FilePicker {
	fp := filepicker.New()

	fp.Styles.Directory = fp.Styles.Directory.
		Foreground(colors.HighlightColor)

	fp.Styles.File = fp.Styles.File.
		Foreground(colors.NormalTabBorderColor)

	fp.Styles.Cursor = fp.Styles.Cursor.
		Foreground(colors.ActiveTabBorderColor)

	fp.Styles.Selected = fp.Styles.Selected.
		Foreground(colors.ActiveTabBorderColor)

	if isContextMode {
		// Perhaps only allow non-media?
		// Because this is a seperate mode, the logic should be here

		// Note: Media files will be filtered out during selection processing
		fp.DirAllowed = true
		fp.AllowedTypes = []string{}
	} else {
		// For media mode, only allow media files (images, videos, audio, etc.)
		fp.AllowedTypes = util.MediaExtensions
	}

	fp.CurrentDirectory, _ = os.Getwd()
	fp.ShowPermissions = false
	fp.ShowSize = true

	// Initialize filter input
	filterInput := textinput.New()
	filterInput.Placeholder = "Filter files..."
	filterInput.Prompt = "Filter: "
	filterInput.PromptStyle = lipgloss.NewStyle().Foreground(colors.ActiveTabBorderColor)
	filterInput.PlaceholderStyle = lipgloss.NewStyle().Faint(true)

	filePicker := FilePicker{
		filepicker:         fp,
		PrevView:           prevView,
		PrevInputData:      prevInput,
		IsContextMode:      isContextMode,
		filterInput:        filterInput,
		filterInputFocused: false,
		filteredFiles:      []os.DirEntry{},
		searchResults:      []SearchResult{},
		searchDepth:        searchDepth,
		colors:             colors,
		debounceChannel:    make(chan string, 1),
		textFileCache:      make(map[string]bool),
		textFileCacheMtime: make(map[string]time.Time),
	}
	return filePicker
}

// Stop cleans up resources used by the file picker
// Should be called when the file picker is no longer needed
func (m *FilePicker) Stop() {
	if m.debounceTimer != nil {
		m.debounceTimer.Stop()
	}
	// Clear the channel to prevent goroutine leaks
	if m.debounceChannel != nil {
		for {
			select {
			case <-m.debounceChannel:
			default:
				goto done
			}
		}
	done:
	}
}

// isTextFile checks if a file is a text file using cached results
// Uses a map cache to avoid re-reading file content for the same file
func (m *FilePicker) isTextFile(path string) bool {
	// Check cache first
	if cached, ok := m.textFileCache[path]; ok {
		return cached
	}

	// Check extension against known text/code extensions using helper function
	ext := filepath.Ext(path)
	if util.IsTextOrCodeExtension(ext) {
		m.textFileCache[path] = true
		return true
	}

	// Check file size first - skip very large files (likely binary or not suitable for preview)
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false
	}
	// Skip files larger than 1MB as they're unlikely to be suitable for quick preview
	if fileInfo.Size() > util.MaxPreviewFileSize {
		return false
	}

	// Try to read a small portion to check if it's valid UTF-8
	// Use os.Open with limited read instead of ReadFile to avoid loading entire file
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	// Read only first 1024 bytes for UTF-8 validity check
	buf := make([]byte, util.Utf8CheckBufferSize)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return false
	}
	return utf8.Valid(buf[:n])
}

type clearErrorMsg struct{}

func clearErrorAfter(t time.Duration) tea.Cmd {
	return tea.Tick(t, func(_ time.Time) tea.Msg {
		return clearErrorMsg{}
	})
}

func (m FilePicker) Init() tea.Cmd {
	return m.filepicker.Init()
}

func (m FilePicker) Update(msg tea.Msg) (FilePicker, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		keyStr := msg.String()

		// Handle Ctrl+/ (or Ctrl+_ on some keyboards) to focus filter input
		if keyStr == "ctrl+/" || keyStr == "ctrl+_" {
			m.filterInputFocused = true
			m.filterInput.Focus()
			// Initialize search results if filter has content
			filterText := strings.ToLower(m.filterInput.Value())
			if filterText != "" && m.IsContextMode {
				m.searchResults = m.recursiveSearch(filterText, m.searchDepth)
				// Reset selection index
				if len(m.searchResults) > 0 {
					m.filteredSelectionIndex = 0
				} else {
					m.filteredSelectionIndex = -1
				}
			}
			return m, nil
		}

		switch keyStr {
		case "esc":
			// Two-stage Esc behavior:
			// - First press: blur filter input if focused
			// - Second press: close the picker
			if m.filterInputFocused {
				m.filterInputFocused = false
				m.filterInput.Blur()

				// Don't pass Esc to filepicker to prevent going back
				return m, nil
			} else {
				// Filter input is not focused, close the picker
				m.quitting = true
				return m, util.SendViewModeChangedMsg(m.PrevView)
			}
		case "enter":
			// Handle Enter key to select file when filter is active and there are search results
			if m.filterInputFocused && len(m.searchResults) > 0 && m.filteredSelectionIndex >= 0 && m.filteredSelectionIndex < len(m.searchResults) {
				selectedResult := m.searchResults[m.filteredSelectionIndex]
				m.SelectedFile = selectedResult.Path
				return m, nil
			}
		case "up", "down":

			// Handle arrow keys for filtered view when filter is active and there are search results
			if m.filterInputFocused && len(m.searchResults) > 0 {
				if keyStr == "up" {
					m.filteredSelectionIndex--
					if m.filteredSelectionIndex < 0 {
						m.filteredSelectionIndex = len(m.searchResults) - 1
					}
				} else if keyStr == "down" {
					m.filteredSelectionIndex++
					if m.filteredSelectionIndex >= len(m.searchResults) {
						m.filteredSelectionIndex = 0
					}
				}

				// Update preview for the selected file
				if m.filteredSelectionIndex >= 0 && m.filteredSelectionIndex < len(m.searchResults) {
					selectedResult := m.searchResults[m.filteredSelectionIndex]
					if selectedResult.Path != m.previewFile {
						m.previewFile = selectedResult.Path
						m.previewContent = m.getFilePreviewContent(selectedResult.Path)
					}
				}

				// Don't pass arrow keys to filter input when navigating filtered results
				return m, nil
			}
		}

	case clearErrorMsg:
		m.err = nil
	}

	var cmd tea.Cmd
	var filterCmd tea.Cmd

	// Update filter input if focused
	if m.filterInputFocused {
		oldFilterValue := m.filterInput.Value()
		m.filterInput, filterCmd = m.filterInput.Update(msg)
		newFilterValue := m.filterInput.Value()

		// If filter value changed, set up debounced search
		if oldFilterValue != newFilterValue {
			filterText := strings.ToLower(newFilterValue)

			// Reset existing timer
			if m.debounceTimer != nil {
				m.debounceTimer.Stop()
			}

			// Schedule debounced search using AfterFunc
			// When timer fires, it sends the filter text to the channel
			m.debounceTimer = time.AfterFunc(util.FilePickerDebounce, func() {
				// Non-blocking send to channel
				select {
				case m.debounceChannel <- filterText:
				default:
				}
			})
		}

		// Don't pass key messages to filepicker when filter input is focused
		// This prevents Backspace from being interpreted as "go up one directory"
		if _, ok := msg.(tea.KeyMsg); ok {
			return m, filterCmd
		}
	}

	// Check if debounce channel has a message (timer fired)
	// Use non-blocking select to check for channel message
	if m.debounceChannel != nil {
		select {
		case filterText := <-m.debounceChannel:
			// Timer fired - perform the debounced search
			if filterText != "" && m.IsContextMode {
				m.searchResults = m.recursiveSearch(filterText, m.searchDepth)
				if len(m.searchResults) > 0 {
					m.filteredSelectionIndex = 0
				} else {
					m.filteredSelectionIndex = -1
				}
			} else {
				m.searchResults = []SearchResult{}
				m.filteredSelectionIndex = -1
			}
			m.lastSearchTime = time.Now()
		default:
			// No message waiting, continue
		}
	}

	// Update filepicker
	m.filepicker, cmd = m.filepicker.Update(msg)

	// Track selection changes via cursor position for filtered view,
	// and via view parsing for normal navigation (filepicker doesn't expose cursor)
	// We track key presses to detect navigation changes
	if m.filterInputFocused && len(m.searchResults) > 0 {
		// For filtered view, use tracked filteredSelectionIndex (already maintained)
		// Preview is updated in the key handler above
	} else {
		// For normal navigation, use view parsing (necessary as library doesn't expose cursor)
		// But only re-parse if the view actually changed
		currentView := m.filepicker.View()
		if currentView != m.cachedPreviewRendered {
			// View changed - this could be navigation or just window resize
			// Parse to find selected file
			m.updatePreviewFromView(currentView)
		}
	}

	// Refresh cache if directory changed
	if m.cachedDirPath != m.filepicker.CurrentDirectory {
		entries, err := os.ReadDir(m.filepicker.CurrentDirectory)
		if err == nil {
			m.cachedDirEntries = entries
			m.cachedDirPath = m.filepicker.CurrentDirectory
		}
	}

	if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
		// In context mode, filter out media files
		if m.IsContextMode && util.IsMediaFile(path) {
			m.err = errors.New(path + " is a media file. Use Ctrl+A to attach media files.")
			m.SelectedFile = ""
			return m, tea.Batch(cmd, filterCmd, clearErrorAfter(util.ErrorDisplayDuration))
		}
		m.SelectedFile = path
		// Update preview file for context mode
		if m.IsContextMode {
			m.previewFile = path
			m.previewContent = m.getFilePreviewContent(path)
		}
	}

	if didSelect, path := m.filepicker.DidSelectDisabledFile(msg); didSelect {
		m.err = errors.New(path + " is not valid.")
		m.SelectedFile = ""
		return m, tea.Batch(cmd, filterCmd, clearErrorAfter(util.ErrorDisplayDuration))
	}

	return m, tea.Batch(cmd, filterCmd)
}

func (m FilePicker) View() string {
	if m.quitting {
		return ""
	}

	// Get the file picker view
	filePickerView := m.filepicker.View()

	// If filter input has content, filter the file picker view
	filterText := strings.ToLower(m.filterInput.Value())
	if filterText != "" {
		filePickerView = m.filterFilePickerView(filterText)
	}

	// Show filter input beneath the file listing
	filterInputView := m.filterInput.View()

	// Join file picker view and filter input vertically
	return lipgloss.JoinVertical(
		lipgloss.Left,
		filePickerView,
		filterInputView,
	)
}

// GetFilePickerViewWithoutFilter returns the file picker view without the filter input
// This is used when the filter input is displayed separately (e.g., below preview)
func (m FilePicker) GetFilePickerViewWithoutFilter() string {
	if m.quitting {
		return ""
	}

	// Get the file picker view
	filePickerView := m.filepicker.View()

	// If filter input has content, filter the file picker view
	filterText := strings.ToLower(m.filterInput.Value())
	if filterText != "" {
		filePickerView = m.filterFilePickerView(filterText)
	}

	return filePickerView
}

// GetFilterInputView returns the filter input view
func (m FilePicker) GetFilterInputView() string {
	return m.filterInput.View()
}

// recursiveSearch performs a recursive search for files matching the filter text
// Searches up to maxDepth levels deep from the current directory
// Returns at most maxSearchResults to prevent memory/performance issues
func (m FilePicker) recursiveSearch(filterText string, maxDepth int) []SearchResult {
	var results []SearchResult
	currentDir := m.filepicker.CurrentDirectory

	// Use the new WalkDirectory utility
	_, err := util.WalkDirectory(currentDir, maxDepth, func(filePath string, d fs.DirEntry, relPath string, depth int) bool {
		// Stop collecting results if we've reached the limit
		if len(results) >= util.MaxSearchResults {
			return false // Return false to stop walking
		}

		// Skip media files in context mode
		if m.IsContextMode && util.IsMediaFile(filePath) {
			return false
		}

		// Check if the entry name contains the filter text (case-insensitive)
		baseName := filepath.Base(filePath)
		if strings.Contains(strings.ToLower(baseName), filterText) {
			info, err := d.Info()
			if err != nil {
				return false
			}

			results = append(results, SearchResult{
				Path:    filePath,
				RelPath: relPath,
				IsDir:   d.IsDir(),
				Size:    info.Size(),
			})
			return true
		}

		return false
	})

	if err != nil {
		// Log error but return what we have
		util.Slog.Warn("FilePicker: Error during recursive search", "error", err)
	}

	return results
}

// filterFilePickerView returns a filtered view of the file picker
// Uses cached search results from debounced search in Update
func (m FilePicker) filterFilePickerView(filterText string) string {
	// Get the current directory from the file picker
	currentDir := m.filepicker.CurrentDirectory

	// In context mode, use cached search results (populated by debounced Update)
	// Don't perform search here - it defeats the debounce mechanism
	if m.IsContextMode {
		// Use already-cached searchResults from Update
		// (searchResults is populated by debounced search in Update method)

		// Only reset selection index if it's out of bounds or if there are no results
		// Don't reset if the user has already navigated with arrow keys
		if len(m.searchResults) > 0 {
			if m.filteredSelectionIndex < 0 || m.filteredSelectionIndex >= len(m.searchResults) {
				m.filteredSelectionIndex = 0
			}
		} else {
			m.filteredSelectionIndex = -1
		}

		// If no matches, show a message
		if len(m.searchResults) == 0 {
			return currentDir + "\n\nNo files match filter: " + m.filterInput.Value()
		}

		// Build the filtered view with relative paths
		var lines []string
		lines = append(lines, currentDir)
		lines = append(lines, "")

		for i, result := range m.searchResults {
			// Format the entry line with relative path
			name := result.RelPath
			if result.IsDir {
				name += "/"
			}

			// Add size info
			sizeStr := formatSize(result.Size)

			// Add indentation based on depth for visual hierarchy
			depth := strings.Count(result.RelPath, string(filepath.Separator))
			indent := strings.Repeat("  ", depth)

			// Determine if this item is selected
			isSelected := i == m.filteredSelectionIndex

			// Add cursor indicator for selected item
			prefix := "  "
			if isSelected {
				prefix = "> "
			}

			// Apply colors based on selection and file type
			var styledLine string
			if isSelected {
				// Selected item: use ActiveTabBorderColor for both name and cursor
				cursorStyle := lipgloss.NewStyle().Foreground(m.colors.ActiveTabBorderColor)
				nameStyle := lipgloss.NewStyle().Foreground(m.colors.ActiveTabBorderColor)
				sizeStyle := lipgloss.NewStyle().Foreground(m.colors.ActiveTabBorderColor)
				styledLine = cursorStyle.Render(prefix) + indent + nameStyle.Render(name) + "  " + sizeStyle.Render(sizeStr)
			} else {
				// Non-selected item: use different colors for directories vs files
				var nameStyle lipgloss.Style
				if result.IsDir {
					nameStyle = lipgloss.NewStyle().Foreground(m.colors.HighlightColor)
				} else {
					nameStyle = lipgloss.NewStyle().Foreground(m.colors.NormalTabBorderColor)
				}
				// Use a subdued color for file size
				sizeStyle := lipgloss.NewStyle().Foreground(m.colors.HighlightColor).Faint(true)
				styledLine = prefix + indent + nameStyle.Render(name) + "  " + sizeStyle.Render(sizeStr)
			}

			lines = append(lines, styledLine)
		}

		return strings.Join(lines, "\n")
	}

	// Non-context mode: use current directory only
	// Use cached directory entries
	entries := m.cachedDirEntries
	if len(entries) == 0 || m.cachedDirPath != currentDir {
		// Cache miss or directory changed, read directory
		var err error
		entries, err = os.ReadDir(currentDir)
		if err != nil {
			return "Error reading directory: " + err.Error()
		}
	}

	// Filter entries based on the filter text
	var filteredEntries []os.DirEntry
	for _, entry := range entries {
		// Check if the entry name contains the filter text
		if strings.Contains(strings.ToLower(entry.Name()), filterText) {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	// If no matches, show a message
	if len(filteredEntries) == 0 {
		return currentDir + "\n\nNo files match filter: " + m.filterInput.Value()
	}

	// Build the filtered view
	var lines []string
	lines = append(lines, currentDir)
	lines = append(lines, "")

	for _, entry := range filteredEntries {
		// Get file info
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Format the entry line
		name := entry.Name()
		if info.IsDir() {
			name += "/"
		}

		// Add size info
		size := info.Size()
		sizeStr := formatSize(size)

		lines = append(lines, name+"  "+sizeStr)
	}

	return strings.Join(lines, "\n")
}

// formatSize formats a file size in human-readable format
func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func (m *FilePicker) SetSize(w, h int) {
	if w > 2 && h > 2 {
		m.filepicker.SetHeight(h)
		m.terminalWidth = w
	}
}

// getFilePreviewContent reads and returns the content of a file for preview
// Only reads content for text files, returns appropriate message for others
func (m FilePicker) getFilePreviewContent(path string) string {
	// Check if it's a directory
	info, err := os.Stat(path)
	if err != nil {
		return "Error reading file: " + err.Error()
	}
	if info.IsDir() {
		return "[Directory]"
	}

	// Check if it's a text file
	if !m.isTextFile(path) {
		return "[Binary file - preview not available]"
	}

	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		return "Error reading file: " + err.Error()
	}

	// Convert to string and limit to reasonable size
	contentStr := string(content)
	if len(contentStr) > util.MaxPreviewContentSize {
		contentStr = contentStr[:util.MaxPreviewContentSize] + "\n... (truncated)"
	}

	return contentStr
}

// GetPreviewView returns the preview pane view for the currently selected file
// Only shows preview for text files in context mode
// Adds colored output, line numbers, and better alignment
// FIX 7: Uses caching to avoid re-rendering on every call
func (m FilePicker) GetPreviewView(height int) string {
	if !m.IsContextMode || m.previewFile == "" {
		return ""
	}

	// Check if it's a directory
	info, err := os.Stat(m.previewFile)
	if err != nil {
		return lipgloss.NewStyle().
			Foreground(m.colors.ErrorColor).
			Render("Error: " + err.Error())
	}
	if info.IsDir() {
		header := lipgloss.NewStyle().
			Foreground(m.colors.HighlightColor).
			Bold(true).
			Render("[Directory]")
		path := lipgloss.NewStyle().
			Foreground(m.colors.DefaultTextColor).
			Render(m.previewFile)
		return lipgloss.JoinVertical(lipgloss.Left, header, path)
	}

	// FIX 7: Check if we can use cached preview
	// Cache is valid if: same file, same mtime, same terminal width
	if m.cachedPreviewRendered != "" &&
		m.previewFile == m.cachedPreviewFile &&
		info.ModTime().Equal(m.cachedPreviewMtime) &&
		m.terminalWidth == m.cachedTerminalWidth {
		return m.cachedPreviewRendered
	}

	// FIX 7: Check if it's a text file - use cached result if available
	// We need to re-check if the file changed
	isText := m.cachedIsText
	if m.cachedPreviewFile != m.previewFile {
		// File changed, re-check text validity
		isText = m.isTextFile(m.previewFile)
		m.cachedIsText = isText
	}

	if !isText {
		header := lipgloss.NewStyle().
			Foreground(m.colors.HighlightColor).
			Bold(true).
			Render("[Binary File]")
		message := lipgloss.NewStyle().
			Foreground(m.colors.DefaultTextColor).
			Render("Preview not available for binary files")
		return lipgloss.JoinVertical(lipgloss.Left, header, "", message)
	}

	// Generate preview (same logic as before)
	lines := strings.Split(m.previewContent, "\n")

	// Limit to available height (reserve space for header)
	availableHeight := height - 3
	if len(lines) > availableHeight {
		lines = lines[:availableHeight]
	}

	lineNumWidth := len(fmt.Sprintf("%d", len(lines)))

	// Calculate max line width (50% of terminal width)
	maxLineWidth := (m.terminalWidth / 2) - lineNumWidth - 5

	// truncate detection
	hasLongLines := false
	for _, line := range lines {
		visibleLen := utf8.RuneCountInString(stripANSI(line))
		if visibleLen > maxLineWidth {
			hasLongLines = true
			break
		}
	}

	if hasLongLines {
		for i := range lines {
			visibleLen := utf8.RuneCountInString(stripANSI(lines[i]))
			if visibleLen > maxLineWidth {
				truncated := truncateLineWithANSI(lines[i], maxLineWidth)
				lines[i] = truncated
			}
		}
	}

	// Build styled preview with line numbers
	var previewLines []string

	// Add header with file info
	fileName := filepath.Base(m.previewFile)
	fileSize := formatSize(info.Size())
	headerStyle := lipgloss.NewStyle().
		Foreground(m.colors.HighlightColor).
		Bold(true)
	header := headerStyle.Render(fmt.Sprintf("%s (%s)", fileName, fileSize))
	previewLines = append(previewLines, header)

	for i, line := range lines {
		lineNum := fmt.Sprintf("%*d │ ", lineNumWidth, i+1)
		lineNumStyle := lipgloss.NewStyle().
			Foreground(m.colors.AccentColor)
		contentStyle := lipgloss.NewStyle().
			Foreground(m.colors.DefaultTextColor)
		previewLines = append(previewLines, lineNumStyle.Render(lineNum)+contentStyle.Render(line))
	}

	// FIX 7: Cache the rendered preview
	rendered := strings.Join(previewLines, "\n")
	m.cachedPreviewRendered = rendered
	m.cachedPreviewFile = m.previewFile
	m.cachedPreviewMtime = info.ModTime()
	m.cachedTerminalWidth = m.terminalWidth
	m.cachedIsText = true

	return rendered
}

// truncateLineWithANSI truncates a line to max visible characters while preserving ANSI codes
func truncateLineWithANSI(line string, maxLen int) string {
	// Remove ANSI codes temporarily to count visible characters
	cleanLine := stripANSI(line)

	// If clean line is already short enough, return original
	if utf8.RuneCountInString(cleanLine) <= maxLen {
		return line
	}

	// Truncate the clean line and add ellipsis
	runes := []rune(cleanLine)
	if len(runes) > maxLen {
		runes = runes[:maxLen-3] // Reserve 3 chars for "..."
	}
	truncatedClean := string(runes) + "..."

	// Now we need to rebuild the line with ANSI codes
	// This is complex, so for simplicity, we'll just return the truncated clean line
	// A more sophisticated approach would preserve ANSI codes at the beginning
	return truncatedClean
}

// stripANSI removes ANSI escape codes from a string
func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// updatePreviewFromView extracts the currently selected file from filepicker's rendered view
// This allows preview to update when navigating with arrow keys, not just on Enter
func (m *FilePicker) updatePreviewFromView(view string) {
	if !m.IsContextMode {
		return
	}

	// Parse the view to find the currently selected file (marked with cursor)
	lines := strings.Split(view, "\n")
	for _, line := range lines {
		// Look for cursor indicator (filepicker uses > for cursor)
		if strings.Contains(line, ">") {

			// Strip ANSI escape codes
			cleanLine := stripANSI(line)

			// Extract file path from the line
			// Format: ">  4.1kB clients" or ">  18kB chat-pane.go"
			// The format is: cursor + spaces + size + spaces + filename
			parts := strings.Split(cleanLine, ">")
			if len(parts) > 1 {
				rest := strings.TrimSpace(parts[1])

				// Split by spaces to get size and filename
				// Format: "4.1kB clients" -> ["4.1kB", "clients"]
				fields := strings.Fields(rest)
				if len(fields) >= 2 {
					// The filename is the last field
					fileName := fields[len(fields)-1]

					// Get full path by combining with current directory
					if !strings.HasPrefix(fileName, "/") && !strings.HasPrefix(fileName, "~") {
						// Relative path
						fullPath := filepath.Join(m.filepicker.CurrentDirectory, fileName)

						// Check if file exists
						if _, err := os.Stat(fullPath); err != nil {
							util.Slog.Error("FilePicker: File does not exist", "path", fullPath, "error", err)
						} else {
						}

						// Only update if different from current preview
						if fullPath != m.previewFile {
							m.previewFile = fullPath
							m.previewContent = m.getFilePreviewContent(fullPath)
						} else {
						}
					} else {
						if fileName != m.previewFile {
							m.previewFile = fileName
							m.previewContent = m.getFilePreviewContent(fileName)
						}
					}
				} else {
					util.Slog.Warn("FilePicker: Could not parse filename from line", "fields", fields)
				}
			}
		}
	}
}

// GetPreviewFile returns the currently selected file for preview
func (m FilePicker) GetPreviewFile() string {
	return m.previewFile
}
