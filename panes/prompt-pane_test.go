package panes

import (
	"bytes"
	"encoding/base64"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/BalanceBalls/nekot/util"
)

func TestKeyPastePrefersClipboardImage(t *testing.T) {
	image := []byte("png image")
	textRead := false
	p := newClipboardTestPromptPane()
	p.inputMode = util.PromptNormalMode
	p.readClipboardImage = func() []byte { return image }
	p.readClipboardText = func() (string, error) {
		textRead = true
		return "clipboard text", nil
	}

	msg := p.keyPaste()()
	paste, ok := msg.(clipboardPasteMsg)
	if !ok {
		t.Fatalf("expected clipboardPasteMsg, got %T", msg)
	}
	if textRead {
		t.Fatal("text clipboard was read when an image was available")
	}

	if cmd := p.handleClipboardPaste(paste); cmd != nil {
		t.Fatal("image paste unexpectedly returned a command")
	}
	if len(p.attachments) != 1 {
		t.Fatalf("expected one attachment, got %d", len(p.attachments))
	}

	attachment := p.attachments[0]
	if attachment.Path != "clipboard-image-1.png" {
		t.Fatalf("unexpected attachment path: %s", attachment.Path)
	}
	if attachment.Type != "img" {
		t.Fatalf("unexpected attachment type: %s", attachment.Type)
	}
	decoded, err := base64.StdEncoding.DecodeString(attachment.Content)
	if err != nil {
		t.Fatalf("attachment content is not base64: %v", err)
	}
	if !bytes.Equal(decoded, image) {
		t.Fatalf("attachment content does not match clipboard image")
	}
}

func TestKeyPasteFallsBackToExistingTextPath(t *testing.T) {
	p := newClipboardTestPromptPane()
	p.readClipboardImage = func() []byte { return nil }
	p.readClipboardText = func() (string, error) {
		return " clipboard text ", nil
	}

	paste := p.keyPaste()().(clipboardPasteMsg)
	cmd := p.handleClipboardPaste(paste)
	if cmd == nil {
		t.Fatal("expected text paste command")
	}

	msg := cmd()
	textPaste, ok := msg.(tea.PasteMsg)
	if !ok {
		t.Fatalf("expected tea.PasteMsg, got %T", msg)
	}
	if textPaste.Content != "clipboard text" {
		t.Fatalf("unexpected pasted text: %q", textPaste.Content)
	}
	if len(p.attachments) != 0 {
		t.Fatal("text paste created an attachment")
	}
}

func TestClipboardImagesGetUniqueNames(t *testing.T) {
	p := newClipboardTestPromptPane()

	p.handleClipboardPaste(clipboardPasteMsg{image: []byte("first")})
	p.handleClipboardPaste(clipboardPasteMsg{image: []byte("second")})

	if len(p.attachments) != 2 {
		t.Fatalf("expected two attachments, got %d", len(p.attachments))
	}
	if p.attachments[0].Path != "clipboard-image-1.png" {
		t.Fatalf("unexpected first attachment path: %s", p.attachments[0].Path)
	}
	if p.attachments[1].Path != "clipboard-image-2.png" {
		t.Fatalf("unexpected second attachment path: %s", p.attachments[1].Path)
	}
}

func TestClipboardImageHonorsAttachmentSizeLimit(t *testing.T) {
	p := newClipboardTestPromptPane()
	p.maxAttachmentBytes = 3

	cmd := p.handleClipboardPaste(clipboardPasteMsg{image: []byte("large")})
	if cmd == nil {
		t.Fatal("expected attachment size error")
	}
	if _, ok := cmd().(util.ErrorEvent); !ok {
		t.Fatal("expected util.ErrorEvent")
	}
	if len(p.attachments) != 0 {
		t.Fatal("oversized image was attached")
	}
}

func TestSystemPromptPasteSkipsImageClipboard(t *testing.T) {
	imageRead := false
	p := newClipboardTestPromptPane()
	p.operation = util.SystemMessageEditing
	p.readClipboardImage = func() []byte {
		imageRead = true
		return []byte("image")
	}
	p.readClipboardText = func() (string, error) {
		return "system prompt text", nil
	}

	paste := p.keyPaste()().(clipboardPasteMsg)
	if imageRead {
		t.Fatal("image clipboard was read while editing the system prompt")
	}
	if paste.text != "system prompt text" {
		t.Fatalf("unexpected text fallback: %q", paste.text)
	}
}

func newClipboardTestPromptPane() PromptPane {
	return PromptPane{
		inputMode:          util.PromptInsertMode,
		operation:          util.NoOperaton,
		viewMode:           util.NormalMode,
		isSessionIdle:      true,
		isFocused:          true,
		maxAttachmentBytes: 1024,
	}
}
