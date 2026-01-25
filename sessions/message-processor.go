package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/BalanceBalls/nekot/util"
)

type ProcessingResult struct {
	IsSkipped                 bool
	IsCancelled               bool
	HasError                  bool
	PromptTokens              int
	CompletionTokens          int
	CurrentResponse           string
	CurrentResponseDataChunks []util.ProcessApiCompletionResponse
	ToolCalls                 []util.ToolCall
	JSONResponse              util.LocalStoreMessage
	State                     util.ProcessingState
}

type MessageProcessor struct {
	CurrentState          util.ProcessingState
	Settings              util.Settings
	CurrentResponseBuffer string
	ResponseDataChunks    []util.ProcessApiCompletionResponse
}

var (
	legacyThinkStartToken = "<think>"
	legacyThinkEndToken   = "</think>"
)

func NewMessageProcessor(
	chunks []util.ProcessApiCompletionResponse,
	currentResponse string,
	processingState util.ProcessingState,
	settings util.Settings,
) MessageProcessor {
	return MessageProcessor{
		Settings:              settings,
		CurrentResponseBuffer: currentResponse,
		ResponseDataChunks:    chunks,
		CurrentState:          processingState,
	}
}

var stageChangeMap = map[util.ProcessingState][]util.ProcessingState{
	util.Idle:                   {util.ProcessingChunks, util.AwaitingToolCallResult, util.Error},
	util.ProcessingChunks:       {util.AwaitingFinalization, util.AwaitingToolCallResult, util.Finalized, util.Error},
	util.AwaitingToolCallResult: {util.ProcessingChunks, util.AwaitingFinalization, util.Finalized, util.Error},
	util.AwaitingFinalization:   {util.Finalized, util.Error},
	util.Finalized:              {util.AwaitingToolCallResult, util.Idle},
}

func (p MessageProcessor) Process(
	chunk util.ProcessApiCompletionResponse,
) (ProcessingResult, error) {

	result := ProcessingResult{
		State:                     p.CurrentState,
		CurrentResponse:           p.CurrentResponseBuffer,
		CurrentResponseDataChunks: p.ResponseDataChunks,
	}

	result, nonCancelErr := p.handleErrors(result, chunk)
	if nonCancelErr != nil {
		return result, nonCancelErr
	}

	result = result.handleTokenStats(chunk)

	if result.IsCancelled {
		result = p.finalizeProcessing(result)
		return result, nil
	}

	if p.isDuplicate(chunk) {
		result.IsSkipped = true
		util.Slog.Debug("skipped duplicate chunk", "id", chunk.ID)
		return result, nil
	}

	if toolCalls, ok := p.hasToolCalls(chunk); ok {
		result.ToolCalls = toolCalls
		result.CurrentResponseDataChunks = append(p.ResponseDataChunks, chunk)
		result = p.prepareToolCallInterruption(result, chunk)
		util.Slog.Debug("tool calls detected", "tools", toolCalls)
		return result, nil
	}

	result.ToolCalls = nil

	if p.isFinalChunk(chunk) {
		result.CurrentResponseDataChunks = append(p.ResponseDataChunks, chunk)
		result = p.finalizeProcessing(result)
		return result, nil
	}

	if p.shouldSkipProcessing(chunk) {
		result.IsSkipped = true
		result.CurrentResponseDataChunks = append(p.ResponseDataChunks, chunk)
		return result, nil
	}

	if p.isLastResponseChunk(chunk) {
		result.State = p.setProcessingState(util.AwaitingFinalization, result)
	} else {
		result.State = p.setProcessingState(util.ProcessingChunks, result)
	}

	result, bufferErr := result.composeProcessingResult(p, chunk)
	if bufferErr != nil {
		return ProcessingResult{}, bufferErr
	}

	return result, nil
}

func (p MessageProcessor) finalizeProcessing(result ProcessingResult) ProcessingResult {
	result.JSONResponse = p.prepareResponseJSONForDB(nil)
	result.State = p.setProcessingState(util.Finalized, result)
	return result
}

func (p MessageProcessor) prepareToolCallInterruption(
	result ProcessingResult,
	chunk util.ProcessApiCompletionResponse) ProcessingResult {
	result.JSONResponse = p.prepareResponseJSONForDB(&chunk)
	result.State = p.setProcessingState(util.AwaitingToolCallResult, result)
	return result
}

func (p MessageProcessor) orderChunks() MessageProcessor {
	sort.Slice(p.ResponseDataChunks, func(i, j int) bool {
		return p.ResponseDataChunks[i].ID < p.ResponseDataChunks[j].ID
	})
	return p
}

func (p MessageProcessor) setProcessingState(newState util.ProcessingState, result ProcessingResult) util.ProcessingState {
	if p.CurrentState == newState {
		return newState
	}

	if p.CurrentState == util.AwaitingToolCallResult && newState == util.Finalized && !result.IsCancelled {
		util.Slog.Warn("this state change only allowed for cancellation", "old state", p.CurrentState, "new state", newState)
		return p.CurrentState
	}

	if slices.Contains(stageChangeMap[p.CurrentState], newState) {
		util.Slog.Debug("state changed", "old state", p.CurrentState, "new state", newState)
		return newState
	}

	util.Slog.Warn("state change not allowed", "old state", p.CurrentState, "new state", newState)
	return p.CurrentState
}

func (p MessageProcessor) isDuplicate(chunk util.ProcessApiCompletionResponse) bool {
	if slices.ContainsFunc(p.ResponseDataChunks, func(c util.ProcessApiCompletionResponse) bool {
		return c.ID == chunk.ID
	}) {
		util.Slog.Debug("there is already a chunk with such id", "id", chunk.ID)
		return true
	}
	return false
}

func (p MessageProcessor) hasToolCalls(chunk util.ProcessApiCompletionResponse) ([]util.ToolCall, bool) {

	if len(chunk.Result.Choices) == 0 {
		return []util.ToolCall{}, false
	}

	if p.CurrentState == util.AwaitingToolCallResult {
		return []util.ToolCall{}, false
	}

	choice := chunk.Result.Choices[0]
	if toolCalls, ok := getToolCalls(choice.Delta); ok {
		return toolCalls, true
	}

	if len(choice.ToolCalls) > 0 {
		return choice.ToolCalls, true
	}

	return []util.ToolCall{}, false
}

func (p MessageProcessor) shouldSkipProcessing(chunk util.ProcessApiCompletionResponse) bool {

	if len(chunk.Result.Choices) == 0 {
		return true
	}

	choice := chunk.Result.Choices[0]
	hasFinishReason := choice.FinishReason != ""
	_, hasReasoning := getReasoningContent(choice.Delta)
	_, hasContent := getContent(choice.Delta)

	if !hasContent && !hasReasoning && !hasFinishReason {
		return true
	}

	return false
}

func (p MessageProcessor) handleErrors(
	result ProcessingResult,
	chunk util.ProcessApiCompletionResponse,
) (ProcessingResult, error) {
	if chunk.Err == nil {
		return result, nil
	}

	if errors.Is(chunk.Err, context.Canceled) {
		util.Slog.Debug("context cancelled int handleMsgProcessing")
		result.IsCancelled = true
		return result, nil
	}

	result.State = p.setProcessingState(util.Error, result)
	return result, chunk.Err
}

func (r ProcessingResult) handleTokenStats(chunk util.ProcessApiCompletionResponse) ProcessingResult {
	if chunk.Result.Usage != nil {
		r.PromptTokens = chunk.Result.Usage.Prompt
		r.CompletionTokens = chunk.Result.Usage.Completion
	}
	return r
}

func (r ProcessingResult) composeProcessingResult(
	p MessageProcessor,
	newChunk util.ProcessApiCompletionResponse,
) (ProcessingResult, error) {

	updatedResponseBuffer := p.CurrentResponseBuffer
	p.ResponseDataChunks = append(p.ResponseDataChunks, newChunk)

	if p.shouldSkipProcessing(newChunk) {
		r.CurrentResponse = updatedResponseBuffer
		r.CurrentResponseDataChunks = p.ResponseDataChunks
		return r, nil
	}

	if reasoning, ok := p.getChunkReasoningData(newChunk, p.ResponseDataChunks); ok {
		updatedResponseBuffer = updatedResponseBuffer + reasoning
	}

	if choiceString, ok := getContent(newChunk.Result.Choices[0].Delta); ok {
		updatedResponseBuffer = updatedResponseBuffer + choiceString
	}

	r.CurrentResponse = updatedResponseBuffer
	r.CurrentResponseDataChunks = p.ResponseDataChunks
	return r, nil
}

func (p MessageProcessor) isFinalChunk(msg util.ProcessApiCompletionResponse) bool {
	return msg.Final && p.CurrentState == util.AwaitingFinalization
}

func (p MessageProcessor) isLastResponseChunk(msg util.ProcessApiCompletionResponse) bool {
	choice := msg.Result.Choices[0]
	if _, ok := getReasoningContent(choice.Delta); ok {
		return false
	}

	if _, ok := getContent(choice.Delta); ok && choice.FinishReason == "" {
		return false
	}

	if _, ok := getToolCalls(choice.Delta); ok {
		return false
	}

	if choice.FinishReason == "tool_calls" {
		return false
	}

	if choice.FinishReason == "stop" || choice.FinishReason == "length" {
		util.Slog.Debug("response finish reason received", "reason", choice.FinishReason)
		return true
	}

	data, contentOk := choice.Delta["content"]
	util.Slog.Debug(
		"failed to check if response is finished",
		"finish reason", choice.FinishReason,
		"has content", contentOk,
		"content", data,
	)
	return false
}

func (p MessageProcessor) prepareResponseJSONForDB(currentChunk *util.ProcessApiCompletionResponse) util.LocalStoreMessage {
	newMessage := util.LocalStoreMessage{
		Role:    "assistant",
		Content: "",
		Model:   p.Settings.Model}

	if currentChunk != nil {
		p.ResponseDataChunks = append(p.ResponseDataChunks, *currentChunk)
	}

	p = p.orderChunks()
	processed := []util.ProcessApiCompletionResponse{}
	for _, responseChunk := range p.ResponseDataChunks {
		processed = append(processed, responseChunk)

		if len(responseChunk.Result.Choices) == 0 {
			continue
		}

		choice := responseChunk.Result.Choices[0]
		if reasoning, ok := p.getChunkReasoningData(responseChunk, processed); ok {
			newMessage.Resoning += reasoning
		}

		if content, ok := getContent(choice.Delta); ok && content != "" {
			newMessage.Content += content
		}

		if toolCalls, ok := p.hasToolCalls(responseChunk); ok {
			newMessage.ToolCalls = toolCalls
		}
	}

	// newMessage.Resoning = formatThinkingContent(newMessage.Resoning)
	return newMessage
}

func formatThinkingContent(text string) string {
	text = strings.ReplaceAll(text, legacyThinkStartToken, "")
	text = strings.ReplaceAll(text, legacyThinkEndToken, "")

	return text
}

func anyChunkContainsText(chunks []util.ProcessApiCompletionResponse, text string) bool {
	if len(chunks) == 0 {
		return false
	}

	return slices.ContainsFunc(
		chunks,
		func(c util.ProcessApiCompletionResponse) bool {
			if len(c.Result.Choices) == 0 {
				return false
			}

			delta := c.Result.Choices[0].Delta
			contentMatch := false
			reasoningMatch := false
			if content, ok := getContent(delta); ok {
				contentMatch = strings.Contains(content, text)
			}

			if reasoning, ok := getReasoningContent(delta); ok {
				reasoningMatch = strings.Contains(reasoning, text)
			}

			if contentMatch || reasoningMatch {
				return true
			}

			return false
		},
	)
}

func (p MessageProcessor) getChunkReasoningData(
	chunk util.ProcessApiCompletionResponse,
	previousChunks []util.ProcessApiCompletionResponse,
) (string, bool) {
	if len(chunk.Result.Choices) == 0 {
		return "", false
	}

	choice := chunk.Result.Choices[0]

	// check for reasoning and reasoning_content fields in response
	if reasoning, ok := getReasoningContent(choice.Delta); ok {
		return reasoning, true
	}

	if _, ok := getContent(choice.Delta); !ok {
		return "", false
	}

	chunkString, ok := getContent(choice.Delta)
	if !ok {
		return "", false
	}

	// if chunk specifically contains <think> or </think> tokens
	if strings.Contains(chunkString, legacyThinkStartToken) {
		return chunkString, true
	}
	if strings.Contains(chunkString, legacyThinkEndToken) {
		return chunkString, true
	}

	// previous chunks have <think> token but no closing token </think>
	if anyChunkContainsText(previousChunks, legacyThinkStartToken) &&
		!anyChunkContainsText(previousChunks, legacyThinkEndToken) {
		return chunkString, true
	}

	// any chunk contains closing token </think>
	if anyChunkContainsText(previousChunks, legacyThinkEndToken) {
		return "", false
	}

	return "", false
}

func getContent(delta map[string]any) (string, bool) {
	if content, ok := delta["content"]; ok && content != nil {
		if strContent, isString := content.(string); isString {
			return strContent, true
		}
	}
	return "", false
}

func getToolCalls(delta map[string]any) ([]util.ToolCall, bool) {
	if content, ok := delta["tool_calls"]; ok && content != nil {
		if strContent, isString := content.(string); isString {

			var toolCalls []util.ToolCall
			err := json.Unmarshal([]byte(strContent), &toolCalls)
			if err != nil {
				util.Slog.Error("error unmarshalling JSON", "reason", err, "data", strContent)
				return []util.ToolCall{}, false
			}
			return toolCalls, true
		}
	}
	return []util.ToolCall{}, false
}

func getReasoningContent(delta map[string]any) (string, bool) {
	for _, key := range []string{"reasoning_content", "reasoning"} {
		if content, ok := delta[key]; ok {
			if strContent, isString := content.(string); isString {
				return strContent, true
			}
		}
	}

	return "", false
}
