package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/mainLink0435/pushpixel/internal/config"
)

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
	a.mu.Lock()
	tok := a.token
	a.mu.Unlock()

	return a.config.TokenSource(ctx, tok)
}

func (a *Auth) HTTPClient(ctx context.Context) *http.Client {
	return oauth2.NewClient(ctx, a.TokenSource(ctx))
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}
