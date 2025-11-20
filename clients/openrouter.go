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

		stream, err := client.CreateChatCompletionStream(
			context.Background(), openrouter.ChatCompletionRequest{
				Model: "qwen/qwen3-235b-a22b-07-25:free",
				Messages: []openrouter.ChatCompletionMessage{
					openrouter.UserMessage("Hello, how are you?"),
				},
				Stream: true,
			},
		)

		if err != nil {
			return util.MakeErrorMsg(err.Error())
		}
		defer stream.Close()

		util.Slog.Debug("constructing message", "model", modelSettings.Model)

		for {
			response, err := stream.Recv()
			if err != nil && err != io.EOF {
			}
			if errors.Is(err, io.EOF) {
				fmt.Println("EOF, stream finished")
				return nil
			}
			json, err := json.MarshalIndent(response, "", "  ")
			fmt.Println(string(json))
		}
	}

}

func (c OpenrouterClient) RequestModelsList(ctx context.Context) util.ProcessModelsResponse {
	client := openrouter.NewClient(os.Getenv("OPENROUTER_API_KEY"))

	client.ListUserModels(ctx)
	models, err := client.ListModels(ctx)
	util.Slog.Debug("fetched models", "data", models)

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
