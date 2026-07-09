package clients

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"iter"
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
	if len(got.Tools) != 1 ||
		got.Tools[0] != webSearchTool ||
		len(got.Tools[0].FunctionDeclarations) != 2 ||
		got.Tools[0].FunctionDeclarations[0].Name != webSearchToolName ||
		got.Tools[0].FunctionDeclarations[1].Name != currentDatetimeToolName {
		t.Fatalf("Tools = %#v, want web search and current datetime tools", got.Tools)
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
		got[1].Parts[0].FunctionCall.Args["query"] != "current info" ||
		!bytes.Equal(got[1].Parts[0].ThoughtSignature, thoughtSignature) {
		t.Fatalf("function call content = %#v", got[1])
	}

	if got[2].Role != genai.RoleUser ||
		len(got[2].Parts) != 1 ||
		got[2].Parts[0].FunctionResponse == nil ||
		got[2].Parts[0].FunctionResponse.ID != "call-1" ||
		got[2].Parts[0].FunctionResponse.Response["query"] != "current info" ||
		got[2].Parts[0].FunctionResponse.Response["result"] != toolResult {
		t.Fatalf("function response content = %#v", got[2])
	}
}

func TestBuildChatHistoryCurrentDatetimeToolExchange(t *testing.T) {
	toolResult := `{"date":"2026-07-09"}`
	thoughtSignature := []byte("signed reasoning state")
	messages := []util.LocalStoreMessage{
		{
			Role: "assistant",
			ToolCalls: []util.ToolCall{
				{
					Id:               "call-date",
					ThoughtSignature: thoughtSignature,
					Function: util.ToolFunction{
						Name: currentDatetimeToolName,
						Args: map[string]string{},
					},
				},
			},
		},
		{
			Role: "tool",
			ToolCalls: []util.ToolCall{
				{
					Id:     "call-date",
					Result: &toolResult,
					Function: util.ToolFunction{
						Name: currentDatetimeToolName,
						Args: map[string]string{},
					},
				},
			},
		},
	}

	got, err := buildChatHistory(messages, true)
	if err != nil {
		t.Fatalf("buildChatHistory() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(got))
	}

	if got[0].Role != genai.RoleModel ||
		len(got[0].Parts) != 1 ||
		got[0].Parts[0].FunctionCall == nil ||
		got[0].Parts[0].FunctionCall.ID != "call-date" ||
		got[0].Parts[0].FunctionCall.Name != currentDatetimeToolName ||
		len(got[0].Parts[0].FunctionCall.Args) != 0 {
		t.Fatalf("function call content = %#v", got[0])
	}

	if got[1].Role != genai.RoleUser ||
		len(got[1].Parts) != 1 ||
		got[1].Parts[0].FunctionResponse == nil ||
		got[1].Parts[0].FunctionResponse.ID != "call-date" ||
		got[1].Parts[0].FunctionResponse.Name != currentDatetimeToolName ||
		got[1].Parts[0].FunctionResponse.Response["result"] != toolResult {
		t.Fatalf("function response content = %#v", got[1])
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

func TestBuildChatHistorySkipsEmptyMessagesAndStrayToolResults(t *testing.T) {
	toolResult := "orphaned result"
	messages := []util.LocalStoreMessage{
		{Role: "user"},
		{
			Role: "tool",
			ToolCalls: []util.ToolCall{
				{
					Id:     "missing-call",
					Result: &toolResult,
					Function: util.ToolFunction{
						Name: "web_search",
						Args: map[string]string{"query": "current info"},
					},
				},
			},
		},
		{Role: "assistant", Content: "answer"},
	}

	got, err := buildChatHistory(messages, false)
	if err != nil {
		t.Fatalf("buildChatHistory() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(history) = %d, want 1", len(got))
	}
	if got[0].Role != genai.RoleModel ||
		len(got[0].Parts) != 1 ||
		got[0].Parts[0].Text != "answer" {
		t.Fatalf("history = %#v, want only assistant answer", got)
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

func TestProcessResponseChunkCurrentDatetimeFunctionCall(t *testing.T) {
	thoughtSignature := []byte("signed reasoning state")
	response := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{
							ThoughtSignature: thoughtSignature,
							FunctionCall: &genai.FunctionCall{
								ID:   "call-date",
								Name: currentDatetimeToolName,
								Args: map[string]any{},
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
	if toolCall.Id != "call-date" ||
		toolCall.Function.Name != currentDatetimeToolName ||
		len(toolCall.Function.Args) != 0 ||
		!bytes.Equal(toolCall.ThoughtSignature, thoughtSignature) {
		t.Fatalf("tool call = %#v", toolCall)
	}
}

func TestProcessGeminiCompletionStreamFinalizesWithCitations(t *testing.T) {
	stream := geminiResponseStream(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Index:        0,
				FinishReason: genai.FinishReasonStop,
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: "answer"}},
				},
				CitationMetadata: &genai.CitationMetadata{
					Citations: []*genai.Citation{{URI: "https://example.com"}},
				},
			},
		},
	})
	resultChan := make(chan util.ProcessApiCompletionResponse, 4)

	msg := processGeminiCompletionStream(t.Context(), stream, resultChan, 10)
	if msg != nil {
		t.Fatalf("processGeminiCompletionStream() = %#v, want nil", msg)
	}
	if len(resultChan) != 4 {
		t.Fatalf("len(resultChan) = %d, want 4", len(resultChan))
	}

	first := <-resultChan
	if first.ID != 10 ||
		first.Final ||
		first.Result.Choices[0].Delta["content"] != "answer" {
		t.Fatalf("first response = %#v", first)
	}

	citations := <-resultChan
	if citations.ID != 11 ||
		citations.Final ||
		citations.Result.Choices[0].Delta["content"] != "\n\n`Sources`\n\t> [](https://example.com)" {
		t.Fatalf("citations response = %#v", citations)
	}

	stop := <-resultChan
	if stop.ID != 12 ||
		!stop.Final ||
		stop.Result.Choices[0].FinishReason != "stop" {
		t.Fatalf("stop response = %#v", stop)
	}

	empty := <-resultChan
	if empty.ID != 13 ||
		!empty.Final ||
		empty.Result.Choices[0].FinishReason != "" {
		t.Fatalf("empty response = %#v", empty)
	}
}

func TestProcessGeminiCompletionStreamStopsOnToolCall(t *testing.T) {
	stream := geminiResponseStream(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{
							ThoughtSignature: []byte("signed reasoning state"),
							FunctionCall: &genai.FunctionCall{
								ID:   "call-4",
								Name: "web_search",
								Args: map[string]any{"query": "latest release"},
							},
						},
					},
				},
			},
		},
	})
	resultChan := make(chan util.ProcessApiCompletionResponse, 2)

	msg := processGeminiCompletionStream(t.Context(), stream, resultChan, 20)
	if msg != nil {
		t.Fatalf("processGeminiCompletionStream() = %#v, want nil", msg)
	}
	if len(resultChan) != 1 {
		t.Fatalf("len(resultChan) = %d, want only the tool call chunk", len(resultChan))
	}

	got := <-resultChan
	if got.ID != 20 ||
		len(got.Result.Choices) != 1 ||
		len(got.Result.Choices[0].ToolCalls) != 1 ||
		got.Final {
		t.Fatalf("tool call response = %#v", got)
	}
}

func geminiResponseStream(
	responses ...*genai.GenerateContentResponse,
) iter.Seq2[*genai.GenerateContentResponse, error] {
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		for _, response := range responses {
			if !yield(response, nil) {
				return
			}
		}
	}
}

func TestProcessResponseChunkMixedContentSuppressesToolCall(t *testing.T) {
	response := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{Text: "visible"},
						{
							FunctionCall: &genai.FunctionCall{
								ID:   "call-3",
								Name: "web_search",
								Args: map[string]any{"query": "ignored"},
							},
						},
					},
				},
			},
		},
	}

	got, err := processResponseChunk(response, 3)
	if err != nil {
		t.Fatalf("processResponseChunk() error = %v", err)
	}
	if got.isToolCall {
		t.Fatal("isToolCall = true, want false")
	}
	if len(got.chunk.Choices) != 1 {
		t.Fatalf("choices = %#v", got.chunk.Choices)
	}
	if len(got.chunk.Choices[0].ToolCalls) != 0 {
		t.Fatalf("tool calls = %#v, want none", got.chunk.Choices[0].ToolCalls)
	}
	if got.chunk.Choices[0].Delta != nil {
		t.Fatalf("delta = %#v, want nil when text and function call are mixed", got.chunk.Choices[0].Delta)
	}
}

func TestProcessResponseChunkThoughtOnlyPartHasEmptyContent(t *testing.T) {
	response := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{Text: "hidden", Thought: true},
					},
				},
			},
		},
	}

	got, err := processResponseChunk(response, 4)
	if err != nil {
		t.Fatalf("processResponseChunk() error = %v", err)
	}
	if got.chunk.Choices[0].Delta["content"] != "" {
		t.Fatalf("content = %#v, want empty string", got.chunk.Choices[0].Delta["content"])
	}
}

func TestHandleFinishReason(t *testing.T) {
	tests := []struct {
		name    string
		reason  genai.FinishReason
		want    string
		wantErr bool
	}{
		{name: "stop", reason: genai.FinishReasonStop, want: "stop"},
		{name: "max tokens", reason: genai.FinishReasonMaxTokens, want: "length"},
		{name: "missing", reason: "", want: ""},
		{name: "safety", reason: genai.FinishReasonSafety, want: ""},
		{name: "recitation", reason: genai.FinishReasonRecitation, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := handleFinishReason(tt.reason)
			if (err != nil) != tt.wantErr {
				t.Fatalf("handleFinishReason() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("handleFinishReason() = %q, want %q", got, tt.want)
			}
		})
	}
}
