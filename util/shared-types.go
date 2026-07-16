package util

import "context"

type Settings struct {
	ID               int
	Model            string
	MaxTokens        int
	Frequency        *float32
	SystemPrompt     *string
	TopP             *float32
	Temperature      *float32
	PresetName       string
	WebSearchEnabled bool
	HideReasoning    bool
}

type LocalStoreMessage struct {
	Model       string       `json:"model"`
	Role        string       `json:"role"`
	Content     string       `json:"content"`
	Resoning    string       `json:"reasoning"`
	Attachments []Attachment `json:"attachments"`
	ToolCalls   []ToolCall   `json:"tool_calls"`
}

type Attachment struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Type    string `json:"type"`
}

type Choice struct {
	Index        int            `json:"index"`
	Delta        map[string]any `json:"delta"`
	ToolCalls    []ToolCall     `json:"tool_calls"`
	FinishReason string         `json:"finish_reason"`
}

type ToolCall struct {
	Id               string       `json:"id"`
	Type             string       `json:"type"`
	Function         ToolFunction `json:"function"`
	Result           *string      `json:"result"`
	ThoughtSignature []byte       `json:"thought_signature,omitempty"`
	Source           ToolSource   `json:"source,omitempty"`
	ServerID         string       `json:"server_id,omitempty"`
	OriginalName     string       `json:"original_name,omitempty"`
}

type ToolFunction struct {
	Args map[string]any `json:"arguments"`
	Name string         `json:"name"`
}

type ToolSource string

const (
	BuiltinToolSource ToolSource = "builtin"
	MCPToolSource     ToolSource = "mcp"
)

type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    bool   `json:"read_only_hint,omitempty"`
	DestructiveHint *bool  `json:"destructive_hint,omitempty"`
	IdempotentHint  bool   `json:"idempotent_hint,omitempty"`
	OpenWorldHint   *bool  `json:"open_world_hint,omitempty"`
}

type ToolDefinition struct {
	Name             string
	OriginalName     string
	DisplayName      string
	Description      string
	Parameters       any
	Source           ToolSource
	ServerID         string
	RequiresApproval bool
	Annotations      ToolAnnotations
}

type CompletionChunk struct {
	ID               string      `json:"id"`
	Object           string      `json:"object"`
	Created          int         `json:"created"`
	Model            string      `json:"model"`
	SystemFingerpint string      `json:"system_fingerprint"`
	Choices          []Choice    `json:"choices"`
	Usage            *TokenUsage `json:"usage"`
}

type TokenUsage struct {
	Prompt     int `json:"prompt_tokens"`
	Completion int `json:"completion_tokens"`
	Total      int `json:"total_tokens"`
}

type CompletionResponse struct {
	Data CompletionChunk `json:"data"`
}

type ModelDescription struct {
	Id      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ModelsListResponse struct {
	Object string             `json:"object"`
	Data   []ModelDescription `json:"data"`
}

type ProcessApiCompletionResponse struct {
	ID     int
	Result CompletionChunk // or whatever type you need
	Err    error
	Final  bool
}

type ProcessModelsResponse struct {
	Result ModelsListResponse
	Err    error
	Final  bool
}

type ProcessingState int

const (
	Idle ProcessingState = iota
	ProcessingChunks
	AwaitingToolCallResult
	AwaitingFinalization
	Finalized
	Error
)

func WriteToResponseChannel(ctx context.Context, ch chan<- ProcessApiCompletionResponse, msg ProcessApiCompletionResponse) {
	select {
	case ch <- msg:
	case <-ctx.Done():
		Slog.Debug("Context cancelled, skipping write to channel", "msg_id", msg.ID)
	}
}
