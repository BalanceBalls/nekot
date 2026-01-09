package clients

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
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
		config, ok := config.FromContext(ctx)
		if !ok {
			fmt.Println("No config found")
			panic("No config found in context")
		}

		client, err := genai.NewClient(ctx, option.WithAPIKey(os.Getenv("GEMINI_API_KEY")))
		if err != nil {
			resultChan <- util.ProcessApiCompletionResponse{ID: util.ChunkIndexStart, Err: err, Final: true}
			return nil
		}
		defer client.Close()

		util.Slog.Debug("constructing message", "model", modelSettings.Model)

		model := client.GenerativeModel(modelNamePrefix + modelSettings.Model)

		if modelSettings.WebSearchEnabled {
			model.Tools = []*genai.Tool{webSearchTool}
		}

		util.Slog.Debug("added tools", "tools", model.Tools)

		setParams(model, *config, modelSettings)

		cs := model.StartChat()
		cs.History, err = buildChatHistory(chatMsgs, *config.IncludeReasoningTokensInContext)
		if err != nil {
			return util.MakeErrorMsg(err.Error())
		}

		iter := cs.SendMessageStream(ctx)
		processResultID := util.GetNextProcessResultId(chatMsgs)

		var citations []string
		for {
			resp, err := iter.Next()
			if err == iterator.Done {
				util.Slog.Debug(
					"Gemini: Iterator done. processResultID: ",
					"result id",
					processResultID,
				)
				sendCompensationChunk(resultChan, processResultID)
				return nil
			}

			if err != nil {
				var apiErr *googleapi.Error
				if errors.As(err, &apiErr) {
					util.Slog.Error(
						"Gemini: Encountered error while receiving response",
						"error",
						apiErr.Body,
					)
					resultChan <- util.ProcessApiCompletionResponse{ID: processResultID, Err: apiErr}
				} else {
					resultChan <- util.ProcessApiCompletionResponse{ID: processResultID, Err: err}
				}
				break
			}

			result, err := processResponseChunk(resp, processResultID)
			if err != nil {
				util.Slog.Error("Gemini: Encountered error during chunks processing", "error", err)
				resultChan <- util.ProcessApiCompletionResponse{ID: processResultID, Err: err}
				break
			}

			citations = append(citations, result.citations...)
			resultChan <- util.ProcessApiCompletionResponse{
				ID:     processResultID,
				Result: result.chunk,
				Err:    nil,
			}

			processResultID++
			if result.isToolCall {
				break
			}

			if result.isFinal {
				if len(citations) > 0 {
					sendCitationsChunk(resultChan, processResultID, citations)
					processResultID++
				}

				sendCompensationChunk(resultChan, processResultID)
				break
			}
		}

		return nil
	}
}

func (c GeminiClient) RequestModelsList(ctx context.Context) util.ProcessModelsResponse {
	client, err := genai.NewClient(ctx, option.WithAPIKey(os.Getenv("GEMINI_API_KEY")))
	if err != nil {
		return util.ProcessModelsResponse{Err: err}
	}
	defer client.Close()

	modelsIter := client.ListModels(ctx)

	if ctx.Err() == context.DeadlineExceeded {
		return util.ProcessModelsResponse{Err: errors.New("timed out during fetching models")}
	}

	var modelsList []util.ModelDescription
	for {
		model, err := modelsIter.Next()
		if err == iterator.Done {
			return util.ProcessModelsResponse{
				Result: util.ModelsListResponse{
					Data: modelsList,
				},
				Err: nil,
			}
		}

		if err != nil {
			return util.ProcessModelsResponse{Err: err}
		}

		formattedName := strings.TrimPrefix(model.Name, modelNamePrefix)
		modelsList = append(modelsList, util.ModelDescription{Id: formattedName})
	}
}

// Gemini may include actual sources with the response chunks which is pretty neat
// The citations are collected from each chunk and sent together as the last chunk
// because displaying citations all around the response is ugly
func sendCitationsChunk(
	resultChan chan util.ProcessApiCompletionResponse,
	id int,
	citations []string,
) {
	var chunk util.CompletionChunk
	chunk.ID = fmt.Sprint(id)

	citations = util.RemoveDuplicates(citations)
	citationsString := strings.Join(citations, "\n")
	content := "\n\n`Sources`\n" + citationsString

	choice := util.Choice{
		Index: id,
		Delta: map[string]any{
			"content": content,
		},
	}

	chunk.Choices = []util.Choice{choice}
	resultChan <- util.ProcessApiCompletionResponse{
		ID:     id,
		Result: chunk,
		Final:  false,
	}
}

// Since orchestrator is built for openai apis, need to mimic open ai response structure
// Gemeni sends finish reason with the last response, and openai apis send finish reason with an empty response
func sendCompensationChunk(resultChan chan util.ProcessApiCompletionResponse, id int) {
	var chunk util.CompletionChunk
	chunk.ID = fmt.Sprint(id)

	choice := util.Choice{
		Index: id,
		Delta: map[string]any{
			"content": "",
		},
		FinishReason: "stop",
	}

	chunk.Choices = []util.Choice{choice}
	resultChan <- util.ProcessApiCompletionResponse{
		ID:     id,
		Result: chunk,
		Final:  false,
	}

	nextId := id + 2
	chunk.ID = fmt.Sprint(nextId)
	resultChan <- util.ProcessApiCompletionResponse{
		ID:     nextId,
		Result: chunk,
		Final:  true,
	}
	util.Slog.Debug("Gemini: compensation chunks sent")
}

func setParams(model *genai.GenerativeModel, cfg config.Config, settings util.Settings) {
	model.SetMaxOutputTokens(int32(settings.MaxTokens))

	if settings.TopP != nil {
		model.SetTopP(*settings.TopP)
	}

	if settings.Temperature != nil {
		model.SetTemperature(*settings.Temperature)
	}

	if cfg.SystemMessage != "" || (settings.SystemPrompt != nil && *settings.SystemPrompt != "") {
		systemMsg := cfg.SystemMessage
		if settings.SystemPrompt != nil && *settings.SystemPrompt != "" {
			systemMsg = *settings.SystemPrompt
		}
		model.SystemInstruction = genai.NewUserContent(genai.Text(systemMsg))
	}
}

// Maps gemini response model to the openai response model
func processResponseChunk(response *genai.GenerateContentResponse, id int) (processedChunk, error) {
	var chunk util.CompletionChunk
	chunk.ID = fmt.Sprint(id)

	result := processedChunk{}
	for _, candidate := range response.Candidates {
		if candidate.Content == nil {
			break
		}

		finishReason, err := handleFinishReason(candidate.FinishReason)

		if err != nil {
			return result, err
		}

		choice := util.Choice{
			Index:        int(candidate.Index),
			FinishReason: finishReason,
		}

		if len(candidate.Content.Parts) > 0 {
			if candidate.CitationMetadata != nil &&
				len(candidate.CitationMetadata.CitationSources) > 0 {
				for _, source := range candidate.CitationMetadata.CitationSources {
					if source.URI != nil {
						sourceString := fmt.Sprintf("\t> [](%s)", *source.URI)
						result.citations = append(result.citations, sourceString)
					}
				}
			}

			hasResponseContent := hasResponseContent(candidate.Content.Parts)
			toolCalls := candidate.FunctionCalls()

			if len(toolCalls) > 0 && !hasResponseContent {
				responseToolCalls := []util.ToolCall{}
				util.Slog.Debug("decided to include tool call request")
				for _, tc := range toolCalls {
					if tc.Name == webSearchTool.FunctionDeclarations[0].Name {
						query := tc.Args["query"].(string)
						responseToolCalls = append(responseToolCalls, util.ToolCall{
							Id:   "gemini_func",
							Type: "function",
							Function: util.ToolFunction{
								Args: map[string]string{
									"query": query,
								},
								Name: tc.Name,
							},
						})
					}
				}

				choice.ToolCalls = responseToolCalls
				result.isToolCall = true
			}

			if len(toolCalls) == 0 {
				choice.Delta = map[string]any{
					"content": formatResponsePart(candidate.Content.Parts[0]),
				}
			}
		} else {
			choice.Delta = map[string]any{
				"content": "",
			}
		}

		if finishReason != "" {

			util.Slog.Debug("gemini finish reason", "data", finishReason)
			choice.FinishReason = ""
			chunk.Usage = &util.TokenUsage{
				Prompt:     int(response.UsageMetadata.PromptTokenCount),
				Completion: int(response.UsageMetadata.CandidatesTokenCount),
			}

			result.isFinal = true
		}

		chunk.Choices = append(chunk.Choices, choice)
	}

	result.chunk = chunk
	return result, nil
}

func hasResponseContent(parts []genai.Part) bool {
	return slices.ContainsFunc(parts, func(p genai.Part) bool {
		switch p.(type) {
		case genai.Text:
			return true
		default:
			return false
		}
	})

}

func formatResponsePart(part genai.Part) string {
	switch v := part.(type) {
	case genai.Text:
		response := string(v)
		return response
	default:
		panic("Only text type is supported")
	}
}

func handleFinishReason(reason genai.FinishReason) (string, error) {
	switch reason {
	case genai.FinishReasonStop:
		return "stop", nil
	case genai.FinishReasonMaxTokens:
		return "length", nil
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

	util.Slog.Debug("building messages history:", "data", msgs)

	for _, singleMessage := range msgs {
		role := "user"
		if singleMessage.Role == "assistant" {
			role = "model"
		}

		if singleMessage.Role == "tool" {
			role = "function"
		}

		messageContent := ""

		if singleMessage.Resoning != "" && includeReasoning {
			messageContent += singleMessage.Resoning
		}
		if singleMessage.Content != "" {
			messageContent += singleMessage.Content
		}

		message := genai.Content{
			Parts: []genai.Part{},
			Role:  role,
		}

		if messageContent != "" {
			message.Parts = append(message.Parts, genai.Text(messageContent))
		}

		if len(singleMessage.Attachments) != 0 {
			for _, item := range singleMessage.Attachments {
				decodedBytes, err := base64.StdEncoding.DecodeString(item.Content)

				if err != nil {
					util.Slog.Error("failed to decode file bytes", "item", item.Path, "error", err.Error())
					return nil, errors.New("could not prepare attachments for request")
				}

				extension := filepath.Ext(item.Path)
				extension = strings.TrimPrefix(extension, ".")
				part := genai.ImageData(extension, decodedBytes)
				message.Parts = append(message.Parts, part)
			}
		}

		if len(singleMessage.ToolCalls) != 0 {
			for _, tc := range singleMessage.ToolCalls {
				var part genai.Part

				if role == "function" {
					util.Slog.Debug("appending tool call result", "data", tc)
					part = genai.FunctionResponse{
						Name: tc.Function.Name,
						Response: map[string]any{
							"query":  tc.Function.Args["query"],
							"result": *tc.Result,
						}}
				} else {
					util.Slog.Debug("appending tool call request", "data", tc)
					part = genai.FunctionCall{
						Name: tc.Function.Name,
						Args: map[string]any{"query": tc.Function.Args["query"]},
					}
				}

				message.Parts = append(message.Parts, part)
			}
		}

		chat = append(chat, &message)
		util.Slog.Debug("constructed turn", "data", message)
	}

	return chat, nil
}
