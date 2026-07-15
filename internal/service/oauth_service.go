package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/chris/llm-router/internal/auth"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

type OAuthService interface {
	StartAuthFlow(ctx context.Context, providerID int64) (authURL string, state string, err error)
	HandleCallback(ctx context.Context, code, state string) (*models.OAuthToken, error)
	GetValidToken(ctx context.Context, providerID int64) (string, error)
	Disconnect(ctx context.Context, providerID int64) error
	IsConnected(ctx context.Context, providerID int64) (bool, *models.OAuthToken, error)
	StartCallbackServer(ctx context.Context, state string, providerID int64) (*models.OAuthToken, error)
	StartCallbackServerOnAddr(ctx context.Context, state string, providerID int64, addr string) (*models.OAuthToken, error)
}

type oauthService struct {
	oauthRepo      repository.OAuthRepository
	providerRepo   repository.ProviderRepository
	providerService ProviderService
	oauth          *auth.AnthropicOAuth
	logger         *slog.Logger
	states         map[string]int64 // state -> providerID
}

func NewOAuthService(
	oauthRepo repository.OAuthRepository,
	providerRepo repository.ProviderRepository,
	providerService ProviderService,
	logger *slog.Logger,
) OAuthService {
	return &oauthService{
		oauthRepo:       oauthRepo,
		providerRepo:    providerRepo,
		providerService: providerService,
		oauth:           auth.NewAnthropicOAuth("", ""),
		logger:          logger,
		states:          make(map[string]int64),
	}
}

func (s *oauthService) StartAuthFlow(ctx context.Context, providerID int64) (authURL, state string, err error) {
	provider, err := s.providerRepo.GetByID(ctx, providerID)
	if err != nil {
		return "", "", fmt.Errorf("get provider: %w", err)
	}
	if provider == nil {
		return "", "", fmt.Errorf("provider not found: %d", providerID)
	}

	state = generateState()
	s.states[state] = providerID

	authURL = s.oauth.GetAuthorizationURL(state)

	return authURL, state, nil
}

func (s *oauthService) HandleCallback(ctx context.Context, code, state string) (*models.OAuthToken, error) {
	providerID, exists := s.states[state]
	if !exists {
		return nil, fmt.Errorf("invalid or expired state")
	}
	delete(s.states, state)

	tokenInfo, err := s.oauth.ExchangeCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	accountID, email, err := s.oauth.GetUserInfo(ctx, tokenInfo.AccessToken)
	if err != nil {
		s.logger.Warn("failed to get user info", "error", err)
	}

	token := &models.OAuthToken{
		ProviderID:   providerID,
		AccessToken:  tokenInfo.AccessToken,
		RefreshToken: tokenInfo.RefreshToken,
		ExpiresAt:    tokenInfo.ExpiresAt,
		AccountID:    accountID,
		Email:        email,
	}

	if err := s.oauthRepo.Upsert(ctx, token); err != nil {
		return nil, fmt.Errorf("save token: %w", err)
	}

	s.logger.Info("OAuth connection established",
		"provider_id", providerID,
		"account_id", accountID,
		"email", email,
	)

	return token, nil
}

func (s *oauthService) GetValidToken(ctx context.Context, providerID int64) (string, error) {
	token, err := s.oauthRepo.GetByProviderID(ctx, providerID)
	if err != nil {
		return "", fmt.Errorf("get token: %w", err)
	}
	if token == nil {
		return "", fmt.Errorf("not connected")
	}

	if time.Now().Before(token.ExpiresAt.Add(-5 * time.Minute)) {
		return token.AccessToken, nil
	}

	newToken, err := s.oauth.RefreshToken(ctx, token.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh token: %w", err)
	}

	token.AccessToken = newToken.AccessToken
	token.RefreshToken = newToken.RefreshToken
	token.ExpiresAt = newToken.ExpiresAt

	if err := s.oauthRepo.Upsert(ctx, token); err != nil {
		s.logger.Error("failed to save refreshed token", "error", err)
	}

	return token.AccessToken, nil
}

func (s *oauthService) Disconnect(ctx context.Context, providerID int64) error {
	return s.oauthRepo.Delete(ctx, providerID)
}

func (s *oauthService) IsConnected(ctx context.Context, providerID int64) (bool, *models.OAuthToken, error) {
	token, err := s.oauthRepo.GetByProviderID(ctx, providerID)
	if err != nil {
		return false, nil, err
	}
	if token == nil {
		return false, nil, nil
	}

	if time.Now().After(token.ExpiresAt) {
		newToken, err := s.oauth.RefreshToken(ctx, token.RefreshToken)
		if err != nil {
			return false, token, nil
		}
		token.AccessToken = newToken.AccessToken
		token.RefreshToken = newToken.RefreshToken
		token.ExpiresAt = newToken.ExpiresAt
		s.oauthRepo.Upsert(ctx, token)
	}

	return true, token, nil
}

func (s *oauthService) StartCallbackServer(ctx context.Context, state string, providerID int64) (*models.OAuthToken, error) {
	return s.StartCallbackServerOnAddr(ctx, state, providerID, ":8080")
}

func (s *oauthService) StartCallbackServerOnAddr(ctx context.Context, state string, providerID int64, addr string) (*models.OAuthToken, error) {
	tokenCh := make(chan *models.OAuthToken, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		returnedState := r.URL.Query().Get("state")

		if returnedState != state {
			http.Error(w, "Invalid state", http.StatusBadRequest)
			errCh <- fmt.Errorf("invalid state")
			return
		}

		token, err := s.HandleCallback(r.Context(), code, state)
		if err != nil {
			http.Error(w, "Auth failed: "+err.Error(), http.StatusInternalServerError)
			errCh <- err
			return
		}

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>llm-router - Connected!</title>
<style>
body { font-family: system-ui; max-width: 400px; margin: 80px auto; padding: 20px; text-align: center; }
h1 { color: #22c55e; }
p { color: #666; }
</style>
</head>
<body>
<h1>✓ Connected!</h1>
<p>You can close this window and return to the terminal.</p>
</body>
</html>`))

		tokenCh <- token
	})

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("callback server error", "error", err)
			errCh <- err
		}
	}()

	select {
	case token := <-tokenCh:
		server.Shutdown(ctx)
		return token, nil
	case err := <-errCh:
		server.Shutdown(ctx)
		return nil, err
	case <-ctx.Done():
		server.Shutdown(ctx)
		return nil, fmt.Errorf("timeout waiting for OAuth callback")
	}
}

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
