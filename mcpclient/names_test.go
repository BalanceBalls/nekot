package mcpclient

import (
	"strings"
	"testing"

	"github.com/BalanceBalls/nekot/config"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNamespacedToolNameIsStableAndBounded(t *testing.T) {
	name := namespacedToolName(
		"Server With A Very Long Name",
		"Tool With Another Extremely Long Name That Must Be Truncated Without Losing Routing",
	)
	if len(name) > maxToolNameLength {
		t.Fatalf("len(name) = %d, want <= %d: %q", len(name), maxToolNameLength, name)
	}
	if !strings.HasPrefix(name, "mcp__server_with_a_very_long_name__") {
		t.Fatalf("name = %q", name)
	}
	if name != namespacedToolName(
		"Server With A Very Long Name",
		"Tool With Another Extremely Long Name That Must Be Truncated Without Losing Routing",
	) {
		t.Fatal("name is not stable")
	}
	if name == namespacedToolName("other", "Tool With Another Extremely Long Name That Must Be Truncated Without Losing Routing") {
		t.Fatal("server ID is not represented in the namespace")
	}
	if got := namespacedToolName(strings.Repeat("server", 30), "tool"); len(got) > maxToolNameLength {
		t.Fatalf("long server name produced %d characters: %q", len(got), got)
	}
}

func TestToolDefinitionAppliesFiltersAndApproval(t *testing.T) {
	cfg := config.MCPServerConfig{
		AllowTools:  []string{"read_*", "delete_file"},
		DenyTools:   []string{"delete_*"},
		AutoApprove: []string{"read_*"},
	}
	if !toolAllowed(cfg, "read_file") || toolAllowed(cfg, "delete_file") || toolAllowed(cfg, "write_file") {
		t.Fatal("allow/deny precedence is incorrect")
	}
	definition := toolDefinition("filesystem", cfg, &sdk.Tool{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: map[string]any{"type": "object"},
	})
	if definition.RequiresApproval {
		t.Fatal("auto-approved tool requires approval")
	}
	if definition.ServerID != "filesystem" || definition.OriginalName != "read_file" {
		t.Fatalf("definition routing metadata = %#v", definition)
	}
}
