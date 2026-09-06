package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
	_ "modernc.org/sqlite"
)

// --- Test harness ------------------------------------------------------------

// newOAuthTestDB opens an in-memory sqlite DB with the oauth_tokens schema
// as it exists after migration 029 (matching migration 006 + 029).
func newOAuthTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`
		CREATE TABLE oauth_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id INTEGER NOT NULL UNIQUE,
			access_token TEXT NOT NULL,
			refresh_token TEXT NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			account_id TEXT,
			email TEXT,
			last_refresh_at DATETIME,
			id_token TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	// One connection only: each pooled connection would otherwise get its own
	// empty :memory: database (same constraint as production db.Open).
	database.SetMaxOpenConns(1)
	return database
}

type oauthFixture struct {
	svc       *oauthService
	oauthRepo repository.OAuthRepository
	providers *mockProviderRepo
}

func newOAuthFixture(t *testing.T) *oauthFixture {
	t.Helper()
	database := newOAuthTestDB(t)
	providers := newMockProviderRepo()
	f := &oauthFixture{
		svc: NewOAuthService(
			repository.NewOAuthRepository(database),
			providers,
			nil, // providerService is not used by oauthService today
			slog.New(slog.NewTextHandler(io.Discard, nil)),
		).(*oauthService),
		oauthRepo: repository.NewOAuthRepository(database),
		providers: providers,
	}
	return f
}

// addProvider registers a provider row for the given kind.
func (f *oauthFixture) addProvider(id int64, name string, apiType models.APIType) *models.Provider {
	p := &models.Provider{ID: id, Name: name, APIType: apiType, BaseURL: "http://x"}
	f.providers.add(p)
	return p
}

// seedToken inserts an oauth row directly.
func (f *oauthFixture) seedToken(t *testing.T, providerID int64, accessToken, refreshToken string, expiresAt time.Time, lastRefreshAt *time.Time) {
	t.Helper()
	token := &models.OAuthToken{
		ProviderID:    providerID,
		AccessToken:   accessToken,
		RefreshToken:  refreshToken,
		ExpiresAt:     expiresAt,
		LastRefreshAt: lastRefreshAt,
	}
	if err := f.oauthRepo.Upsert(context.Background(), token); err != nil {
		t.Fatalf("seed token: %v", err)
	}
}

func refreshTokenErr(marker string) error {
	return fmt.Errorf("codex token request failed: 400 {\"error\":\"%s\",\"error_description\":\"nope\"}", marker)
}

// --- Strategy selection -------------------------------------------------------

func TestOAuthProviderKind(t *testing.T) {
	cases := []struct {
		provider *models.Provider
		want     string
	}{
		{&models.Provider{Name: "chatgpt", APIType: models.APITypeAnthropic}, oauthKindCodex},
		{&models.Provider{Name: "anything", APIType: "codex"}, oauthKindCodex},
		{&models.Provider{Name: "codex", APIType: models.APITypeOpenAI}, oauthKindCodex},
		{&models.Provider{Name: "ChatGPT", APIType: models.APITypeOpenAI}, oauthKindCodex},
		{&models.Provider{Name: "claude-code", APIType: models.APITypeAnthropic}, oauthKindAnthropic},
		{&models.Provider{Name: "my-openai", APIType: models.APITypeOpenAI}, oauthKindAnthropic},
		{nil, oauthKindAnthropic},
	}
	for _, tc := range cases {
		if got := oauthProviderKind(tc.provider); got != tc.want {
			t.Errorf("oauthProviderKind(%+v) = %q, want %q", tc.provider, got, tc.want)
		}
	}
}

func TestOAuthCallbackPath(t *testing.T) {
	if got := oauthCallbackPath(oauthKindAnthropic); got != "/v1/oauth/callback" {
		t.Errorf("anthropic callback path = %q", got)
	}
	if got := oauthCallbackPath(oauthKindCodex); got != "/auth/callback" {
		t.Errorf("codex callback path = %q", got)
	}
}

// StartAuthFlow must select the strategy by provider and keep existing
// (anthropic) callers working unchanged.
func TestStartAuthFlow_SelectsStrategyByProvider(t *testing.T) {
	f := newOAuthFixture(t)
	ctx := context.Background()

	anthro := f.addProvider(1, "claude-code", models.APITypeAnthropic)
	f.addProvider(2, "chatgpt", models.APITypeAnthropic) // name-based selection

	anthroURL, _, err := f.svc.StartAuthFlow(ctx, anthro.ID)
	if err != nil {
		t.Fatalf("StartAuthFlow(anthropic): %v", err)
	}
	if !strings.HasPrefix(anthroURL, "https://claude.ai/oauth/authorize") {
		t.Errorf("anthropic flow should use claude.ai, got %q", anthroURL)
	}

	codexURL, _, err := f.svc.StartAuthFlow(ctx, 2) // "chatgpt" provider registered above
	if err != nil {
		t.Fatalf("StartAuthFlow(codex): %v", err)
	}
	if !strings.HasPrefix(codexURL, "https://auth.openai.com/oauth/authorize") {
		t.Errorf("codex flow should use auth.openai.com, got %q", codexURL)
	}
	if !strings.Contains(codexURL, "originator=codex_cli_rs") {
		t.Errorf("codex flow should carry codex params, got %q", codexURL)
	}
}

// --- Exchange path (HandleCallback) -------------------------------------------

func TestHandleCallback_Codex_StoresIDTokenClaims(t *testing.T) {
	f := newOAuthFixture(t)
	ctx := context.Background()

	f.addProvider(7, "chatgpt", models.APITypeAnthropic)
	f.svc.codexOAuth.SetRedirectURI("http://localhost:1455/auth/callback")
	f.svc.codexOAuth.GetAuthorizationURL("state-1") // sets PKCE verifier

	// Stand-in id_token whose claims the adapter should parse into the row.
	idToken := buildFakeIDToken(t, "acc-777", "pro", "dev@example.com")
	f.svc.refreshOverride = func(_ context.Context, _ string) (*models.OAuthToken, error) {
		return &models.OAuthToken{
			AccessToken:  "at-codex",
			RefreshToken: "rt-rotated",
			ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
			IDToken:      idToken,
		}, nil
	}

	// ExchangeCode itself would hit the network; drive the adapter directly
	// through refreshOAuthToken to validate merge+stamp behavior, then assert
	// the persisted row.
	token, err := f.svc.refreshOAuthToken(ctx, 7, &models.OAuthToken{
		ProviderID:   7,
		AccessToken:  "at-old",
		RefreshToken: "rt-old",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}, oauthKindCodex)
	if err != nil {
		t.Fatalf("refreshOAuthToken: %v", err)
	}

	if token.AccessToken != "at-codex" {
		t.Errorf("AccessToken = %q", token.AccessToken)
	}
	if token.RefreshToken != "rt-rotated" {
		t.Errorf("rotated RefreshToken = %q, want rt-rotated", token.RefreshToken)
	}
	if token.AccountID != "acc-777" || token.Email != "dev@example.com" {
		t.Errorf("id_token claims not mapped: accountID=%q email=%q", token.AccountID, token.Email)
	}
	if token.LastRefreshAt == nil {
		t.Error("LastRefreshAt should be stamped after refresh")
	}

	stored, err := f.oauthRepo.GetByProviderID(ctx, 7)
	if err != nil || stored == nil {
		t.Fatalf("stored token: %v, %v", stored, err)
	}
	if stored.RefreshToken != "rt-rotated" || stored.AccountID != "acc-777" {
		t.Errorf("stored row wrong: %+v", stored)
	}
	if stored.LastRefreshAt == nil {
		t.Error("stored row missing last_refresh_at")
	}
	if stored.IDToken != idToken {
		t.Errorf("stored IDToken not persisted")
	}
}

// --- Codex refresh policy ------------------------------------------------------

func TestGetValidToken_CodexFreshToken_NoRefresh(t *testing.T) {
	f := newOAuthFixture(t)
	ctx := context.Background()

	f.addProvider(1, "chatgpt", models.APITypeAnthropic)
	now := time.Now()
	f.seedToken(t, 1, "at-ok", "rt-1", now.Add(30*24*time.Hour), &now)

	calls := 0
	f.svc.refreshOverride = func(_ context.Context, _ string) (*models.OAuthToken, error) {
		calls++
		return nil, errors.New("should not refresh")
	}

	got, err := f.svc.GetValidToken(ctx, 1)
	if err != nil {
		t.Fatalf("GetValidToken: %v", err)
	}
	if got != "at-ok" {
		t.Errorf("token = %q, want at-ok", got)
	}
	if calls != 0 {
		t.Error("fresh token should not trigger refresh")
	}
}

func TestGetValidToken_CodexNearExpiry_Refreshes(t *testing.T) {
	f := newOAuthFixture(t)
	ctx := context.Background()

	f.addProvider(1, "chatgpt", models.APITypeAnthropic)
	now := time.Now()
	lastRefresh := now.Add(-time.Hour)
	f.seedToken(t, 1, "at-old", "rt-1", now.Add(4*24*time.Hour), &lastRefresh) // < 5d to expiry

	calls := 0
	f.svc.refreshOverride = func(_ context.Context, rt string) (*models.OAuthToken, error) {
		calls++
		if rt != "rt-1" {
			t.Errorf("refresh used wrong token %q", rt)
		}
		return &models.OAuthToken{
			AccessToken:  "at-new",
			RefreshToken: "rt-2",
			ExpiresAt:    time.Now().Add(30 * 24 * time.Hour),
		}, nil
	}

	got, err := f.svc.GetValidToken(ctx, 1)
	if err != nil {
		t.Fatalf("GetValidToken: %v", err)
	}
	if got != "at-new" {
		t.Errorf("token = %q, want at-new", got)
	}
	if calls != 1 {
		t.Errorf("refresh calls = %d, want 1", calls)
	}

	stored, _ := f.oauthRepo.GetByProviderID(ctx, 1)
	if stored.RefreshToken != "rt-2" {
		t.Errorf("rotated refresh token not persisted: %q", stored.RefreshToken)
	}
	if stored.LastRefreshAt == nil || time.Since(*stored.LastRefreshAt) > time.Minute {
		t.Errorf("last_refresh_at not stamped: %v", stored.LastRefreshAt)
	}
}

func TestGetValidToken_CodexRotationWindowDue_Refreshes(t *testing.T) {
	f := newOAuthFixture(t)
	ctx := context.Background()

	f.addProvider(1, "chatgpt", models.APITypeAnthropic)
	now := time.Now()
	lastRefresh := now.Add(-9 * 24 * time.Hour) // > 8 days, but token still valid for 30d
	f.seedToken(t, 1, "at-ok", "rt-1", now.Add(30*24*time.Hour), &lastRefresh)

	calls := 0
	f.svc.refreshOverride = func(_ context.Context, _ string) (*models.OAuthToken, error) {
		calls++
		return &models.OAuthToken{
			AccessToken:  "at-rot",
			RefreshToken: "rt-2",
			ExpiresAt:    time.Now().Add(30 * 24 * time.Hour),
		}, nil
	}

	got, err := f.svc.GetValidToken(ctx, 1)
	if err != nil {
		t.Fatalf("GetValidToken: %v", err)
	}
	if got != "at-rot" {
		t.Errorf("token = %q, want at-rot", got)
	}
	if calls != 1 {
		t.Errorf("refresh calls = %d, want 1", calls)
	}
}

func TestGetValidToken_CodexRotationWindowNotDue_NoRefresh(t *testing.T) {
	f := newOAuthFixture(t)
	ctx := context.Background()

	f.addProvider(1, "chatgpt", models.APITypeAnthropic)
	now := time.Now()
	lastRefresh := now.Add(-7 * 24 * time.Hour) // < 8 days
	f.seedToken(t, 1, "at-ok", "rt-1", now.Add(30*24*time.Hour), &lastRefresh)

	calls := 0
	f.svc.refreshOverride = func(_ context.Context, _ string) (*models.OAuthToken, error) {
		calls++
		return nil, errors.New("should not refresh")
	}

	got, err := f.svc.GetValidToken(ctx, 1)
	if err != nil {
		t.Fatalf("GetValidToken: %v", err)
	}
	if got != "at-ok" || calls != 0 {
		t.Errorf("token = %q, calls = %d; want at-ok/0", got, calls)
	}
}

// Anthropic keeps the legacy behavior: only the 5-minute skew triggers a
// refresh; an old last_refresh_at alone must not.
func TestGetValidToken_Anthropic_IgnoresRotationWindow(t *testing.T) {
	f := newOAuthFixture(t)
	ctx := context.Background()

	f.addProvider(1, "claude-code", models.APITypeAnthropic)
	now := time.Now()
	lastRefresh := now.Add(-9 * 24 * time.Hour)
	f.seedToken(t, 1, "at-ok", "rt-1", now.Add(30*24*time.Hour), &lastRefresh)

	calls := 0
	f.svc.refreshOverride = func(_ context.Context, _ string) (*models.OAuthToken, error) {
		calls++
		return nil, errors.New("should not refresh")
	}

	got, err := f.svc.GetValidToken(ctx, 1)
	if err != nil {
		t.Fatalf("GetValidToken: %v", err)
	}
	if got != "at-ok" || calls != 0 {
		t.Errorf("token = %q, calls = %d; want at-ok/0", got, calls)
	}
}

// --- Unrecoverable refresh errors ------------------------------------------------

func TestGetValidToken_CodexUnrecoverable_DeletesRowAndReturnsReconnectError(t *testing.T) {
	markers := []string{"invalid_grant", "refresh_token_reused", "refresh_token_expired", "refresh_token_invalidated"}
	for _, marker := range markers {
		t.Run(marker, func(t *testing.T) {
			f := newOAuthFixture(t)
			ctx := context.Background()

			f.addProvider(1, "chatgpt", models.APITypeAnthropic)
			now := time.Now()
			f.seedToken(t, 1, "at-old", "rt-1", now.Add(4*24*time.Hour), &now)

			f.svc.refreshOverride = func(_ context.Context, _ string) (*models.OAuthToken, error) {
				return nil, refreshTokenErr(marker)
			}

			_, err := f.svc.GetValidToken(ctx, 1)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrCodexReconnectRequired) {
				t.Errorf("error %v should wrap ErrCodexReconnectRequired", err)
			}
			if !strings.Contains(err.Error(), "llm-router connect --provider chatgpt") {
				t.Errorf("error should mention reconnect command: %v", err)
			}

			stored, gerr := f.oauthRepo.GetByProviderID(ctx, 1)
			if gerr != nil {
				t.Fatalf("get after delete: %v", gerr)
			}
			if stored != nil {
				t.Errorf("oauth row should be deleted for marker %q", marker)
			}
		})
	}
}

func TestGetValidToken_CodexRecoverableError_KeepsRow(t *testing.T) {
	f := newOAuthFixture(t)
	ctx := context.Background()

	f.addProvider(1, "chatgpt", models.APITypeAnthropic)
	now := time.Now()
	f.seedToken(t, 1, "at-old", "rt-1", now.Add(4*24*time.Hour), &now)

	f.svc.refreshOverride = func(_ context.Context, _ string) (*models.OAuthToken, error) {
		return nil, errors.New("codex token request failed: 500 server_error (body: busy)")
	}

	_, err := f.svc.GetValidToken(ctx, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrCodexReconnectRequired) {
		t.Errorf("transient error must not be classified as reconnect-required: %v", err)
	}

	stored, _ := f.oauthRepo.GetByProviderID(ctx, 1)
	if stored == nil {
		t.Error("oauth row should survive transient refresh errors")
	}
}

func TestIsConnected_CodexUnrecoverable_ReportsNotConnected(t *testing.T) {
	f := newOAuthFixture(t)
	ctx := context.Background()

	f.addProvider(1, "chatgpt", models.APITypeAnthropic)
	now := time.Now()
	f.seedToken(t, 1, "at-old", "rt-1", now.Add(4*24*time.Hour), &now)

	f.svc.refreshOverride = func(_ context.Context, _ string) (*models.OAuthToken, error) {
		return nil, refreshTokenErr("invalid_grant")
	}

	connected, token, err := f.svc.IsConnected(ctx, 1)
	if err != nil {
		t.Fatalf("IsConnected: %v", err)
	}
	if connected || token != nil {
		t.Errorf("expected (false, nil), got (%v, %v)", connected, token)
	}

	stored, _ := f.oauthRepo.GetByProviderID(ctx, 1)
	if stored != nil {
		t.Error("oauth row should be deleted after unrecoverable refresh error")
	}
}

// --- Single-flight refresh ---------------------------------------------------------

func TestGetValidToken_SingleFlight_OneRefresh(t *testing.T) {
	f := newOAuthFixture(t)
	ctx := context.Background()

	f.addProvider(1, "chatgpt", models.APITypeAnthropic)
	now := time.Now()
	f.seedToken(t, 1, "at-old", "rt-1", now.Add(4*24*time.Hour), &now)

	var calls int
	var mu sync.Mutex
	started := make(chan struct{}, 10)
	release := make(chan struct{})
	f.svc.refreshOverride = func(_ context.Context, _ string) (*models.OAuthToken, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		started <- struct{}{}
		<-release
		return &models.OAuthToken{
			AccessToken:  "at-new",
			RefreshToken: "rt-2",
			ExpiresAt:    time.Now().Add(30 * 24 * time.Hour),
		}, nil
	}

	const n = 5
	results := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			tok, err := f.svc.GetValidToken(ctx, 1)
			results <- tok
			errs <- err
		}()
	}

	// Wait until at least one refresh is in flight, then let them all finish.
	<-started
	close(release)

	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("GetValidToken: %v", err)
		}
		if tok := <-results; tok != "at-new" {
			t.Fatalf("token = %q, want at-new", tok)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("refresh calls = %d, want 1 (single-flight)", calls)
	}
}

// --- Helpers -------------------------------------------------------------------------

// buildFakeIDToken creates a header.payload.sig JWT with the OpenAI auth
// claim namespace (same fixture style as the auth package tests).
func buildFakeIDToken(t *testing.T, accountID, planType, email string) string {
	t.Helper()
	b64 := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

	claims := map[string]any{
		"email": email,
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": accountID,
			"chatgpt_plan_type":  planType,
		},
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return b64(`{"alg":"RS256","typ":"JWT"}`) + "." + b64(string(payload)) + ".sig"
}
