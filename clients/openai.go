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
	"slices"
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

type OpenAIConversationTurn struct {
	Model      string           `json:"model"`
	Role       string           `json:"role"`
	Content    []OpenAiContent  `json:"content"`
	ToolCalls  []OpenAiToolCall `json:"tool_calls,omitempty"`
	ToolCallId string           `json:"tool_call_id"`
}

type OpenAiContent interface{}

type OpenAiToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type OpenAiImageContent struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL OpenAiImage `json:"image_url,omitempty"`
}

type OpenAiTextContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type OpenAiImage struct {
	URL string `json:"url"`
}

type OpenAiToolDefinition struct {
	Type     string         `json:"type"`
	Function OpenAiFunction `json:"function"`
}

type OpenAiFunction struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Parameters  OpenAiFuncitonParameters `json:"parameters"`
}

type OpenAiFuncitonParameters struct {
	Type       string         `json:"type"`
	Required   []string       `json:"required"`
	Properties map[string]any `json:"properties"`
}

type OpenAiToolCall struct {
	Id       string             `json:"id"`
	Type     string             `json:"type"`
	Function OpenAiToolFunction `json:"function"`
}

type OpenAiToolCallsDelta struct {
	Id       *string            `json:"id"`
	Type     *string            `json:"type"`
	Function OpenAiToolFunction `json:"function"`
	Index    int                `json:"index"`
}

type OpenAiToolFunction struct {
	Name      *string `json:"name"`
	Arguments string  `json:"arguments"`
}

type OpenAiToolCallsBuffer struct {
	Chunks []OpenAiToolCallsDelta
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

var openAIwebSearchTool = OpenAiToolDefinition{
	Type: "function",
	Function: OpenAiFunction{
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

func (c OpenAiClient) RequestCompletion(
	ctx context.Context,
	chatMsgs []util.LocalStoreMessage,
	modelSettings util.Settings,
	resultChan chan util.ProcessApiCompletionResponse,
) tea.Cmd {
	apiKey := os.Getenv("OPENAI_API_KEY")
	path := "v1/chat/completions"
	processResultID := util.GetNextProcessResultId(chatMsgs)

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

		c.processCompletionResponse(ctx, resp, resultChan, &processResultID)
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

func constructMessage(msg util.LocalStoreMessage) []OpenAIConversationTurn {
	if msg.Role == "tool" {
		turns := []OpenAIConversationTurn{}
		for _, tc := range msg.ToolCalls {
			result := ""
			if tc.Result != nil {
				result = *tc.Result
			}
			turn := OpenAIConversationTurn{
				Role:       msg.Role,
				Content:    []OpenAiContent{OpenAiTextContent{Type: "text", Text: result}},
				ToolCallId: tc.Id,
			}
			turns = append(turns, turn)
		}
		return turns
	}

	turn := OpenAIConversationTurn{
		Role:    msg.Role,
		Content: []OpenAiContent{},
	}

	if len(msg.Attachments) != 0 {
		for _, attachment := range msg.Attachments {
			data := getImageURLString(attachment)
			image := OpenAiImageContent{
				Type: "image_url",
				ImageURL: OpenAiImage{
					URL: data,
				},
			}
			turn.Content = append(turn.Content, image)
		}
	}

	if len(msg.ToolCalls) != 0 {
		for _, tc := range msg.ToolCalls {
			util.Slog.Debug("appending tool call request", "data", tc)
			turn.ToolCalls = append(turn.ToolCalls, toOpenAiToolCall(tc))
		}
	}

	if len(msg.Content) != 0 {
		text := OpenAiTextContent{
			Type: "text",
			Text: msg.Content,
		}
		turn.Content = append(turn.Content, text)
	}

	return []OpenAIConversationTurn{turn}
}

func getImageURLString(attachment util.Attachment) string {
	extension := filepath.Ext(attachment.Path)
	extension = strings.TrimPrefix(extension, ".")
	content := "data:image/" + extension + ";base64," + attachment.Content
	return content
}

func constructSystemMessage(content string) OpenAIConversationTurn {
	return OpenAIConversationTurn{
		Role: "system",
		Content: []OpenAiContent{
			OpenAiTextContent{
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
	messages := []OpenAIConversationTurn{}

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

		if singleMessage.ContextContent != "" {
			util.Slog.Debug("OpenAI: Including ContextContent in message",
				"contextContentLength", len(singleMessage.ContextContent))
			if messageContent != "" {
				messageContent += "\n\n"
			}
			messageContent += singleMessage.ContextContent
		}

		if messageContent != "" {
			singleMessage.Content = messageContent
		}

		conversationTurns := constructMessage(singleMessage)
		messages = append(messages, conversationTurns...)
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

	if settings.WebSearchEnabled {
		reqParams["tools"] = []any{openAIwebSearchTool}
	}

	util.TransformRequestHeaders(c.provider, reqParams)

	body, err := json.Marshal(reqParams)
	if err != nil {
		util.Slog.Error("error marshaling JSON", "error", err.Error())
		return nil, err
	}

	// util.Slog.Debug("serialized request", "data", string(body))

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
	ctx context.Context,
	resp *http.Response,
	resultChan chan util.ProcessApiCompletionResponse,
	processResultID *int,
) {
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{ID: *processResultID, Err: err})
			return
		}
		util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{ID: *processResultID, Err: fmt.Errorf("%s", string(bodyBytes))})
		return
	}

	toolCallsBuffer := OpenAiToolCallsBuffer{
		Chunks: []OpenAiToolCallsDelta{},
	}

	util.Slog.Debug("starting response processing loop")

	scanner := bufio.NewReader(resp.Body)
	for {
		line, err := scanner.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				util.Slog.Warn("OpenAI: scanner returned EOF", "error", err.Error())
				util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{ID: *processResultID, Err: io.ErrUnexpectedEOF, Final: true})
				break
			}

			util.Slog.Error(
				"OpenAI: Encountered error during receiving respone: ",
				"error",
				err.Error(),
			)
			util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{ID: *processResultID, Err: err, Final: true})
			return
		}

		if line == "data: [DONE]\n" {
			util.Slog.Info("OpenAI: Received [DONE]")
			util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{ID: *processResultID, Err: nil, Final: true})
			return
		}

		if after, ok := strings.CutPrefix(line, "data:"); ok {
			jsonStr := after
			chunk := processChunk(jsonStr, *processResultID)
			if isToolCall(chunk, toolCallsBuffer) {
				toolCallChunk, isReady := toolCallsBuffer.handleToolCallChunk(chunk)
				if !isReady {
					continue
				}

				util.Slog.Info("OpenAI: Tool call interruption sent")
				util.WriteToResponseChannel(ctx, resultChan, toolCallChunk)
				*processResultID++
				util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{ID: *processResultID, Err: nil, Final: true})
				break
			}

			util.WriteToResponseChannel(ctx, resultChan, chunk)
			*processResultID++
		}
	}
}

func isToolCall(chunk util.ProcessApiCompletionResponse, buffer OpenAiToolCallsBuffer) bool {
	if len(buffer.Chunks) > 0 {
		return true
	}

	if slices.ContainsFunc(chunk.Result.Choices, func(c util.Choice) bool {
		_, isOk := c.Delta["tool_calls"]
		return isOk
	}) {
		return true
	}

	return false
}

func (b *OpenAiToolCallsBuffer) handleToolCallChunk(chunk util.ProcessApiCompletionResponse) (util.ProcessApiCompletionResponse, bool) {
	toolCallsDeltaArr := []OpenAiToolCallsDelta{}

	util.Slog.Debug("handling tool call chunk", "id", chunk.Result.ID)

	finishReason := chunk.Result.Choices[0].FinishReason
	if finishReason == "tool_calls" {

		util.Slog.Debug("tool_calls finish reason hit")

		toolCalls, err := b.mergeBuffer(chunk)
		if err != nil {
			return util.ProcessApiCompletionResponse{ID: chunk.ID, Result: util.CompletionChunk{}, Err: err}, true
		}

		chunk.Result.Choices[0].ToolCalls = toolCalls
		return chunk, true
	}

	toolCallsJson, err := json.Marshal(chunk.Result.Choices[0].Delta["tool_calls"])
	if err != nil {
		util.Slog.Error("error marshaling JSON", "error", err.Error())
	}

	err = json.Unmarshal(toolCallsJson, &toolCallsDeltaArr)
	if err != nil {
		util.Slog.Error("error unmarshalling tool_calls delta:", "delta", string(toolCallsJson), "error", err.Error())
		return util.ProcessApiCompletionResponse{ID: chunk.ID, Result: util.CompletionChunk{}, Err: err}, false
	}

	toolCallsDelta := toolCallsDeltaArr[0]
	if len(b.Chunks) == 0 {
		b.Chunks = append(b.Chunks, toolCallsDelta)
		return util.ProcessApiCompletionResponse{}, false
	}

	b.Chunks = append(b.Chunks, toolCallsDelta)
	return util.ProcessApiCompletionResponse{}, false
}

func (b *OpenAiToolCallsBuffer) mergeBuffer(chunk util.ProcessApiCompletionResponse) ([]util.ToolCall, error) {
	result := []util.ToolCall{}

	idx2call := map[int]*util.ToolCall{}
	idx2Json := map[int]string{}

	if len(b.Chunks) == 0 {
		choice := chunk.Result.Choices[0]
		if content, ok := choice.Delta["tool_calls"]; ok {
			util.Slog.Debug("toolcalls found in delta")

			toolCallsJSON, err := json.Marshal(content)
			if err != nil {
				return []util.ToolCall{}, err
			}

			var toolCalls []OpenAiToolCall
			err = json.Unmarshal(toolCallsJSON, &toolCalls)
			if err != nil {
				util.Slog.Error("error unmarshalling JSON", "reason", err, "data", string(toolCallsJSON))
				return []util.ToolCall{}, err
			}

			util.Slog.Debug("toolcalls parsed", "tool_calls", toolCalls)

			result := []util.ToolCall{}
			for _, tc := range toolCalls {
				result = append(result, fromOpenAiToolCall(tc))
			}

			return result, nil
		}
		return []util.ToolCall{}, nil
	}

	util.Slog.Debug("merging tool call buffer", "data", b.Chunks)
	for _, part := range b.Chunks {
		idx := part.Index
		if _, exists := idx2call[idx]; !exists {
			idx2call[idx] = &util.ToolCall{}
		}

		if part.Id != nil {
			idx2call[idx].Id = *part.Id
		}

		if part.Type != nil {
			idx2call[idx].Type = *part.Type
		}

		if part.Function.Name != nil {
			idx2call[idx].Function.Name = *part.Function.Name
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
			b.Chunks = []OpenAiToolCallsDelta{}
			return result, err
		}
		tc.Function.Args = args
		tc.Type = "function"

		util.Slog.Debug("merged tool call", "data", *tc)
		result = append(result, *tc)
	}

	b.Chunks = []OpenAiToolCallsDelta{}
	return result, nil
}

func processChunk(chunkData string, id int) util.ProcessApiCompletionResponse {
	var chunk util.CompletionChunk
	err := json.Unmarshal([]byte(chunkData), &chunk)
	if err != nil {
		util.Slog.Error("error unmarshalling:", "chunk data", chunkData, "error", err.Error())
		return util.ProcessApiCompletionResponse{ID: id, Result: util.CompletionChunk{}, Err: err}
	}

	return util.ProcessApiCompletionResponse{ID: id, Result: chunk, Err: nil}
}

func toOpenAiToolCall(tc util.ToolCall) OpenAiToolCall {
	args, _ := json.Marshal(tc.Function.Args)
	return OpenAiToolCall{
		Id:   tc.Id,
		Type: tc.Type,
		Function: OpenAiToolFunction{
			Name:      &tc.Function.Name,
			Arguments: string(args),
		},
	}
}

func fromOpenAiToolCall(tc OpenAiToolCall) util.ToolCall {
	var args map[string]string

	json.Unmarshal([]byte(tc.Function.Arguments), &args)
	return util.ToolCall{
		Id:   tc.Id,
		Type: tc.Type,
		Function: util.ToolFunction{
			Name: *tc.Function.Name,
			Args: args,
		},
	}
}
