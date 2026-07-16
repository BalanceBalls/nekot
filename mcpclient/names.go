package mcpclient

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"strings"
	"unicode"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxToolNameLength = 64

func toolDefinition(serverID string, cfg config.MCPServerConfig, tool *sdk.Tool) util.ToolDefinition {
	name := namespacedToolName(serverID, tool.Name)
	displayName := tool.Title
	annotations := util.ToolAnnotations{}
	if tool.Annotations != nil {
		annotations = util.ToolAnnotations{
			Title:           tool.Annotations.Title,
			ReadOnlyHint:    tool.Annotations.ReadOnlyHint,
			DestructiveHint: tool.Annotations.DestructiveHint,
			IdempotentHint:  tool.Annotations.IdempotentHint,
			OpenWorldHint:   tool.Annotations.OpenWorldHint,
		}
		if displayName == "" {
			displayName = tool.Annotations.Title
		}
	}
	if displayName == "" {
		displayName = tool.Name
	}

	parameters := tool.InputSchema
	if parameters == nil {
		parameters = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return util.ToolDefinition{
		Name:             name,
		OriginalName:     tool.Name,
		DisplayName:      displayName,
		Description:      tool.Description,
		Parameters:       parameters,
		Source:           util.MCPToolSource,
		ServerID:         serverID,
		RequiresApproval: !matchesAny(cfg.AutoApprove, tool.Name),
		Annotations:      annotations,
	}
}

func namespacedToolName(serverID, toolName string) string {
	sum := sha256.Sum256([]byte(serverID + "\x00" + toolName))
	hash := hex.EncodeToString(sum[:4])
	suffix := "__" + hash
	serverSlug := slug(serverID)
	maxServerLength := maxToolNameLength - len("mcp__") - len("__") - len(suffix) - 1
	if len(serverSlug) > maxServerLength {
		serverSlug = serverSlug[:maxServerLength]
	}
	prefix := "mcp__" + serverSlug + "__"
	available := maxToolNameLength - len(prefix) - len(suffix)
	toolSlug := slug(toolName)
	if available < 1 {
		available = 1
	}
	if len(toolSlug) > available {
		toolSlug = toolSlug[:available]
	}
	return prefix + toolSlug + suffix
}

func slug(value string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(value) {
		valid := unicode.IsLetter(r) || unicode.IsDigit(r)
		if valid && r <= unicode.MaxASCII {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	result := strings.Trim(b.String(), "_")
	if result == "" {
		return "tool"
	}
	return result
}

func toolAllowed(cfg config.MCPServerConfig, name string) bool {
	if matchesAny(cfg.DenyTools, name) {
		return false
	}
	return len(cfg.AllowTools) == 0 || matchesAny(cfg.AllowTools, name)
}

func matchesAny(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if matched, _ := path.Match(pattern, name); matched {
			return true
		}
	}
	return false
}
