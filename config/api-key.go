package config

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/BalanceBalls/nekot/util"
)

const apiKeyResolveCommandPrefix = "cmd:"

func (c Config) ResolvedAPIKey() string {
	return c.resolvedAPIKey
}

func (c *Config) resolveAPIKey() error {
	c.resolvedAPIKey = ""

	if c.Provider == util.OpenAiProviderType && util.IsLocalProvider(c.ProviderBaseUrl) {
		return nil
	}

	source := strings.TrimSpace(c.APIKeyResolveCommand)
	if source != "" {
		if !strings.HasPrefix(source, apiKeyResolveCommandPrefix) {
			return fmt.Errorf("apiKeyResolveCommand must start with %q", apiKeyResolveCommandPrefix)
		}

		command := strings.TrimSpace(strings.TrimPrefix(source, apiKeyResolveCommandPrefix))
		if command == "" {
			return fmt.Errorf("apiKeyResolveCommand command cannot be empty")
		}

		output, err := runAPIKeyCommand(command)
		if err != nil {
			return fmt.Errorf("apiKeyResolveCommand command failed: %w", err)
		}

		apiKey := strings.TrimSpace(string(output))
		if apiKey == "" {
			return fmt.Errorf("apiKeyResolveCommand command returned an empty value")
		}

		c.resolvedAPIKey = apiKey
		return nil
	}

	envName := apiKeyEnvironmentVariable(c.Provider)
	apiKey := os.Getenv(envName)
	if apiKey == "" {
		return fmt.Errorf("%s is not set and apiKeyResolveCommand is not configured", envName)
	}

	c.resolvedAPIKey = apiKey
	return nil
}

func apiKeyEnvironmentVariable(provider string) string {
	switch provider {
	case util.OpenrouterProviderType:
		return "OPENROUTER_API_KEY"
	case util.GeminiProviderType:
		return "GEMINI_API_KEY"
	default:
		return "OPENAI_API_KEY"
	}
}

func runAPIKeyCommand(command string) ([]byte, error) {
	if runtime.GOOS == "windows" {
		shell := os.Getenv("COMSPEC")
		if shell == "" {
			shell = "cmd.exe"
		}
		return exec.Command(shell, "/C", command).Output()
	}

	return exec.Command("/bin/sh", "-c", command).Output()
}

// LogValue prevents the resolved credential and credential command from being
// emitted when Config is passed to slog.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("chatGPTApiUrl", c.ChatGPTApiUrl),
		slog.String("providerBaseUrl", c.ProviderBaseUrl),
		slog.Bool("apiKeyConfigured", c.APIKeyResolveCommand != ""),
		slog.String("systemMessage", c.SystemMessage),
		slog.String("defaultModel", c.DefaultModel),
		slog.String("provider", c.Provider),
		slog.String("colorScheme", string(c.ColorScheme)),
		slog.Int("maxAttachmentSizeMb", c.MaxAttachmentSizeMb),
		slog.String("sessionExportDir", c.SessionExportDir),
	)
}
