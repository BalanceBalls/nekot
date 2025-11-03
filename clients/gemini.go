package clients

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	chunk     util.CompletionChunk
	isFinal   bool
	citations []string
}

type GeminiClient struct {
	systemMessage string
}

func NewGeminiClient(systemMessage string) *GeminiClient {
	return &GeminiClient{
		systemMessage: systemMessage,
	}
}

func (c GeminiClient) RequestCompletion(
	ctx context.Context,
	chatMsgs []util.MessageToSend,
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
			return util.MakeErrorMsg(err.Error())
		}
		defer client.Close()

		util.Slog.Debug("constructing message", "model", modelSettings.Model)

		model := client.GenerativeModel(modelNamePrefix + modelSettings.Model)
		setParams(model, *config, modelSettings)

		currentPrompt := chatMsgs[len(chatMsgs)-1].Content
		cs := model.StartChat()
		cs.History = buildChatHistory(chatMsgs)

		iter := cs.SendMessageStream(ctx, genai.Text(currentPrompt))

		processResultID := util.ChunkIndexStart
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
			if result.isFinal {
				if len(citations) > 0 {
					sendCitationsChunk(resultChan, processResultID, citations)
					processResultID++
				}
				sendCompensationChunk(resultChan, processResultID)
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
		return util.ProcessModelsResponse{Err: errors.New("Timedout during fetching models")}
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
	util.Slog.Debug("Gemini: compensation chunk sent")
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

			choice.Delta = map[string]any{
				"content": formatResponsePart(candidate.Content.Parts[0]),
			}
		} else {
			choice.Delta = map[string]any{
				"content": "",
			}
		}

		if finishReason != "" {
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
		util.Slog.Error(fmt.Sprintf("unexpected genai.FinishReason", "finish reason", reason))
		return "", errors.New("GeminiAPI: unsupported finish reason")
	}

	return "", nil
}

func buildChatHistory(msgs []util.MessageToSend) []*genai.Content {
	chat := []*genai.Content{}

	for _, singleMessage := range msgs {
		role := "user"
		if singleMessage.Role == "assistant" {
			role = "model"
		}

		if singleMessage.Content != "" {
			message := genai.Content{
				Parts: []genai.Part{
					genai.Text(singleMessage.Content),
				},
				Role: role,
			}
			chat = append(chat, &message)
		}
	}

	return chat
}
