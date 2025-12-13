package util

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

type OpenAIConversationTurn struct {
	Model   string          `json:"model"`
	Role    string          `json:"role"`
	Content []OpenAiContent `json:"content"`
}

type OpenAiContent struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL OpenAiImage `json:"image_url,omitempty"`
}

type OpenAiImage struct {
	URL string `json:"url"`
}

type Choice struct {
	Index        int            `json:"index"`
	Delta        map[string]any `json:"delta"`
	ToolCalls    []ToolCall     `json:"tool_calls"`
	FinishReason string         `json:"finish_reason"`
}

type ToolCall struct {
	Args   map[string]string `json:"arguments"`
	Name   string            `json:"name"`
	Result *string           `json:"result"`
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

// Define a type for the data you want to return, if needed
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
