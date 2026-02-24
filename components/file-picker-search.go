package components

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/lipgloss"
)

// RecursiveSearch performs a recursive search for files matching the filter text
// Searches up to maxDepth levels deep from the current directory
// Returns at most maxSearchResults to prevent memory/performance issues
func (m *FilePicker) RecursiveSearch(filterText string, maxDepth int) []util.SearchResult {
	var results []util.SearchResult
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

			results = append(results, util.SearchResult{
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

// FilterFilePickerView returns a filtered view of the file picker
// Uses cached search results from debounced search in Update
func (m *FilePicker) FilterFilePickerView(filterText string) string {
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
			sizeStr := util.FormatSize(result.Size)

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
		sizeStr := util.FormatSize(size)

		lines = append(lines, name+"  "+sizeStr)
	}

	return strings.Join(lines, "\n")
}

// UpdateSearchResults updates the search results based on the filter text
func (m *FilePicker) UpdateSearchResults(filterText string) {
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
}

// GetSelectedSearchResult returns the currently selected search result
func (m *FilePicker) GetSelectedSearchResult() *util.SearchResult {
	if len(m.searchResults) > 0 && m.filteredSelectionIndex >= 0 && m.filteredSelectionIndex < len(m.searchResults) {
		return &m.searchResults[m.filteredSelectionIndex]
	}
	return nil
}

// NavigateSearchResults moves the selection up or down in the search results
func (m *FilePicker) NavigateSearchResults(direction int) {
	if len(m.searchResults) == 0 {
		return
	}

	m.filteredSelectionIndex += direction
	if m.filteredSelectionIndex < 0 {
		m.filteredSelectionIndex = len(m.searchResults) - 1
	} else if m.filteredSelectionIndex >= len(m.searchResults) {
		m.filteredSelectionIndex = 0
	}
}

// ClearSearch clears the search results and resets the selection
func (m *FilePicker) ClearSearch() {
	m.searchResults = []util.SearchResult{}
	m.filteredSelectionIndex = -1
}
