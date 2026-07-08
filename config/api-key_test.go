package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/BalanceBalls/nekot/util"
)

func TestResolveAPIKeyFromCommand(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "environment-key")

	cfg := Config{
		Provider:             util.OpenrouterProviderType,
		APIKeyResolveCommand: "cmd:" + commandReturning(" command-key "),
	}

	if err := cfg.resolveAPIKey(); err != nil {
		t.Fatalf("resolveAPIKey() error = %v", err)
	}
	if got := cfg.ResolvedAPIKey(); got != "command-key" {
		t.Fatalf("ResolvedAPIKey() = %q, want %q", got, "command-key")
	}
}

func TestResolveAPIKeyFromProviderEnvironment(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		envName  string
	}{
		{name: "OpenAI", provider: util.OpenAiProviderType, envName: "OPENAI_API_KEY"},
		{name: "Gemini", provider: util.GeminiProviderType, envName: "GEMINI_API_KEY"},
		{name: "OpenRouter", provider: util.OpenrouterProviderType, envName: "OPENROUTER_API_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envName, "environment-key")
			cfg := Config{
				Provider:        tt.provider,
				ProviderBaseUrl: "https://example.com",
			}

			if err := cfg.resolveAPIKey(); err != nil {
				t.Fatalf("resolveAPIKey() error = %v", err)
			}
			if got := cfg.ResolvedAPIKey(); got != "environment-key" {
				t.Fatalf("ResolvedAPIKey() = %q, want %q", got, "environment-key")
			}
		})
	}
}

func TestResolveAPIKeyCommandFailureDoesNotFallback(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "environment-key")

	cfg := Config{
		Provider:             util.OpenAiProviderType,
		ProviderBaseUrl:      "https://api.openai.com",
		APIKeyResolveCommand: "cmd:" + failingCommand("leaked-command-output"),
	}

	err := cfg.resolveAPIKey()
	if err == nil {
		t.Fatal("resolveAPIKey() error = nil, want command failure")
	}
	if strings.Contains(err.Error(), "leaked-command-output") {
		t.Fatalf("resolveAPIKey() error exposed command output: %v", err)
	}
	if got := cfg.ResolvedAPIKey(); got != "" {
		t.Fatalf("ResolvedAPIKey() = %q, want empty value", got)
	}
}

func TestResolveAPIKeyCommandTimeout(t *testing.T) {
	t.Setenv("NEKOT_API_KEY_TEST_HELPER", "1")
	oldTimeout := apiKeyCommandTimeout
	apiKeyCommandTimeout = 50 * time.Millisecond
	defer func() {
		apiKeyCommandTimeout = oldTimeout
	}()

	cfg := Config{
		Provider:             util.OpenAiProviderType,
		ProviderBaseUrl:      "https://api.openai.com",
		APIKeyResolveCommand: "cmd:" + apiKeyHelperCommand("timeout"),
	}

	err := cfg.resolveAPIKey()
	if err == nil {
		t.Fatal("resolveAPIKey() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("resolveAPIKey() error = %v, want timeout", err)
	}
	if got := cfg.ResolvedAPIKey(); got != "" {
		t.Fatalf("ResolvedAPIKey() = %q, want empty value", got)
	}
}

func TestResolveAPIKeyCommandOutputLimit(t *testing.T) {
	t.Setenv("NEKOT_API_KEY_TEST_HELPER", "1")
	oldLimit := apiKeyCommandOutputLimitBytes
	apiKeyCommandOutputLimitBytes = 16
	defer func() {
		apiKeyCommandOutputLimitBytes = oldLimit
	}()

	cfg := Config{
		Provider:             util.OpenAiProviderType,
		ProviderBaseUrl:      "https://api.openai.com",
		APIKeyResolveCommand: "cmd:" + apiKeyHelperCommand("large-output"),
	}

	err := cfg.resolveAPIKey()
	if err == nil {
		t.Fatal("resolveAPIKey() error = nil, want output limit error")
	}
	if !strings.Contains(err.Error(), "exceeds 16 bytes") {
		t.Fatalf("resolveAPIKey() error = %v, want output limit error", err)
	}
	if got := cfg.ResolvedAPIKey(); got != "" {
		t.Fatalf("ResolvedAPIKey() = %q, want empty value", got)
	}
}

func TestResolveAPIKeyRejectsInvalidSources(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "plaintext", source: "plaintext-key"},
		{name: "empty command", source: "cmd:   "},
		{name: "empty output", source: "cmd:" + emptyCommand()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Provider:             util.OpenAiProviderType,
				ProviderBaseUrl:      "https://api.openai.com",
				APIKeyResolveCommand: tt.source,
			}
			if err := cfg.resolveAPIKey(); err == nil {
				t.Fatal("resolveAPIKey() error = nil, want an error")
			}
		})
	}
}

func TestResolveAPIKeyAllowsLocalProviderWithoutCredentials(t *testing.T) {
	cfg := Config{
		Provider:             util.OpenAiProviderType,
		ProviderBaseUrl:      "http://localhost:11434",
		APIKeyResolveCommand: "not-a-command",
	}

	if err := cfg.resolveAPIKey(); err != nil {
		t.Fatalf("resolveAPIKey() error = %v", err)
	}
	if got := cfg.ResolvedAPIKey(); got != "" {
		t.Fatalf("ResolvedAPIKey() = %q, want empty value", got)
	}
}

func TestConfigDoesNotSerializeOrLogResolvedAPIKey(t *testing.T) {
	cfg := Config{
		Provider:             util.OpenrouterProviderType,
		APIKeyResolveCommand: "cmd:credential-command",
		resolvedAPIKey:       "resolved-secret",
	}

	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), "resolved-secret") {
		t.Fatalf("serialized config exposed resolved API key: %s", encoded)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Debug("config loaded", "values", cfg)
	if strings.Contains(logs.String(), "resolved-secret") {
		t.Fatalf("config log exposed resolved API key: %s", logs.String())
	}
	if strings.Contains(logs.String(), "credential-command") {
		t.Fatalf("config log exposed API key command: %s", logs.String())
	}
}

func TestAPIKeyCommandHelper(t *testing.T) {
	if os.Getenv("NEKOT_API_KEY_TEST_HELPER") != "1" {
		return
	}

	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "timeout":
		time.Sleep(time.Second)
		fmt.Println("late-key")
	case "large-output":
		fmt.Print(strings.Repeat("x", 1024))
	default:
		os.Exit(2)
	}

	os.Exit(0)
}

func commandReturning(value string) string {
	if runtime.GOOS == "windows" {
		return "echo " + value
	}
	return "printf '" + value + "\\n'"
}

func failingCommand(output string) string {
	if runtime.GOOS == "windows" {
		return "echo " + output + " & exit /b 1"
	}
	return "printf '" + output + "'; exit 1"
}

func emptyCommand() string {
	if runtime.GOOS == "windows" {
		return "ver > nul"
	}
	return "printf ''"
}

func apiKeyHelperCommand(mode string) string {
	command := quoteShellArg(os.Args[0]) + " -test.run=TestAPIKeyCommandHelper -- " + quoteShellArg(mode)
	return command
}

func quoteShellArg(value string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}

	return `'` + strings.ReplaceAll(value, `'`, `'\''`) + `'`
}
