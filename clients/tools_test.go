package clients

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
	"github.com/revrost/go-openrouter"
)

func TestOpenAIToolsIncludeCurrentDatetimeWhenWebSearchEnabled(t *testing.T) {
	includeReasoning := true
	client := NewOpenAiClient("https://api.openai.com", "")
	body, err := client.constructCompletionRequestPayload(
		nil,
		config.Config{IncludeReasoningTokensInContext: &includeReasoning},
		util.Settings{
			Model:            "gpt-test",
			MaxTokens:        128,
			WebSearchEnabled: true,
		},
		ToolsForSettings(util.Settings{WebSearchEnabled: true}, nil),
	)
	if err != nil {
		t.Fatalf("constructCompletionRequestPayload() error = %v", err)
	}

	var payload struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if len(payload.Tools) != 2 ||
		payload.Tools[0].Function.Name != webSearchToolName ||
		payload.Tools[1].Function.Name != currentDatetimeToolName {
		t.Fatalf("tools = %#v, want web search and current datetime tools", payload.Tools)
	}
}

func TestProviderToolsPreserveArbitraryJSONSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"payload": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "integer"},
					},
				},
			},
		},
	}
	tools := []util.ToolDefinition{{
		Name: "mcp__server__nested__12345678", Description: "Nested input", Parameters: schema,
	}}

	includeReasoning := true
	client := NewOpenAiClient("https://api.openai.com", "")
	body, err := client.constructCompletionRequestPayload(
		nil,
		config.Config{IncludeReasoningTokensInContext: &includeReasoning},
		util.Settings{Model: "gpt-test", MaxTokens: 128},
		tools,
	)
	if err != nil {
		t.Fatal(err)
	}
	var openAI struct {
		Tools []struct {
			Function struct {
				Parameters any `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &openAI); err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(schema)
	gotJSON, _ := json.Marshal(openAI.Tools[0].Function.Parameters)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("OpenAI schema = %s, want %s", gotJSON, wantJSON)
	}

	request := openrouter.ChatCompletionRequest{}
	setRequestParams(&request, util.Settings{Model: "test", MaxTokens: 128}, tools)
	if !reflect.DeepEqual(request.Tools[0].Function.Parameters, schema) {
		t.Fatalf("OpenRouter schema = %#v", request.Tools[0].Function.Parameters)
	}

	gemini := geminiTools(tools)
	if !reflect.DeepEqual(gemini[0].ParametersJsonSchema, schema) {
		t.Fatalf("Gemini schema = %#v", gemini[0].ParametersJsonSchema)
	}
}

func TestOpenAIAndOpenRouterToolCallsPreserveNestedArguments(t *testing.T) {
	arguments := map[string]any{
		"payload": map[string]any{
			"items": []any{float64(1), float64(2)},
			"flag":  true,
		},
	}
	call := util.ToolCall{
		Id: "call-1",
		Function: util.ToolFunction{
			Name: "mcp__server__nested__12345678",
			Args: arguments,
		},
	}
	if got := fromOpenAiToolCall(toOpenAiToolCall(call)); !reflect.DeepEqual(got.Function.Args, arguments) {
		t.Fatalf("OpenAI arguments = %#v", got.Function.Args)
	}
	if got := fromOpenRouterToolCall(toOpenRouterToolCall(call)); !reflect.DeepEqual(got.Function.Args, arguments) {
		t.Fatalf("OpenRouter arguments = %#v", got.Function.Args)
	}
}

func TestOpenRouterToolsIncludeCurrentDatetimeWhenWebSearchEnabled(t *testing.T) {
	request := openrouter.ChatCompletionRequest{}
	setRequestParams(&request, util.Settings{
		Model:            "test-model",
		MaxTokens:        128,
		WebSearchEnabled: true,
	}, ToolsForSettings(util.Settings{WebSearchEnabled: true}, nil))

	if len(request.Tools) != 2 ||
		request.Tools[0].Function.Name != webSearchToolName ||
		request.Tools[1].Function.Name != currentDatetimeToolName {
		t.Fatalf("tools = %#v, want web search and current datetime tools", request.Tools)
	}
}
