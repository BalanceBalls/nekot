package util

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

func GetMessagesAsPrettyString(
	msgsToRender []LocalStoreMessage,
	w int,
	colors SchemeColors,
	isQuickChat bool,
) string {
	var messages string

	for _, message := range msgsToRender {

		if message.Content == "" && len(message.ToolCalls) > 0 && message.Role != "tool" {
			continue
		}

		messageToUse := message.Content

		switch message.Role {
		case "user":
			messageToUse = RenderUserMessage(message, w, colors, false)
		case "assistant":
			messageToUse = RenderBotMessage(message, w, colors, false)
		case "tool":
			messageToUse = RenderToolCall(message, w, colors, false)
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

func GetVisualModeView(msgsToRender []LocalStoreMessage, w int, colors SchemeColors) string {
	var messages string
	w = w - TextSelectorMaxWidthCorrection
	for _, message := range msgsToRender {
		if message.Content == "" && len(message.ToolCalls) > 0 && message.Role != "tool" {
			continue
		}

		messageToUse := message.Content

		switch message.Role {
		case "user":
			messageToUse = RenderUserMessage(message, w, colors, true)
		case "assistant":
			messageToUse = RenderBotMessage(message, w, colors, true)
		case "tool":
			messageToUse = RenderToolCall(message, w, colors, true)
		}

		if messages == "" {
			messages = messageToUse
			continue
		}

		messages = messages + "\n" + messageToUse
	}

	return messages
}

func RenderUserMessage(userMessage LocalStoreMessage, width int, colors SchemeColors, isVisualMode bool) string {
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(width-WordWrapDelta),
		colors.RendererThemeOption,
	)
	msg := userMessage.Content
	if isVisualMode {
		msg = "\n💁 " + msg
		userMsg, _ := renderer.Render(msg)
		output := strings.TrimSpace(userMsg)
		return lipgloss.NewStyle().Render("\n" + output + "\n")
	}

	msg = "\n💁 **[Prooompter]**\n" + msg + "\n"
	if len(userMessage.Attachments) != 0 {
		attachments := "\n *Attachments:* \n"
		for _, file := range userMessage.Attachments {
			fileName := filepath.Base(file.Path)
			attachments += "# [" + fileName + "] \n"
		}
		msg += attachments
	}

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
	msg LocalStoreMessage,
	width int,
	colors SchemeColors,
	isVisualMode bool,
) string {

	if len(msg.ToolCalls) == 0 && msg.Content == "" {
		return ""
	}

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(width-WordWrapDelta),
		colors.RendererThemeOption,
	)

	content := ""
	if msg.Resoning != "" {
		reasoningLines := strings.Split(msg.Resoning, "\n")

		content += "\n" + "## Reasoning content:" + "\n"
		content += "<div>--------------------</div>\n"

		for _, reasoningLine := range reasoningLines {
			if reasoningLine == "" || reasoningLine == "\n" {
				continue
			}

			content += "<div>" + reasoningLine + "</div>\n"
		}
		content += "<div>--------------------</div>\n"
		content += "\n  \n"
	}

	// markdown renderer glitches when code block appears on a line with different text
	if strings.HasPrefix(msg.Content, "```") {
		msg.Content = "\n" + msg.Content
	}

	content += msg.Content
	modelName := ""
	icon := "\n 🤖 "
	if len(msg.Model) > 0 {
		modelName = "**[" + msg.Model + "]**\n"
	}

	if isVisualMode {
		content = icon + content
		userMsg, _ := renderer.Render(content)
		output := strings.TrimSpace(userMsg)
		return lipgloss.NewStyle().Render(output + "\n")
	}

	content = icon + modelName + content + "\n"
	aiResponse, _ := renderer.Render(content)
	output := strings.TrimSpace(aiResponse)
	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.InnerHalfBlockBorder()).
		BorderLeftForeground(colors.ActiveTabBorderColor).
		Width(width - 1).
		Render(output)
}

func RenderToolCall(msg LocalStoreMessage,
	width int,
	colors SchemeColors,
	isVisualMode bool) string {

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(width-WordWrapDelta),
		colors.RendererThemeOption,
	)

	content := ""

	if msg.Role == "tool" {

		//toolData := "\n" + "## Tool calls:" + "\n"
		toolData := "<div>--------------------</div>\n"

		for _, tc := range msg.ToolCalls {
			toolData += fmt.Sprintf("<div>[Executed tool call: %s] Args: %v </div>\n", tc.Function.Name, tc.Function.Args)
		}
		toolData += "<div>--------------------</div>\n"
		toolData += "\n  \n"

		content += toolData
	}

	if isVisualMode {
		userMsg, _ := renderer.Render(content)
		output := strings.TrimSpace(userMsg)
		return lipgloss.NewStyle().Render(output + "\n")
	}

	aiResponse, _ := renderer.Render(content)
	output := strings.TrimSpace(aiResponse)
	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.InnerHalfBlockBorder()).
		BorderLeftForeground(colors.HighlightColor).
		Width(width - 1).
		Render(output)
}

func RenderBotChunk(
	chunk string,
	width int,
	colors SchemeColors) string {

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(width-WordWrapDelta),
		colors.RendererThemeOption,
	)
	userMsg, _ := renderer.Render(chunk)
	output := strings.TrimSpace(userMsg)
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
