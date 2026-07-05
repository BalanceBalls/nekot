package util

import (
	"sync/atomic"

	imageclipboard "golang.design/x/clipboard"
)

var imageClipboardReady atomic.Bool

// InitImageClipboard initializes optional image clipboard support.
// Text clipboard operations continue to use the existing clipboard package.
func InitImageClipboard() error {
	if err := imageclipboard.Init(); err != nil {
		return err
	}

	imageClipboardReady.Store(true)
	return nil
}

// ReadClipboardImage returns PNG-encoded clipboard image data when available.
func ReadClipboardImage() []byte {
	if !imageClipboardReady.Load() {
		return nil
	}

	return imageclipboard.Read(imageclipboard.FmtImage)
}
