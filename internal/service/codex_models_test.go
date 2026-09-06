package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chris/llm-router/internal/models"
)

// --- Codex discovery test doubles ---------------------------------------------

// fakeCodexTokens queues access tokens: call N returns tokens[N] (last one
// repeats). Models the OAuthService seam GetValidToken.
type fakeCodexTokens struct {
	mu     sync.Mutex
	tokens []string
	calls  int
}

func (f *fakeCodexTokens) GetValidToken(_ context.Context, _ int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.calls
	f.calls++
	if i >= len(f.tokens) {
		i = len(f.tokens) - 1
	}
	return f.tokens[i], nil
}

// fakeCodexRows models the OAuthRepository seam: the stored oauth row carries
// the ChatGPT account id.
type fakeCodexRows struct {
	accountID string
}

func (f *fakeCodexRows) GetByProviderID(_ context.Context, _ int64) (*models.OAuthToken, error) {
	return &models.OAuthToken{AccountID: f.accountID}, nil
}

// httpCatalogLister is the seam implemented the way the cmd adapter + proxy
// CodexClient behave: GET {base}/models?client_version=0.136.0 with the codex
// header set, catalog JSON decoding, and 401 wrapped in ErrCodexUnauthorized.
type httpCatalogLister struct{}

func (httpCatalogLister) ListModels(ctx context.Context, baseURL, accessToken, accountID string) ([]CodexModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(baseURL, "/")+"/models?client_version=0.136.0", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("User-Agent", "codex_cli_rs/0.136.0")
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-ID", accountID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codex: list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("%w: catalog rejected token", ErrCodexUnauthorized)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("codex: list models failed: %d", resp.StatusCode)
	}

	var parsed struct {
		Models []CodexModelInfo `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("codex: decode models: %w", err)
	}
	return parsed.Models, nil
}

// capturedRequest records the catalog request shape for assertions.
type capturedRequest struct {
	Path       string
	RawQuery   string
	Auth       string
	AccountID  string
	Originator string
	UserAgent  string
}

type catalogRecorder struct {
	mu       sync.Mutex
	requests []capturedRequest
}

func (r *catalogRecorder) record(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, capturedRequest{
		Path:       req.URL.Path,
		RawQuery:   req.URL.RawQuery,
		Auth:       req.Header.Get("Authorization"),
		AccountID:  req.Header.Get("ChatGPT-Account-ID"),
		Originator: req.Header.Get("Originator"),
		UserAgent:  req.Header.Get("User-Agent"),
	})
}

func (r *catalogRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *catalogRecorder) last() capturedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[len(r.requests)-1]
}

type catalogStage func(w http.ResponseWriter, r *http.Request)

// newCatalogServer stages responses: request N gets stages[N]; the final stage
// repeats for any further requests.
func newCatalogServer(t *testing.T, stages ...catalogStage) (*httptest.Server, *catalogRecorder) {
	t.Helper()
	rec := &catalogRecorder{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		i := rec.count() - 1
		if i >= len(stages) {
			i = len(stages) - 1
		}
		stages[i](w, r)
	}))
	t.Cleanup(ts.Close)
	return ts, rec
}

func stage401(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":{"message":"token expired"}}`))
}

func stage500(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte("boom"))
}

func stageCatalog(modelsJSON string) catalogStage {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(modelsJSON))
	}
}

// newCodexDiscoverFixture builds a model service for a codex provider with the
// discovery collaborators wired. provService is nil: the codex branch never
// decrypts an API key.
func newCodexDiscoverFixture(t *testing.T, baseURL string, tokens *fakeCodexTokens, rows oauthRowSource, lister CodexModelLister) (ModelService, *mockModelRepo, *mockTagRepo) {
	t.Helper()
	modelRepo := newMockModelRepo()
	tagRepo := newMockTagRepo()
	providerRepo := newMockProviderRepo()
	providerRepo.add(&models.Provider{
		ID:      7,
		Name:    "chatgpt",
		APIType: models.APITypeCodex,
		BaseURL: baseURL,
	})
	ms := NewModelService(modelRepo, tagRepo, providerRepo, nil, newMockGlobalMetaRepo(), nil)
	SetCodexDiscovery(ms, tokens, rows, lister)
	return ms, modelRepo, tagRepo
}

func tagsFor(t *testing.T, tagRepo *mockTagRepo, modelID int64) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, tag := range tagRepo.tags[modelID] {
		out[tag.Key] = tag.Value
	}
	return out
}

// --- Tests ---------------------------------------------------------------------

func TestDiscoverCodex_CatalogFilteringAndTagImport(t *testing.T) {
	catalog := `{"models":[
		{"slug":"gpt-5.1-codex","display_name":"GPT-5.1 Codex","default_reasoning_level":"medium",
		 "supported_reasoning_levels":[{"effort":"low","description":"low"},{"effort":"high","description":"high"}],
		 "visibility":"list","supported_in_api":true,"context_window":272000,"priority":1},
		{"slug":"gpt-5.1-codex-hidden","visibility":"hidden","supported_in_api":true,"context_window":9,"priority":2},
		{"slug":"gpt-5.1-codex-noinapi","visibility":"list","supported_in_api":false,"context_window":8,"priority":3},
		{"slug":"gpt-5.1-codex-flagomit","visibility":"list","context_window":128000,"priority":4}
	]}`
	ts, rec := newCatalogServer(t, stageCatalog(catalog))

	tokens := &fakeCodexTokens{tokens: []string{"tok-1"}}
	ms, modelRepo, tagRepo := newCodexDiscoverFixture(t, ts.URL, tokens, &fakeCodexRows{accountID: "acct-123"}, httpCatalogLister{})

	result, err := ms.Discover(context.Background(), 7)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// Only visibility=="list" with supported_in_api != false survive.
	wantAdded := []string{"gpt-5.1-codex", "gpt-5.1-codex-flagomit"}
	if len(result.Added) != len(wantAdded) {
		t.Fatalf("Added = %v, want %v", result.Added, wantAdded)
	}
	for i, name := range wantAdded {
		if result.Added[i] != name {
			t.Errorf("Added[%d] = %q, want %q", i, result.Added[i], name)
		}
	}

	// Slug → model name mapping.
	kept := map[string]bool{}
	for _, m := range modelRepo.models {
		kept[m.Name] = true
	}
	for _, name := range wantAdded {
		if !kept[name] {
			t.Errorf("model %q not upserted", name)
		}
	}
	for _, name := range []string{"gpt-5.1-codex-hidden", "gpt-5.1-codex-noinapi"} {
		if kept[name] {
			t.Errorf("filtered model %q was upserted", name)
		}
	}

	// Metadata import: context_window + default_reasoning_level tags on the
	// first kept model (mock upsert assigns ID 1).
	tags := tagsFor(t, tagRepo, 1)
	if tags["context_window"] != "272000" {
		t.Errorf("context_window tag = %q, want 272000", tags["context_window"])
	}
	if tags["default_reasoning_level"] != "medium" {
		t.Errorf("default_reasoning_level tag = %q, want medium", tags["default_reasoning_level"])
	}
	// Omitted supported_in_api stays in (!= false rule) and imports its window.
	tags2 := tagsFor(t, tagRepo, 2)
	if tags2["context_window"] != "128000" {
		t.Errorf("context_window tag = %q, want 128000", tags2["context_window"])
	}
	if _, ok := tags2["default_reasoning_level"]; ok {
		t.Error("default_reasoning_level tag set without catalog value")
	}

	// Request contract: codex catalog endpoint only — never the OpenAI
	// /v1/models path — with the codex header set.
	req := rec.last()
	if req.Path != "/models" {
		t.Errorf("request path = %q, want /models", req.Path)
	}
	if req.RawQuery != "client_version=0.136.0" {
		t.Errorf("query = %q, want client_version=0.136.0", req.RawQuery)
	}
	if req.Auth != "Bearer tok-1" {
		t.Errorf("Authorization = %q, want Bearer tok-1", req.Auth)
	}
	if req.AccountID != "acct-123" {
		t.Errorf("ChatGPT-Account-ID = %q, want acct-123", req.AccountID)
	}
	if req.Originator != "codex_cli_rs" {
		t.Errorf("Originator = %q, want codex_cli_rs", req.Originator)
	}
	if req.UserAgent != "codex_cli_rs/0.136.0" {
		t.Errorf("User-Agent = %q, want codex_cli_rs/0.136.0", req.UserAgent)
	}
}

func TestDiscoverCodex_401RefreshRetry(t *testing.T) {
	catalog := `{"models":[
		{"slug":"gpt-5-codex","visibility":"list","supported_in_api":true,"context_window":400000,"priority":1}
	]}`
	// First call: 401. Second call (after re-resolving the token): catalog.
	ts, rec := newCatalogServer(t, stage401, stageCatalog(catalog))

	tokens := &fakeCodexTokens{tokens: []string{"tok-stale", "tok-rotated"}}
	ms, modelRepo, _ := newCodexDiscoverFixture(t, ts.URL, tokens, &fakeCodexRows{accountID: "acct-9"}, httpCatalogLister{})

	result, err := ms.Discover(context.Background(), 7)
	if err != nil {
		t.Fatalf("Discover: %v", err) // discovery must never fail
	}

	if rec.count() != 2 {
		t.Fatalf("catalog requests = %d, want 2 (one retry)", rec.count())
	}
	if got := rec.last().Auth; got != "Bearer tok-rotated" {
		t.Errorf("retry Authorization = %q, want Bearer tok-rotated", got)
	}
	if len(result.Added) != 1 || result.Added[0] != "gpt-5-codex" {
		t.Errorf("Added = %v, want [gpt-5-codex]", result.Added)
	}
	if len(modelRepo.models) != 1 || modelRepo.models[0].Name != "gpt-5-codex" {
		t.Errorf("models = %v, want [gpt-5-codex]", modelRepo.models)
	}
}

func TestDiscoverCodex_401RetryExhaustedFallsBack(t *testing.T) {
	// 401 twice: the retry happens once with the re-resolved token, then
	// discovery falls back to the static seed list instead of failing.
	ts, rec := newCatalogServer(t, stage401, stage401)

	tokens := &fakeCodexTokens{tokens: []string{"tok-1"}}
	ms, modelRepo, _ := newCodexDiscoverFixture(t, ts.URL, tokens, &fakeCodexRows{}, httpCatalogLister{})

	result, err := ms.Discover(context.Background(), 7)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if rec.count() != 2 {
		t.Errorf("catalog requests = %d, want 2 (exactly one retry)", rec.count())
	}
	if len(result.Added) != len(CodexFallbackModels) {
		t.Fatalf("Added = %v, want the %d-entry seed list", result.Added, len(CodexFallbackModels))
	}
	for i, name := range CodexFallbackModels {
		if result.Added[i] != name {
			t.Errorf("Added[%d] = %q, want %q", i, result.Added[i], name)
		}
	}
	if len(modelRepo.models) != len(CodexFallbackModels) {
		t.Errorf("models = %d, want %d", len(modelRepo.models), len(CodexFallbackModels))
	}
}

func TestDiscoverCodex_FallbackOnCatalog500(t *testing.T) {
	ts, _ := newCatalogServer(t, stage500)

	tokens := &fakeCodexTokens{tokens: []string{"tok-1"}}
	ms, _, tagRepo := newCodexDiscoverFixture(t, ts.URL, tokens, &fakeCodexRows{accountID: "acct-1"}, httpCatalogLister{})

	result, err := ms.Discover(context.Background(), 7)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(result.Added) != len(CodexFallbackModels) {
		t.Fatalf("Added = %v, want the seed list", result.Added)
	}
	for i, name := range CodexFallbackModels {
		if result.Added[i] != name {
			t.Errorf("Added[%d] = %q, want %q", i, result.Added[i], name)
		}
	}
	// Fallback imports no metadata tags.
	for _, m := range tagRepo.tags {
		if len(m) != 0 {
			t.Errorf("fallback imported tags: %v", m)
		}
	}
}

func TestDiscoverCodex_FallbackOnTransportFailure(t *testing.T) {
	// Unreachable base URL: transport error → seed fallback, never an error.
	tokens := &fakeCodexTokens{tokens: []string{"tok-1"}}
	ms, _, _ := newCodexDiscoverFixture(t, "http://127.0.0.1:1", tokens, &fakeCodexRows{}, httpCatalogLister{})

	result, err := ms.Discover(context.Background(), 7)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.Added) != len(CodexFallbackModels) {
		t.Fatalf("Added = %v, want the seed list", result.Added)
	}
}

func TestDiscoverCodex_FallbackWhenNotWired(t *testing.T) {
	// No SetCodexDiscovery wiring (e.g. CLI paths): codex discovery still
	// yields the seed list and must not error on the missing key/OAuth.
	modelRepo := newMockModelRepo()
	tagRepo := newMockTagRepo()
	providerRepo := newMockProviderRepo()
	providerRepo.add(&models.Provider{ID: 3, Name: "chatgpt", APIType: models.APITypeCodex, BaseURL: "https://chatgpt.com/backend-api/codex"})
	ms := NewModelService(modelRepo, tagRepo, providerRepo, nil, newMockGlobalMetaRepo(), nil)

	result, err := ms.Discover(context.Background(), 3)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.Added) != len(CodexFallbackModels) {
		t.Fatalf("Added = %v, want the seed list", result.Added)
	}
	for i, name := range CodexFallbackModels {
		if result.Added[i] != name {
			t.Errorf("Added[%d] = %q, want %q", i, result.Added[i], name)
		}
	}
}

func TestDiscoverCodex_RemovesStaleModels(t *testing.T) {
	catalog := `{"models":[{"slug":"gpt-5.1-codex","visibility":"list","supported_in_api":true,"priority":1}]}`
	ts, _ := newCatalogServer(t, stageCatalog(catalog))

	tokens := &fakeCodexTokens{tokens: []string{"tok-1"}}
	ms, modelRepo, _ := newCodexDiscoverFixture(t, ts.URL, tokens, &fakeCodexRows{}, httpCatalogLister{})

	// Pre-existing model no longer in the catalog.
	if err := modelRepo.Create(context.Background(), &models.Model{ProviderID: 7, Name: "retired-model"}); err != nil {
		t.Fatalf("seed stale model: %v", err)
	}

	result, err := ms.Discover(context.Background(), 7)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "retired-model" {
		t.Errorf("Removed = %v, want [retired-model]", result.Removed)
	}
	for _, m := range modelRepo.models {
		if m.Name == "retired-model" && !m.Disabled {
			t.Error("stale model not disabled")
		}
	}
}

// Compile-time guard: the production OAuth service satisfies the token seam.
var _ oauthTokenSource = OAuthService(nil)
