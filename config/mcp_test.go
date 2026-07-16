package config

import (
	"testing"
)

func TestMCPConfigDefaultsAndOverrides(t *testing.T) {
	configuration := MCPConfig{Servers: map[string]MCPServerConfig{
		"local": {
			Transport:          MCPTransportStdio,
			Command:            "server",
			ToolTimeoutSeconds: 7,
		},
	}}
	configuration.setDefaults()
	server := configuration.Servers["local"]
	if configuration.ConnectTimeoutSeconds != DefaultMCPConnectTimeoutSeconds ||
		server.ConnectTimeoutSeconds != DefaultMCPConnectTimeoutSeconds ||
		server.ToolTimeoutSeconds != 7 ||
		server.MaxResultBytes != DefaultMCPMaxResultBytes {
		t.Fatalf("defaults were not applied correctly: %#v / %#v", configuration, server)
	}
	if err := configuration.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestMCPServerValidation(t *testing.T) {
	base := MCPServerConfig{
		Transport:             MCPTransportStreamableHTTP,
		URL:                   "https://example.com/mcp",
		ConnectTimeoutSeconds: 1,
		ToolTimeoutSeconds:    1,
		OAuthTimeoutSeconds:   1,
		MaxResultBytes:        1024,
	}

	tests := []struct {
		name   string
		mutate func(*MCPServerConfig)
	}{
		{name: "invalid ID", mutate: func(*MCPServerConfig) {}},
		{name: "mixed transport", mutate: func(server *MCPServerConfig) { server.Command = "server" }},
		{name: "unprefixed header", mutate: func(server *MCPServerConfig) {
			server.Headers = map[string]string{"X-Token": "secret"}
		}},
		{name: "oauth and authorization header", mutate: func(server *MCPServerConfig) {
			server.Headers = map[string]string{"authorization": "literal:token"}
			server.OAuth = &MCPOAuthConfig{Registration: MCPOAuthDynamic}
		}},
		{name: "invalid tool pattern", mutate: func(server *MCPServerConfig) { server.AllowTools = []string{"["} }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := base
			test.mutate(&server)
			id := "remote"
			if test.name == "invalid ID" {
				id = "not valid"
			}
			if err := server.Validate(id); err == nil {
				t.Fatal("Validate() error = nil")
			}
		})
	}
}

func TestMCPOAuthValidation(t *testing.T) {
	if err := (MCPOAuthConfig{Registration: MCPOAuthDynamic}).Validate(); err != nil {
		t.Fatalf("dynamic Validate() error = %v", err)
	}
	if err := (MCPOAuthConfig{
		Registration: MCPOAuthPreregistered,
		ClientID:     "client",
		RedirectURL:  "http://127.0.0.1:38475/callback",
	}).Validate(); err != nil {
		t.Fatalf("pre-registered Validate() error = %v", err)
	}
	if err := (MCPOAuthConfig{
		Registration: MCPOAuthPreregistered,
		ClientID:     "client",
		RedirectURL:  "http://127.0.0.1/callback",
	}).Validate(); err == nil {
		t.Fatal("pre-registered redirect without a port was accepted")
	}
}

func TestResolveMCPValueSource(t *testing.T) {
	t.Setenv("NEKOT_MCP_TEST_TOKEN", "from-env")
	value, err := ResolveValueSource("env:NEKOT_MCP_TEST_TOKEN")
	if err != nil || value != "from-env" {
		t.Fatalf("ResolveValueSource(env) = %q, %v", value, err)
	}
	value, err = ResolveValueSource("literal:explicit")
	if err != nil || value != "explicit" {
		t.Fatalf("ResolveValueSource(literal) = %q, %v", value, err)
	}
	if _, err := ResolveValueSource("NEKOT_MCP_TEST_TOKEN"); err == nil {
		t.Fatal("unprefixed value source was accepted")
	}
}
