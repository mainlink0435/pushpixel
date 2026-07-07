package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/mainLink0435/pushpixel/internal/config"
)

var ErrTokenExpired = errors.New("oauth token expired or revoked")

type Auth struct {
	config *oauth2.Config
	store  TokenStore
	token  *oauth2.Token
	state  string
	mu     sync.Mutex
}

func NewAuth(cfg config.AuthConfig) (*Auth, error) {
	store, err := NewFileTokenStore(cfg.TokenDir)
	if err != nil {
		return nil, fmt.Errorf("token store: %w", err)
	}

	a := &Auth{
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     google.Endpoint,
			Scopes: []string{
				"https://www.googleapis.com/auth/photoslibrary.appendonly",
				"https://www.googleapis.com/auth/photoslibrary.readonly.appcreateddata",
			},
		},
		store: store,
	}

	token, err := store.Load()
	if err == nil && token != nil {
		a.token = token
		slog.Info("loaded existing auth token")
	}

	return a, nil
}

func (a *Auth) SetRedirectURL(url string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config.RedirectURL = url
}

func (a *Auth) IsAuthenticated() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token == nil {
		return false
	}
	return a.token.AccessToken != "" && (a.token.Valid() || a.token.RefreshToken != "")
}

func (a *Auth) Token() *oauth2.Token {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.token
}

func (a *Auth) AuthorizationURL() (string, string, error) {
	state, err := randomState()
	if err != nil {
		return "", "", err
	}

	a.mu.Lock()
	a.state = state
	a.mu.Unlock()

	url := a.config.AuthCodeURL(state, oauth2.AccessTypeOffline)
	return url, state, nil
}

func (a *Auth) Exchange(ctx context.Context, code, state string) error {
	a.mu.Lock()
	expected := a.state
	a.mu.Unlock()

	if state != expected {
		return fmt.Errorf("state mismatch: got %s, expected %s", state, expected)
	}

	token, err := a.config.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("exchange code: %w", err)
	}

	if token.RefreshToken == "" {
		a.mu.Lock()
		if a.token != nil && a.token.RefreshToken != "" {
			token.RefreshToken = a.token.RefreshToken
		}
		a.mu.Unlock()
	}

	if err := a.store.Save(token); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	a.mu.Lock()
	a.token = token
	a.state = ""
	a.mu.Unlock()

	slog.Info("authentication successful")
	return nil
}

func (a *Auth) TokenSource(ctx context.Context) oauth2.TokenSource {
	return oauth2.ReuseTokenSource(nil, &dynamicTokenSource{auth: a})
}

type dynamicTokenSource struct {
	auth *Auth
}

func (s *dynamicTokenSource) Token() (*oauth2.Token, error) {
	s.auth.mu.Lock()
	tok := s.auth.token
	s.auth.mu.Unlock()

	if tok == nil {
		return nil, fmt.Errorf("not authenticated")
	}

	src := s.auth.config.TokenSource(context.Background(), tok)
	t, err := src.Token()
	if err != nil {
		if isTokenExpired(err) {
			s.auth.mu.Lock()
			s.auth.token = nil
			s.auth.mu.Unlock()
			_ = s.auth.store.Delete()
			slog.Warn("oauth token expired or revoked — cleared token, re-authentication required")
			return nil, fmt.Errorf("%w: %w", ErrTokenExpired, err)
		}
		return nil, err
	}

	if t.AccessToken != tok.AccessToken {
		s.auth.mu.Lock()
		s.auth.token = t
		s.auth.mu.Unlock()
		_ = s.auth.store.Save(t)
	}

	return t, nil
}

func (a *Auth) HTTPClient(ctx context.Context) *http.Client {
	return oauth2.NewClient(ctx, a.TokenSource(ctx))
}

func isTokenExpired(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid_grant") ||
		strings.Contains(msg, "token has been expired or revoked")
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}
