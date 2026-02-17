package components

import (
	"errors"
	"io"
	"os"
	"path/filepath"
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
	searchResults []util.SearchResult
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
		searchResults:      []util.SearchResult{},
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
				m.searchResults = m.RecursiveSearch(filterText, m.searchDepth)
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
						m.previewContent = m.GetFilePreviewContent(selectedResult.Path)
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
				m.searchResults = m.RecursiveSearch(filterText, m.searchDepth)
				if len(m.searchResults) > 0 {
					m.filteredSelectionIndex = 0
				} else {
					m.filteredSelectionIndex = -1
				}
			} else {
				m.searchResults = []util.SearchResult{}
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
			m.UpdatePreviewFromView(currentView)
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
			m.previewContent = m.GetFilePreviewContent(path)
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
		filePickerView = m.FilterFilePickerView(filterText)
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
		filePickerView = m.FilterFilePickerView(filterText)
	}

	return filePickerView
}

// GetFilterInputView returns the filter input view
func (m FilePicker) GetFilterInputView() string {
	return m.filterInput.View()
}
