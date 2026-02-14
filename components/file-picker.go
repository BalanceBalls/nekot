package components

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
	// Currently selected file for preview (for context mode)
	previewFile string
	// Preview content
	previewContent string
	// Cached directory entries to avoid excessive I/O
	cachedDirEntries []os.DirEntry
	cachedDirPath    string
}

func NewFilePicker(
	prevView util.ViewMode,
	prevInput string,
	colors util.SchemeColors,
	isContextMode bool,
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
	filterInput.Prompt = "/"
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
	}
	return filePicker
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

		// Debug: Log all key events when in FilePickerMode
		util.Slog.Debug("FilePicker: Key event received", "keyStr", keyStr, "isContextMode", m.IsContextMode)

		// Handle Ctrl+/ (or Ctrl+_ on some keyboards) to focus filter input
		if keyStr == "ctrl+/" || keyStr == "ctrl+_" {
			util.Slog.Debug("FilePicker: Ctrl+/ detected, focusing filter input")
			m.filterInputFocused = true
			m.filterInput.Focus()
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
		case "q":
			// Only close picker if filter input is not focused
			if !m.filterInputFocused {
				m.quitting = true
				return m, util.SendViewModeChangedMsg(m.PrevView)
			}
			// Consume "q" when filter is focused
			return m, nil
		}

	case clearErrorMsg:
		m.err = nil
	}

	var cmd tea.Cmd
	var filterCmd tea.Cmd

	// Update filter input if focused
	if m.filterInputFocused {
		m.filterInput, filterCmd = m.filterInput.Update(msg)
		// Don't pass key messages to filepicker when filter input is focused
		// This prevents Backspace from being interpreted as "go up one directory"
		if _, ok := msg.(tea.KeyMsg); ok {
			return m, filterCmd
		}
	}

	// Update filepicker
	m.filepicker, cmd = m.filepicker.Update(msg)

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
		if m.IsContextMode && isMediaFile(path) {
			m.err = errors.New(path + " is a media file. Use Ctrl+A to attach media files.")
			m.SelectedFile = ""
			return m, tea.Batch(cmd, filterCmd, clearErrorAfter(2*time.Second))
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
		return m, tea.Batch(cmd, filterCmd, clearErrorAfter(2*time.Second))
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

// filterFilePickerView returns a filtered view of the file picker
func (m FilePicker) filterFilePickerView(filterText string) string {
	// Get the current directory from the file picker
	currentDir := m.filepicker.CurrentDirectory

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
	}
}

func isMediaFile(path string) bool {
	for _, ext := range util.MediaExtensions {
		if strings.HasSuffix(strings.ToLower(path), ext) {
			return true
		}
	}
	return false
}

// getFilePreviewContent reads and returns the content of a file for preview
func (m FilePicker) getFilePreviewContent(path string) string {
	// Check if it's a directory
	info, err := os.Stat(path)
	if err != nil {
		return "Error reading file: " + err.Error()
	}
	if info.IsDir() {
		return "[Directory]"
	}

	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		return "Error reading file: " + err.Error()
	}

	// Convert to string and limit to reasonable size
	contentStr := string(content)
	if len(contentStr) > 10000 {
		contentStr = contentStr[:10000] + "\n... (truncated)"
	}

	return contentStr
}

// GetPreviewView returns the preview pane view for the currently selected file
func (m FilePicker) GetPreviewView(height int) string {
	if !m.IsContextMode || m.previewFile == "" {
		return ""
	}

	// Check if it's a directory
	info, err := os.Stat(m.previewFile)
	if err != nil {
		return "Error: " + err.Error()
	}
	if info.IsDir() {
		return "[Directory Preview]\n\n" + m.previewFile
	}

	// Split content into lines
	lines := strings.Split(m.previewContent, "\n")

	// Limit to available height
	if len(lines) > height {
		lines = lines[:height]
	}

	// Add header
	header := "[File Preview: " + m.previewFile + "]"
	previewLines := append([]string{header, ""}, lines...)

	return strings.Join(previewLines, "\n")
}

// GetPreviewFile returns the currently selected file for preview
func (m FilePicker) GetPreviewFile() string {
	return m.previewFile
}
