package util

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

func GetMessagesAsPrettyString(msgsToRender []MessageToSend, w int, colors SchemeColors) string {
	var messages string
	for _, message := range msgsToRender {
		messageToUse := message.Content

		switch {
		case message.Role == "user":
			messageToUse = RenderUserMessage(messageToUse, w, colors, false)
		case message.Role == "assistant":
			messageToUse = RenderBotMessage(messageToUse, w, colors, false)
		}

		if messages == "" {
			messages = messageToUse
			continue
		}

		messages = messages + "\n" + messageToUse
	}

	return messages
}

func GetVisualModeView(msgsToRender []MessageToSend, w int, colors SchemeColors) string {
	var messages string
	for _, message := range msgsToRender {
		messageToUse := message.Content

		switch {
		case message.Role == "user":
			messageToUse = RenderUserMessage(messageToUse, w, colors, true)
		case message.Role == "assistant":
			messageToUse = RenderBotMessage(messageToUse, w, colors, true)
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

	instructions, _ := renderer.Render("\n## Inspect the error, fix the problem and restart the app\n\n" + ErrorHelp)
	instructionsOutput := strings.TrimSpace(instructions)

	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.InnerHalfBlockBorder()).
		BorderLeftForeground(colors.ErrorColor).
		Width(width).
		Foreground(colors.HighlightColor).
		Render(errOutput + "\n\n" + instructionsOutput)
}

func RenderBotMessage(msg string, width int, colors SchemeColors, isVisualMode bool) string {
	if msg == "" {
		return ""
	}

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(width-WordWrapDelta),
		colors.RendererThemeOption,
	)

	// markdown renderer glitches when code block appears on a line with different text
	if strings.HasPrefix(msg, "```") {
		msg = "\n" + msg
	}

	if isVisualMode {
		msg = "\n🤖 " + msg
		userMsg, _ := renderer.Render(msg)
		output := strings.TrimSpace(userMsg)
		return lipgloss.NewStyle().Render("\n" + output + "\n")
	}

	msg = "\n🤖 " + msg + "\n"
	aiResponse, _ := renderer.Render(msg)
	output := strings.TrimSpace(aiResponse)
	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.InnerHalfBlockBorder()).
		BorderLeftForeground(colors.ActiveTabBorderColor).
		Width(width - 1).
		Render(output)
}

func StripAnsiCodes(str string) string {
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[mG]`)
	return ansiRegex.ReplaceAllString(str, "")
}
