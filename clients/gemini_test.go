package clients

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
	"google.golang.org/genai"
)

func TestBuildGenerateContentConfig(t *testing.T) {
	topP := float32(0.8)
	temperature := float32(0.3)
	systemPrompt := "per-request prompt"

	got := buildGenerateContentConfig(
		config.Config{SystemMessage: "default prompt"},
		util.Settings{
			MaxTokens:        512,
			TopP:             &topP,
			Temperature:      &temperature,
			SystemPrompt:     &systemPrompt,
			WebSearchEnabled: true,
		},
	)

	if got.MaxOutputTokens != 512 {
		t.Fatalf("MaxOutputTokens = %d, want 512", got.MaxOutputTokens)
	}
	if got.TopP == nil || *got.TopP != topP {
		t.Fatalf("TopP = %v, want %v", got.TopP, topP)
	}
	if got.Temperature == nil || *got.Temperature != temperature {
		t.Fatalf("Temperature = %v, want %v", got.Temperature, temperature)
	}
	if len(got.Tools) != 1 || got.Tools[0] != webSearchTool {
		t.Fatalf("Tools = %#v, want webSearchTool", got.Tools)
	}
	if got.SystemInstruction == nil ||
		len(got.SystemInstruction.Parts) != 1 ||
		got.SystemInstruction.Parts[0].Text != systemPrompt {
		t.Fatalf("SystemInstruction = %#v, want %q", got.SystemInstruction, systemPrompt)
	}
}

func TestBuildChatHistory(t *testing.T) {
	toolResult := "search result"
	thoughtSignature := []byte("signed reasoning state")
	messages := []util.LocalStoreMessage{
		{
			Role:     "user",
			Resoning: "reasoning: ",
			Content:  "question",
			Attachments: []util.Attachment{
				{
					Path:    "image.png",
					Content: base64.StdEncoding.EncodeToString([]byte("image bytes")),
				},
			},
		},
		{
			Role: "assistant",
			ToolCalls: []util.ToolCall{
				{
					Id:               "call-1",
					ThoughtSignature: thoughtSignature,
					Function: util.ToolFunction{
						Name: "web_search",
						Args: map[string]string{"query": "current info"},
					},
				},
			},
		},
		{
			Role: "tool",
			ToolCalls: []util.ToolCall{
				{
					Id:     "call-1",
					Result: &toolResult,
					Function: util.ToolFunction{
						Name: "web_search",
						Args: map[string]string{"query": "current info"},
					},
				},
			},
		},
	}

	got, err := buildChatHistory(messages, true)
	if err != nil {
		t.Fatalf("buildChatHistory() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(history) = %d, want 3", len(got))
	}

	if got[0].Role != genai.RoleUser {
		t.Fatalf("user role = %q, want %q", got[0].Role, genai.RoleUser)
	}
	if len(got[0].Parts) != 2 || got[0].Parts[0].Text != "reasoning: question" {
		t.Fatalf("user parts = %#v", got[0].Parts)
	}
	if got[0].Parts[1].InlineData == nil ||
		got[0].Parts[1].InlineData.MIMEType != "image/png" ||
		string(got[0].Parts[1].InlineData.Data) != "image bytes" {
		t.Fatalf("attachment part = %#v", got[0].Parts[1])
	}

	if got[1].Role != genai.RoleModel ||
		len(got[1].Parts) != 1 ||
		got[1].Parts[0].FunctionCall == nil ||
		got[1].Parts[0].FunctionCall.ID != "call-1" ||
		!bytes.Equal(got[1].Parts[0].ThoughtSignature, thoughtSignature) {
		t.Fatalf("function call content = %#v", got[1])
	}

	if got[2].Role != genai.RoleUser ||
		len(got[2].Parts) != 1 ||
		got[2].Parts[0].FunctionResponse == nil ||
		got[2].Parts[0].FunctionResponse.ID != "call-1" ||
		got[2].Parts[0].FunctionResponse.Response["result"] != toolResult {
		t.Fatalf("function response content = %#v", got[2])
	}
}

func TestBuildChatHistorySkipsUnsignedLegacyToolExchange(t *testing.T) {
	toolResult := "search result"
	messages := []util.LocalStoreMessage{
		{Role: "user", Content: "question"},
		{
			Role: "assistant",
			ToolCalls: []util.ToolCall{
				{
					Id: "unsigned-call",
					Function: util.ToolFunction{
						Name: "web_search",
						Args: map[string]string{"query": "current info"},
					},
				},
			},
		},
		{
			Role: "tool",
			ToolCalls: []util.ToolCall{
				{
					Id:     "unsigned-call",
					Result: &toolResult,
					Function: util.ToolFunction{
						Name: "web_search",
						Args: map[string]string{"query": "current info"},
					},
				},
			},
		},
	}

	got, err := buildChatHistory(messages, false)
	if err != nil {
		t.Fatalf("buildChatHistory() error = %v", err)
	}
	if len(got) != 1 || got[0].Parts[0].Text != "question" {
		t.Fatalf("history = %#v, want only the user question", got)
	}
}

func TestToolCallThoughtSignatureJSONRoundTrip(t *testing.T) {
	want := util.ToolCall{
		Id:               "call-1",
		ThoughtSignature: []byte("signed reasoning state"),
		Function: util.ToolFunction{
			Name: "web_search",
			Args: map[string]string{"query": "current info"},
		},
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got util.ToolCall
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !bytes.Equal(got.ThoughtSignature, want.ThoughtSignature) {
		t.Fatalf("ThoughtSignature = %q, want %q", got.ThoughtSignature, want.ThoughtSignature)
	}
}

func TestProcessResponseChunk(t *testing.T) {
	response := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Index:        0,
				FinishReason: genai.FinishReasonStop,
				Content: &genai.Content{
					Parts: []*genai.Part{
						{Text: "visible"},
						{Text: "hidden", Thought: true},
					},
				},
				CitationMetadata: &genai.CitationMetadata{
					Citations: []*genai.Citation{{URI: "https://example.com"}},
				},
			},
		},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     10,
			CandidatesTokenCount: 4,
		},
	}

	got, err := processResponseChunk(response, 7)
	if err != nil {
		t.Fatalf("processResponseChunk() error = %v", err)
	}
	if !got.isFinal {
		t.Fatal("isFinal = false, want true")
	}
	if len(got.chunk.Choices) != 1 || got.chunk.Choices[0].Delta["content"] != "visible" {
		t.Fatalf("choices = %#v", got.chunk.Choices)
	}
	if got.chunk.Usage == nil || got.chunk.Usage.Prompt != 10 || got.chunk.Usage.Completion != 4 {
		t.Fatalf("usage = %#v", got.chunk.Usage)
	}
	if len(got.citations) != 1 || got.citations[0] != "\t> [](https://example.com)" {
		t.Fatalf("citations = %#v", got.citations)
	}
}

func TestProcessResponseChunkAcceptsMissingFinishReason(t *testing.T) {
	response := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: "partial"}},
				},
			},
		},
	}

	got, err := processResponseChunk(response, 1)
	if err != nil {
		t.Fatalf("processResponseChunk() error = %v", err)
	}
	if got.isFinal {
		t.Fatal("isFinal = true, want false")
	}
	if got.chunk.Choices[0].Delta["content"] != "partial" {
		t.Fatalf("content = %#v, want partial", got.chunk.Choices[0].Delta["content"])
	}
}

func TestProcessResponseChunkFunctionCall(t *testing.T) {
	thoughtSignature := []byte("signed reasoning state")
	response := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{
							ThoughtSignature: thoughtSignature,
							FunctionCall: &genai.FunctionCall{
								ID:   "call-2",
								Name: "web_search",
								Args: map[string]any{"query": "latest release"},
							},
						},
					},
				},
			},
		},
	}

	got, err := processResponseChunk(response, 2)
	if err != nil {
		t.Fatalf("processResponseChunk() error = %v", err)
	}
	if !got.isToolCall {
		t.Fatal("isToolCall = false, want true")
	}
	if len(got.chunk.Choices) != 1 || len(got.chunk.Choices[0].ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v", got.chunk.Choices)
	}
	toolCall := got.chunk.Choices[0].ToolCalls[0]
	if toolCall.Id != "call-2" ||
		toolCall.Function.Name != "web_search" ||
		toolCall.Function.Args["query"] != "latest release" ||
		!bytes.Equal(toolCall.ThoughtSignature, thoughtSignature) {
		t.Fatalf("tool call = %#v", toolCall)
	}
}
