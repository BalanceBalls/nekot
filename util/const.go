package util

import "time"

// Need manual review, should this be merged into defaults.go?
const (
	MaxReadFileSize    = 1024 * 1024            // 1MB - max file size for reading
	FilePickerDebounce = 400 * time.Millisecond // Debounce delay for file picker search
)
