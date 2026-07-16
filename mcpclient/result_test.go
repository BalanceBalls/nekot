package mcpclient

import (
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSerializeResultKeepsTextAndStructuredContent(t *testing.T) {
	result := serializeResult(&sdk.CallToolResult{
		Content: []sdk.Content{
			&sdk.TextContent{Text: "done"},
			&sdk.ImageContent{MIMEType: "image/png", Data: []byte("ignored")},
		},
		StructuredContent: map[string]any{"count": float64(2)},
	}, nil, 4096)
	if result.IsError {
		t.Fatalf("result = %#v", result)
	}
	var envelope resultEnvelope
	if err := json.Unmarshal([]byte(result.Result), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(envelope.Text) != 1 || envelope.Text[0] != "done" || len(envelope.Warnings) != 1 {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestSerializeResultEnforcesLimit(t *testing.T) {
	result := serializeResult(&sdk.CallToolResult{
		Content: []sdk.Content{&sdk.TextContent{Text: strings.Repeat("x", 100)}},
	}, nil, 20)
	if !result.IsError || !strings.Contains(result.Result, "exceeds configured limit") {
		t.Fatalf("result = %#v", result)
	}
}
