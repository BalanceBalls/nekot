package clients

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
	tea "github.com/charmbracelet/bubbletea"
)

type OpenAiClient struct {
	apiUrl        string
	systemMessage string
	provider      util.ApiProvider
	client        http.Client
}

func NewOpenAiClient(apiUrl, systemMessage string) *OpenAiClient {
	provider := util.GetOpenAiInferenceProvider(util.OpenAiProviderType, apiUrl)
	return &OpenAiClient{
		provider:      provider,
		apiUrl:        apiUrl,
		systemMessage: systemMessage,
		client:        http.Client{},
	}
}

func (c OpenAiClient) RequestCompletion(
	ctx context.Context,
	chatMsgs []util.LocalStoreMessage,
	modelSettings util.Settings,
	resultChan chan util.ProcessApiCompletionResponse,
) tea.Cmd {
	apiKey := os.Getenv("OPENAI_API_KEY")
	path := "v1/chat/completions"
	processResultID := util.ChunkIndexStart

	return func() tea.Msg {
		config, ok := config.FromContext(ctx)
		if !ok {
			util.Slog.Error("No config found in a context")
			panic("No config found in context")
		}

		body, err := c.constructCompletionRequestPayload(chatMsgs, *config, modelSettings)
		if err != nil {
			return util.MakeErrorMsg(err.Error())
		}

		resp, err := c.postOpenAiAPI(ctx, apiKey, path, body)
		if err != nil {
			return util.MakeErrorMsg(err.Error())
		}

		c.processCompletionResponse(resp, resultChan, &processResultID)
		return nil
	}
}

func (c OpenAiClient) RequestModelsList(ctx context.Context) util.ProcessModelsResponse {
	apiKey := os.Getenv("OPENAI_API_KEY")
	path := "v1/models"

	resp, err := c.getOpenAiAPI(ctx, apiKey, path)

	if err != nil {
		util.Slog.Error("OpenAI: failed to fetch a list of models", "error", err.Error())
		return util.ProcessModelsResponse{Err: err}
	}

	return processModelsListResponse(resp)
}

func constructUserMessage(msg util.LocalStoreMessage) util.OpenAIConversationTurn {
	content := []util.OpenAiContent{
		{
			Type: "text",
			Text: msg.Content,
		},
	}

	if len(msg.Attachments) != 0 {
		for _, attachment := range msg.Attachments {
			data := getImageURLString(attachment)
			image := util.OpenAiContent{
				Type: "image_url",
				ImageURL: util.OpenAiImage{
					URL: data,
				},
			}
			content = append(content, image)
		}
	}

	return util.OpenAIConversationTurn{
		Role:    msg.Role,
		Content: content,
	}
}

func getImageURLString(attachment util.Attachment) string {
	extension := filepath.Ext(attachment.Path)
	extension = strings.TrimPrefix(extension, ".")
	content := "data:image/" + extension + ";base64," + attachment.Content
	return content
}

func constructSystemMessage(content string) util.OpenAIConversationTurn {
	return util.OpenAIConversationTurn{
		Role: "system",
		Content: []util.OpenAiContent{
			{
				Type: "text",
				Text: content,
			},
		},
	}
}

func (c OpenAiClient) constructCompletionRequestPayload(
	chatMsgs []util.LocalStoreMessage,
	cfg config.Config,
	settings util.Settings,
) ([]byte, error) {
	messages := []util.OpenAIConversationTurn{}

	if util.IsSystemMessageSupported(c.provider, settings.Model) {
		if cfg.SystemMessage != "" || settings.SystemPrompt != nil {
			systemMsg := cfg.SystemMessage
			if settings.SystemPrompt != nil && *settings.SystemPrompt != "" {
				systemMsg = *settings.SystemPrompt
			}

			messages = append(messages, constructSystemMessage(systemMsg))
		}
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
			conversationTurn := constructUserMessage(singleMessage)
			messages = append(messages, conversationTurn)
		}
	}

	util.Slog.Debug("Constructing message", "model", settings.Model)

	reqParams := map[string]any{
		"model":      settings.Model,
		"max_tokens": settings.MaxTokens,
		"stream":     true,
		"messages":   messages,
	}

	if settings.Temperature != nil {
		reqParams["temperature"] = *settings.Temperature
	}

	if settings.Frequency != nil {
		reqParams["frequency_penalty"] = *settings.Frequency
	}

	if settings.TopP != nil {
		reqParams["top_p"] = *settings.TopP
	}

	util.TransformRequestHeaders(c.provider, reqParams)

	body, err := json.Marshal(reqParams)
	if err != nil {
		util.Slog.Error("error marshaling JSON", "error", err.Error())
		return nil, err
	}

	return body, nil
}

func getBaseUrl(configUrl string) string {
	parsedUrl, err := url.Parse(configUrl)
	if err != nil {
		util.Slog.Error("failed to parse openAi api url from config")
	}
	baseUrl := fmt.Sprintf("%s://%s", parsedUrl.Scheme, parsedUrl.Host)
	return baseUrl
}

func (c OpenAiClient) getOpenAiAPI(
	ctx context.Context,
	apiKey string,
	path string,
) (*http.Response, error) {
	baseUrl := getBaseUrl(c.apiUrl)
	requestUrl := fmt.Sprintf("%s/%s", baseUrl, path)

	req, err := http.NewRequestWithContext(ctx, "GET", requestUrl, nil)

	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	client := &http.Client{}
	return client.Do(req)
}

func (c OpenAiClient) postOpenAiAPI(
	ctx context.Context,
	apiKey, path string,
	body []byte,
) (*http.Response, error) {
	baseUrl := getBaseUrl(c.apiUrl)
	requestUrl := fmt.Sprintf("%s/%s", baseUrl, path)

	req, err := http.NewRequestWithContext(ctx, "POST", requestUrl, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	client := &http.Client{}
	return client.Do(req)
}

func processModelsListResponse(resp *http.Response) util.ProcessModelsResponse {
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return util.ProcessModelsResponse{Err: err}
		}
		return util.ProcessModelsResponse{Err: fmt.Errorf("%s", string(bodyBytes))}
	}

	resBody, err := io.ReadAll(resp.Body)

	if err != nil {
		util.Slog.Error("response body read failed", "error", err)
		return util.ProcessModelsResponse{Err: err}
	}

	var models util.ModelsListResponse
	if err = json.Unmarshal(resBody, &models); err != nil {
		util.Slog.Error("response parsing failed", "error", err)
		return util.ProcessModelsResponse{Err: err}
	}

	return util.ProcessModelsResponse{Result: models, Err: nil}
}

func (c OpenAiClient) processCompletionResponse(
	resp *http.Response,
	resultChan chan util.ProcessApiCompletionResponse,
	processResultID *int,
) {
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			resultChan <- util.ProcessApiCompletionResponse{ID: *processResultID, Err: err}
			return
		}
		resultChan <- util.ProcessApiCompletionResponse{ID: *processResultID, Err: fmt.Errorf("%s", string(bodyBytes))}
		return
	}

	scanner := bufio.NewReader(resp.Body)
	for {
		line, err := scanner.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				util.Slog.Warn("OpenAI: scanner returned EOF", "error", err.Error())
				break
			}

			util.Slog.Error(
				"OpenAI: Encountered error during receiving respone: ",
				"error",
				err.Error(),
			)
			resultChan <- util.ProcessApiCompletionResponse{ID: *processResultID, Err: err, Final: true}
			return
		}

		if line == "data: [DONE]\n" {
			util.Slog.Info("OpenAI: Received [DONE]")
			resultChan <- util.ProcessApiCompletionResponse{ID: *processResultID, Err: nil, Final: true}
			return
		}

		if after, ok := strings.CutPrefix(line, "data:"); ok {
			jsonStr := after
			resultChan <- processChunk(jsonStr, *processResultID)
			*processResultID++ // Increment the ID for each processed chunk
		}
	}
}

func processChunk(chunkData string, id int) util.ProcessApiCompletionResponse {
	var chunk util.CompletionChunk
	err := json.Unmarshal([]byte(chunkData), &chunk)
	if err != nil {
		util.Slog.Error("Error unmarshalling:", "chunk data", chunkData, "error", err.Error())
		return util.ProcessApiCompletionResponse{ID: id, Result: util.CompletionChunk{}, Err: err}
	}

	return util.ProcessApiCompletionResponse{ID: id, Result: chunk, Err: nil}
}
