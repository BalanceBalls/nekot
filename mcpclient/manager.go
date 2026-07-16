package mcpclient

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/util"
	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type Manager struct {
	mu           sync.RWMutex
	db           *sql.DB
	rootConfig   config.Config
	config       config.MCPConfig
	servers      map[string]*managedServer
	ctx          context.Context
	cancel       context.CancelFunc
	authStore    *oauthStore
	authStoreErr error
	openBrowser  browserOpener
	started      bool
}

func NewManager(db *sql.DB, cfg config.Config) *Manager {
	path, pathErr := defaultOAuthStorePath()
	var store *oauthStore
	var storeErr error
	if pathErr == nil {
		store, storeErr = newOAuthStore(path)
	} else {
		storeErr = pathErr
	}
	if store == nil {
		store = &oauthStore{records: make(map[string]oauthRecord)}
	}
	return &Manager{
		db:           db,
		rootConfig:   cfg,
		config:       cfg.MCP.WithDefaults(),
		servers:      make(map[string]*managedServer),
		authStore:    store,
		authStoreErr: storeErr,
		openBrowser:  openBrowser,
	}
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started = true
	m.mu.Unlock()

	if err := m.config.Validate(); err != nil {
		return err
	}
	states, err := loadEnabledState(m.db)
	if err != nil {
		return err
	}

	for id := range states {
		if _, exists := m.config.Servers[id]; !exists {
			if err := deleteEnabledState(m.db, id); err != nil {
				return err
			}
			delete(states, id)
		}
	}

	m.mu.Lock()
	for id, serverConfig := range m.config.Servers {
		enabled, persisted := states[id]
		if !persisted {
			enabled = serverConfig.DefaultEnabled
		}
		state := ServerDisabled
		if enabled {
			state = ServerConnecting
		}
		m.servers[id] = &managedServer{
			config:  serverConfig,
			enabled: enabled,
			state:   state,
			tools:   make(map[string]util.ToolDefinition),
		}
	}
	m.mu.Unlock()

	for id, runtime := range m.serverSnapshot() {
		m.mu.RLock()
		enabled := runtime.enabled
		m.mu.RUnlock()
		if enabled {
			go m.connect(id, runtime, false)
		}
	}
	return nil
}

func (m *Manager) Statuses() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	statuses := make([]ServerStatus, 0, len(m.servers))
	for id, server := range m.servers {
		statuses = append(statuses, ServerStatus{
			ID:               id,
			Transport:        server.config.Transport,
			Enabled:          server.enabled,
			State:            server.state,
			DiscoveredTools:  server.discoveredTools,
			ExposedTools:     len(server.tools),
			LastError:        server.lastError,
			AuthorizationURL: server.authorizationURL,
			OAuth:            server.config.OAuth != nil,
		})
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	return statuses
}

func (m *Manager) Tools() []util.ToolDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var tools []util.ToolDefinition
	for _, server := range m.servers {
		if server.state != ServerConnected {
			continue
		}
		for _, tool := range server.tools {
			tools = append(tools, tool)
		}
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

func (m *Manager) Resolve(name string) (util.ToolDefinition, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, server := range m.servers {
		tool, ok := server.tools[name]
		if ok && server.state == ServerConnected {
			return tool, true
		}
	}
	return util.ToolDefinition{}, false
}

func (m *Manager) ToolsForServer(id string) []util.ToolDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	server, exists := m.servers[id]
	if !exists {
		return nil
	}
	tools := make([]util.ToolDefinition, 0, len(server.tools))
	for _, tool := range server.tools {
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].OriginalName < tools[j].OriginalName })
	return tools
}

func (m *Manager) Execute(ctx context.Context, name string, args map[string]any) ToolExecutionResult {
	m.mu.RLock()
	var runtime *managedServer
	var definition util.ToolDefinition
	for _, candidate := range m.servers {
		if tool, ok := candidate.tools[name]; ok {
			runtime = candidate
			definition = tool
			break
		}
	}
	if runtime == nil || runtime.state != ServerConnected || runtime.session == nil {
		m.mu.RUnlock()
		return errorResult(fmt.Sprintf("MCP tool %q is not available", name))
	}
	session := runtime.session
	toolTimeout := runtime.config.ToolTimeoutSeconds
	maxBytes := runtime.config.MaxResultBytes
	m.mu.RUnlock()

	callCtx, cancel := context.WithTimeout(ctx, time.Duration(toolTimeout)*time.Second)
	defer cancel()
	result, err := session.CallTool(callCtx, &sdk.CallToolParams{
		Name:      definition.OriginalName,
		Arguments: args,
	})
	if errors.Is(err, context.DeadlineExceeded) {
		err = fmt.Errorf("MCP tool %q timed out after %d seconds", definition.OriginalName, toolTimeout)
	}
	return serializeResult(result, err, maxBytes)
}

func (m *Manager) SetEnabled(id string, enabled bool) error {
	m.mu.RLock()
	runtime, exists := m.servers[id]
	m.mu.RUnlock()
	if !exists {
		return fmt.Errorf("MCP server %q is not configured", id)
	}
	if err := saveEnabledState(m.db, id, enabled); err != nil {
		return err
	}

	m.mu.Lock()
	if m.servers[id] != runtime {
		m.mu.Unlock()
		return fmt.Errorf("MCP server %q changed while updating state", id)
	}
	runtime.enabled = enabled
	if enabled {
		runtime.state = ServerConnecting
		runtime.lastError = ""
	} else {
		runtime.generation++
		runtime.state = ServerDisabled
		runtime.lastError = ""
		runtime.authorizationURL = ""
		runtime.tools = make(map[string]util.ToolDefinition)
		runtime.discoveredTools = 0
	}
	m.mu.Unlock()
	if enabled {
		go m.connect(id, runtime, false)
	} else {
		m.closeRuntime(runtime)
	}
	return nil
}

func (m *Manager) Reconnect(id string) error {
	m.mu.RLock()
	runtime, exists := m.servers[id]
	enabled := exists && runtime.enabled
	m.mu.RUnlock()
	if !exists {
		return fmt.Errorf("MCP server %q is not configured", id)
	}
	if !enabled {
		return fmt.Errorf("MCP server %q is disabled", id)
	}
	go m.connect(id, runtime, false)
	return nil
}

func (m *Manager) Authorize(id string) error {
	m.mu.RLock()
	runtime, exists := m.servers[id]
	usesOAuth := exists && runtime.config.OAuth != nil
	enabled := exists && runtime.enabled
	m.mu.RUnlock()
	if !exists {
		return fmt.Errorf("MCP server %q is not configured", id)
	}
	if !usesOAuth {
		return fmt.Errorf("MCP server %q does not use OAuth", id)
	}
	if !enabled {
		return fmt.Errorf("MCP server %q is disabled", id)
	}
	if m.authStoreErr != nil {
		return m.authStoreErr
	}
	go m.connect(id, runtime, true)
	return nil
}

func (m *Manager) Logout(id string) error {
	m.mu.RLock()
	runtime, exists := m.servers[id]
	usesOAuth := exists && runtime.config.OAuth != nil
	m.mu.RUnlock()
	if !exists {
		return fmt.Errorf("MCP server %q is not configured", id)
	}
	if !usesOAuth {
		return fmt.Errorf("MCP server %q does not use OAuth", id)
	}
	m.mu.Lock()
	if m.servers[id] == runtime {
		runtime.generation++
		session := runtime.session
		cancel := runtime.sessionCancel
		done := runtime.operationDone
		runtime.session = nil
		runtime.sessionCancel = nil
		runtime.operationDone = nil
		runtime.tools = make(map[string]util.ToolDefinition)
		runtime.discoveredTools = 0
		runtime.authorizationURL = ""
		if runtime.enabled {
			runtime.state = ServerAuthRequired
		} else {
			runtime.state = ServerDisabled
		}
		m.mu.Unlock()
		if err := closeSession(session, cancel, done); err != nil {
			return err
		}
		return m.authStore.delete(id)
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) Reload() error {
	updated, err := m.rootConfig.ReloadMCPConfig()
	if err != nil {
		return err
	}
	return m.reconcile(updated)
}

func (m *Manager) reconcile(updated config.MCPConfig) error {
	if err := updated.Validate(); err != nil {
		return err
	}
	states, err := loadEnabledState(m.db)
	if err != nil {
		return err
	}

	type connection struct {
		id      string
		runtime *managedServer
	}
	var closeList []*managedServer
	var connectList []connection
	m.mu.Lock()
	for id, runtime := range m.servers {
		newConfig, exists := updated.Servers[id]
		if !exists {
			delete(m.servers, id)
			closeList = append(closeList, runtime)
			continue
		}
		if reflect.DeepEqual(runtime.config, newConfig) {
			continue
		}
		closeList = append(closeList, runtime)
		replacement := &managedServer{
			config:  newConfig,
			enabled: runtime.enabled,
			state:   ServerDisabled,
			tools:   make(map[string]util.ToolDefinition),
		}
		if replacement.enabled {
			replacement.state = ServerConnecting
			connectList = append(connectList, connection{id: id, runtime: replacement})
		}
		m.servers[id] = replacement
	}
	for id, serverConfig := range updated.Servers {
		if _, exists := m.servers[id]; exists {
			continue
		}
		enabled, persisted := states[id]
		if !persisted {
			enabled = serverConfig.DefaultEnabled
		}
		runtime := &managedServer{
			config: serverConfig, enabled: enabled, state: ServerDisabled,
			tools: make(map[string]util.ToolDefinition),
		}
		if enabled {
			runtime.state = ServerConnecting
			connectList = append(connectList, connection{id: id, runtime: runtime})
		}
		m.servers[id] = runtime
	}
	m.config = updated
	m.mu.Unlock()

	for _, runtime := range closeList {
		m.closeRuntime(runtime)
	}
	for id := range states {
		if _, exists := updated.Servers[id]; !exists {
			if err := deleteEnabledState(m.db, id); err != nil {
				return err
			}
		}
	}
	for _, item := range connectList {
		go m.connect(item.id, item.runtime, false)
	}
	return nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	servers := make([]*managedServer, 0, len(m.servers))
	for _, runtime := range m.servers {
		runtime.generation++
		servers = append(servers, runtime)
	}
	m.mu.Unlock()
	var firstErr error
	for _, runtime := range servers {
		if err := m.closeRuntime(runtime); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Manager) connect(id string, runtime *managedServer, interactive bool) {
	m.mu.RLock()
	baseCtx := m.ctx
	m.mu.RUnlock()
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	sessionCtx, sessionCancel := context.WithCancel(baseCtx)
	done := make(chan struct{})

	m.mu.Lock()
	if m.servers[id] != runtime || !runtime.enabled {
		m.mu.Unlock()
		sessionCancel()
		return
	}
	runtime.state = ServerConnecting
	runtime.lastError = ""
	runtime.authorizationURL = ""
	runtime.tools = make(map[string]util.ToolDefinition)
	runtime.discoveredTools = 0
	runtime.generation++
	generation := runtime.generation
	previousSession := runtime.session
	previousCancel := runtime.sessionCancel
	previousDone := runtime.operationDone
	runtime.session = nil
	runtime.sessionCancel = sessionCancel
	runtime.operationDone = done
	m.mu.Unlock()
	_ = closeSession(previousSession, previousCancel, previousDone)
	defer m.finishConnect(runtime, generation, done)
	if !m.isCurrentGeneration(id, runtime, generation) {
		return
	}

	var oauthHandler auth.OAuthHandler
	cleanup := func() {}
	if runtime.config.OAuth != nil {
		if m.authStoreErr != nil {
			m.fail(id, runtime, generation, m.authStoreErr)
			return
		}
		if !interactive && m.authStore.token(id) == nil {
			m.authRequired(id, runtime, generation, "authorization has not been completed")
			return
		}
		if interactive {
			var err error
			oauthHandler, cleanup, err = buildInteractiveOAuthHandler(
				id,
				runtime.config,
				m.authStore,
				m.openBrowser,
				func(target string) { m.setAuthorizationURL(id, runtime, generation, target) },
			)
			if err != nil {
				m.fail(id, runtime, generation, err)
				return
			}
		} else {
			oauthHandler = newPersistentOAuthHandler(id, m.authStore, nil, false, nil)
		}
	}
	defer cleanup()

	transport, err := buildTransport(id, runtime.config, oauthHandler)
	if err != nil {
		m.fail(id, runtime, generation, err)
		return
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "nekot", Version: "1"}, &sdk.ClientOptions{
		KeepAlive: 30 * time.Second,
		ToolListChangedHandler: func(context.Context, *sdk.ToolListChangedRequest) {
			go m.refreshTools(id, runtime)
		},
	})

	timeout := time.Duration(runtime.config.ConnectTimeoutSeconds) * time.Second
	if interactive {
		timeout = time.Duration(runtime.config.OAuthTimeoutSeconds) * time.Second
	}
	type connectResult struct {
		session *sdk.ClientSession
		err     error
	}
	resultCh := make(chan connectResult, 1)
	go func() {
		session, connectErr := client.Connect(sessionCtx, transport, nil)
		resultCh <- connectResult{session: session, err: connectErr}
	}()

	var connected connectResult
	select {
	case connected = <-resultCh:
	case <-time.After(timeout):
		sessionCancel()
		m.fail(id, runtime, generation, fmt.Errorf("connection timed out after %s", timeout))
		return
	case <-baseCtx.Done():
		sessionCancel()
		return
	}
	if connected.err != nil {
		sessionCancel()
		if errors.Is(connected.err, ErrAuthorizationRequired) {
			m.authRequired(id, runtime, generation, "stored authorization is no longer valid")
		} else {
			m.fail(id, runtime, generation, connected.err)
		}
		return
	}

	tools, discovered, err := m.discoverTools(connected.session, id, runtime.config)
	if err != nil {
		sessionCancel()
		_ = connected.session.Close()
		m.fail(id, runtime, generation, err)
		return
	}
	m.mu.Lock()
	if m.servers[id] != runtime || !runtime.enabled || runtime.generation != generation {
		m.mu.Unlock()
		sessionCancel()
		_ = connected.session.Close()
		return
	}
	runtime.session = connected.session
	runtime.tools = tools
	runtime.discoveredTools = discovered
	runtime.state = ServerConnected
	runtime.lastError = ""
	runtime.authorizationURL = ""
	m.mu.Unlock()
}

func (m *Manager) isCurrentGeneration(id string, runtime *managedServer, generation uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.servers[id] == runtime && runtime.enabled && runtime.generation == generation
}

func (m *Manager) finishConnect(runtime *managedServer, generation uint64, done chan struct{}) {
	close(done)
	m.mu.Lock()
	defer m.mu.Unlock()
	if runtime.generation != generation || runtime.operationDone != done {
		return
	}
	runtime.operationDone = nil
	if runtime.session == nil {
		runtime.sessionCancel = nil
	}
}

func (m *Manager) discoverTools(
	session *sdk.ClientSession,
	id string,
	cfg config.MCPServerConfig,
) (map[string]util.ToolDefinition, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ConnectTimeoutSeconds)*time.Second)
	defer cancel()
	tools := make(map[string]util.ToolDefinition)
	discovered := 0
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, discovered, fmt.Errorf("list tools: %w", err)
		}
		if tool == nil {
			continue
		}
		discovered++
		if !toolAllowed(cfg, tool.Name) {
			continue
		}
		definition := toolDefinition(id, cfg, tool)
		tools[definition.Name] = definition
	}
	return tools, discovered, nil
}

func (m *Manager) refreshTools(id string, runtime *managedServer) {
	m.mu.RLock()
	if m.servers[id] != runtime || runtime.session == nil || runtime.state != ServerConnected {
		m.mu.RUnlock()
		return
	}
	session := runtime.session
	cfg := runtime.config
	generation := runtime.generation
	m.mu.RUnlock()
	tools, discovered, err := m.discoverTools(session, id, cfg)
	if err != nil {
		m.mu.Lock()
		if m.servers[id] == runtime && runtime.session == session && runtime.generation == generation {
			runtime.lastError = "refresh tools: " + err.Error()
		}
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	if m.servers[id] == runtime && runtime.session == session && runtime.generation == generation {
		runtime.tools = tools
		runtime.discoveredTools = discovered
	}
	m.mu.Unlock()
}

func (m *Manager) fail(id string, runtime *managedServer, generation uint64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.servers[id] != runtime || runtime.generation != generation {
		return
	}
	runtime.state = ServerFailed
	runtime.lastError = err.Error()
	runtime.tools = make(map[string]util.ToolDefinition)
	runtime.discoveredTools = 0
	util.Slog.Warn("MCP server failed", "server", id, "error", err)
}

func (m *Manager) authRequired(id string, runtime *managedServer, generation uint64, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.servers[id] != runtime || runtime.generation != generation {
		return
	}
	runtime.state = ServerAuthRequired
	runtime.lastError = reason
	runtime.tools = make(map[string]util.ToolDefinition)
	runtime.discoveredTools = 0
}

func (m *Manager) setAuthorizationURL(id string, runtime *managedServer, generation uint64, target string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.servers[id] == runtime && runtime.generation == generation {
		runtime.authorizationURL = target
	}
}

func (m *Manager) serverSnapshot() map[string]*managedServer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*managedServer, len(m.servers))
	for id, runtime := range m.servers {
		result[id] = runtime
	}
	return result
}

func (m *Manager) closeRuntime(runtime *managedServer) error {
	if runtime == nil {
		return nil
	}
	m.mu.Lock()
	session := runtime.session
	cancel := runtime.sessionCancel
	done := runtime.operationDone
	runtime.session = nil
	runtime.sessionCancel = nil
	runtime.operationDone = nil
	m.mu.Unlock()
	return closeSession(session, cancel, done)
}

func closeSession(session *sdk.ClientSession, cancel context.CancelFunc, done chan struct{}) error {
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	if session != nil {
		return session.Close()
	}
	return nil
}
