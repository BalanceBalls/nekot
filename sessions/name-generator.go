package sessions

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/BalanceBalls/nekot/clients"
	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
	tea "github.com/charmbracelet/bubbletea"
)

var (
	timeFormatRegex = regexp.MustCompile(`^[A-Z][a-z]{2} [A-Z][a-z]{2} \s?\d{1,2} \d{2}:\d{2}:\d{2} \d{4}$`)
)

const maxTitleLength = 50
const reducedTitleSuffix = "..."
const DefaultTitle = "New session"

func IsAutoTitle(name string) bool {
	if timeFormatRegex.MatchString(name) {
		return true
	}
	return false
}

func EnsureMaxTitleLength(title string) string {
	if len(title) < maxTitleLength {
		return title
	}

	title = title[:maxTitleLength-len(reducedTitleSuffix)]
	lastSpace := strings.LastIndex(title, " ")
	if lastSpace > 0 {
		title = title[:lastSpace]
	}

	return title + reducedTitleSuffix
}

func SanitizeTitle(title string) string {
	t := strings.TrimSpace(title)
	t = strings.Trim(t, `"'`)
	t = strings.ReplaceAll(t, "\n", " ")
	t = strings.Join(strings.Fields(t), " ")
	t = strings.TrimRight(t, ".!?:;,")

	t = EnsureMaxTitleLength(t)

	if t == "" {
		return DefaultTitle
	}
	return strings.ToUpper(t[:1]) + t[1:]
}

func GetFirstMeaningfulUserMessage(msgs []util.LocalStoreMessage) (string, bool) {
	for _, m := range msgs {
		if m.Role != "user" {
			continue
		}

		c := strings.TrimSpace(m.Content)
		if len(c) > 5 {
			return c, true
		}
	}
	return "", false
}

type SessionNameGenerator struct {
	mu           sync.Mutex
	isProcessing bool
}

var GlobalSessionNameGenerator = &SessionNameGenerator{}

func (g *SessionNameGenerator) GenerateTitleAsync(
	ctx context.Context,
	cfg config.Config,
	settings util.Settings,
	sessionID int,
	currentName string,
	msgs []util.LocalStoreMessage,
) tea.Cmd {
	return func() tea.Msg {
		if cfg.TitleGeneration == nil || !cfg.TitleGeneration.Enabled {
			return nil
		}

		if !IsAutoTitle(currentName) {
			return nil
		}

		userText, ok := GetFirstMeaningfulUserMessage(msgs)
		if !ok {
			return nil
		}

		g.mu.Lock()
		if g.isProcessing {
			g.mu.Unlock()
			return g.fallbackHeuristic(sessionID, userText)
		}
		g.isProcessing = true
		g.mu.Unlock()

		defer func() {
			g.mu.Lock()
			g.isProcessing = false
			g.mu.Unlock()
		}()

		titleCtx, cancel, resultChan := g.requestTitleCompletion(ctx, cfg, settings, userText)
		defer cancel()

		title, success := g.waitForTitleGeneration(titleCtx, resultChan)

		if !success || strings.TrimSpace(title) == "" {
			return g.fallbackHeuristic(sessionID, userText)
		}
		finalTitle := SanitizeTitle(title)
		if finalTitle == DefaultTitle {
			return g.fallbackHeuristic(sessionID, userText)
		}
		return SendSessionTitleGeneratedMsg(sessionID, finalTitle)()
	}
}

func (g *SessionNameGenerator) waitForTitleGeneration(
	titleCtx context.Context,
	resultChan <-chan util.ProcessApiCompletionResponse,
) (string, bool) {
	var titleBuilder strings.Builder
	success := false
loop:
	for {
		select {
		case <-titleCtx.Done():
			util.Slog.Warn("name generation timed out")
			break loop
		case res := <-resultChan:
			if res.Err != nil {
				util.Slog.Error("name generation error", "err", res.Err.Error())
				break loop
			}
			if len(res.Result.Choices) > 0 {
				if content, ok := res.Result.Choices[0].Delta["content"].(string); ok {
					titleBuilder.WriteString(content)
				}
			}
			if res.Final {
				success = true
				break loop
			}
		}
	}
	return titleBuilder.String(), success
}

func (g *SessionNameGenerator) requestTitleCompletion(
	ctx context.Context,
	cfg config.Config,
	settings util.Settings,
	userText string,
) (context.Context, context.CancelFunc, <-chan util.ProcessApiCompletionResponse) {

	timeout := time.Duration(cfg.TitleGeneration.TimeoutSeconds)
	titleCtx, cancel := context.WithTimeout(ctx, timeout*time.Second)
	llmClient := clients.ResolveLlmClient(
		cfg.Provider,
		cfg.ProviderBaseUrl,
		cfg.SystemMessage,
	)
	modelSettings := settings
	if modelSettings.Model == "" {
		modelSettings.Model = cfg.DefaultModel
	}
	temp := float32(0.2)
	modelSettings.Temperature = &temp
	modelSettings.WebSearchEnabled = false
	genPrompt := "Think ultra fast - every second counts. Generate a concise session title from the data above. Return only the title. Use 4-7 words, plain text, no quotes, no markdown, and no punctuation at the end."
	dummyMsgs := []util.LocalStoreMessage{
		{Role: "user", Content: userText},
		{Role: "user", Content: genPrompt},
	}
	resultChan := make(chan util.ProcessApiCompletionResponse)
	go llmClient.RequestCompletion(titleCtx, dummyMsgs, modelSettings, resultChan)()

	return titleCtx, cancel, resultChan
}

func (g *SessionNameGenerator) fallbackHeuristic(sessionID int, userText string) tea.Msg {
	title := SanitizeTitle(userText)
	return SendSessionTitleGeneratedMsg(sessionID, title)()
}
