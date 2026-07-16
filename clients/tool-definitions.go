package clients

import "github.com/BalanceBalls/nekot/util"

const (
	webSearchToolName       = "web_search"
	currentDatetimeToolName = "current_datetime"
)

var builtinWebSearchTool = util.ToolDefinition{
	Name:         webSearchToolName,
	OriginalName: webSearchToolName,
	DisplayName:  "Web search",
	Description:  "Perform a web search to retrieve up to date information or knowledge you have doubts about.",
	Source:       util.BuiltinToolSource,
	Parameters: map[string]any{
		"type":     "object",
		"required": []string{"query"},
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query string. Make it specific and moderately detailed for accurate retrieval.",
			},
		},
	},
}

var builtinCurrentDatetimeTool = util.ToolDefinition{
	Name:         currentDatetimeToolName,
	OriginalName: currentDatetimeToolName,
	DisplayName:  "Current date and time",
	Description:  "Get the current local date, time, weekday, timezone, UTC offset, RFC3339 datetime, and Unix timestamp. Use this before web_search when the query depends on today's date or current time.",
	Source:       util.BuiltinToolSource,
	Parameters: map[string]any{
		"type":       "object",
		"required":   []string{},
		"properties": map[string]any{},
	},
}

func ToolsForSettings(settings util.Settings, external []util.ToolDefinition) []util.ToolDefinition {
	tools := make([]util.ToolDefinition, 0, len(external)+2)
	if settings.WebSearchEnabled {
		tools = append(tools, builtinWebSearchTool, builtinCurrentDatetimeTool)
	}
	tools = append(tools, external...)
	return tools
}
