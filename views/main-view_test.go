package views

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
)

func TestAttachmentToBase64UsesInMemoryContent(t *testing.T) {
	data := []byte("clipboard image")
	view := MainView{config: config.Config{MaxAttachmentSizeMb: 1}}
	attachment := util.Attachment{
		Path:    "clipboard-image-1.png",
		Content: base64.StdEncoding.EncodeToString(data),
		Type:    "img",
	}

	content, err := view.attachmentToBase64(attachment)
	if err != nil {
		t.Fatalf("attachmentToBase64 returned an error: %v", err)
	}
	if content != attachment.Content {
		t.Fatal("in-memory attachment content changed")
	}
}

func TestAttachmentToBase64RejectsInvalidContent(t *testing.T) {
	view := MainView{config: config.Config{MaxAttachmentSizeMb: 1}}
	attachment := util.Attachment{
		Path:    "clipboard-image-1.png",
		Content: "not base64",
		Type:    "img",
	}

	if _, err := view.attachmentToBase64(attachment); err == nil {
		t.Fatal("expected invalid attachment content error")
	}
}

func TestAttachmentToBase64RechecksSizeLimit(t *testing.T) {
	data := []byte(strings.Repeat("x", 1024*1024+1))
	view := MainView{config: config.Config{MaxAttachmentSizeMb: 1}}
	attachment := util.Attachment{
		Path:    "clipboard-image-1.png",
		Content: base64.StdEncoding.EncodeToString(data),
		Type:    "img",
	}

	if _, err := view.attachmentToBase64(attachment); err == nil {
		t.Fatal("expected attachment size error")
	}
}
