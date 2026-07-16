package mcpclient

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BalanceBalls/nekot/config"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	_ "modernc.org/sqlite"
)

func TestManagerDiscoversFiltersAndExecutesHTTPTool(t *testing.T) {
	server := sdk.NewServer(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	server.AddTool(&sdk.Tool{
		Name:        "echo",
		Description: "Echo nested input",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"payload": map[string]any{"type": "object"},
			},
		},
	}, func(_ context.Context, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		var arguments map[string]any
		if err := json.Unmarshal(request.Params.Arguments, &arguments); err != nil {
			return nil, err
		}
		return &sdk.CallToolResult{
			Content:           []sdk.Content{&sdk.TextContent{Text: "ok"}},
			StructuredContent: arguments,
		}, nil
	})
	server.AddTool(&sdk.Tool{Name: "hidden", InputSchema: map[string]any{"type": "object"}}, nil)
	httpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return server }, nil,
	))
	t.Cleanup(httpServer.Close)

	db := newManagerTestDB(t)
	cfg := config.Config{MCP: config.MCPConfig{Servers: map[string]config.MCPServerConfig{
		"remote": {
			Transport:      config.MCPTransportStreamableHTTP,
			DefaultEnabled: true,
			URL:            httpServer.URL,
			AllowTools:     []string{"echo", "new_tool"},
			AutoApprove:    []string{"echo"},
		},
	}}}
	manager := newTestManager(t, db, cfg)
	if err := manager.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	waitForServerState(t, manager, "remote", ServerConnected)
	tools := manager.Tools()
	if len(tools) != 1 || tools[0].OriginalName != "echo" || tools[0].RequiresApproval {
		t.Fatalf("tools = %#v", tools)
	}
	result := manager.Execute(t.Context(), tools[0].Name, map[string]any{
		"payload": map[string]any{"count": float64(2)},
	})
	if result.IsError || !containsJSONField(t, result.Result, "structured") {
		t.Fatalf("Execute() = %#v", result)
	}

	status := manager.Statuses()[0]
	if status.DiscoveredTools != 2 || status.ExposedTools != 1 {
		t.Fatalf("status = %#v", status)
	}

	server.AddTool(&sdk.Tool{
		Name: "new_tool", InputSchema: map[string]any{"type": "object"},
	}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "new"}}}, nil
	})
	waitForToolCount(t, manager, 2)
}

func TestManagerDiscoversAndExecutesStdioTool(t *testing.T) {
	db := newManagerTestDB(t)
	cfg := config.Config{MCP: config.MCPConfig{Servers: map[string]config.MCPServerConfig{
		"local": {
			Transport:      config.MCPTransportStdio,
			DefaultEnabled: true,
			Command:        os.Args[0],
			Args:           []string{"-test.run=TestMCPHelperProcess"},
			Env:            map[string]string{"NEKOT_MCP_HELPER": "literal:1"},
			AutoApprove:    []string{"echo"},
		},
	}}}
	manager := newTestManager(t, db, cfg)
	if err := manager.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	waitForServerState(t, manager, "local", ServerConnected)
	tools := manager.Tools()
	if len(tools) != 1 || tools[0].OriginalName != "echo" {
		t.Fatalf("tools = %#v", tools)
	}
	result := manager.Execute(t.Context(), tools[0].Name, map[string]any{"value": "stdio"})
	if result.IsError || !containsJSONField(t, result.Result, "text") {
		t.Fatalf("Execute() = %#v", result)
	}
}

func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("NEKOT_MCP_HELPER") != "1" {
		return
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "stdio-test", Version: "1"}, nil)
	server.AddTool(&sdk.Tool{
		Name: "echo", InputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		var arguments map[string]any
		if err := json.Unmarshal(request.Params.Arguments, &arguments); err != nil {
			return nil, err
		}
		return &sdk.CallToolResult{Content: []sdk.Content{
			&sdk.TextContent{Text: arguments["value"].(string)},
		}}, nil
	})
	if err := server.Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestPersistedEnablementOverridesDefault(t *testing.T) {
	db := newManagerTestDB(t)
	if err := saveEnabledState(db, "local", false); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MCP: config.MCPConfig{Servers: map[string]config.MCPServerConfig{
		"local": {Transport: config.MCPTransportStdio, Command: "does-not-run", DefaultEnabled: true},
	}}}
	manager := newTestManager(t, db, cfg)
	if err := manager.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	status := manager.Statuses()[0]
	if status.Enabled || status.State != ServerDisabled {
		t.Fatalf("status = %#v", status)
	}
}

func newManagerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE mcp_server_state (
		server_id TEXT PRIMARY KEY,
		enabled INTEGER NOT NULL,
		updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestManager(t *testing.T, db *sql.DB, cfg config.Config) *Manager {
	t.Helper()
	store := &oauthStore{
		path:    filepath.Join(t.TempDir(), "mcp-oauth.json"),
		records: make(map[string]oauthRecord),
	}
	return &Manager{
		db:          db,
		rootConfig:  cfg,
		config:      cfg.MCP.WithDefaults(),
		servers:     make(map[string]*managedServer),
		authStore:   store,
		openBrowser: func(string) error { return nil },
	}
}

func waitForServerState(t *testing.T, manager *Manager, id string, wanted ServerState) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, status := range manager.Statuses() {
			if status.ID == id && status.State == wanted {
				return
			}
			if status.ID == id && status.State == ServerFailed {
				t.Fatalf("server failed: %s", status.LastError)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server %q did not reach state %q: %#v", id, wanted, manager.Statuses())
}

func waitForToolCount(t *testing.T, manager *Manager, wanted int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(manager.Tools()) == wanted {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("tool count = %d, want %d", len(manager.Tools()), wanted)
}

func containsJSONField(t *testing.T, data, field string) bool {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(data), &value); err != nil {
		t.Fatal(err)
	}
	_, exists := value[field]
	return exists
}
