package components

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/lipgloss"
)

// SetSize sets the dimensions for the file picker
func (m *FilePicker) SetSize(w, h int) {
	if w > 2 && h > 2 {
		m.filepicker.SetHeight(h)
		m.terminalWidth = w
	}
}

// GetFilePreviewContent reads and returns the content of a file for preview
// Only reads content for text files, returns appropriate message for others
func (m FilePicker) GetFilePreviewContent(path string) string {
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
// Uses caching to avoid re-rendering on every call
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

	// Check if we can use cached preview
	// Cache is valid if: same file, same mtime, same terminal width
	if m.cachedPreviewRendered != "" &&
		m.previewFile == m.cachedPreviewFile &&
		info.ModTime().Equal(m.cachedPreviewMtime) &&
		m.terminalWidth == m.cachedTerminalWidth {
		return m.cachedPreviewRendered
	}

	// Check if it's a text file - use cached result if available
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

	// Generate preview
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
		visibleLen := utf8.RuneCountInString(util.StripAnsiCodes(line))
		if visibleLen > maxLineWidth {
			hasLongLines = true
			break
		}
	}

	if hasLongLines {
		for i := range lines {
			visibleLen := utf8.RuneCountInString(util.StripAnsiCodes(lines[i]))
			if visibleLen > maxLineWidth {
				truncated := util.TruncateLineWithANSI(lines[i], maxLineWidth)
				lines[i] = truncated
			}
		}
	}

	// Build styled preview with line numbers
	var previewLines []string

	// Add header with file info
	fileName := filepath.Base(m.previewFile)
	fileSize := util.FormatSize(info.Size())
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

	// Cache the rendered preview
	rendered := strings.Join(previewLines, "\n")
	m.cachedPreviewRendered = rendered
	m.cachedPreviewFile = m.previewFile
	m.cachedPreviewMtime = info.ModTime()
	m.cachedTerminalWidth = m.terminalWidth
	m.cachedIsText = true

	return rendered
}

// UpdatePreviewFromView extracts the currently selected file from filepicker's rendered view
// This allows preview to update when navigating with arrow keys, not just on Enter
func (m *FilePicker) UpdatePreviewFromView(view string) {
	if !m.IsContextMode {
		return
	}

	// Parse the view to find the currently selected file (marked with cursor)
	lines := strings.Split(view, "\n")
	for _, line := range lines {
		// Look for cursor indicator (filepicker uses > for cursor)
		if strings.Contains(line, ">") {
			// Strip ANSI escape codes
			cleanLine := util.StripAnsiCodes(line)

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
						}

						// Only update if different from current preview
						if fullPath != m.previewFile {
							m.previewFile = fullPath
							m.previewContent = m.GetFilePreviewContent(fullPath)
						}
					} else {
						if fileName != m.previewFile {
							m.previewFile = fileName
							m.previewContent = m.GetFilePreviewContent(fileName)
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

// GetPreviewContent returns the preview content for the currently selected file
func (m FilePicker) GetContent() string {
	return m.previewContent
}

// RefreshPreview refreshes the preview for the current file
func (m *FilePicker) RefreshPreview() {
	if m.previewFile != "" {
		m.previewContent = m.GetFilePreviewContent(m.previewFile)
		// Invalidate cache
		m.cachedPreviewRendered = ""
	}
}

// ClearPreview clears the preview content and cache
func (m *FilePicker) ClearPreview() {
	m.previewFile = ""
	m.previewContent = ""
	m.cachedPreviewRendered = ""
	m.cachedPreviewFile = ""
	m.cachedPreviewMtime = time.Time{}
}
