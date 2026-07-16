package mcpclient

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type headerTransport struct {
	base    http.RoundTripper
	headers http.Header
}

func (t headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	for name, values := range t.headers {
		clone.Header.Del(name)
		clone.Header[name] = append([]string(nil), values...)
	}
	return t.base.RoundTrip(clone)
}

type serverLogWriter struct{ serverID string }

func (w serverLogWriter) Write(data []byte) (int, error) {
	message := strings.TrimSpace(string(data))
	if message != "" {
		util.Slog.Debug("MCP server stderr", "server", w.serverID, "message", message)
	}
	return len(data), nil
}

func buildTransport(
	serverID string,
	cfg config.MCPServerConfig,
	oauthHandler auth.OAuthHandler,
) (sdk.Transport, error) {
	switch cfg.Transport {
	case config.MCPTransportStdio:
		command := exec.Command(cfg.Command, cfg.Args...)
		command.Dir = cfg.Cwd
		command.Env = os.Environ()
		for name, source := range cfg.Env {
			value, err := config.ResolveValueSource(source)
			if err != nil {
				return nil, fmt.Errorf("resolve environment variable %q: %w", name, err)
			}
			command.Env = setEnvironmentValue(command.Env, name, value)
		}
		command.Stderr = io.Writer(serverLogWriter{serverID: serverID})
		return &sdk.CommandTransport{
			Command:           command,
			TerminateDuration: 5 * time.Second,
		}, nil

	case config.MCPTransportStreamableHTTP:
		headers := make(http.Header, len(cfg.Headers))
		for name, source := range cfg.Headers {
			value, err := config.ResolveValueSource(source)
			if err != nil {
				return nil, fmt.Errorf("resolve header %q: %w", name, err)
			}
			headers.Set(name, value)
		}
		client := &http.Client{Transport: headerTransport{
			base:    http.DefaultTransport,
			headers: headers,
		}}
		return &sdk.StreamableClientTransport{
			Endpoint:     cfg.URL,
			HTTPClient:   client,
			MaxRetries:   2,
			OAuthHandler: oauthHandler,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported MCP transport %q", cfg.Transport)
	}
}

func setEnvironmentValue(environment []string, name, value string) []string {
	prefix := name + "="
	result := environment[:0]
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}
