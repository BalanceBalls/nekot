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
	Model          string            `json:"model"`
	Role           string            `json:"role"`
	Content        string            `json:"content"`
	Resoning       string            `json:"reasoning"`
	Attachments    []Attachment      `json:"attachments"`
	ToolCalls      []ToolCall        `json:"tool_calls"`
	ContextChips   []FileContextChip `json:"context_chips"`
	ContextContent string            `json:"context_content"`
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
	Id       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
	Result   *string      `json:"result"`
}

type ToolFunction struct {
	Args map[string]string `json:"arguments"`
	Name string            `json:"name"`
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

// FileContextChip represents a selected file/folder context in the prompt
type FileContextChip struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	IsFolder  bool   `json:"is_folder"`
	Size      int64  `json:"size"`
	FileCount int    `json:"file_count"`
}

// MediaExtensions contains file extensions to exclude from context import
var MediaExtensions = []string{
	".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg",
	".mp4", ".avi", ".mov", ".mkv", ".webm",
	".mp3", ".wav", ".ogg", ".flac",
	".pdf", ".zip", ".tar", ".gz", ".7z", ".rar",
}

// CodeExtensions contains file extensions that should be wrapped in code blocks
var CodeExtensions = []string{
	".go", ".js", ".ts", ".py", ".java", ".c", ".cpp", ".h", ".hpp",
	".rs", ".rb", ".php", ".swift", ".kt", ".scala", ".cs", ".sh",
	".bash", ".zsh", ".fish", ".ps1", ".bat", ".cmd", ".sql", ".html",
	".css", ".scss", ".sass", ".less", ".json", ".xml", ".yaml", ".yml",
	".toml", ".ini", ".cfg", ".conf", ".md", ".markdown", ".txt",
}
