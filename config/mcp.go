package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
)

const (
	DefaultMCPConnectTimeoutSeconds = 10
	DefaultMCPToolTimeoutSeconds    = 60
	DefaultMCPOAuthTimeoutSeconds   = 120
	DefaultMCPMaxResultBytes        = 1024 * 1024

	MCPTransportStdio          = "stdio"
	MCPTransportStreamableHTTP = "streamable-http"

	MCPOAuthDynamic       = "dynamic"
	MCPOAuthPreregistered = "preregistered"
)

var mcpServerIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type MCPConfig struct {
	ConnectTimeoutSeconds int                        `json:"connectTimeoutSeconds"`
	ToolTimeoutSeconds    int                        `json:"toolTimeoutSeconds"`
	OAuthTimeoutSeconds   int                        `json:"oauthTimeoutSeconds"`
	MaxResultBytes        int64                      `json:"maxResultBytes"`
	Servers               map[string]MCPServerConfig `json:"servers"`
}

type MCPServerConfig struct {
	Transport             string            `json:"transport"`
	DefaultEnabled        bool              `json:"defaultEnabled"`
	Command               string            `json:"command"`
	Args                  []string          `json:"args"`
	Cwd                   string            `json:"cwd"`
	Env                   map[string]string `json:"env"`
	URL                   string            `json:"url"`
	Headers               map[string]string `json:"headers"`
	OAuth                 *MCPOAuthConfig   `json:"oauth"`
	AllowTools            []string          `json:"allowTools"`
	DenyTools             []string          `json:"denyTools"`
	AutoApprove           []string          `json:"autoApprove"`
	ConnectTimeoutSeconds int               `json:"connectTimeoutSeconds"`
	ToolTimeoutSeconds    int               `json:"toolTimeoutSeconds"`
	OAuthTimeoutSeconds   int               `json:"oauthTimeoutSeconds"`
	MaxResultBytes        int64             `json:"maxResultBytes"`
}

type MCPOAuthConfig struct {
	Registration       string   `json:"registration"`
	ClientID           string   `json:"clientId"`
	ClientSecretSource string   `json:"clientSecretSource"`
	Scopes             []string `json:"scopes"`
	RedirectURL        string   `json:"redirectURL"`
}

func (c *MCPConfig) setDefaults() {
	if c.ConnectTimeoutSeconds <= 0 {
		c.ConnectTimeoutSeconds = DefaultMCPConnectTimeoutSeconds
	}
	if c.ToolTimeoutSeconds <= 0 {
		c.ToolTimeoutSeconds = DefaultMCPToolTimeoutSeconds
	}
	if c.OAuthTimeoutSeconds <= 0 {
		c.OAuthTimeoutSeconds = DefaultMCPOAuthTimeoutSeconds
	}
	if c.MaxResultBytes <= 0 {
		c.MaxResultBytes = DefaultMCPMaxResultBytes
	}
	if c.Servers == nil {
		c.Servers = map[string]MCPServerConfig{}
	}

	for id, server := range c.Servers {
		if server.ConnectTimeoutSeconds <= 0 {
			server.ConnectTimeoutSeconds = c.ConnectTimeoutSeconds
		}
		if server.ToolTimeoutSeconds <= 0 {
			server.ToolTimeoutSeconds = c.ToolTimeoutSeconds
		}
		if server.OAuthTimeoutSeconds <= 0 {
			server.OAuthTimeoutSeconds = c.OAuthTimeoutSeconds
		}
		if server.MaxResultBytes <= 0 {
			server.MaxResultBytes = c.MaxResultBytes
		}
		if server.OAuth != nil && server.OAuth.Registration == "" {
			server.OAuth.Registration = MCPOAuthDynamic
		}
		c.Servers[id] = server
	}
}

func (c MCPConfig) WithDefaults() MCPConfig {
	c.setDefaults()
	return c
}

func (c Config) ReloadMCPConfig() (MCPConfig, error) {
	if c.sourcePath == "" {
		return MCPConfig{}, fmt.Errorf("configuration source path is not available")
	}

	data, err := os.ReadFile(c.sourcePath)
	if err != nil {
		return MCPConfig{}, err
	}

	var loaded struct {
		MCP MCPConfig `json:"mcp"`
	}
	if err := json.Unmarshal(data, &loaded); err != nil {
		return MCPConfig{}, err
	}
	loaded.MCP.setDefaults()
	if err := loaded.MCP.Validate(); err != nil {
		return MCPConfig{}, err
	}
	return loaded.MCP, nil
}

func (c MCPConfig) Validate() error {
	if c.ConnectTimeoutSeconds <= 0 || c.ToolTimeoutSeconds <= 0 || c.OAuthTimeoutSeconds <= 0 {
		return fmt.Errorf("MCP timeouts must be greater than zero")
	}
	if c.MaxResultBytes <= 0 {
		return fmt.Errorf("MCP maxResultBytes must be greater than zero")
	}
	for id, server := range c.Servers {
		if err := server.Validate(id); err != nil {
			return fmt.Errorf("MCP server %q: %w", id, err)
		}
	}
	return nil
}

func (s MCPServerConfig) Validate(id string) error {
	if !mcpServerIDPattern.MatchString(id) {
		return fmt.Errorf("server ID must contain only letters, numbers, underscores, and dashes")
	}
	if s.ConnectTimeoutSeconds <= 0 || s.ToolTimeoutSeconds <= 0 || s.OAuthTimeoutSeconds <= 0 {
		return fmt.Errorf("timeouts must be greater than zero")
	}
	if s.MaxResultBytes <= 0 {
		return fmt.Errorf("maxResultBytes must be greater than zero")
	}

	switch s.Transport {
	case MCPTransportStdio:
		if strings.TrimSpace(s.Command) == "" {
			return fmt.Errorf("stdio transport requires command")
		}
		if s.URL != "" || s.OAuth != nil || len(s.Headers) > 0 {
			return fmt.Errorf("stdio transport cannot define url, headers, or oauth")
		}
	case MCPTransportStreamableHTTP:
		parsed, err := url.Parse(s.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("streamable-http transport requires a valid http or https URL")
		}
		if s.Command != "" || len(s.Args) > 0 || s.Cwd != "" || len(s.Env) > 0 {
			return fmt.Errorf("streamable-http transport cannot define command, args, cwd, or env")
		}
	default:
		return fmt.Errorf("unsupported transport %q", s.Transport)
	}

	for name, source := range s.Env {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("environment variable name cannot be empty")
		}
		if err := ValidateValueSource(source); err != nil {
			return fmt.Errorf("environment variable %q: %w", name, err)
		}
	}
	for name, source := range s.Headers {
		if strings.TrimSpace(name) == "" || http.CanonicalHeaderKey(name) == "" {
			return fmt.Errorf("header name cannot be empty")
		}
		if err := ValidateValueSource(source); err != nil {
			return fmt.Errorf("header %q: %w", name, err)
		}
	}

	if s.OAuth != nil {
		if s.Transport != MCPTransportStreamableHTTP {
			return fmt.Errorf("oauth is supported only for streamable-http servers")
		}
		for name := range s.Headers {
			if strings.EqualFold(name, "Authorization") {
				return fmt.Errorf("Authorization header cannot be combined with oauth")
			}
		}
		if err := s.OAuth.Validate(); err != nil {
			return err
		}
	}

	for _, list := range [][]string{s.AllowTools, s.DenyTools, s.AutoApprove} {
		for _, pattern := range list {
			if strings.TrimSpace(pattern) == "" {
				return fmt.Errorf("tool patterns cannot be empty")
			}
			if _, err := path.Match(pattern, "tool"); err != nil {
				return fmt.Errorf("invalid tool pattern %q: %w", pattern, err)
			}
		}
	}

	return nil
}

func (c MCPOAuthConfig) Validate() error {
	switch c.Registration {
	case MCPOAuthDynamic:
		if c.ClientID != "" || c.ClientSecretSource != "" {
			return fmt.Errorf("dynamic oauth registration cannot define clientId or clientSecretSource")
		}
		if c.RedirectURL != "" {
			return fmt.Errorf("dynamic oauth registration uses an automatically assigned loopback redirectURL")
		}
	case MCPOAuthPreregistered:
		if strings.TrimSpace(c.ClientID) == "" {
			return fmt.Errorf("pre-registered oauth requires clientId")
		}
		if strings.TrimSpace(c.RedirectURL) == "" {
			return fmt.Errorf("pre-registered oauth requires redirectURL")
		}
		if c.ClientSecretSource != "" {
			if err := ValidateValueSource(c.ClientSecretSource); err != nil {
				return fmt.Errorf("clientSecretSource: %w", err)
			}
		}
	default:
		return fmt.Errorf("unsupported oauth registration %q", c.Registration)
	}

	if c.RedirectURL != "" {
		parsed, err := url.Parse(c.RedirectURL)
		if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" {
			return fmt.Errorf("oauth redirectURL must use http://127.0.0.1 with an explicit port")
		}
	}
	for _, scope := range c.Scopes {
		if strings.TrimSpace(scope) == "" {
			return fmt.Errorf("oauth scopes cannot contain empty values")
		}
	}
	return nil
}

func ValidateValueSource(source string) error {
	for _, prefix := range []string{"env:", "cmd:", "literal:"} {
		if strings.HasPrefix(source, prefix) && strings.TrimSpace(strings.TrimPrefix(source, prefix)) != "" {
			return nil
		}
	}
	return fmt.Errorf("value must use env:, cmd:, or literal: source")
}

func ResolveValueSource(source string) (string, error) {
	if err := ValidateValueSource(source); err != nil {
		return "", err
	}

	switch {
	case strings.HasPrefix(source, "env:"):
		name := strings.TrimSpace(strings.TrimPrefix(source, "env:"))
		value, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("environment variable %s is not set", name)
		}
		return value, nil
	case strings.HasPrefix(source, "cmd:"):
		command := strings.TrimSpace(strings.TrimPrefix(source, "cmd:"))
		output, err := runAPIKeyCommand(command)
		if err != nil {
			return "", fmt.Errorf("credential command failed: %w", err)
		}
		value := strings.TrimSpace(string(output))
		if value == "" {
			return "", fmt.Errorf("credential command returned an empty value")
		}
		return value, nil
	default:
		return strings.TrimPrefix(source, "literal:"), nil
	}
}
