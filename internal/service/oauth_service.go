package service

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
	"time"

	"github.com/chris/llm-router/internal/auth"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

type OAuthService interface {
	StartAuthFlow(ctx context.Context, providerID int64) (authURL string, state string, err error)
	StartAuthFlowWithCallback(ctx context.Context, providerID int64, callbackAddr string) (authURL string, state string, err error)
	HandleCallback(ctx context.Context, code, state string) (*models.OAuthToken, error)
	GetValidToken(ctx context.Context, providerID int64) (string, error)
	Disconnect(ctx context.Context, providerID int64) error
	IsConnected(ctx context.Context, providerID int64) (bool, *models.OAuthToken, error)
	StartCallbackServer(ctx context.Context, state string, providerID int64) (*models.OAuthToken, error)
	StartCallbackServerOnAddr(ctx context.Context, state string, providerID int64, addr string) (*models.OAuthToken, error)
	// RefreshNow unconditionally refreshes the provider's OAuth token and
	// returns the fresh row, bypassing the refresh policy. Used by the proxy
	// engine when an upstream 401 proves the access token dead even though
	// the policy still considered it valid (codex retry-once, design §4.5).
	RefreshNow(ctx context.Context, provider *models.Provider) (*models.OAuthToken, error)
	// GetTokenRow returns the stored oauth token row without evaluating the
	// refresh policy. A nil row (and nil error) means the provider is not
	// connected. Used to read ChatGPT-Account-ID for codex headers.
	GetTokenRow(ctx context.Context, providerID int64) (*models.OAuthToken, error)
}

// ErrCodexReconnectRequired is returned by GetValidToken when a ChatGPT
// (codex) OAuth session cannot be recovered by refreshing. The engine can
// surface it verbatim; the fix is a manual re-connect, not a retry.
var ErrCodexReconnectRequired = errors.New("chatgpt session expired — re-run 'llm-router connect --provider chatgpt' to reconnect")

const (
	oauthKindAnthropic = "anthropic"
	oauthKindCodex     = "codex"

	// Codex refresh policy (design §4.3): refresh when less than 5 days to
	// expiry OR more than 8 days since the last refresh (stays inside
	// ChatGPT's refresh-token reuse/rotation window).
	codexRefreshBeforeExpiry = 5 * 24 * time.Hour
	codexMaxRefreshInterval  = 8 * 24 * time.Hour

	// Anthropic keeps its existing behavior: refresh 5 minutes before expiry.
	anthropicRefreshSkew = 5 * time.Minute
)

// oauthProviderClient is the per-flow OAuth strategy implemented by the
// anthropic (claude-code) and codex (chatgpt) adapters below.
type oauthProviderClient interface {
	SetRedirectURI(uri string)
	GetAuthorizationURL(state string) string
	ExchangeCode(ctx context.Context, code, state string) (*models.OAuthToken, error)
	RefreshToken(ctx context.Context, refreshToken string) (*models.OAuthToken, error)
}

// --- Strategy adapters ------------------------------------------------------

// anthropicOAuthClient adapts auth.AnthropicOAuth to the strategy interface.
type anthropicOAuthClient struct {
	oauth *auth.AnthropicOAuth
}

func (a anthropicOAuthClient) SetRedirectURI(uri string) { a.oauth.SetRedirectURI(uri) }

func (a anthropicOAuthClient) GetAuthorizationURL(state string) string {
	return a.oauth.GetAuthorizationURL(state)
}

func (a anthropicOAuthClient) ExchangeCode(ctx context.Context, code, state string) (*models.OAuthToken, error) {
	info, err := a.oauth.ExchangeCode(ctx, code, state)
	if err != nil {
		return nil, err
	}
	return &models.OAuthToken{
		AccessToken:  info.AccessToken,
		RefreshToken: info.RefreshToken,
		ExpiresAt:    info.ExpiresAt,
	}, nil
}

func (a anthropicOAuthClient) RefreshToken(ctx context.Context, refreshToken string) (*models.OAuthToken, error) {
	info, err := a.oauth.RefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	return &models.OAuthToken{
		AccessToken:  info.AccessToken,
		RefreshToken: info.RefreshToken,
		ExpiresAt:    info.ExpiresAt,
	}, nil
}

// codexOAuthClient adapts auth.CodexOAuth to the strategy interface.
type codexOAuthClient struct {
	oauth *auth.CodexOAuth
}

func (c codexOAuthClient) SetRedirectURI(uri string) { c.oauth.SetRedirectURI(uri) }

func (c codexOAuthClient) GetAuthorizationURL(state string) string {
	return c.oauth.GetAuthorizationURL(state)
}

func (c codexOAuthClient) ExchangeCode(ctx context.Context, code, _ string) (*models.OAuthToken, error) {
	tokens, err := c.oauth.ExchangeCode(ctx, code)
	if err != nil {
		return nil, err
	}
	return codexTokensToModel(tokens), nil
}

func (c codexOAuthClient) RefreshToken(ctx context.Context, refreshToken string) (*models.OAuthToken, error) {
	tokens, err := c.oauth.RefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	return codexTokensToModel(tokens), nil
}

func codexTokensToModel(tokens *auth.CodexTokens) *models.OAuthToken {
	token := &models.OAuthToken{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second),
		IDToken:      tokens.IDToken,
	}
	applyCodexIDTokenClaims(token)
	return token
}

// applyCodexIDTokenClaims fills account/email from a codex id_token JWT when
// present and not already known. plan_type is intentionally not persisted
// (design §4.3: logged at connect time only). Applied in the service layer so
// it holds regardless of how the token response arrived.
func applyCodexIDTokenClaims(token *models.OAuthToken) {
	if token == nil || token.IDToken == "" || token.AccountID != "" {
		return
	}
	if accountID, _, email, err := auth.ParseIDToken(token.IDToken); err == nil {
		token.AccountID = accountID
		if token.Email == "" {
			token.Email = email
		}
	}
}

// --- Strategy registry ------------------------------------------------------

// oauthProviderKind maps a provider row onto an OAuth flow kind. Codex covers
// providers with api_type "codex" or the canonical chatgpt/codex names;
// everything else (incl. claude-code) keeps the anthropic flow.
func oauthProviderKind(provider *models.Provider) string {
	if provider == nil {
		return oauthKindAnthropic
	}
	apiType := strings.ToLower(string(provider.APIType))
	name := strings.ToLower(provider.Name)
	if apiType == "codex" || name == "chatgpt" || name == "codex" {
		return oauthKindCodex
	}
	return oauthKindAnthropic
}

// oauthCallbackPath is the browser-callback path per kind. Anthropic keeps
// its historical /v1/oauth/callback; codex uses the fixed Codex CLI path.
func oauthCallbackPath(kind string) string {
	if kind == oauthKindCodex {
		return "/auth/callback"
	}
	return "/v1/oauth/callback"
}

// --- Service ----------------------------------------------------------------

type oauthService struct {
	oauthRepo       repository.OAuthRepository
	providerRepo    repository.ProviderRepository
	providerService ProviderService
	oauth           *auth.AnthropicOAuth // anthropic (claude-code) strategy
	codexOAuth      *auth.CodexOAuth     // codex (chatgpt) strategy
	logger          *slog.Logger
	states          map[string]int64  // state -> providerID
	redirectURIs    map[string]string // state -> redirect URI used for that flow

	mu              sync.Mutex // guards states/redirectURIs
	refreshLocks    map[int64]*sync.Mutex
	refreshOverride func(ctx context.Context, refreshToken string) (*models.OAuthToken, error) // test seam
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
		codexOAuth:      auth.NewCodexOAuth("", ""),
		logger:          logger,
		states:          make(map[string]int64),
		redirectURIs:    make(map[string]string),
		refreshLocks:    make(map[int64]*sync.Mutex),
	}
}

// strategyFor returns the OAuth strategy for a kind. The anthropic and codex
// instances are service-lifetime singletons: the PKCE code verifier generated
// by GetAuthorizationURL must survive until HandleCallback exchanges the code.
func (s *oauthService) strategyFor(kind string) oauthProviderClient {
	if kind == oauthKindCodex {
		return codexOAuthClient{oauth: s.codexOAuth}
	}
	return anthropicOAuthClient{oauth: s.oauth}
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

	s.mu.Lock()
	s.states[state] = providerID
	s.mu.Unlock()

	authURL = s.strategyFor(oauthProviderKind(provider)).GetAuthorizationURL(state)

	return authURL, state, nil
}

func (s *oauthService) StartAuthFlowWithCallback(ctx context.Context, providerID int64, callbackAddr string) (authURL, state string, err error) {
	provider, err := s.providerRepo.GetByID(ctx, providerID)
	if err != nil {
		return "", "", fmt.Errorf("get provider: %w", err)
	}
	if provider == nil {
		return "", "", fmt.Errorf("provider not found: %d", providerID)
	}

	kind := oauthProviderKind(provider)
	strategy := s.strategyFor(kind)
	redirectURI := fmt.Sprintf("http://%s%s", callbackAddr, oauthCallbackPath(kind))
	strategy.SetRedirectURI(redirectURI)

	state = generateState()

	s.mu.Lock()
	s.states[state] = providerID
	s.redirectURIs[state] = redirectURI
	s.mu.Unlock()

	authURL = strategy.GetAuthorizationURL(state)

	return authURL, state, nil
}

func (s *oauthService) HandleCallback(ctx context.Context, code, state string) (*models.OAuthToken, error) {
	s.mu.Lock()
	providerID, exists := s.states[state]
	redirectURI := s.redirectURIs[state]
	delete(s.states, state)
	delete(s.redirectURIs, state)
	s.mu.Unlock()
	if !exists {
		return nil, fmt.Errorf("invalid or expired state")
	}

	provider, err := s.providerRepo.GetByID(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	kind := oauthProviderKind(provider)

	strategy := s.strategyFor(kind)
	if redirectURI != "" {
		strategy.SetRedirectURI(redirectURI)
	}

	token, err := strategy.ExchangeCode(ctx, code, state)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}
	token.ProviderID = providerID

	// The initial exchange counts as a refresh for rotation-window purposes.
	now := time.Now()
	token.LastRefreshAt = &now

	if err := s.oauthRepo.Upsert(ctx, token); err != nil {
		return nil, fmt.Errorf("save token: %w", err)
	}

	s.logger.Info("OAuth connection established",
		"provider_id", providerID,
		"expires_at", token.ExpiresAt,
		"account_id", token.AccountID,
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

	// Cheap gate first (no provider lookup on the hot path): if the token
	// outlives the strictest per-kind margin (codex's 5-day policy) and no
	// rotation window has elapsed, both kinds can serve it as-is.
	now := time.Now()
	possiblyDue := token.ExpiresAt.Sub(now) < codexRefreshBeforeExpiry ||
		(token.LastRefreshAt != nil && now.Sub(*token.LastRefreshAt) > codexMaxRefreshInterval)
	if !possiblyDue {
		return token.AccessToken, nil
	}

	provider, err := s.providerRepo.GetByID(ctx, providerID)
	if err != nil {
		return "", fmt.Errorf("get provider: %w", err)
	}
	kind := oauthProviderKind(provider)

	// The precise policy is applied under the refresh lock (needsRefresh):
	// an anthropic token within 5 days of expiry is still perfectly usable.
	if kind != oauthKindCodex && token.ExpiresAt.Sub(now) >= anthropicRefreshSkew {
		return token.AccessToken, nil
	}

	unlock := s.lockRefresh(providerID)
	defer unlock()

	// Re-read under the lock: a concurrent caller may have refreshed already.
	token, err = s.oauthRepo.GetByProviderID(ctx, providerID)
	if err != nil {
		return "", fmt.Errorf("get token: %w", err)
	}
	if token == nil {
		return "", fmt.Errorf("not connected")
	}

	if !s.needsRefresh(token, kind) {
		return token.AccessToken, nil
	}

	refreshed, err := s.refreshOAuthToken(ctx, providerID, token, kind)
	if err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

// needsRefresh applies the per-kind refresh policy.
func (s *oauthService) needsRefresh(token *models.OAuthToken, kind string) bool {
	now := time.Now()
	if kind == oauthKindCodex {
		return token.ExpiresAt.Sub(now) < codexRefreshBeforeExpiry ||
			(token.LastRefreshAt != nil && now.Sub(*token.LastRefreshAt) > codexMaxRefreshInterval)
	}
	return !now.Before(token.ExpiresAt.Add(-anthropicRefreshSkew))
}

// lockRefresh serializes refreshes per provider (single-flight).
func (s *oauthService) lockRefresh(providerID int64) func() {
	s.mu.Lock()
	if s.refreshLocks == nil {
		s.refreshLocks = make(map[int64]*sync.Mutex)
	}
	m, ok := s.refreshLocks[providerID]
	if !ok {
		m = &sync.Mutex{}
		s.refreshLocks[providerID] = m
	}
	s.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// refreshOAuthToken exchanges the stored refresh token for a new one, merges
// the result and stamps last_refresh_at. On unrecoverable codex refresh
// errors the oauth row is deleted and ErrCodexReconnectRequired is returned.
func (s *oauthService) refreshOAuthToken(ctx context.Context, providerID int64, token *models.OAuthToken, kind string) (*models.OAuthToken, error) {
	var newToken *models.OAuthToken
	var err error
	if s.refreshOverride != nil {
		newToken, err = s.refreshOverride(ctx, token.RefreshToken)
	} else {
		newToken, err = s.strategyFor(kind).RefreshToken(ctx, token.RefreshToken)
	}
	if err != nil {
		if kind == oauthKindCodex {
			if marker, unrecoverable := codexUnrecoverableRefreshMarker(err); unrecoverable {
				if delErr := s.oauthRepo.Delete(ctx, providerID); delErr != nil {
					s.logger.Error("failed to delete invalidated codex token", "provider_id", providerID, "error", delErr)
				}
				return nil, fmt.Errorf("codex refresh rejected (%s): %w", marker, ErrCodexReconnectRequired)
			}
		}
		return nil, fmt.Errorf("refresh token: %w", err)
	}

	merged := *token
	merged.AccessToken = newToken.AccessToken
	if newToken.RefreshToken != "" {
		merged.RefreshToken = newToken.RefreshToken
	}
	if !newToken.ExpiresAt.IsZero() {
		merged.ExpiresAt = newToken.ExpiresAt
	}
	if newToken.AccountID != "" {
		merged.AccountID = newToken.AccountID
	}
	if newToken.Email != "" {
		merged.Email = newToken.Email
	}
	if newToken.IDToken != "" {
		merged.IDToken = newToken.IDToken
	}
	// Fill account/email from the codex id_token when the response carries one.
	applyCodexIDTokenClaims(&merged)
	now := time.Now()
	merged.LastRefreshAt = &now

	if err := s.oauthRepo.Upsert(ctx, &merged); err != nil {
		s.logger.Error("failed to save refreshed token", "error", err)
	}

	return &merged, nil
}

// RefreshNow refreshes the provider's token unconditionally, bypassing the
// per-kind refresh policy (design §4.5). It reuses refreshOAuthToken so the
// single-flight lock, rotation stamping, upsert, and the codex
// ErrCodexReconnectRequired classification all behave exactly like a
// policy-triggered refresh. Both the codex and anthropic refresh clients are
// supported (the anthropic client refreshes whenever given a refresh token).
func (s *oauthService) RefreshNow(ctx context.Context, provider *models.Provider) (*models.OAuthToken, error) {
	if provider == nil {
		return nil, fmt.Errorf("refresh: provider is nil")
	}
	token, err := s.oauthRepo.GetByProviderID(ctx, provider.ID)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	if token == nil {
		return nil, fmt.Errorf("not connected (re-run 'llm-router connect --provider %s')", provider.Name)
	}

	unlock := s.lockRefresh(provider.ID)
	defer unlock()

	// Re-read under the lock: a concurrent refresh may have already landed.
	token, err = s.oauthRepo.GetByProviderID(ctx, provider.ID)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	if token == nil {
		return nil, fmt.Errorf("not connected (re-run 'llm-router connect --provider %s')", provider.Name)
	}

	return s.refreshOAuthToken(ctx, provider.ID, token, oauthProviderKind(provider))
}

// GetTokenRow returns the stored oauth token row without evaluating the
// refresh policy (nil row when the provider is not connected).
func (s *oauthService) GetTokenRow(ctx context.Context, providerID int64) (*models.OAuthToken, error) {
	return s.oauthRepo.GetByProviderID(ctx, providerID)
}

// codexUnrecoverableRefreshMarkers are the ChatGPT refresh errors that mean
// the session is dead (design §3.1): re-login required, retrying is futile.
var codexUnrecoverableRefreshMarkers = []string{
	"invalid_grant",
	"refresh_token_reused",
	"refresh_token_expired",
	"refresh_token_invalidated",
}

func codexUnrecoverableRefreshMarker(err error) (string, bool) {
	msg := err.Error()
	for _, marker := range codexUnrecoverableRefreshMarkers {
		if strings.Contains(msg, marker) {
			return marker, true
		}
	}
	return "", false
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

	provider, err := s.providerRepo.GetByID(ctx, providerID)
	if err != nil {
		return false, token, nil
	}
	kind := oauthProviderKind(provider)

	if !s.needsRefresh(token, kind) {
		return true, token, nil
	}

	refreshed, err := s.refreshOAuthToken(ctx, providerID, token, kind)
	if err != nil {
		if errors.Is(err, ErrCodexReconnectRequired) {
			// The invalidated row was already deleted; report as not
			// connected so the CLI proceeds with a fresh auth flow.
			return false, nil, nil
		}
		return false, token, nil
	}

	return true, refreshed, nil
}

func (s *oauthService) StartCallbackServer(ctx context.Context, state string, providerID int64) (*models.OAuthToken, error) {
	return s.StartCallbackServerOnAddr(ctx, state, providerID, ":8080")
}

func (s *oauthService) StartCallbackServerOnAddr(ctx context.Context, state string, providerID int64, addr string) (*models.OAuthToken, error) {
	// The callback path follows the provider's OAuth flow kind: anthropic
	// keeps /v1/oauth/callback, codex uses the Codex CLI's /auth/callback.
	callbackPath := oauthCallbackPath(oauthKindAnthropic)
	if provider, err := s.providerRepo.GetByID(ctx, providerID); err == nil && provider != nil {
		callbackPath = oauthCallbackPath(oauthProviderKind(provider))
	}

	// Record the redirect URI for the exchange, unless StartAuthFlowWithCallback
	// already set a flow-specific one. The caller must pass the same host:port
	// the authorize URL advertised (e.g. "localhost:1455").
	s.mu.Lock()
	if _, ok := s.redirectURIs[state]; !ok {
		s.redirectURIs[state] = "http://" + addr + callbackPath
	}
	s.mu.Unlock()

	tokenCh := make(chan *models.OAuthToken, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
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
