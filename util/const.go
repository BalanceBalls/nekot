package util

import "time"

// Need manual review, should this be merged into defaults.go?
const (
	MaxReadFileSize    = 1024 * 1024            // 1MB - max file size for reading
	FilePickerDebounce = 400 * time.Millisecond // Debounce delay for file picker search
)

// File picker constants
const (
	MaxPreviewFileSize    = 1024 * 1024     // 1MB - max file size for text preview, assume to be the same as read file size
	MaxPreviewContentSize = 10000           // Max characters to show in preview
	Utf8CheckBufferSize   = 1024            // Bytes to read for UTF-8 validity check
	ErrorDisplayDuration  = 2 * time.Second // Duration to show error messages
	MaxSearchResults      = 200             // Maximum number of search results to return
)
