package sessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/BalanceBalls/nekot/util"
)

func TestToolContinuationAllowsAnotherToolCall(t *testing.T) {
	orchestrator := Orchestrator{
		ResponseProcessingState: util.AwaitingToolCallResult,
	}
	if err := orchestrator.prepareToolContinuation(); err != nil {
		t.Fatalf("prepareToolContinuation() error = %v", err)
	}
	if orchestrator.ResponseProcessingState != util.ProcessingChunks {
		t.Fatalf(
			"processing state = %d, want %d",
			orchestrator.ResponseProcessingState,
			util.ProcessingChunks,
		)
	}

	chunk := util.ProcessApiCompletionResponse{
		ID: 1,
		Result: util.CompletionChunk{
			Choices: []util.Choice{
				{
					ToolCalls: []util.ToolCall{
						{
							Id: "call-2",
							Function: util.ToolFunction{
								Name: "web_search",
								Args: map[string]string{"query": "second query"},
							},
						},
					},
				},
			},
		},
	}

	processor := NewMessageProcessor(
		nil,
		"",
		orchestrator.ResponseProcessingState,
		util.Settings{},
	)
	result, err := processor.Process(chunk)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want one tool call", result.ToolCalls)
	}
	if result.State != util.AwaitingToolCallResult {
		t.Fatalf("processing state = %d, want %d", result.State, util.AwaitingToolCallResult)
	}
}

func TestToolContinuationRejectsInvalidState(t *testing.T) {
	orchestrator := Orchestrator{
		ResponseProcessingState: util.Idle,
	}
	if err := orchestrator.prepareToolContinuation(); err == nil {
		t.Fatal("prepareToolContinuation() error = nil, want invalid-state error")
	}
}

func TestCurrentDatetimeResultFormatsLocalTime(t *testing.T) {
	location := time.FixedZone("AMT", 4*60*60)
	now := time.Date(2026, time.July, 9, 13, 14, 15, 0, location)

	got := currentDatetimeResult(now)
	if got.Date != "2026-07-09" ||
		got.Time != "13:14:15" ||
		got.Weekday != "Thursday" ||
		got.Timezone != "AMT" ||
		got.UTCOffset != "+04:00" ||
		got.DatetimeRFC3339 != "2026-07-09T13:14:15+04:00" ||
		got.Unix != now.Unix() {
		t.Fatalf("currentDatetimeResult() = %#v", got)
	}
}

func TestDoCurrentDatetimeReturnsSuccessfulToolCall(t *testing.T) {
	orchestrator := &Orchestrator{}

	msg := orchestrator.doCurrentDatetime("call-date")()
	got, ok := msg.(ToolCallComplete)
	if !ok {
		t.Fatalf("doCurrentDatetime() returned %T, want ToolCallComplete", msg)
	}
	if got.Id != "call-date" ||
		!got.IsSuccess ||
		got.Name != "current_datetime" ||
		got.Result == "" {
		t.Fatalf("tool result = %#v", got)
	}

	var result currentDatetimeToolResult
	if err := json.Unmarshal([]byte(got.Result), &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if result.Date == "" ||
		result.Time == "" ||
		result.Weekday == "" ||
		result.Timezone == "" ||
		result.UTCOffset == "" ||
		result.DatetimeRFC3339 == "" ||
		result.Unix == 0 {
		t.Fatalf("current datetime payload = %#v", result)
	}
}
