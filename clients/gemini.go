package clients

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"iter"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
	"google.golang.org/genai"
)

const modelNamePrefix = "models/"

type processedChunk struct {
	chunk      util.CompletionChunk
	isFinal    bool
	isToolCall bool
	citations  []string
}

type GeminiClient struct {
	systemMessage string
}

func NewGeminiClient(systemMessage string) *GeminiClient {
	return &GeminiClient{
		systemMessage: systemMessage,
	}
}

var webSearchTool = &genai.Tool{
	FunctionDeclarations: []*genai.FunctionDeclaration{
		{
			Name:        "web_search",
			Description: "Perform a web search to retrieve up to date info or piece of knowledge you have doubts about.",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"query": {
						Type:        genai.TypeString,
						Description: "The search query string. Should be very specific and moderately detailed for accurate retrieval.",
					},
				},
				Required: []string{"query"},
			},
		},
	},
}

func (c GeminiClient) RequestCompletion(
	ctx context.Context,
	chatMsgs []util.LocalStoreMessage,
	modelSettings util.Settings,
	resultChan chan util.ProcessApiCompletionResponse,
) tea.Cmd {

	return func() tea.Msg {
		cfg, ok := config.FromContext(ctx)
		if !ok {
			fmt.Println("No config found")
			panic("No config found in context")
		}

		client, err := newGeminiAPIClient(ctx, *cfg)
		if err != nil {
			util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{ID: util.ChunkIndexStart, Err: err, Final: true})
			return nil
		}

		contents, generationConfig, err := prepareGeminiCompletionRequest(chatMsgs, *cfg, modelSettings)
		if err != nil {
			return util.MakeErrorMsg(err.Error())
		}

		stream := client.Models.GenerateContentStream(
			ctx,
			modelSettings.Model,
			contents,
			generationConfig,
		)
		return processGeminiCompletionStream(
			ctx,
			stream,
			resultChan,
			util.GetNextProcessResultId(chatMsgs),
		)
	}
}

func newGeminiAPIClient(ctx context.Context, cfg config.Config) (*genai.Client, error) {
	return genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  cfg.ResolvedAPIKey(),
		Backend: genai.BackendGeminiAPI,
	})
}

func prepareGeminiCompletionRequest(
	chatMsgs []util.LocalStoreMessage,
	cfg config.Config,
	modelSettings util.Settings,
) ([]*genai.Content, *genai.GenerateContentConfig, error) {
	util.Slog.Debug("constructing message", "model", modelSettings.Model)

	generationConfig := buildGenerateContentConfig(cfg, modelSettings)
	util.Slog.Debug("added tools", "tools", generationConfig.Tools)

	contents, err := buildChatHistory(chatMsgs, *cfg.IncludeReasoningTokensInContext)
	if err != nil {
		return nil, nil, err
	}

	return contents, generationConfig, nil
}

func processGeminiCompletionStream(
	ctx context.Context,
	stream iter.Seq2[*genai.GenerateContentResponse, error],
	resultChan chan util.ProcessApiCompletionResponse,
	processResultID int,
) tea.Msg {
	var citations []string
	for resp, err := range stream {
		if err != nil {
			writeGeminiStreamError(ctx, resultChan, processResultID, err)
			return nil
		}

		result, err := processResponseChunk(resp, processResultID)
		if err != nil {
			util.Slog.Error("Gemini: Encountered error during chunks processing", "error", err)
			util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{ID: processResultID, Err: err})
			break
		}

		citations = append(citations, result.citations...)
		util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{
			ID:     processResultID,
			Result: result.chunk,
			Err:    nil,
		})

		processResultID++
		if result.isToolCall {
			return nil
		}

		if result.isFinal {
			sendFinalGeminiChunks(ctx, resultChan, processResultID, citations)
			return nil
		}
	}

	util.Slog.Debug(
		"Gemini: stream done",
		"result id",
		processResultID,
	)
	sendCompensationChunk(ctx, resultChan, processResultID)
	return nil
}

func writeGeminiStreamError(
	ctx context.Context,
	resultChan chan util.ProcessApiCompletionResponse,
	id int,
	err error,
) {
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		util.Slog.Error(
			"Gemini: Encountered error while receiving response",
			"error",
			apiErr.Message,
		)
		util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{ID: id, Err: apiErr})
		return
	}

	util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{ID: id, Err: err})
}

func sendFinalGeminiChunks(
	ctx context.Context,
	resultChan chan util.ProcessApiCompletionResponse,
	processResultID int,
	citations []string,
) {
	if len(citations) > 0 {
		sendCitationsChunk(ctx, resultChan, processResultID, citations)
		processResultID++
	}

	sendCompensationChunk(ctx, resultChan, processResultID)
}

func (c GeminiClient) RequestModelsList(ctx context.Context) util.ProcessModelsResponse {
	config, ok := config.FromContext(ctx)
	if !ok {
		return util.ProcessModelsResponse{Err: fmt.Errorf("no config found in context")}
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  config.ResolvedAPIKey(),
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return util.ProcessModelsResponse{Err: err}
	}

	var modelsList []util.ModelDescription
	for model, err := range client.Models.All(ctx) {
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) ||
				errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return util.ProcessModelsResponse{Err: errors.New("timed out during fetching models")}
			}
			return util.ProcessModelsResponse{Err: err}
		}

		formattedName := strings.TrimPrefix(model.Name, modelNamePrefix)
		modelsList = append(modelsList, util.ModelDescription{Id: formattedName})
	}

	return util.ProcessModelsResponse{
		Result: util.ModelsListResponse{
			Data: modelsList,
		},
		Err: nil,
	}
}

// Gemini may include actual sources with the response chunks which is pretty neat
// The citations are collected from each chunk and sent together as the last chunk
// because displaying citations all around the response is ugly
func sendCitationsChunk(
	ctx context.Context,
	resultChan chan util.ProcessApiCompletionResponse,
	id int,
	citations []string,
) {
	citations = util.RemoveDuplicates(citations)
	citationsString := strings.Join(citations, "\n")
	content := "\n\n`Sources`\n" + citationsString

	util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{
		ID:     id,
		Result: singleChoiceChunk(id, content, ""),
		Final:  false,
	})
}

// Since orchestrator is built for openai apis, need to mimic open ai response structure
// Gemeni sends finish reason with the last response, and openai apis send finish reason with an empty response
func sendCompensationChunk(ctx context.Context, resultChan chan util.ProcessApiCompletionResponse, id int) {
	util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{
		ID:     id,
		Result: singleChoiceChunk(id, "", "stop"),
		Final:  true,
	})

	nextId := id + 1
	util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{
		ID:     nextId,
		Result: singleChoiceChunk(nextId, "", ""),
		Final:  true,
	})
	util.Slog.Debug("Gemini: compensation chunks sent")
}

func singleChoiceChunk(id int, content, finishReason string) util.CompletionChunk {
	return util.CompletionChunk{
		ID: fmt.Sprint(id),
		Choices: []util.Choice{
			{
				Index:        id,
				Delta:        contentDelta(content),
				FinishReason: finishReason,
			},
		},
	}
}

func buildGenerateContentConfig(cfg config.Config, settings util.Settings) *genai.GenerateContentConfig {
	generationConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: int32(settings.MaxTokens),
	}

	if settings.TopP != nil {
		generationConfig.TopP = settings.TopP
	}

	if settings.Temperature != nil {
		generationConfig.Temperature = settings.Temperature
	}

	if settings.WebSearchEnabled {
		generationConfig.Tools = []*genai.Tool{webSearchTool}
	}

	if cfg.SystemMessage != "" || (settings.SystemPrompt != nil && *settings.SystemPrompt != "") {
		systemMsg := cfg.SystemMessage
		if settings.SystemPrompt != nil && *settings.SystemPrompt != "" {
			systemMsg = *settings.SystemPrompt
		}
		generationConfig.SystemInstruction = genai.NewContentFromText(systemMsg, genai.RoleUser)
	}

	return generationConfig
}

// Maps gemini response model to the openai response model
func processResponseChunk(response *genai.GenerateContentResponse, id int) (processedChunk, error) {
	result := processedChunk{
		chunk: util.CompletionChunk{ID: fmt.Sprint(id)},
	}
	for _, candidate := range response.Candidates {
		if candidate.Content == nil {
			break
		}

		choice, err := processCandidate(candidate, &result)
		if err != nil {
			return result, err
		}

		if result.isFinal {
			if response.UsageMetadata != nil {
				result.chunk.Usage = tokenUsage(response.UsageMetadata)
			}
		}

		result.chunk.Choices = append(result.chunk.Choices, choice)
	}

	return result, nil
}

func processCandidate(candidate *genai.Candidate, result *processedChunk) (util.Choice, error) {
	finishReason, err := handleFinishReason(candidate.FinishReason)
	if err != nil {
		return util.Choice{}, err
	}

	choice := util.Choice{
		Index:        int(candidate.Index),
		FinishReason: finishReason,
	}

	if len(candidate.Content.Parts) > 0 {
		result.citations = append(result.citations, citationSources(candidate)...)
	}
	if err := applyCandidateParts(candidate.Content.Parts, &choice, result); err != nil {
		return util.Choice{}, err
	}

	if finishReason != "" {
		util.Slog.Debug("gemini finish reason", "data", finishReason)
		choice.FinishReason = ""
		result.isFinal = true
	}

	return choice, nil
}

func applyCandidateParts(parts []*genai.Part, choice *util.Choice, result *processedChunk) error {
	if len(parts) == 0 {
		choice.Delta = contentDelta("")
		return nil
	}

	toolCallParts := functionCallParts(parts)
	if len(toolCallParts) > 0 {
		if hasResponseContent(parts) {
			return nil
		}

		toolCalls, err := responseToolCalls(toolCallParts)
		if err != nil {
			return err
		}
		choice.ToolCalls = toolCalls
		result.isToolCall = true
		return nil
	}

	choice.Delta = contentDelta(formatResponseParts(parts))
	return nil
}

func citationSources(candidate *genai.Candidate) []string {
	if candidate.CitationMetadata == nil {
		return nil
	}

	var sources []string
	for _, source := range candidate.CitationMetadata.Citations {
		if source != nil && source.URI != "" {
			sources = append(sources, fmt.Sprintf("\t> [](%s)", source.URI))
		}
	}

	return sources
}

func responseToolCalls(parts []*genai.Part) ([]util.ToolCall, error) {
	util.Slog.Debug("decided to include tool call request")

	var toolCalls []util.ToolCall
	for _, part := range parts {
		tc := part.FunctionCall
		if tc.Name != webSearchTool.FunctionDeclarations[0].Name {
			continue
		}

		query, ok := tc.Args["query"].(string)
		if !ok {
			return nil, errors.New("GeminiAPI: web search tool call has no string query")
		}

		toolCalls = append(toolCalls, util.ToolCall{
			Id:   geminiCallID(tc.ID),
			Type: "function",
			Function: util.ToolFunction{
				Args: map[string]string{
					"query": query,
				},
				Name: tc.Name,
			},
			ThoughtSignature: append([]byte(nil), part.ThoughtSignature...),
		})
	}

	return toolCalls, nil
}

func geminiCallID(id string) string {
	if id != "" {
		return id
	}
	return "gemini_func"
}

func contentDelta(content string) map[string]any {
	return map[string]any{"content": content}
}

func tokenUsage(metadata *genai.GenerateContentResponseUsageMetadata) *util.TokenUsage {
	return &util.TokenUsage{
		Prompt:     int(metadata.PromptTokenCount),
		Completion: int(metadata.CandidatesTokenCount),
	}
}

func hasResponseContent(parts []*genai.Part) bool {
	for _, part := range parts {
		if part != nil && part.Text != "" && !part.Thought {
			return true
		}
	}

	return false
}

func functionCallParts(parts []*genai.Part) []*genai.Part {
	var calls []*genai.Part
	for _, part := range parts {
		if part != nil && part.FunctionCall != nil {
			calls = append(calls, part)
		}
	}

	return calls
}

func formatResponseParts(parts []*genai.Part) string {
	var response strings.Builder
	for _, part := range parts {
		if part != nil && !part.Thought {
			response.WriteString(part.Text)
		}
	}

	return response.String()
}

func handleFinishReason(reason genai.FinishReason) (string, error) {
	switch reason {
	case genai.FinishReasonStop:
		return "stop", nil
	case genai.FinishReasonMaxTokens:
		return "length", nil
	case "":
	case genai.FinishReasonOther:
	case genai.FinishReasonUnspecified:
	case genai.FinishReasonRecitation:
		return "", errors.New(
			"LLM stopped responding due to response containing copyright material",
		)
	case genai.FinishReasonSafety:
	default:
		util.Slog.Error("unexpected genai.FinishReason", "finish reason", reason)
		return "", errors.New("GeminiAPI: unsupported finish reason")
	}

	return "", nil
}

func buildChatHistory(msgs []util.LocalStoreMessage, includeReasoning bool) ([]*genai.Content, error) {
	chat := []*genai.Content{}
	builder := geminiHistoryBuilder{
		includeReasoning:  includeReasoning,
		signedToolCallIDs: make(map[string]struct{}),
	}

	util.Slog.Debug("building messages history:", "data", msgs)

	for _, singleMessage := range msgs {
		message, err := builder.contentFromMessage(singleMessage)
		if err != nil {
			return nil, err
		}

		if len(message.Parts) == 0 {
			continue
		}

		chat = append(chat, &message)
		util.Slog.Debug("constructed turn", "data", message)
	}

	return chat, nil
}

type geminiHistoryBuilder struct {
	includeReasoning  bool
	signedToolCallIDs map[string]struct{}
}

func (b *geminiHistoryBuilder) contentFromMessage(msg util.LocalStoreMessage) (genai.Content, error) {
	message := genai.Content{
		Parts: []*genai.Part{},
		Role:  geminiRole(msg.Role),
	}

	if content := messageContent(msg, b.includeReasoning); content != "" {
		message.Parts = append(message.Parts, genai.NewPartFromText(content))
	}

	attachmentParts, err := attachmentParts(msg.Attachments)
	if err != nil {
		return message, err
	}
	message.Parts = append(message.Parts, attachmentParts...)
	message.Parts = append(message.Parts, b.toolCallParts(msg)...)

	return message, nil
}

func geminiRole(role string) string {
	if role == "assistant" {
		return genai.RoleModel
	}
	return genai.RoleUser
}

func messageContent(msg util.LocalStoreMessage, includeReasoning bool) string {
	var content strings.Builder
	if includeReasoning {
		content.WriteString(msg.Resoning)
	}
	content.WriteString(msg.Content)
	return content.String()
}

func attachmentParts(attachments []util.Attachment) ([]*genai.Part, error) {
	var parts []*genai.Part
	for _, item := range attachments {
		decodedBytes, err := base64.StdEncoding.DecodeString(item.Content)
		if err != nil {
			util.Slog.Error("failed to decode file bytes", "item", item.Path, "error", err.Error())
			return nil, errors.New("could not prepare attachments for request")
		}

		extension := strings.TrimPrefix(filepath.Ext(item.Path), ".")
		parts = append(parts, genai.NewPartFromBytes(decodedBytes, "image/"+extension))
	}

	return parts, nil
}

func (b *geminiHistoryBuilder) toolCallParts(msg util.LocalStoreMessage) []*genai.Part {
	var parts []*genai.Part
	for _, tc := range msg.ToolCalls {
		part := b.toolCallPart(msg.Role, tc)
		if part != nil {
			parts = append(parts, part)
		}
	}

	return parts
}

func (b *geminiHistoryBuilder) toolCallPart(role string, tc util.ToolCall) *genai.Part {
	if role == "tool" {
		return b.toolResponsePart(tc)
	}
	return b.toolRequestPart(tc)
}

func (b *geminiHistoryBuilder) toolResponsePart(tc util.ToolCall) *genai.Part {
	if _, ok := b.signedToolCallIDs[tc.Id]; !ok {
		util.Slog.Warn(
			"skipping Gemini tool response without a signed function call",
			"tool call id",
			tc.Id,
		)
		return nil
	}

	util.Slog.Debug("appending tool call result", "data", tc)
	delete(b.signedToolCallIDs, tc.Id)

	result := ""
	if tc.Result != nil {
		result = *tc.Result
	}

	return &genai.Part{
		FunctionResponse: &genai.FunctionResponse{
			ID:   tc.Id,
			Name: tc.Function.Name,
			Response: map[string]any{
				"query":  tc.Function.Args["query"],
				"result": result,
			},
		},
	}
}

func (b *geminiHistoryBuilder) toolRequestPart(tc util.ToolCall) *genai.Part {
	if len(tc.ThoughtSignature) == 0 {
		util.Slog.Warn(
			"skipping Gemini function call without a thought signature",
			"tool call id",
			tc.Id,
		)
		return nil
	}

	util.Slog.Debug("appending tool call request", "data", tc)
	b.signedToolCallIDs[tc.Id] = struct{}{}

	return &genai.Part{
		FunctionCall: &genai.FunctionCall{
			ID:   tc.Id,
			Name: tc.Function.Name,
			Args: map[string]any{"query": tc.Function.Args["query"]},
		},
		ThoughtSignature: append([]byte(nil), tc.ThoughtSignature...),
	}
}
