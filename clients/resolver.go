package clients

import "github.com/BalanceBalls/nekot/util"

func ResolveLlmClient(apiType string, apiUrl string, systemMessage string) util.LlmClient {
	switch apiType {
	case util.OpenAiProviderType:
		return NewOpenAiClient(apiUrl, systemMessage)
	case util.GeminiProviderType:
		return NewGeminiClient(systemMessage)
	case util.OpenrouterProviderType:
		return NewOpenrouterClient(systemMessage)
	default:
		panic("Api type not supported: " + apiType)
	}
}
