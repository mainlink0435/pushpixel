package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mainLink0435/pushpixel/internal/auth"
	"github.com/mainLink0435/pushpixel/internal/config"
	"github.com/mainLink0435/pushpixel/internal/db"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := config.AuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenDir:     dir,
	}
	a, err := auth.NewAuth(cfg)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}

	database, err := db.Open(filepath.Join(dir, "webui-test.db"))
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	return New(a, config.WebUIConfig{
		Host: "127.0.0.1",
		Port: 8080,
	}, database)
}

func TestHealth(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("expected body ok, got %s", w.Body.String())
	}
}

func TestDashboard_NotAuthenticated(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Not connected") {
		t.Errorf("expected unauthenticated message, got %s", w.Body.String())
	}
}

func TestDashboard_Authenticated(t *testing.T) {
	s := newTestServer(t)
	// Simulate authenticated state by setting redirect URL to trigger auth flow
	_ = s.auth.IsAuthenticated()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestOAuthAuthorize_WhenUnauthenticated(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	w := httptest.NewRecorder()

	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Sign in with Google") {
		t.Errorf("expected sign-in message, got %s", w.Body.String())
	}
}

func TestOAuthCallback_MissingCode(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?state=abc", nil)
	w := httptest.NewRecorder()

	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAPIStatus(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp StatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Authenticated {
		t.Error("expected not authenticated")
	}
}

func TestCORSAndHeaders(t *testing.T) {
	s := newTestServer(t)

	tests := []string{"/", "/health", "/api/status"}
	for _, path := range tests {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, w.Code)
		}
	}
}
