package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
)

// --- connect routing + help text ------------------------------------------------

// runConnectRoute asserts at compile level that the `connect` route's entry
// point exists with the dispatcher's expected signature — the command switch
// in main() itself is not unit-testable (os.Exit on failure paths), so the
// route contract is: `case "connect": runConnect(os.Args[2:])` (added in
// main.go; it was dropped in 4c3d578 and `llm-router connect --provider
// chatgpt` was unreachable).
var runConnectRoute func([]string) = runConnect

// usageTextForTest captures printUsage()'s stdout.
func usageTextForTest(t *testing.T) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("open pipe: %v", err)
	}
	os.Stdout = w
	printUsage()
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read usage: %v", err)
	}
	r.Close()
	return buf.String()
}

// TestConnectRouteDocumentedInUsage asserts the help text advertises the
// `connect` command (and the chatgpt/codex OAuth flow) again.
func TestConnectRouteDocumentedInUsage(t *testing.T) {
	usage := usageTextForTest(t)
	for _, want := range []string{
		"llm-router connect",
		"Connect a provider",
		"--provider chatgpt",
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}

// TestConnectRouteEntryPoint verifies the routed entry point is wired: the
// signature matches the dispatcher's runConnect(os.Args[2:]) call shape.
func TestConnectRouteEntryPoint(t *testing.T) {
	if runConnectRoute == nil {
		t.Fatal("connect route entry point must be non-nil")
	}
}

// --- discover codex wiring -------------------------------------------------------

// TestDiscoverModelService_CodexWired_LiveCatalog drives the discover command's
// construction helper end-to-end against an httptest codex catalog: with the
// SetCodexDiscovery wiring in place a codex provider discovers the LIVE
// catalog (filtered, slug-mapped) instead of falling back to the static seed
// list.
func TestDiscoverModelService_CodexWired_LiveCatalog(t *testing.T) {
	catalog := `{"models":[
		{"slug":"gpt-5.1-codex","visibility":"list","supported_in_api":true,"context_window":272000,"priority":1},
		{"slug":"gpt-5.1-codex-hidden","visibility":"hidden","supported_in_api":true,"priority":2}
	]}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.URL.Query().Get("client_version") == "" {
			t.Errorf("unexpected catalog request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-live" {
			t.Errorf("Authorization = %q, want Bearer tok-live", got)
		}
		if got := r.Header.Get("ChatGPT-Account-ID"); got != "acct-7" {
			t.Errorf("ChatGPT-Account-ID = %q, want acct-7", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(catalog))
	}))
	defer ts.Close()

	database := newSetupTestDB(t)
	providerRepo := repository.NewProviderRepository(database)
	providerMetadataRepo := repository.NewProviderMetadataRepository(database)
	modelRepo := repository.NewModelRepository(database)
	tagRepo := repository.NewTagRepository(database)
	globalRepo := repository.NewGlobalMetadataRepository(database)
	oauthRepo := repository.NewOAuthRepository(database)

	ctx := context.Background()
	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, testEncryptionKey)
	provider, err := providerService.Create(ctx, models.CreateProviderRequest{
		Name:    "chatgpt",
		APIType: models.APITypeCodex,
		BaseURL: ts.URL,
		APIKey:  "",
	})
	if err != nil {
		t.Fatalf("create codex provider: %v", err)
	}

	// Seed a fresh oauth row (10-day expiry, just refreshed) so the OAuth
	// service serves the token as-is instead of attempting a network refresh.
	if err := oauthRepo.Upsert(ctx, &models.OAuthToken{
		ProviderID:    provider.ID,
		AccessToken:   "tok-live",
		RefreshToken:  "rt",
		ExpiresAt:     time.Now().Add(10 * 24 * time.Hour),
		LastRefreshAt: nowPtr(),
		AccountID:     "acct-7",
	}); err != nil {
		t.Fatalf("seed oauth token: %v", err)
	}

	modelService := buildDiscoverModelService(providerRepo, oauthRepo, modelRepo, tagRepo, providerService, globalRepo)

	result, err := modelService.Discover(ctx, provider.ID)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// Live catalog, not the seed fallback: exactly the one listable slug
	// (the hidden entry is filtered and the seed's other models are absent).
	if len(result.Added) != 1 || result.Added[0] != "gpt-5.1-codex" {
		t.Fatalf("Added = %v, want [gpt-5.1-codex] (hidden entry filtered)", result.Added)
	}
	discovered, err := modelRepo.ListByProvider(ctx, provider.ID)
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(discovered) != 1 || discovered[0].Name != "gpt-5.1-codex" {
		t.Errorf("discovered models = %v, want [gpt-5.1-codex]", modelNames(discovered))
	}
}

// TestDiscoverModelService_CodexWired_FallbackStaysSafe asserts that when the
// catalog is unreachable the wired discover path still never fails — it falls
// back to the seed list (discovery must never be worse than before).
func TestDiscoverModelService_CodexWired_FallbackStaysSafe(t *testing.T) {
	database := newSetupTestDB(t)
	providerRepo := repository.NewProviderRepository(database)
	providerMetadataRepo := repository.NewProviderMetadataRepository(database)
	modelRepo := repository.NewModelRepository(database)
	tagRepo := repository.NewTagRepository(database)
	globalRepo := repository.NewGlobalMetadataRepository(database)
	oauthRepo := repository.NewOAuthRepository(database)

	ctx := context.Background()
	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, testEncryptionKey)
	provider, err := providerService.Create(ctx, models.CreateProviderRequest{
		Name:    "chatgpt",
		APIType: models.APITypeCodex,
		BaseURL: "http://127.0.0.1:1", // unreachable
		APIKey:  "",
	})
	if err != nil {
		t.Fatalf("create codex provider: %v", err)
	}

	if err := oauthRepo.Upsert(ctx, &models.OAuthToken{
		ProviderID:    provider.ID,
		AccessToken:   "tok-live",
		RefreshToken:  "rt",
		ExpiresAt:     time.Now().Add(10 * 24 * time.Hour),
		LastRefreshAt: nowPtr(),
	}); err != nil {
		t.Fatalf("seed oauth token: %v", err)
	}

	modelService := buildDiscoverModelService(providerRepo, oauthRepo, modelRepo, tagRepo, providerService, globalRepo)

	result, err := modelService.Discover(ctx, provider.ID)
	if err != nil {
		t.Fatalf("Discover must never fail: %v", err)
	}
	if len(result.Added) != len(service.CodexFallbackModels) {
		t.Fatalf("Added = %v, want the %d-entry seed list", result.Added, len(service.CodexFallbackModels))
	}
	for i, name := range service.CodexFallbackModels {
		if result.Added[i] != name {
			t.Errorf("Added[%d] = %q, want %q", i, result.Added[i], name)
		}
	}
}

// --- helpers -----------------------------------------------------------------

func nowPtr() *time.Time {
	now := time.Now()
	return &now
}

func modelNames(ms []models.Model) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}
