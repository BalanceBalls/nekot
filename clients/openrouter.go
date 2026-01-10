package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/revrost/go-openrouter"
)

var openRouterwebSearchTool = openrouter.Tool{
	Type: openrouter.ToolTypeFunction,
	Function: &openrouter.FunctionDefinition{
		Name:        "web_search",
		Description: "Perform a web search to retrieve up to date info or piece of knowledge you have doubts about.",
		Parameters: OpenAiFuncitonParameters{
			Type:     "object",
			Required: []string{"query"},
			Properties: map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query string. Should be very specific and moderately detailed for accurate retrieval.",
				},
			},
		},
	},
}

type OpenrouterClient struct {
	systemMessage string
}

type OpenRouterToolCallsBuffer struct {
	Chunks []openrouter.ToolCall
}

func NewOpenrouterClient(systemMessage string) *OpenrouterClient {
	return &OpenrouterClient{
		systemMessage: systemMessage,
	}
}

func (c OpenrouterClient) RequestCompletion(
	ctx context.Context,
	chatMsgs []util.LocalStoreMessage,
	modelSettings util.Settings,
	resultChan chan util.ProcessApiCompletionResponse,
) tea.Cmd {

	return func() tea.Msg {
		config, ok := config.FromContext(ctx)
		if !ok {
			fmt.Println("No config found")
			panic("No config found in context")
		}

		client := openrouter.NewClient(os.Getenv("OPENROUTER_API_KEY"))

		request := openrouter.ChatCompletionRequest{}
		setRequestParams(&request, modelSettings)
		setRequestContext(&request, *config, modelSettings, chatMsgs)

		stream, err := client.CreateChatCompletionStream(ctx, request)
		if err != nil {
			resultChan <- util.ProcessApiCompletionResponse{ID: util.ChunkIndexStart, Err: err, Final: true}
			return nil
		}
		defer stream.Close()

		util.Slog.Debug("constructing message", "model", modelSettings.Model)

		processResultID := util.GetNextProcessResultId(chatMsgs)
		toolCallsBuffer := OpenRouterToolCallsBuffer{
			Chunks: []openrouter.ToolCall{},
		}

		for {
			response, err := stream.Recv()

			if err != nil && err != io.EOF {
				util.Slog.Error(
					"Openrouter: Encountered error while receiving response",
					"error",
					err.Error(),
				)
				resultChan <- util.ProcessApiCompletionResponse{ID: processResultID, Err: err, Final: true}
				break
			}

			processResultID++
			if errors.Is(err, io.EOF) {
				util.Slog.Info("Openrouter: Received [DONE]")
				resultChan <- util.ProcessApiCompletionResponse{ID: processResultID, Err: nil, Final: true}
				break
			}

			util.Slog.Debug("going through chunk", "data", response.Choices)
			if isOpenRouterToolCall(response, toolCallsBuffer) {
				toolCallChunk, isReady := toolCallsBuffer.handleOpenRouterToolCallChunk(response)
				if !isReady {
					continue
				}

				util.Slog.Info("OpenRouter: Tool call interruption sent")
				resultChan <- util.ProcessApiCompletionResponse{
					ID:     processResultID,
					Result: toolCallChunk,
					Err:    nil,
					Final:  false,
				}

				processResultID++
				resultChan <- util.ProcessApiCompletionResponse{ID: processResultID, Err: nil, Final: true}
				break
			}

			result, err := processCompletionChunk(response)
			if err != nil {
				resultChan <- util.ProcessApiCompletionResponse{ID: processResultID, Err: err}
				break
			}

			resultChan <- util.ProcessApiCompletionResponse{
				ID:     processResultID,
				Result: result,
				Err:    nil,
				Final:  false,
			}
		}

		return nil
	}
}

func (c OpenrouterClient) RequestModelsList(ctx context.Context) util.ProcessModelsResponse {
	client := openrouter.NewClient(os.Getenv("OPENROUTER_API_KEY"))

	client.ListUserModels(ctx)
	models, err := client.ListModels(ctx)

	if err != nil {
		util.Slog.Error("failed to fetch models", "error", err.Error())
		return util.ProcessModelsResponse{Err: err}
	}

	if ctx.Err() == context.DeadlineExceeded {
		return util.ProcessModelsResponse{Err: errors.New("timed out during fetching models")}
	}

	var modelsList []util.ModelDescription
	for _, model := range models {
		m := util.ModelDescription{
			Id:      model.ID,
			Created: model.Created,
		}
		modelsList = append(modelsList, m)
	}

	return util.ProcessModelsResponse{
		Result: util.ModelsListResponse{
			Data: modelsList,
		},
		Err: nil,
	}
}

func constructOpenrouterToolCalls(msg util.LocalStoreMessage) []openrouter.ChatCompletionMessage {
	toolCallTurns := []openrouter.ChatCompletionMessage{}
	for _, tc := range msg.ToolCalls {
		util.Slog.Debug("appending tool call request", "data", tc)

		if tc.Result == nil {
			continue
		}

		toolResult := openrouter.ChatCompletionMessage{
			Role: openrouter.ChatMessageRoleTool,
			//ToolCalls:  []openrouter.ToolCall{toOpenRouterToolCall(tc)},
			Content:    openrouter.Content{Text: *tc.Result},
			ToolCallID: tc.Id,
		}

		toolCallTurns = append(toolCallTurns, toolResult)
	}

	util.Slog.Debug("constructed tool calls", "data", toolCallTurns)
	return toolCallTurns
}

func constructOpenrouterMessage(msg util.LocalStoreMessage) openrouter.ChatCompletionMessage {
	isUserMsg := msg.Role == "user"

	if len(msg.Attachments) == 0 && len(msg.ToolCalls) == 0 {
		if isUserMsg {
			return openrouter.UserMessage(msg.Content)
		}

		return openrouter.AssistantMessage(msg.Content)
	}

	message := openrouter.ChatCompletionMessage{
		Role: "user",
		Content: openrouter.Content{
			Multi: []openrouter.ChatMessagePart{
				{
					Type: "text",
					Text: msg.Content,
				},
			},
		},
	}

	if len(msg.Attachments) > 0 {
		for _, attachment := range msg.Attachments {
			data := getImageURLString(attachment)

			part := openrouter.ChatMessagePart{
				Type: "image_url",
				ImageURL: &openrouter.ChatMessageImageURL{
					URL: data,
				},
			}

			message.Content.Multi = append(message.Content.Multi, part)
		}
	}

	return message
}

func setRequestContext(
	r *openrouter.ChatCompletionRequest,
	cfg config.Config,
	settings util.Settings,
	chatMsgs []util.LocalStoreMessage,
) {
	chat := []openrouter.ChatCompletionMessage{}

	if cfg.SystemMessage != "" || (settings.SystemPrompt != nil && *settings.SystemPrompt != "") {
		systemMsg := cfg.SystemMessage
		if settings.SystemPrompt != nil && *settings.SystemPrompt != "" {
			systemMsg = *settings.SystemPrompt
		}

		systemPrompt := openrouter.ChatCompletionMessage{
			Role: "system",
			Content: openrouter.Content{
				Text: systemMsg,
			},
		}
		chat = append(chat, systemPrompt)
	}

	for _, singleMessage := range chatMsgs {
		messageContent := ""
		if singleMessage.Resoning != "" && *cfg.IncludeReasoningTokensInContext {
			messageContent += singleMessage.Resoning
		}

		if singleMessage.Content != "" {
			messageContent += singleMessage.Content
		}

		if len(singleMessage.ToolCalls) > 0 {
			toolCalls := constructOpenrouterToolCalls(singleMessage)
			chat = append(chat, toolCalls...)
			continue
		}

		if messageContent != "" {
			singleMessage.Content = messageContent
			conversationTurn := constructOpenrouterMessage(singleMessage)
			chat = append(chat, conversationTurn)
		}

	}

	r.Messages = chat
}

func setRequestParams(
	r *openrouter.ChatCompletionRequest,
	settings util.Settings) {

	r.Stream = true
	r.Model = settings.Model
	r.MaxTokens = settings.MaxTokens

	if settings.TopP != nil {
		r.TopP = *settings.TopP
	}

	if settings.Temperature != nil {
		r.Temperature = *settings.Temperature
	}

	if settings.Frequency != nil {
		r.FrequencyPenalty = *settings.Frequency
	}

	if settings.WebSearchEnabled {
		r.Tools = []openrouter.Tool{openRouterwebSearchTool}
	}
}

func processCompletionChunk(chunk openrouter.ChatCompletionStreamResponse) (util.CompletionChunk, error) {
	result := util.CompletionChunk{
		ID:               chunk.ID,
		Object:           chunk.Object,
		Created:          int(chunk.Created),
		Model:            chunk.Model,
		SystemFingerpint: chunk.SystemFingerprint,
	}

	if chunk.Usage != nil {
		result.Usage = &util.TokenUsage{
			Prompt:     chunk.Usage.PromptTokens,
			Completion: chunk.Usage.CompletionTokens,
			Total:      chunk.Usage.TotalTokens,
		}
	}

	if chunk.Choices != nil {
		choices := []util.Choice{}

		for _, choice := range chunk.Choices {

			delta, err := json.Marshal(choice.Delta)
			if err != nil {
				return result, err
			}

			var deltaMap map[string]any
			err = json.Unmarshal(delta, &deltaMap)
			if err != nil {
				return result, err
			}

			c := util.Choice{
				Index:        choice.Index,
				Delta:        deltaMap,
				FinishReason: string(choice.FinishReason),
			}

			choices = append(choices, c)
		}

		result.Choices = choices
	}

	return result, nil
}

func isOpenRouterToolCall(chunk openrouter.ChatCompletionStreamResponse, buffer OpenRouterToolCallsBuffer) bool {
	if len(buffer.Chunks) > 0 {
		return true
	}

	if len(chunk.Choices) != 0 && len(chunk.Choices[0].Delta.ToolCalls) != 0 {
		return true
	}

	return false
}

func (b *OpenRouterToolCallsBuffer) handleOpenRouterToolCallChunk(chunk openrouter.ChatCompletionStreamResponse) (util.CompletionChunk, bool) {

	util.Slog.Debug("handling tool call chunk", "chunk", chunk)

	if len(chunk.Choices) != 0 && chunk.Choices[0].FinishReason == "tool_calls" {

		util.Slog.Debug("tool_calls finish reason hit")

		toolCalls, err := b.mergeOpenRouterBuffer(chunk)
		if err != nil {
			util.Slog.Error("error occured when mergin tool calls buffer", "error", err)
		}

		result := util.CompletionChunk{
			ID:               chunk.ID,
			Object:           chunk.Object,
			Created:          int(chunk.Created),
			Model:            chunk.Model,
			SystemFingerpint: chunk.SystemFingerprint,
			Choices: []util.Choice{
				{
					Index:        chunk.Choices[0].Index,
					Delta:        map[string]any{},
					ToolCalls:    toolCalls,
					FinishReason: "tool_calls",
				},
			},
		}
		return result, true
	}

	if len(b.Chunks) == 0 {
		b.Chunks = append(b.Chunks, chunk.Choices[0].Delta.ToolCalls...)
		return util.CompletionChunk{}, false
	}

	b.Chunks = append(b.Chunks, chunk.Choices[0].Delta.ToolCalls...)
	return util.CompletionChunk{}, false
}

func (b *OpenRouterToolCallsBuffer) mergeOpenRouterBuffer(chunk openrouter.ChatCompletionStreamResponse) ([]util.ToolCall, error) {
	result := []util.ToolCall{}

	idx2call := map[int]*util.ToolCall{}
	idx2Json := map[int]string{}

	if len(b.Chunks) == 0 {
		if len(chunk.Choices) == 0 {
			return []util.ToolCall{}, nil
		}
		choice := chunk.Choices[0]
		if len(choice.Delta.ToolCalls) > 0 {
			util.Slog.Debug("toolcalls found in delta")

			result := []util.ToolCall{}
			for _, tc := range choice.Delta.ToolCalls {
				result = append(result, fromOpenRouterToolCall(tc))
			}

			return result, nil
		}
		return []util.ToolCall{}, nil
	}

	util.Slog.Debug("merging tool call buffer", "data", b.Chunks)
	for _, part := range b.Chunks {
		idx := *part.Index
		if _, exists := idx2call[idx]; !exists {
			idx2call[idx] = &util.ToolCall{}
		}

		if part.ID != "" {
			idx2call[idx].Id = part.ID
		}

		if part.Type != "" {
			idx2call[idx].Type = string(part.Type)
		}

		if part.Function.Name != "" {
			idx2call[idx].Function.Name = part.Function.Name
		}

		if part.Function.Arguments != "" {
			idx2Json[idx] += part.Function.Arguments
		}
	}

	for tcIdx, tc := range idx2call {
		argsJson := ""

		for jsonIdx, jsonPart := range idx2Json {
			if jsonIdx == tcIdx {
				argsJson += jsonPart
			}
		}

		var args map[string]string

		err := json.Unmarshal([]byte(argsJson), &args)
		if err != nil {
			util.Slog.Error("error unmarshalling JSON", "reason", err, "data", argsJson)
			b.Chunks = []openrouter.ToolCall{}
			return result, err
		}
		tc.Function.Args = args
		tc.Type = "function"

		util.Slog.Debug("merged tool call", "data", *tc)
		result = append(result, *tc)
	}

	b.Chunks = []openrouter.ToolCall{}
	return result, nil
}
func toOpenRouterToolCall(tc util.ToolCall) openrouter.ToolCall {
	args, _ := json.Marshal(tc.Function.Args)
	return openrouter.ToolCall{
		ID:   tc.Id,
		Type: openrouter.ToolTypeFunction,
		Function: openrouter.FunctionCall{
			Name:      tc.Function.Name,
			Arguments: string(args),
		},
	}
}

func fromOpenRouterToolCall(tc openrouter.ToolCall) util.ToolCall {
	var args map[string]string

	json.Unmarshal([]byte(tc.Function.Arguments), &args)
	return util.ToolCall{
		Id:   tc.ID,
		Type: string(tc.Type),
		Function: util.ToolFunction{
			Name: tc.Function.Name,
			Args: args,
		},
	}
}
