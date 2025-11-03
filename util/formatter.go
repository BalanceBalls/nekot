package util

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

func GetMessagesAsPrettyString(
	msgsToRender []MessageToSend,
	w int,
	colors SchemeColors,
	isQuickChat bool,
) string {
	var messages string

	for _, message := range msgsToRender {
		messageToUse := message.Content

		switch message.Role {
		case "user":
			messageToUse = RenderUserMessage(messageToUse, w, colors, false)
		case "assistant":
			messageToUse = RenderBotMessage(message, w, colors, false)
		}

		if messages == "" {
			messages = messageToUse
			continue
		}

		messages = messages + "\n" + messageToUse
	}

	if isQuickChat {
		quickChatDisclaimer := GetQuickChatDisclaimer(w, colors)
		messages = quickChatDisclaimer + "\n" + messages
	}

	return messages
}

func GetVisualModeView(msgsToRender []MessageToSend, w int, colors SchemeColors) string {
	var messages string
	w = w - TextSelectorMaxWidthCorrection
	for _, message := range msgsToRender {
		messageToUse := message.Content

		switch message.Role {
		case "user":
			messageToUse = RenderUserMessage(messageToUse, w, colors, true)
		case "assistant":
			messageToUse = RenderBotMessage(message, w, colors, true)
		}

		if messages == "" {
			messages = messageToUse
			continue
		}

		messages = messages + "\n" + messageToUse
	}

	return messages
}

func RenderUserMessage(msg string, width int, colors SchemeColors, isVisualMode bool) string {
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(width-WordWrapDelta),
		colors.RendererThemeOption,
	)
	if isVisualMode {
		msg = "\n💁 " + msg
		userMsg, _ := renderer.Render(msg)
		output := strings.TrimSpace(userMsg)
		return lipgloss.NewStyle().Render("\n" + output + "\n")
	}

	msg = "\n💁 " + msg + "\n"
	userMsg, _ := renderer.Render(msg)
	output := strings.TrimSpace(userMsg)
	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.InnerHalfBlockBorder()).
		BorderLeftForeground(colors.NormalTabBorderColor).
		Render("\n" + output + "\n")
}

func RenderErrorMessage(msg string, width int, colors SchemeColors) string {
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(width-WordWrapDelta),
		colors.RendererThemeOption,
	)
	msg = " ⛔ **Encountered error:**\n ```json\n" + msg + "\n```"
	errMsg, _ := renderer.Render(msg)
	errOutput := strings.TrimSpace(errMsg)

	instructions, _ := renderer.Render(
		"\n## Inspect the error, fix the problem and restart the app\n\n" + ErrorHelp,
	)
	instructionsOutput := strings.TrimSpace(instructions)

	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.InnerHalfBlockBorder()).
		BorderLeftForeground(colors.ErrorColor).
		Width(width).
		Foreground(colors.HighlightColor).
		Render(errOutput + "\n\n" + instructionsOutput)
}

func RenderBotMessage(
	msg MessageToSend,
	width int,
	colors SchemeColors,
	isVisualMode bool,
) string {
	if msg.Content == "" {
		return ""
	}

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(width-WordWrapDelta),
		colors.RendererThemeOption,
	)

	content := msg.Content
	// markdown renderer glitches when code block appears on a line with different text
	if strings.HasPrefix(content, "```") {
		content = "\n" + content
	}

	if isVisualMode {
		content = "\n🤖 " + content
		userMsg, _ := renderer.Render(content)
		output := strings.TrimSpace(userMsg)
		return lipgloss.NewStyle().Render("\n" + output + "\n")
	}

	modelName := ""
	if len(msg.Model) > 0 {
		modelName = "**" + msg.Model + "**\n"
	}
	content = "\n🤖 " + modelName + content + "\n"
	aiResponse, _ := renderer.Render(content)
	output := strings.TrimSpace(aiResponse)
	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.InnerHalfBlockBorder()).
		BorderLeftForeground(colors.ActiveTabBorderColor).
		Width(width - 1).
		Render(output)
}

func GetQuickChatDisclaimer(w int, colors SchemeColors) string {
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		colors.RendererThemeOption,
	)

	output, _ := renderer.Render(QuickChatWarning)
	return lipgloss.NewStyle().
		MaxWidth(w).
		Render(output)
}

func GetManual(w int, colors SchemeColors) string {
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(40),
		colors.RendererThemeOption,
	)
	output, _ := renderer.Render(ManualContent)
	return lipgloss.NewStyle().
		MaxWidth(w).
		Render(output)
}

func StripAnsiCodes(str string) string {
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[mG]`)
	return ansiRegex.ReplaceAllString(str, "")
}
