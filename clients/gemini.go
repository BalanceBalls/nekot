package clients

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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
		config, ok := config.FromContext(ctx)
		if !ok {
			fmt.Println("No config found")
			panic("No config found in context")
		}

		client, err := genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:  config.ResolvedAPIKey(),
			Backend: genai.BackendGeminiAPI,
		})
		if err != nil {
			util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{ID: util.ChunkIndexStart, Err: err, Final: true})
			return nil
		}

		util.Slog.Debug("constructing message", "model", modelSettings.Model)

		generationConfig := buildGenerateContentConfig(*config, modelSettings)
		util.Slog.Debug("added tools", "tools", generationConfig.Tools)

		contents, err := buildChatHistory(chatMsgs, *config.IncludeReasoningTokensInContext)
		if err != nil {
			return util.MakeErrorMsg(err.Error())
		}

		stream := client.Models.GenerateContentStream(
			ctx,
			modelSettings.Model,
			contents,
			generationConfig,
		)
		processResultID := util.GetNextProcessResultId(chatMsgs)

		var citations []string
		for resp, err := range stream {
			if err != nil {
				var apiErr genai.APIError
				if errors.As(err, &apiErr) {
					util.Slog.Error(
						"Gemini: Encountered error while receiving response",
						"error",
						apiErr.Message,
					)
					util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{ID: processResultID, Err: apiErr})
				} else {
					util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{ID: processResultID, Err: err})
				}
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
				if len(citations) > 0 {
					sendCitationsChunk(ctx, resultChan, processResultID, citations)
					processResultID++
				}

				sendCompensationChunk(ctx, resultChan, processResultID)
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
	util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{
		ID:     id,
		Result: chunk,
		Final:  false,
	})
}

// Since orchestrator is built for openai apis, need to mimic open ai response structure
// Gemeni sends finish reason with the last response, and openai apis send finish reason with an empty response
func sendCompensationChunk(ctx context.Context, resultChan chan util.ProcessApiCompletionResponse, id int) {
	util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{
		ID: id,
		Result: util.CompletionChunk{
			ID: fmt.Sprint(id),
			Choices: []util.Choice{
				{
					Index: id,
					Delta: map[string]any{
						"content": "",
					},
					FinishReason: "stop",
				},
			},
		},
		Final: true,
	})

	nextId := id + 1
	util.WriteToResponseChannel(ctx, resultChan, util.ProcessApiCompletionResponse{
		ID: nextId,
		Result: util.CompletionChunk{
			ID: fmt.Sprint(nextId),
			Choices: []util.Choice{
				{
					Index: nextId,
					Delta: map[string]any{
						"content": "",
					},
					FinishReason: "",
				},
			},
		},
		Final: true,
	})
	util.Slog.Debug("Gemini: compensation chunks sent")
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
				len(candidate.CitationMetadata.Citations) > 0 {
				for _, source := range candidate.CitationMetadata.Citations {
					if source != nil && source.URI != "" {
						sourceString := fmt.Sprintf("\t> [](%s)", source.URI)
						result.citations = append(result.citations, sourceString)
					}
				}
			}

			hasResponseContent := hasResponseContent(candidate.Content.Parts)
			toolCallParts := functionCallParts(candidate.Content.Parts)

			if len(toolCallParts) > 0 && !hasResponseContent {
				responseToolCalls := []util.ToolCall{}
				util.Slog.Debug("decided to include tool call request")
				for _, part := range toolCallParts {
					tc := part.FunctionCall
					if tc.Name == webSearchTool.FunctionDeclarations[0].Name {
						query, ok := tc.Args["query"].(string)
						if !ok {
							return result, errors.New("GeminiAPI: web search tool call has no string query")
						}
						callID := tc.ID
						if callID == "" {
							callID = "gemini_func"
						}
						responseToolCalls = append(responseToolCalls, util.ToolCall{
							Id:   callID,
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
				}

				choice.ToolCalls = responseToolCalls
				result.isToolCall = true
			}

			if len(toolCallParts) == 0 {
				choice.Delta = map[string]any{
					"content": formatResponseParts(candidate.Content.Parts),
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
			if response.UsageMetadata != nil {
				chunk.Usage = &util.TokenUsage{
					Prompt:     int(response.UsageMetadata.PromptTokenCount),
					Completion: int(response.UsageMetadata.CandidatesTokenCount),
				}
			}

			result.isFinal = true
		}

		chunk.Choices = append(chunk.Choices, choice)
	}

	result.chunk = chunk
	return result, nil
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
	signedToolCallIDs := make(map[string]struct{})

	util.Slog.Debug("building messages history:", "data", msgs)

	for _, singleMessage := range msgs {
		role := "user"
		if singleMessage.Role == "assistant" {
			role = "model"
		}

		if singleMessage.Role == "tool" {
			role = "user"
		}

		messageContent := ""

		if singleMessage.Resoning != "" && includeReasoning {
			messageContent += singleMessage.Resoning
		}
		if singleMessage.Content != "" {
			messageContent += singleMessage.Content
		}

		message := genai.Content{
			Parts: []*genai.Part{},
			Role:  role,
		}

		if messageContent != "" {
			message.Parts = append(message.Parts, genai.NewPartFromText(messageContent))
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
				part := genai.NewPartFromBytes(decodedBytes, "image/"+extension)
				message.Parts = append(message.Parts, part)
			}
		}

		if len(singleMessage.ToolCalls) != 0 {
			for _, tc := range singleMessage.ToolCalls {
				var part *genai.Part

				if singleMessage.Role == "tool" {
					if _, ok := signedToolCallIDs[tc.Id]; !ok {
						util.Slog.Warn(
							"skipping Gemini tool response without a signed function call",
							"tool call id",
							tc.Id,
						)
						continue
					}
					util.Slog.Debug("appending tool call result", "data", tc)
					result := ""
					if tc.Result != nil {
						result = *tc.Result
					}
					part = &genai.Part{
						FunctionResponse: &genai.FunctionResponse{
							ID:   tc.Id,
							Name: tc.Function.Name,
							Response: map[string]any{
								"query":  tc.Function.Args["query"],
								"result": result,
							},
						},
					}
					delete(signedToolCallIDs, tc.Id)
				} else {
					if len(tc.ThoughtSignature) == 0 {
						util.Slog.Warn(
							"skipping Gemini function call without a thought signature",
							"tool call id",
							tc.Id,
						)
						continue
					}
					util.Slog.Debug("appending tool call request", "data", tc)
					part = &genai.Part{
						FunctionCall: &genai.FunctionCall{
							ID:   tc.Id,
							Name: tc.Function.Name,
							Args: map[string]any{"query": tc.Function.Args["query"]},
						},
						ThoughtSignature: append([]byte(nil), tc.ThoughtSignature...),
					}
					signedToolCallIDs[tc.Id] = struct{}{}
				}

				message.Parts = append(message.Parts, part)
			}
		}

		if len(message.Parts) == 0 {
			continue
		}

		chat = append(chat, &message)
		util.Slog.Debug("constructed turn", "data", message)
	}

	return chat, nil
}
