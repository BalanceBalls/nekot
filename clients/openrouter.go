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

type OpenrouterClient struct {
	systemMessage string
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

		processResultID := util.ChunkIndexStart
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

func constructOpenrouterMessage(msg util.LocalStoreMessage) openrouter.ChatCompletionMessage {
	isUserMsg := msg.Role == "user"

	if len(msg.Attachments) == 0 {
		if isUserMsg {
			return openrouter.UserMessage(msg.Content)
		}

		return openrouter.AssistantMessage(msg.Content)
	}

	messageWithImages := openrouter.ChatCompletionMessage{
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

	for _, attachment := range msg.Attachments {
		data := getImageURLString(attachment)

		part := openrouter.ChatMessagePart{
			Type: "image_url",
			ImageURL: &openrouter.ChatMessageImageURL{
				URL: data,
			},
		}

		messageWithImages.Content.Multi = append(messageWithImages.Content.Multi, part)
	}

	return messageWithImages
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

			var deltaMap map[string]interface{}
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
