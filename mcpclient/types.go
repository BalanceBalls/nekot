package mcpclient

import (
	"context"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ServerState string

const (
	ServerDisabled     ServerState = "disabled"
	ServerConnecting   ServerState = "connecting"
	ServerAuthRequired ServerState = "auth required"
	ServerConnected    ServerState = "connected"
	ServerFailed       ServerState = "failed"
)

type ServerStatus struct {
	ID               string
	Transport        string
	Enabled          bool
	State            ServerState
	DiscoveredTools  int
	ExposedTools     int
	LastError        string
	AuthorizationURL string
	OAuth            bool
}

type ToolExecutionResult struct {
	Result  string
	IsError bool
}

type managedServer struct {
	config           config.MCPServerConfig
	enabled          bool
	state            ServerState
	lastError        string
	authorizationURL string
	discoveredTools  int
	tools            map[string]util.ToolDefinition
	session          *sdk.ClientSession
	sessionCancel    context.CancelFunc
	operationDone    chan struct{}
	generation       uint64
}
