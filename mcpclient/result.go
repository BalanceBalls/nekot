package mcpclient

import (
	"encoding/json"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type resultEnvelope struct {
	Text       []string `json:"text,omitempty"`
	Structured any      `json:"structured,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
	Error      string   `json:"error,omitempty"`
	IsError    bool     `json:"isError,omitempty"`
}

func serializeResult(result *sdk.CallToolResult, callErr error, maxBytes int64) ToolExecutionResult {
	envelope := resultEnvelope{}
	if callErr != nil {
		envelope.Error = callErr.Error()
		envelope.IsError = true
	} else if result == nil {
		envelope.Error = "MCP server returned an empty tool result"
		envelope.IsError = true
	} else {
		envelope.Structured = result.StructuredContent
		envelope.IsError = result.IsError
		for _, content := range result.Content {
			switch value := content.(type) {
			case *sdk.TextContent:
				envelope.Text = append(envelope.Text, value.Text)
			case *sdk.ImageContent:
				envelope.Warnings = append(envelope.Warnings, "omitted image content")
			case *sdk.AudioContent:
				envelope.Warnings = append(envelope.Warnings, "omitted audio content")
			case *sdk.EmbeddedResource:
				envelope.Warnings = append(envelope.Warnings, "omitted embedded resource content")
			case *sdk.ResourceLink:
				envelope.Warnings = append(envelope.Warnings, "omitted resource link content")
			default:
				envelope.Warnings = append(envelope.Warnings, "omitted unsupported MCP content")
			}
		}
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to serialize MCP tool result: %v", err))
	}
	if int64(len(data)) > maxBytes {
		return errorResult(fmt.Sprintf("MCP tool result exceeds configured limit of %d bytes", maxBytes))
	}
	return ToolExecutionResult{Result: string(data), IsError: envelope.IsError}
}

func errorResult(message string) ToolExecutionResult {
	data, _ := json.Marshal(resultEnvelope{Error: message, IsError: true})
	return ToolExecutionResult{Result: string(data), IsError: true}
}
