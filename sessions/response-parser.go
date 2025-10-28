package sessions

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/BalanceBalls/nekot/util"
	"golang.org/x/net/context"
)

type ParsingResult struct {
	IsSkipped        bool
	IsCancelled      bool
	HasError         bool
	IsFinished       bool
	PromptTokens     int
	CompletionTokens int
	CurrentResponse  string
	JSONResponse     util.MessageToSend
}

type MessageProcessor struct {
	Settings              util.Settings
	CurrentResponseBuffer string
	ResponseDataChunks    []util.ProcessApiCompletionResponse
}

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
	if p.shouldSkipProcessing(chunk) {
		return ParsingResult{IsSkipped: true}, nil
	}

	result := ParsingResult{}
	result, nonCancelErr := result.handleErrors(chunk)
	if nonCancelErr != nil {
		return result, nonCancelErr
	}

	result = result.handleTokenStats(chunk)
	chunk.Result.Choices[0] = p.processReasoningTags(chunk)

	if p.isFinalResponseChunk(chunk) || result.IsCancelled {
		result.IsFinished = true
		result.JSONResponse = p.prepareResponseJSONForDB()
	}

	p.ResponseDataChunks = append(p.ResponseDataChunks, chunk)
	result, bufferErr := result.rebuildResponseBuffer(p)
	if bufferErr != nil {
		return ParsingResult{}, bufferErr
	}

	return result, nil
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

func (p MessageProcessor) IsResponseFinalized() bool {
	return slices.ContainsFunc(
		p.ResponseDataChunks,
		func(c util.ProcessApiCompletionResponse) bool {
			return c.Final
		},
	)
}

func (p MessageProcessor) validateChunkIds() error {
	if !isChunkOrderingValid(getChunkIds(p.ResponseDataChunks)) {

		return errors.New(
			"chunk ids order is corrupted. Chunks amount: " + fmt.Sprintf(
				"%d",
				len(p.ResponseDataChunks),
			),
		)
	}
	return nil
}

func (r ParsingResult) handleErrors(
	chunk util.ProcessApiCompletionResponse,
) (ParsingResult, error) {
	if chunk.Err != nil {
		if errors.Is(chunk.Err, context.Canceled) {
			log.Println("Context cancelled int handleMsgProcessing")
			r.IsCancelled = true
			return r, nil
		}

		return r, chunk.Err
	}
	return r, nil
}

func (r ParsingResult) handleTokenStats(chunk util.ProcessApiCompletionResponse) ParsingResult {
	if chunk.Result.Usage != nil {
		r.PromptTokens = chunk.Result.Usage.Prompt
		r.CompletionTokens = chunk.Result.Usage.Completion
	}
	return r
}

func (r ParsingResult) rebuildResponseBuffer(
	p MessageProcessor,
) (ParsingResult, error) {

	updatedResponseBuffer := ""

	for _, chunk := range p.ResponseDataChunks {
		if p.shouldSkipProcessing(chunk) {
			continue
		}

		choiceString, ok := chunk.Result.Choices[0].Delta["content"].(string)
		if !ok {
			continue
			// r.HasError = true
			// return r, errors.New("response content is not a string")
		}

		updatedResponseBuffer = updatedResponseBuffer + choiceString
	}

	r.CurrentResponse = updatedResponseBuffer
	return r, nil
}

func (p MessageProcessor) processReasoningTags(
	chunk util.ProcessApiCompletionResponse,
) util.Choice {
	if len(chunk.Result.Choices) == 0 {
		return util.Choice{}
	}

	choice := chunk.Result.Choices[0]
	if content, ok := choice.Delta["reasoning_content"]; ok {
		chunkString := content.(string)

		if len(p.CurrentResponseBuffer) == 0 {
			chunkString = "\n" + "# Thinking..." + "\n<!------------>" + chunkString
		}

		choice.Delta["content"] = chunkString
		return choice
	}

	if content, ok := choice.Delta["content"]; ok && content != nil {

		chunkString := content.(string)

		if strings.Contains(p.CurrentResponseBuffer, "# Thinking...") &&
			!strings.Contains(p.CurrentResponseBuffer, "# Done thinking") {
			chunkString = "\n" + "# Done thinking" + "\n" + chunkString
		}

		if strings.HasPrefix(chunkString, "<think>") {
			chunkString = "\n" + "# Thinking..." + "\n<!------------>" + chunkString
		}

		if strings.Contains(chunkString, "</think>") {
			chunkString = chunkString + "\n" + "# Done thinking"
		}

		choice.Delta["content"] = chunkString
	}

	return choice
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

	modelName := "**" + p.Settings.Model + "**\n"
	newMessage.Content = modelName + newMessage.Content

	return newMessage
}

func isChunkOrderingValid(ids []int) bool {
	if len(ids) == 0 {
		return false
	}

	for i := 0; i < len(ids)-1; i++ {
		if ids[i+1] != ids[i]+1 {
			return false
		}
	}

	return true
}

func getChunkIds(arr []util.ProcessApiCompletionResponse) []int {
	ids := []int{}
	for _, msg := range arr {
		ids = append(ids, msg.ID)
	}
	return ids
}
