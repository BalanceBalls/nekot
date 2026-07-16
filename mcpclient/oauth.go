package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/BalanceBalls/nekot/config"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

var ErrAuthorizationRequired = errors.New("MCP authorization required")

type browserOpener func(string) error

type oauthRecord struct {
	Token     *oauth2.Token        `json:"token"`
	Refresh   oauthRefreshMetadata `json:"refresh,omitempty"`
	UpdatedAt time.Time            `json:"updatedAt"`
}

type oauthRefreshMetadata struct {
	ClientID     string           `json:"clientId,omitempty"`
	ClientSecret string           `json:"clientSecret,omitempty"`
	TokenURL     string           `json:"tokenUrl,omitempty"`
	RedirectURL  string           `json:"redirectUrl,omitempty"`
	Scopes       []string         `json:"scopes,omitempty"`
	AuthStyle    oauth2.AuthStyle `json:"authStyle,omitempty"`
}

func (m oauthRefreshMetadata) complete() bool {
	return m.ClientID != "" && m.TokenURL != ""
}

type oauthFile struct {
	Servers map[string]oauthRecord `json:"servers"`
}

type oauthStore struct {
	mu      sync.Mutex
	path    string
	records map[string]oauthRecord
}

func newOAuthStore(path string) (*oauthStore, error) {
	store := &oauthStore{path: path, records: make(map[string]oauthRecord)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read MCP OAuth store: %w", err)
	}
	var file oauthFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse MCP OAuth store: %w", err)
	}
	if file.Servers != nil {
		store.records = file.Servers
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("protect MCP OAuth store: %w", err)
	}
	return store, nil
}

func defaultOAuthStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nekot", "mcp-oauth.json"), nil
}

func (s *oauthStore) token(serverID string) *oauth2.Token {
	record, ok := s.record(serverID)
	if !ok || record.Token == nil {
		return nil
	}
	return record.Token
}

func (s *oauthStore) record(serverID string) (oauthRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[serverID]
	if !ok || record.Token == nil {
		return oauthRecord{}, false
	}
	copy := *record.Token
	record.Token = &copy
	record.Refresh.Scopes = append([]string(nil), record.Refresh.Scopes...)
	return record, true
}

func (s *oauthStore) save(serverID string, token *oauth2.Token, refresh oauthRefreshMetadata) error {
	if token == nil {
		return errors.New("cannot persist an empty OAuth token")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *token
	refresh.Scopes = append([]string(nil), refresh.Scopes...)
	s.records[serverID] = oauthRecord{Token: &copy, Refresh: refresh, UpdatedAt: time.Now().UTC()}
	return s.writeLocked()
}

func (s *oauthStore) delete(serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, serverID)
	return s.writeLocked()
}

func (s *oauthStore) writeLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create MCP OAuth directory: %w", err)
	}
	data, err := json.MarshalIndent(oauthFile{Servers: s.records}, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize MCP OAuth store: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".mcp-oauth-*")
	if err != nil {
		return fmt.Errorf("create temporary MCP OAuth store: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, s.path); err != nil {
		return fmt.Errorf("replace MCP OAuth store: %w", err)
	}
	return os.Chmod(s.path, 0o600)
}

type persistentOAuthHandler struct {
	mu          sync.Mutex
	serverID    string
	store       *oauthStore
	delegate    auth.OAuthHandler
	tokenSource oauth2.TokenSource
	interactive bool
	metadata    func() oauthRefreshMetadata
}

func newPersistentOAuthHandler(
	serverID string,
	store *oauthStore,
	delegate auth.OAuthHandler,
	interactive bool,
	metadata func() oauthRefreshMetadata,
) *persistentOAuthHandler {
	handler := &persistentOAuthHandler{
		serverID:    serverID,
		store:       store,
		delegate:    delegate,
		interactive: interactive,
		metadata:    metadata,
	}
	if record, ok := store.record(serverID); ok {
		source := oauth2.TokenSource(oauth2.StaticTokenSource(record.Token))
		if record.Refresh.complete() {
			oauthConfig := &oauth2.Config{
				ClientID:     record.Refresh.ClientID,
				ClientSecret: record.Refresh.ClientSecret,
				Endpoint: oauth2.Endpoint{
					TokenURL:  record.Refresh.TokenURL,
					AuthStyle: record.Refresh.AuthStyle,
				},
				RedirectURL: record.Refresh.RedirectURL,
				Scopes:      record.Refresh.Scopes,
			}
			refreshCtx := context.WithValue(context.Background(), oauth2.HTTPClient, http.DefaultClient)
			source = oauthConfig.TokenSource(refreshCtx, record.Token)
		}
		persisted := &persistingTokenSource{
			serverID: serverID,
			source:   source,
			store:    store,
			metadata: func() oauthRefreshMetadata { return record.Refresh },
		}
		handler.tokenSource = oauth2.ReuseTokenSource(record.Token, persisted)
	}
	return handler
}

func (h *persistentOAuthHandler) TokenSource(context.Context) (oauth2.TokenSource, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tokenSource, nil
}

func (h *persistentOAuthHandler) Authorize(ctx context.Context, request *http.Request, response *http.Response) error {
	if !h.interactive || h.delegate == nil {
		response.Body.Close()
		return ErrAuthorizationRequired
	}
	if err := h.delegate.Authorize(ctx, request, response); err != nil {
		return err
	}
	source, err := h.delegate.TokenSource(ctx)
	if err != nil {
		return err
	}
	if source == nil {
		return errors.New("OAuth authorization completed without a token source")
	}
	persisted := &persistingTokenSource{
		serverID: h.serverID,
		source:   source,
		store:    h.store,
		metadata: h.metadata,
	}
	token, err := persisted.Token()
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.tokenSource = oauth2.ReuseTokenSource(token, persisted)
	h.mu.Unlock()
	return nil
}

type persistingTokenSource struct {
	serverID string
	source   oauth2.TokenSource
	store    *oauthStore
	metadata func() oauthRefreshMetadata
}

func (s *persistingTokenSource) Token() (*oauth2.Token, error) {
	token, err := s.source.Token()
	if err != nil {
		return nil, err
	}
	metadata := oauthRefreshMetadata{}
	if s.metadata != nil {
		metadata = s.metadata()
	}
	if err := s.store.save(s.serverID, token, metadata); err != nil {
		return nil, err
	}
	return token, nil
}

type oauthMetadataCapture struct {
	mu       sync.Mutex
	base     http.RoundTripper
	metadata oauthRefreshMetadata
}

func (c *oauthMetadataCapture) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := c.base.RoundTrip(request)
	if err != nil || response.Body == nil {
		return response, err
	}
	data, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(data))
	if readErr != nil {
		return response, nil
	}
	var fields struct {
		TokenEndpoint                     string   `json:"token_endpoint"`
		TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
		ClientID                          string   `json:"client_id"`
		ClientSecret                      string   `json:"client_secret"`
		TokenEndpointAuthMethod           string   `json:"token_endpoint_auth_method"`
		AccessToken                       string   `json:"access_token"`
		Scope                             string   `json:"scope"`
	}
	if json.Unmarshal(data, &fields) != nil {
		return response, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if fields.TokenEndpoint != "" {
		c.metadata.TokenURL = fields.TokenEndpoint
	}
	if len(fields.TokenEndpointAuthMethodsSupported) > 0 {
		c.metadata.AuthStyle = preferredAuthStyle(fields.TokenEndpointAuthMethodsSupported)
	}
	if fields.ClientID != "" {
		c.metadata.ClientID = fields.ClientID
		c.metadata.ClientSecret = fields.ClientSecret
		c.metadata.AuthStyle = authStyle(fields.TokenEndpointAuthMethod)
	}
	if fields.AccessToken != "" {
		c.metadata.TokenURL = request.URL.String()
		if fields.Scope != "" {
			c.metadata.Scopes = strings.Fields(fields.Scope)
		}
	}
	return response, nil
}

func (c *oauthMetadataCapture) snapshot() oauthRefreshMetadata {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := c.metadata
	result.Scopes = append([]string(nil), result.Scopes...)
	return result
}

func preferredAuthStyle(methods []string) oauth2.AuthStyle {
	for _, wanted := range []string{"client_secret_post", "client_secret_basic", "none"} {
		for _, method := range methods {
			if method == wanted {
				return authStyle(method)
			}
		}
	}
	return oauth2.AuthStyleAutoDetect
}

func authStyle(method string) oauth2.AuthStyle {
	switch method {
	case "client_secret_post", "none":
		return oauth2.AuthStyleInParams
	case "client_secret_basic":
		return oauth2.AuthStyleInHeader
	default:
		return oauth2.AuthStyleAutoDetect
	}
}

type loopbackReceiver struct {
	listener    net.Listener
	redirectURL string
	opener      browserOpener
	setURL      func(string)
}

func newLoopbackReceiver(redirectURL string, opener browserOpener, setURL func(string)) (*loopbackReceiver, error) {
	var address string
	var callbackPath string
	if redirectURL == "" {
		address = "127.0.0.1:0"
		callbackPath = "/callback"
	} else {
		parsed, err := url.Parse(redirectURL)
		if err != nil {
			return nil, err
		}
		address = parsed.Host
		callbackPath = parsed.Path
		if callbackPath == "" {
			callbackPath = "/"
		}
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen for OAuth callback: %w", err)
	}
	if redirectURL == "" {
		redirectURL = "http://" + listener.Addr().String() + callbackPath
	}
	return &loopbackReceiver{
		listener:    listener,
		redirectURL: redirectURL,
		opener:      opener,
		setURL:      setURL,
	}, nil
}

func (r *loopbackReceiver) fetch(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
	type callbackResult struct {
		result *auth.AuthorizationResult
		err    error
	}
	resultCh := make(chan callbackResult, 1)
	parsed, _ := url.Parse(r.redirectURL)
	mux := http.NewServeMux()
	mux.HandleFunc(parsed.Path, func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if oauthErr := query.Get("error"); oauthErr != "" {
			select {
			case resultCh <- callbackResult{err: fmt.Errorf("OAuth authorization failed: %s", oauthErr)}:
			default:
			}
			http.Error(writer, "Authorization failed. You can close this window.", http.StatusBadRequest)
			return
		}
		code := query.Get("code")
		state := query.Get("state")
		if code == "" || state == "" {
			select {
			case resultCh <- callbackResult{err: errors.New("OAuth callback is missing code or state")}:
			default:
			}
			http.Error(writer, "Invalid callback. You can close this window.", http.StatusBadRequest)
			return
		}
		_, _ = writer.Write([]byte("Authorization complete. You can close this window."))
		select {
		case resultCh <- callbackResult{result: &auth.AuthorizationResult{Code: code, State: state}}:
		default:
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(r.listener) }()

	if r.setURL != nil {
		r.setURL(args.URL)
	}
	if r.opener != nil {
		_ = r.opener(args.URL)
	}

	select {
	case result := <-resultCh:
		_ = server.Shutdown(context.Background())
		return result.result, result.err
	case <-ctx.Done():
		_ = server.Shutdown(context.Background())
		return nil, ctx.Err()
	}
}

func (r *loopbackReceiver) close() { _ = r.listener.Close() }

func buildInteractiveOAuthHandler(
	serverID string,
	cfg config.MCPServerConfig,
	store *oauthStore,
	opener browserOpener,
	setURL func(string),
) (auth.OAuthHandler, func(), error) {
	receiver, err := newLoopbackReceiver(cfg.OAuth.RedirectURL, opener, setURL)
	if err != nil {
		return nil, nil, err
	}
	cleanup := receiver.close
	handlerConfig := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL: receiver.redirectURL,
		AuthorizationCodeFetcher: func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			target, err := authorizationURLWithScopes(args.URL, cfg.OAuth.Scopes)
			if err != nil {
				return nil, err
			}
			return receiver.fetch(ctx, &auth.AuthorizationArgs{URL: target})
		},
	}
	capture := &oauthMetadataCapture{
		base: http.DefaultTransport,
		metadata: oauthRefreshMetadata{
			RedirectURL: receiver.redirectURL,
			Scopes:      append([]string(nil), cfg.OAuth.Scopes...),
		},
	}
	handlerConfig.Client = &http.Client{Transport: capture}
	if cfg.OAuth.Registration == config.MCPOAuthPreregistered {
		credentials := &oauthex.ClientCredentials{ClientID: cfg.OAuth.ClientID}
		if cfg.OAuth.ClientSecretSource != "" {
			secret, err := config.ResolveValueSource(cfg.OAuth.ClientSecretSource)
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			credentials.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: secret}
		}
		handlerConfig.PreregisteredClient = credentials
		capture.metadata.ClientID = credentials.ClientID
		if credentials.ClientSecretAuth != nil {
			capture.metadata.ClientSecret = credentials.ClientSecretAuth.ClientSecret
		}
	} else {
		handlerConfig.DynamicClientRegistrationConfig = &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				RedirectURIs:            []string{receiver.redirectURL},
				TokenEndpointAuthMethod: "none",
				GrantTypes:              []string{"authorization_code", "refresh_token"},
				ResponseTypes:           []string{"code"},
				ClientName:              "nekot",
				Scope:                   strings.Join(cfg.OAuth.Scopes, " "),
			},
		}
	}
	delegate, err := auth.NewAuthorizationCodeHandler(handlerConfig)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return newPersistentOAuthHandler(serverID, store, delegate, true, capture.snapshot), cleanup, nil
}

func authorizationURLWithScopes(target string, configured []string) (string, error) {
	if len(configured) == 0 {
		return target, nil
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	scopes := strings.Fields(query.Get("scope"))
	seen := make(map[string]struct{}, len(scopes)+len(configured))
	for _, scope := range scopes {
		seen[scope] = struct{}{}
	}
	for _, scope := range configured {
		if _, exists := seen[scope]; exists {
			continue
		}
		scopes = append(scopes, scope)
		seen[scope] = struct{}{}
	}
	query.Set("scope", strings.Join(scopes, " "))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func openBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	return command.Start()
}
