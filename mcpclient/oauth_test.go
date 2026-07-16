package mcpclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestOAuthStoreRestoresAndRefreshesExpiredToken(t *testing.T) {
	refreshRequests := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if request.Form.Get("grant_type") != "refresh_token" || request.Form.Get("refresh_token") != "refresh" {
			t.Errorf("unexpected refresh form: %v", request.Form)
		}
		refreshRequests++
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"access_token":  "renewed",
			"refresh_token": "refresh-2",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	t.Cleanup(tokenServer.Close)

	path := filepath.Join(t.TempDir(), "mcp-oauth.json")
	store, err := newOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	expired := &oauth2.Token{
		AccessToken: "expired", RefreshToken: "refresh", TokenType: "Bearer",
		Expiry: time.Now().Add(-time.Hour),
	}
	metadata := oauthRefreshMetadata{
		ClientID: "client", TokenURL: tokenServer.URL, AuthStyle: oauth2.AuthStyleInParams,
	}
	if err := store.save("remote", expired, metadata); err != nil {
		t.Fatal(err)
	}

	restored, err := newOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	handler := newPersistentOAuthHandler("remote", restored, nil, false, nil)
	source, err := handler.TokenSource(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	token, err := source.Token()
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "renewed" || refreshRequests != 1 {
		t.Fatalf("token = %#v, refresh requests = %d", token, refreshRequests)
	}
	record, ok := restored.record("remote")
	if !ok || record.Token.AccessToken != "renewed" || record.Token.RefreshToken != "refresh-2" {
		t.Fatalf("persisted record = %#v", record)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("OAuth store mode = %o, want 600", info.Mode().Perm())
	}
}

func TestAuthorizationURLMergesConfiguredScopes(t *testing.T) {
	got, err := authorizationURLWithScopes(
		"https://auth.example/authorize?scope=read+profile&state=abc",
		[]string{"profile", "write"},
	)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("scope") != "read profile write" || parsed.Query().Get("state") != "abc" {
		t.Fatalf("authorization URL = %q", got)
	}
}
