package util

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

const maxReadFileSize = 5 * 1024 * 1024 // 5MB

// WalkDirectoryFunc is a callback function for WalkDirectory
// Return true to include the file in results, false to skip
type WalkDirectoryFunc func(path string, info fs.DirEntry, relPath string, depth int) bool

// WalkDirectory recursively walks a directory tree and calls the filter function for each entry
// maxDepth limits how deep the walk goes (0 = current dir only, 1 = one level deep, etc.)
// The filterFunc is called for each file/directory with:
//   - path: absolute file path
//   - info: the directory entry
//   - relPath: relative path from root
//   - depth: depth level (0 = root)
//
// Returns all paths where filterFunc returned true, or an error if walking fails
func WalkDirectory(rootPath string, maxDepth int, filterFunc WalkDirectoryFunc) ([]string, error) {
	var results []string

	err := filepath.WalkDir(rootPath, func(filePath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // Skip files with errors
		}

		// Skip the root directory itself
		if filePath == rootPath {
			return nil
		}

		// Calculate relative path and depth
		relPath, relErr := filepath.Rel(rootPath, filePath)
		if relErr != nil {
			return nil
		}
		depth := strings.Count(relPath, string(filepath.Separator))

		if depth > maxDepth {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// Skip hidden files and directories
		baseName := filepath.Base(filePath)
		if strings.HasPrefix(baseName, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// Call the filter function
		if filterFunc(filePath, d, relPath, depth) {
			results = append(results, filePath)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return results, nil
}

// ReadFileContent reads the content of a file and returns it as a string
// Returns an error if the file is binary, too large, or cannot be read
func ReadFileContent(path string) (string, error) {
	// Check file size before reading
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to get file info: %w", err)
	}

	// Reject files larger than maxReadFileSize
	if info.Size() > maxReadFileSize {
		return "", fmt.Errorf("file too large (%d bytes, max %d bytes)", info.Size(), maxReadFileSize)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Check if the content is valid UTF-8 (text file)
	if !utf8.Valid(content) {
		return "", fmt.Errorf("file appears to be binary")
	}

	return string(content), nil
}

// ReadFolderContents recursively reads all non-media files in a folder
// Returns a map of file paths to their contents
func ReadFolderContents(path string, maxDepth int) (map[string]string, []string, error) {
	contents := make(map[string]string)
	var filePaths []string

	err := filepath.WalkDir(path, func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if filePath == path {
			return nil
		}

		// Calculate depth
		relPath, err := filepath.Rel(path, filePath)
		if err != nil {
			return err
		}
		depth := strings.Count(relPath, string(filepath.Separator))

		if depth > maxDepth {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// Skip hidden files and directories
		baseName := filepath.Base(filePath)
		if strings.HasPrefix(baseName, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// Skip directories (we'll process files)
		if d.IsDir() {
			return nil
		}

		// Skip media files
		ext := strings.ToLower(filepath.Ext(filePath))
		if slices.Contains(MediaExtensions, ext) {
			return nil
		}

		// Read file content
		content, err := ReadFileContent(filePath)
		if err != nil {
			// Log error but continue with other files
			Slog.Warn("failed to read file in folder", "path", filePath, "error", err.Error())
			return nil
		}

		contents[filePath] = content
		filePaths = append(filePaths, filePath)

		return nil
	})

	if err != nil {
		return contents, filePaths, fmt.Errorf("failed to read folder: %w", err)
	}

	return contents, filePaths, nil
}

// IsMediaFile checks if a file is a media file based on its extension
func IsMediaFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return slices.Contains(MediaExtensions, ext)
}

// GetFileSize returns the size of a file in bytes
func GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("failed to get file size: %w", err)
	}
	return info.Size(), nil
}

// FormatFileContent formats file content for inclusion in the message
// Adds a header with the file path and wraps code files in markdown code blocks
func FormatFileContent(path, content string) string {
	ext := strings.ToLower(filepath.Ext(path))
	var formatted strings.Builder

	// Add file header
	formatted.WriteString(fmt.Sprintf("--- File: %s ---\n", path))

	// Check if it's a code file
	if slices.Contains(CodeExtensions, ext) {
		// Get language for code block
		lang := strings.TrimPrefix(ext, ".")
		formatted.WriteString(fmt.Sprintf("```%s\n", lang))
		formatted.WriteString(content)
		formatted.WriteString("\n```\n")
	} else {
		// Plain text file
		formatted.WriteString(content)
	}

	formatted.WriteString("\n---\n")

	return formatted.String()
}

// FormatFolderContents formats multiple file contents for inclusion in the message
func FormatFolderContents(contents map[string]string, filePaths []string) string {
	var formatted strings.Builder

	formatted.WriteString(fmt.Sprintf("--- Folder: %d files ---\n", len(filePaths)))

	for _, path := range filePaths {
		content, ok := contents[path]
		if ok {
			formatted.WriteString(FormatFileContent(path, content))
		}
	}

	return formatted.String()
}

// CountFilesInFolder counts the number of non-media files in a folder
func CountFilesInFolder(path string, maxDepth int) (int, error) {
	// Use WalkDirectory to count non-media, non-directory files
	results, err := WalkDirectory(path, maxDepth, func(filePath string, d fs.DirEntry, relPath string, depth int) bool {
		// Skip directories
		if d.IsDir() {
			return false
		}
		// Skip media files
		ext := strings.ToLower(filepath.Ext(filePath))
		if slices.Contains(MediaExtensions, ext) {
			return false
		}
		return true
	})

	if err != nil {
		return 0, fmt.Errorf("failed to count files in folder: %w", err)
	}

	return len(results), nil
}

// ListFolderEntries returns a list of files and folders in a directory (non-recursive)
// Returns a formatted string with file/folder names and their types
func ListFolderEntries(path string) (string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %w", err)
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("--- Folder: %s ---\n", filepath.Base(path)))

	for _, entry := range entries {
		// Skip hidden files and directories
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		if entry.IsDir() {
			result.WriteString(fmt.Sprintf("📁 %s/\n", entry.Name()))
		} else {
			// Skip media files
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if slices.Contains(MediaExtensions, ext) {
				continue
			}
			result.WriteString(fmt.Sprintf("📄 %s\n", entry.Name()))
		}
	}

	result.WriteString("---\n")
	return result.String(), nil
}
