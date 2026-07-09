package clients

import (
	"encoding/json"
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

func TestOpenRouterToolsIncludeCurrentDatetimeWhenWebSearchEnabled(t *testing.T) {
	request := openrouter.ChatCompletionRequest{}
	setRequestParams(&request, util.Settings{
		Model:            "test-model",
		MaxTokens:        128,
		WebSearchEnabled: true,
	})

	if len(request.Tools) != 2 ||
		request.Tools[0].Function.Name != webSearchToolName ||
		request.Tools[1].Function.Name != currentDatetimeToolName {
		t.Fatalf("tools = %#v, want web search and current datetime tools", request.Tools)
	}
}
