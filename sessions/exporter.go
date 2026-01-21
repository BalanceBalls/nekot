package sessions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func ExportSessionToMarkdown(session Session, exportDir string) error {
	if exportDir == "" {
		var err error
		exportDir, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	content := generateMarkdownContent(session)
	filename := sanitizeFilename(session.SessionName) + ".md"
	fullPath := filepath.Join(exportDir, filename)

	if _, err := os.Stat(fullPath); err == nil {
		timestamp := time.Now().Unix()
		filename = fmt.Sprintf("%s_%d.md", sanitizeFilename(session.SessionName), timestamp)
		fullPath = filepath.Join(exportDir, filename)
	}

	return os.WriteFile(fullPath, []byte(content), 0644)
}

func generateMarkdownContent(session Session) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s\n", session.SessionName))
	sb.WriteString(fmt.Sprintf("*Created at: %s*\n\n", session.CreatedAt))
	sb.WriteString("---\n\n")

	for _, msg := range session.Messages {
		role := strings.ToUpper(msg.Role)
		if msg.Model != "" {
			role += fmt.Sprintf(" (%s)", msg.Model)
		}
		sb.WriteString(fmt.Sprintf("## %s\n\n", role))

		if msg.Resoning != "" {
			sb.WriteString("### Reasoning\n")
			sb.WriteString(msg.Resoning)
			sb.WriteString("\n\n")
		}

		if len(msg.ToolCalls) > 0 {
			sb.WriteString("### Tool Calls\n")
			for _, tc := range msg.ToolCalls {
				argsJson, _ := json.Marshal(tc.Function.Args)
				sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", tc.Function.Name, string(argsJson)))
			}
			sb.WriteString("\n")
		}

		if msg.Content != "" {
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")
		}

		if len(msg.Attachments) > 0 {
			sb.WriteString("### Attachments\n")
			for _, att := range msg.Attachments {
				sb.WriteString(fmt.Sprintf("- %s (%s)\n", att.Path, att.Type))
			}
			sb.WriteString("\n")
		}

		sb.WriteString("---\n\n")
	}

	return sb.String()
}

func sanitizeFilename(name string) string {
	re := regexp.MustCompile(`[<>:"/\\|?*]`)
	sanitized := re.ReplaceAllString(name, "_")
	sanitized = strings.Trim(sanitized, " .")
	reSpace := regexp.MustCompile(`[\s_]+`)
	sanitized = reSpace.ReplaceAllString(sanitized, "_")

	if sanitized == "" {
		return "session_export"
	}
	return sanitized
}
