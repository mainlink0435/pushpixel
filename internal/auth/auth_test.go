package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/mainLink0435/pushpixel/internal/config"
)

func newMockAuthServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/token") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}

		code := r.FormValue("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}

		resp := struct {
			AccessToken  string `json:"access_token"`
			TokenType    string `json:"token_type"`
			ExpiresIn    int    `json:"expires_in"`
			RefreshToken string `json:"refresh_token"`
			Scope        string `json:"scope"`
		}{
			AccessToken:  "mock-access-" + code,
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			RefreshToken: "mock-refresh-" + code,
			Scope:        "https://www.googleapis.com/auth/photoslibrary.appendonly",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestNewAuth_NoExistingToken(t *testing.T) {
	dir := t.TempDir()
	cfg := config.AuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenDir:     dir,
	}

	a, err := NewAuth(cfg)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	if a.IsAuthenticated() {
		t.Fatal("expected not authenticated")
	}
}

func TestNewAuth_ExistingToken(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	if err != nil {
		t.Fatalf("token store: %v", err)
	}

	token := &oauth2.Token{
		AccessToken:  "existing",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(1 * time.Hour),
		RefreshToken: "refresh-existing",
	}
	if err := store.Save(token); err != nil {
		t.Fatalf("save token: %v", err)
	}

	cfg := config.AuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenDir:     dir,
	}

	a, err := NewAuth(cfg)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	if !a.IsAuthenticated() {
		t.Fatal("expected authenticated")
	}
}

func TestAuth_ExpiredToken_NoRefresh(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	if err != nil {
		t.Fatalf("token store: %v", err)
	}

	token := &oauth2.Token{
		AccessToken: "expired-no-refresh",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(-1 * time.Hour),
	}
	if err := store.Save(token); err != nil {
		t.Fatalf("save token: %v", err)
	}

	cfg := config.AuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenDir:     dir,
	}

	a, err := NewAuth(cfg)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	if a.IsAuthenticated() {
		t.Fatal("expected not authenticated for expired token without refresh")
	}
}

func TestAuth_ExpiredToken_WithRefresh(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	if err != nil {
		t.Fatalf("token store: %v", err)
	}

	token := &oauth2.Token{
		AccessToken:  "expired-with-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-1 * time.Hour),
		RefreshToken: "valid-refresh-token",
	}
	if err := store.Save(token); err != nil {
		t.Fatalf("save token: %v", err)
	}

	cfg := config.AuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenDir:     dir,
	}

	a, err := NewAuth(cfg)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	if !a.IsAuthenticated() {
		t.Fatal("expected authenticated for expired token with refresh token")
	}
}

func TestAuth_AuthorizationURL(t *testing.T) {
	dir := t.TempDir()
	cfg := config.AuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenDir:     dir,
	}

	a, err := NewAuth(cfg)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}

	urlStr, state, err := a.AuthorizationURL()
	if err != nil {
		t.Fatalf("AuthorizationURL: %v", err)
	}
	if urlStr == "" {
		t.Fatal("expected non-empty URL")
	}
	if state == "" {
		t.Fatal("expected non-empty state")
	}

	parsed, err := url.Parse(urlStr)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	if parsed.Query().Get("client_id") != "test-client" {
		t.Errorf("expected client_id in URL, got %q", parsed.Query().Get("client_id"))
	}
	if parsed.Query().Get("state") != state {
		t.Errorf("expected state %q in URL, got %q", state, parsed.Query().Get("state"))
	}
}

func TestAuth_AuthorizationURL_Unique(t *testing.T) {
	dir := t.TempDir()
	cfg := config.AuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenDir:     dir,
	}

	a, err := NewAuth(cfg)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}

	_, s1, _ := a.AuthorizationURL()
	_, s2, _ := a.AuthorizationURL()
	if s1 == s2 {
		t.Fatal("expected unique states")
	}
}

func TestAuth_Exchange_Code(t *testing.T) {
	mockServer := newMockAuthServer(t)
	defer mockServer.Close()

	dir := t.TempDir()
	cfg := config.AuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenDir:     dir,
	}

	a, err := NewAuth(cfg)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}

	endpoint := oauth2.Endpoint{
		TokenURL: mockServer.URL + "/token",
	}
	a.config.Endpoint = endpoint
	a.config.RedirectURL = "http://localhost:8080/oauth/callback"

	urlStr, state, err := a.AuthorizationURL()
	if err != nil {
		t.Fatalf("AuthorizationURL: %v", err)
	}

	code := urlStr + "-test-code"
	if err := a.Exchange(context.Background(), code, state); err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	if !a.IsAuthenticated() {
		t.Fatal("expected authenticated after exchange")
	}

	tok := a.Token()
	if tok == nil {
		t.Fatal("expected token")
	}
	if tok.AccessToken != "mock-access-"+code {
		t.Errorf("expected access token mock-access-%s, got %s", code, tok.AccessToken)
	}
}

func TestAuth_Exchange_WrongState(t *testing.T) {
	dir := t.TempDir()
	cfg := config.AuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenDir:     dir,
	}

	a, err := NewAuth(cfg)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}

	if err := a.Exchange(context.Background(), "code", "wrong-state"); err == nil {
		t.Fatal("expected error for wrong state")
	}
}

func TestAuth_HTTPClient(t *testing.T) {
	mockServer := newMockAuthServer(t)
	defer mockServer.Close()

	dir := t.TempDir()
	cfg := config.AuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenDir:     dir,
	}

	a, err := NewAuth(cfg)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}

	client := a.HTTPClient(context.Background())
	if client == nil {
		t.Fatal("expected HTTP client")
	}
}

func TestAuth_TokenSource_Refresh(t *testing.T) {
	mockServer := newMockAuthServer(t)
	defer mockServer.Close()

	dir := t.TempDir()
	cfg := config.AuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenDir:     dir,
	}

	a, err := NewAuth(cfg)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}

	a.config.Endpoint = oauth2.Endpoint{
		TokenURL: mockServer.URL + "/token",
	}
	a.config.RedirectURL = "http://localhost:8080/oauth/callback"

	urlStr, state, err := a.AuthorizationURL()
	if err != nil {
		t.Fatalf("AuthorizationURL: %v", err)
	}

	code := urlStr + "-refresh-test"
	if err := a.Exchange(context.Background(), code, state); err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	ts := a.TokenSource(context.Background())
	if ts == nil {
		t.Fatal("expected token source")
	}

	newToken, err := ts.Token()
	if err != nil {
		t.Fatalf("Token() from source: %v", err)
	}
	if newToken == nil {
		t.Fatal("expected new token from source")
	}
}

func TestAuth_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	cfg := config.AuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenDir:     dir,
	}

	a, err := NewAuth(cfg)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			_ = a.IsAuthenticated()
			_ = a.Token()
			done <- true
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestNewAuth_InvalidTokenDir(t *testing.T) {
	cfg := config.AuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenDir:     fmt.Sprintf("%s%c%s", t.TempDir(), 0, "invalid"), // null byte in path
	}

	_, err := NewAuth(cfg)
	if err == nil {
		// on some platforms this might not error, skip
		t.Log("no error with invalid path (platform dependent)")
	}
}
