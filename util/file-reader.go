package util

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// ReadFileContent reads the content of a file and returns it as a string
// Returns an error if the file is binary or cannot be read
func ReadFileContent(path string) (string, error) {
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

// IsTextFile checks if a file is a text file by reading a small portion
// and checking if it's valid UTF-8
func IsTextFile(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	// Read first 512 bytes to check
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err.Error() != "EOF" {
		return false
	}

	// Check if the content is valid UTF-8
	return utf8.Valid(buf[:n])
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

// DetectEncoding attempts to detect if a file is text or binary
// Returns true if the file appears to be text
func DetectEncoding(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	// Read first 8192 bytes to check
	buf := make([]byte, 8192)
	n, err := file.Read(buf)
	if err != nil && err.Error() != "EOF" {
		return false
	}

	// Check for null bytes (common in binary files)
	if bytes.Contains(buf[:n], []byte{0}) {
		return false
	}

	// Check if the content is valid UTF-8
	return utf8.Valid(buf[:n])
}

// CountFilesInFolder counts the number of non-media files in a folder
func CountFilesInFolder(path string, maxDepth int) (int, error) {
	count := 0

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

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Skip media files
		ext := strings.ToLower(filepath.Ext(filePath))
		if slices.Contains(MediaExtensions, ext) {
			return nil
		}

		count++
		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("failed to count files in folder: %w", err)
	}

	return count, nil
}
