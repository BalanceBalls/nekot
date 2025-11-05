package sessions

import (
	"errors"
	"regexp"
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
	thinkingStartText     = "# Thinking..."
	thinkingDoneText      = "# Done thinking"
	legacyThinkStartToken = "<think>"
	legacyThinkEndToken   = "</think>"
	parsingTokenStart     = "<thinkblock>"
	parsingTokenEnd       = "</thinkblock>"
	startThinkFormatting  = "\n" + thinkingStartText + "\n" + parsingTokenStart
	endThinkFormatting    = parsingTokenEnd + "\n" + thinkingDoneText + "\n"
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
	chunk.Result.Choices[0] = p.processReasoningTags(chunk)

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
	if content, ok := choice.Delta["content"]; ok && content == nil {
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

		choiceString, ok := chunk.Result.Choices[0].Delta["content"].(string)
		if ok {
			updatedResponseBuffer = updatedResponseBuffer + choiceString
		}
	}

	r.CurrentResponse = updatedResponseBuffer
	r.CurrentResponseDataChunks = p.ResponseDataChunks
	return r, nil
}

func (p MessageProcessor) isFinalChunk(msg util.ProcessApiCompletionResponse) bool {
	if msg.Final {
		return true
	}

	return false
}

func (p MessageProcessor) isLastResponseChunk(msg util.ProcessApiCompletionResponse) bool {
	choice := msg.Result.Choices[0]
	if _, ok := choice.Delta["content"]; ok && choice.FinishReason == "" {
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
	newMessage := util.MessageToSend{Role: "assistant", Content: "", Model: p.Settings.Model}

	p = p.orderChunks()
	for _, responseChunk := range p.ResponseDataChunks {
		if p.isFinalChunk(responseChunk) {
			break
		}

		if len(responseChunk.Result.Choices) == 0 {
			continue
		}

		choice := responseChunk.Result.Choices[0]
		if content, ok := choice.Delta["content"].(string); ok {
			newMessage.Content += content
		}
	}

	newMessage.Content = formatThinkingContent(newMessage.Content)
	return newMessage
}

func formatThinkingContent(text string) string {
	re := regexp.MustCompile(`(?s)` + parsingTokenStart + `(.*?)` + parsingTokenEnd)

	return re.ReplaceAllStringFunc(text, func(match string) string {
		content := strings.TrimPrefix(match, parsingTokenStart)
		content = strings.TrimSuffix(content, parsingTokenEnd)

		if content == "" {
			return match
		}

		lines := strings.Split(content, "\n")

		var builder strings.Builder
		builder.WriteString("\n")

		for i, line := range lines {
			if i > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString("<div>" + line + "</div>")
		}

		builder.WriteString("\n")
		return builder.String()
	})
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

			if content, ok := c.Result.Choices[0].Delta["content"]; ok && content != nil {
				chunkContent := content.(string)
				return strings.Contains(chunkContent, text)
			}

			return false
		},
	)
}

func (p MessageProcessor) processReasoningTags(
	chunk util.ProcessApiCompletionResponse,
) util.Choice {

	if len(chunk.Result.Choices) == 0 {
		return util.Choice{}
	}

	choice := chunk.Result.Choices[0]

	if chunkString, ok := getReasoningContent(choice.Delta); ok {
		// TODO: also check current response buffer ORRR filter out duplicates at response finalization
		if !p.anyChunkContainsText(thinkingStartText) {
			chunkString = startThinkFormatting + chunkString
		}
		choice.Delta["content"] = chunkString
		return choice
	}

	if p.isPrevChunkReasoning() {
		if content, ok := choice.Delta["content"]; ok && content != nil {
			if chunkString, isString := content.(string); isString {
				choice.Delta["content"] = endThinkFormatting + chunkString
				return choice
			}
		}
	}

	if content, ok := choice.Delta["content"]; ok && content != nil {
		if chunkString, isString := content.(string); isString {

			if strings.HasPrefix(chunkString, legacyThinkStartToken) {
				chunkString = startThinkFormatting + chunkString
			}
			if strings.Contains(chunkString, legacyThinkEndToken) {
				chunkString += endThinkFormatting
			}

			choice.Delta["content"] = chunkString
		}
	}

	return choice
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

func (p *MessageProcessor) isPrevChunkReasoning() bool {
	if len(p.ResponseDataChunks) == 0 {
		return false
	}

	prevChunk := p.ResponseDataChunks[len(p.ResponseDataChunks)-1]
	if len(prevChunk.Result.Choices) == 0 {
		return false
	}

	prevDelta := prevChunk.Result.Choices[0].Delta
	_, hasReasoningContent := prevDelta["reasoning_content"]
	_, hasReasoning := prevDelta["reasoning"]

	return hasReasoningContent || hasReasoning
}
