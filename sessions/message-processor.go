package sessions

import (
	"errors"
	"log"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/BalanceBalls/nekot/util"
	"golang.org/x/net/context"
)

type ParsingResult struct {
	IsSkipped                 bool
	IsCancelled               bool
	HasError                  bool
	IsFinished                bool
	PromptTokens              int
	CompletionTokens          int
	CurrentResponse           string
	CurrentResponseDataChunks []util.ProcessApiCompletionResponse
	JSONResponse              util.MessageToSend
}

type MessageProcessor struct {
	Settings              util.Settings
	CurrentResponseBuffer string
	ResponseDataChunks    []util.ProcessApiCompletionResponse
}

var (
	thinkingStartText     = "# Thinking..."
	thinkingDoneText      = "# Done thinking"
	legacyThinkStartToken = "<think>"
	legacyThinkEndToken   = "<think>"
	parsingTokenStart     = "<thinkblock>"
	parsingTokenEnd       = "</thinkblock>"
	startThinkFormatting  = "\n" + thinkingStartText + "\n" + parsingTokenStart
	endThinkFormatting    = parsingTokenEnd + "\n" + thinkingDoneText + "\n"
)

func NewMessageProcessor(
	chunks []util.ProcessApiCompletionResponse,
	currentResponse string,
	settings util.Settings,
) MessageProcessor {
	return MessageProcessor{
		Settings:              settings,
		CurrentResponseBuffer: currentResponse,
		ResponseDataChunks:    chunks,
	}
}

func (p MessageProcessor) Process(
	chunk util.ProcessApiCompletionResponse,
) (ParsingResult, error) {

	result := ParsingResult{}
	result, nonCancelErr := result.handleErrors(chunk)
	if nonCancelErr != nil {
		return result, nonCancelErr
	}

	if result.IsCancelled {
		// Save already received chunks
		result.JSONResponse = p.prepareResponseJSONForDB()
		return result, nil
	}

	result = result.handleTokenStats(chunk)
	if p.shouldSkipProcessing(chunk) || p.isDuplicate(chunk) {
		result.IsSkipped = true
		return result, nil
	}

	chunk.Result.Choices[0] = p.processReasoningTags(chunk)

	if p.isFinalResponseChunk(chunk) {
		result.IsFinished = true
		result.JSONResponse = p.prepareResponseJSONForDB()
	}

	result, bufferErr := result.composeProcessingResult(p, chunk)
	if bufferErr != nil {
		return ParsingResult{}, bufferErr
	}

	return result, nil
}

func (p MessageProcessor) orderChunks() MessageProcessor {
	sort.Slice(p.ResponseDataChunks, func(i, j int) bool {
		return p.ResponseDataChunks[i].ID < p.ResponseDataChunks[j].ID
	})
	return p
}

func (p MessageProcessor) isDuplicate(chunk util.ProcessApiCompletionResponse) bool {
	if slices.ContainsFunc(p.ResponseDataChunks, func(c util.ProcessApiCompletionResponse) bool {
		return c.ID == chunk.ID
	}) {
		log.Println("there is already a chunk with id: ", chunk.ID)
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
	if _, ok := choice.Delta["role"]; ok {
		return true
	}

	return false
}

func (p MessageProcessor) IsResponseFinalized() bool {
	return slices.ContainsFunc(
		p.ResponseDataChunks,
		func(c util.ProcessApiCompletionResponse) bool {
			return c.Final
		},
	)
}

func (r ParsingResult) handleErrors(
	chunk util.ProcessApiCompletionResponse,
) (ParsingResult, error) {
	if chunk.Err == nil {
		return r, nil
	}

	if errors.Is(chunk.Err, context.Canceled) {
		log.Println("Context cancelled int handleMsgProcessing")
		r.IsCancelled = true
		return r, nil
	}

	return r, chunk.Err
}

func (r ParsingResult) handleTokenStats(chunk util.ProcessApiCompletionResponse) ParsingResult {
	if chunk.Result.Usage != nil {
		r.PromptTokens = chunk.Result.Usage.Prompt
		r.CompletionTokens = chunk.Result.Usage.Completion
	}
	return r
}

func (r ParsingResult) composeProcessingResult(
	p MessageProcessor,
	newChunk util.ProcessApiCompletionResponse,
) (ParsingResult, error) {

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

func (p MessageProcessor) isFinalResponseChunk(msg util.ProcessApiCompletionResponse) bool {
	if msg.Final {
		return true
	}

	choice := msg.Result.Choices[0]
	if _, ok := choice.Delta["content"]; ok && choice.FinishReason == "" {
		return false
	}

	if choice.FinishReason == "stop" || choice.FinishReason == "length" {
		log.Println("FinishReason received:", choice.FinishReason)
		return true
	}

	data, contentOk := choice.Delta["content"]
	log.Printf(
		"Failed to check if response is finished. Finish reason: %s  ;\n HasContent: %t ;\n Content: %s",
		choice.FinishReason,
		contentOk,
		data,
	)
	return false
}

func (p MessageProcessor) prepareResponseJSONForDB() util.MessageToSend {
	newMessage := util.MessageToSend{Role: "assistant", Content: ""}

	for _, responseChunk := range p.ResponseDataChunks {
		if len(responseChunk.Result.Choices) == 0 {
			continue
		}

		if p.isFinalResponseChunk(responseChunk) {
			break
		}

		choice := responseChunk.Result.Choices[0]
		if content, ok := choice.Delta["content"].(string); ok {
			newMessage.Content += content
		}
	}

	modelName := "**" + p.Settings.Model + "**"
	shouldSetName := newMessage.Content != "" && !strings.Contains(newMessage.Content, modelName)

	if shouldSetName {
		log.Println("model name has been set")
		newMessage.Content = modelName + "\n" + newMessage.Content
	}

	newMessage.Content = formatThinkingContent(newMessage.Content)
	return newMessage
}

func formatThinkingContent(text string) string {
	re := regexp.MustCompile(`(?s)<thinkblock>(.*?)</thinkblock>`)

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

	return slices.ContainsFunc(p.ResponseDataChunks, func(c util.ProcessApiCompletionResponse) bool {
		if len(c.Result.Choices) == 0 {
			return false
		}

		if content, ok := c.Result.Choices[0].Delta["content"]; ok && content != nil {
			chunkContent := content.(string)
			return strings.Contains(chunkContent, text)
		}

		return false
	})
}

func (p MessageProcessor) processReasoningTags(
	chunk util.ProcessApiCompletionResponse,
) util.Choice {

	if len(chunk.Result.Choices) == 0 {
		return util.Choice{}
	}

	choice := chunk.Result.Choices[0]

	if chunkString, ok := getReasoningContent(choice.Delta); ok {
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
