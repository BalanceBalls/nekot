package sessions

import (
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/BalanceBalls/nekot/util"
	"golang.org/x/net/context"
)

type ProcessingResult struct {
	IsSkipped                 bool
	IsCancelled               bool
	HasError                  bool
	PromptTokens              int
	CompletionTokens          int
	CurrentResponse           string
	CurrentResponseDataChunks []util.ProcessApiCompletionResponse
	JSONResponse              util.MessageToSend
	State                     ProcessingState
}

type MessageProcessor struct {
	CurrentState          ProcessingState
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
	processingState ProcessingState,
	settings util.Settings,
) MessageProcessor {
	return MessageProcessor{
		Settings:              settings,
		CurrentResponseBuffer: currentResponse,
		ResponseDataChunks:    chunks,
		CurrentState:          processingState,
	}
}

type ProcessingState int

const (
	Idle ProcessingState = iota
	ProcessingChunks
	AwaitingFinalization
	Finalized
	Error
)

var stageChangeMap = map[ProcessingState][]ProcessingState{
	Idle:                 {ProcessingChunks, Error},
	ProcessingChunks:     {AwaitingFinalization, Finalized, Error},
	AwaitingFinalization: {Finalized, Error},
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

	if result.IsCancelled {
		result = p.finalizeProcessing(result)
		return result, nil
	}

	if p.isDuplicate(chunk) {
		result.IsSkipped = true
		util.Slog.Debug("skipped duplicate chunk", "id", chunk.ID)
		return result, nil
	}

	result = result.handleTokenStats(chunk)

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

	result.State = p.setProcessingState(ProcessingChunks)

	if p.isLastResponseChunk(chunk) {
		result.State = p.setProcessingState(AwaitingFinalization)
	}

	result, bufferErr := result.composeProcessingResult(p, chunk)
	if bufferErr != nil {
		return ProcessingResult{}, bufferErr
	}

	return result, nil
}

func (p MessageProcessor) finalizeProcessing(result ProcessingResult) ProcessingResult {
	result.JSONResponse = p.prepareResponseJSONForDB()
	result.State = p.setProcessingState(Finalized)
	return result
}

func (p MessageProcessor) orderChunks() MessageProcessor {
	sort.Slice(p.ResponseDataChunks, func(i, j int) bool {
		return p.ResponseDataChunks[i].ID < p.ResponseDataChunks[j].ID
	})
	return p
}

func (p MessageProcessor) setProcessingState(newState ProcessingState) ProcessingState {
	if p.CurrentState == newState {
		return newState
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

func (p MessageProcessor) shouldSkipProcessing(chunk util.ProcessApiCompletionResponse) bool {
	if chunk.Result.Choices == nil {
		return true
	}

	if len(chunk.Result.Choices) == 0 {
		return true
	}

	choice := chunk.Result.Choices[0]

	_, hasReasoning := getReasoningContent(choice.Delta)
	_, hasContent := getContent(choice.Delta)

	if !hasContent && !hasReasoning {
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

	result.State = p.setProcessingState(Error)
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

	updatedResponseBuffer := ""

	p.ResponseDataChunks = append(p.ResponseDataChunks, newChunk)
	p = p.orderChunks()

	for _, chunk := range p.ResponseDataChunks {
		if p.shouldSkipProcessing(chunk) {
			continue
		}

		if choiceString, ok := getContent(chunk.Result.Choices[0].Delta); ok {
			updatedResponseBuffer = updatedResponseBuffer + choiceString
		}

		if reasoning, ok := p.getChunkReasoningData(chunk); ok {
			updatedResponseBuffer = updatedResponseBuffer + reasoning
		}
	}

	r.CurrentResponse = updatedResponseBuffer
	r.CurrentResponseDataChunks = p.ResponseDataChunks
	return r, nil
}

func (p MessageProcessor) isFinalChunk(msg util.ProcessApiCompletionResponse) bool {
	return msg.Final
}

func (p MessageProcessor) isLastResponseChunk(msg util.ProcessApiCompletionResponse) bool {
	choice := msg.Result.Choices[0]
	if _, ok := getContent(choice.Delta); ok && choice.FinishReason == "" {
		return false
	}

	if choice.FinishReason == "stop" || choice.FinishReason == "length" {
		util.Slog.Debug("response finish reason received", "reason", choice.FinishReason)
		return true
	}

	data, contentOk := choice.Delta["content"]
	util.Slog.Error(
		"failed to check if response is finished",
		"finish reason", choice.FinishReason,
		"has content", contentOk,
		"content", data,
	)
	return false
}

func (p MessageProcessor) prepareResponseJSONForDB() util.MessageToSend {
	newMessage := util.MessageToSend{
		Role:    "assistant",
		Content: "",
		Model:   p.Settings.Model}

	p = p.orderChunks()
	for _, responseChunk := range p.ResponseDataChunks {
		if p.isFinalChunk(responseChunk) {
			break
		}

		if len(responseChunk.Result.Choices) == 0 {
			continue
		}

		choice := responseChunk.Result.Choices[0]
		if content, ok := getContent(choice.Delta); ok && content != "" {
			newMessage.Content += content
			continue
		}

		if reasoning, ok := p.getChunkReasoningData(responseChunk); ok {
			newMessage.Resoning += reasoning
		}
	}

	newMessage.Resoning = formatThinkingContent(newMessage.Resoning)
	return newMessage
}

func formatThinkingContent(text string) string {
	text = strings.ReplaceAll(text, legacyThinkStartToken, "")
	text = strings.ReplaceAll(text, legacyThinkEndToken, "")

	return text
}

func (p MessageProcessor) anyChunkContainsText(text string) bool {
	if len(p.ResponseDataChunks) == 0 {
		return false
	}

	return slices.ContainsFunc(
		p.ResponseDataChunks,
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
	if strings.HasPrefix(chunkString, legacyThinkStartToken) {
		return chunkString, true
	}
	if strings.Contains(chunkString, legacyThinkEndToken) {
		return chunkString, true
	}

	// previous chunks have <think> token but no closing token </think>
	if p.anyChunkContainsText(legacyThinkStartToken) &&
		!p.anyChunkContainsText(legacyThinkEndToken) {
		return chunkString, true
	}

	// any chunk contains closing token </think>
	if p.anyChunkContainsText(legacyThinkEndToken) {
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
