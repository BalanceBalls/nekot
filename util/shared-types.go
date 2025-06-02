package util

type Settings struct {
	ID           int
	Model        string
	MaxTokens    int
	Frequency    *float32
	SystemPrompt *string
	TopP         *float32
	Temperature  *float32
	PresetName   string
}

type MessageToSend struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Choice struct {
	Index        int                    `json:"index"`
	Delta        map[string]interface{} `json:"delta"`
	FinishReason string                 `json:"finish_reason"`
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
