package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/BalanceBalls/nekot/util"
)

const apiKeyResolveCommandPrefix = "cmd:"

var (
	apiKeyCommandTimeout          = 10 * time.Second
	apiKeyCommandOutputLimitBytes = int64(8 * 1024)
)

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
	ctx, cancel := context.WithTimeout(context.Background(), apiKeyCommandTimeout)
	defer cancel()

	stdout := &limitedOutputBuffer{limit: apiKeyCommandOutputLimitBytes}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		shell := os.Getenv("COMSPEC")
		if shell == "" {
			shell = "cmd.exe"
		}
		cmd = exec.CommandContext(ctx, shell, "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
	}

	cmd.Stdout = stdout

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("apiKeyResolveCommand command timed out after %s", apiKeyCommandTimeout)
	}
	if stdout.exceeded {
		return nil, fmt.Errorf(
			"apiKeyResolveCommand command output exceeds %d bytes",
			apiKeyCommandOutputLimitBytes,
		)
	}
	if err != nil {
		return nil, err
	}

	return stdout.bytes(), nil
}

type limitedOutputBuffer struct {
	limit    int64
	data     []byte
	exceeded bool
}

func (b *limitedOutputBuffer) Write(p []byte) (int, error) {
	if int64(len(b.data)) < b.limit {
		remaining := b.limit - int64(len(b.data))
		if int64(len(p)) <= remaining {
			b.data = append(b.data, p...)
		} else {
			b.data = append(b.data, p[:remaining]...)
			b.exceeded = true
		}
	} else if len(p) > 0 {
		b.exceeded = true
	}

	return len(p), nil
}

func (b *limitedOutputBuffer) bytes() []byte {
	return append([]byte(nil), b.data...)
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
