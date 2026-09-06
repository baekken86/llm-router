package main

import (
	"context"
	"strings"
	"testing"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/service"
)

// --- supportedProviders registry ---------------------------------------------------

// TestSetup_ChatgptSupportedProvider verifies the canonical chatgpt setup
// entry: api_type codex, OAuth auth, canonical ChatGPT backend URL (design
// §4.4).
func TestSetup_ChatgptSupportedProvider(t *testing.T) {
	cfg, exists := supportedProviders["chatgpt"]
	if !exists {
		t.Fatal("chatgpt should be in supportedProviders")
	}
	if cfg.name != "chatgpt" {
		t.Errorf("name = %q, want chatgpt", cfg.name)
	}
	if cfg.apiType != "codex" {
		t.Errorf("apiType = %q, want codex", cfg.apiType)
	}
	if cfg.auth != "oauth" {
		t.Errorf("auth = %q, want oauth", cfg.auth)
	}
	if cfg.baseURL != "https://chatgpt.com/backend-api/codex" {
		t.Errorf("baseURL = %q, want https://chatgpt.com/backend-api/codex", cfg.baseURL)
	}
}

// TestSetup_CodexAliasResolvesToChatgpt verifies the "codex" alias maps onto
// the canonical chatgpt entry through the shared normalizer.
func TestSetup_CodexAliasResolvesToChatgpt(t *testing.T) {
	cfg, ok := supportedProviders[normalizeChatgptProviderName("codex")]
	if !ok {
		t.Fatal("normalized 'codex' should resolve to the chatgpt entry")
	}
	if cfg.apiType != "codex" || cfg.name != "chatgpt" {
		t.Errorf("alias resolution wrong: %+v", cfg)
	}
}

// TestSetup_ExistingProvidersUnchanged guards the pre-existing entries while
// the chatgpt row is added.
func TestSetup_ExistingProvidersUnchanged(t *testing.T) {
	checks := map[string]providerConfig{
		"claude-code": {name: "claude-code", apiType: "anthropic", auth: "oauth"},
		"opencode-go": {name: "opencode-go", apiType: "openai", auth: "apikey"},
		"openai":      {name: "openai", apiType: "openai", auth: "apikey"},
		"anthropic":   {name: "anthropic", apiType: "anthropic", auth: "apikey"},
		"cloudflare":  {name: "cloudflare", apiType: "cloudflare", auth: "apikey"},
	}
	for key, want := range checks {
		got, ok := supportedProviders[key]
		if !ok {
			t.Errorf("%s missing from supportedProviders", key)
			continue
		}
		if got.apiType != want.apiType || got.auth != want.auth || got.name != want.name {
			t.Errorf("supportedProviders[%s] = %+v, want %+v", key, got, want)
		}
	}
}

// --- Predefined models ----------------------------------------------------------------

// TestSetup_ChatgptPredefinedModelsAreCodexSeed verifies createPredefinedModels
// uses the shared codex seed list for chatgpt (design §3.3/§4.4).
func TestSetup_ChatgptPredefinedModelsAreCodexSeed(t *testing.T) {
	f := newSetupModelFixture(t)
	defer f.Close()
	ctx := context.Background()

	provider := f.seedProvider(t, "chatgpt", models.APITypeCodex)
	createPredefinedModels(ctx, f.models, f.tags, f.globals, provider, "chatgpt", testLogger())

	got := f.listModels(t, provider.ID)
	if len(got) != len(service.CodexFallbackModels) {
		t.Fatalf("created %d models, want %d (CodexFallbackModels)", len(got), len(service.CodexFallbackModels))
	}
	gotSet := map[string]bool{}
	for _, name := range got {
		gotSet[name] = true
	}
	for _, want := range service.CodexFallbackModels {
		if !gotSet[want] {
			t.Errorf("expected codex seed model %q, got %v", want, got)
		}
	}
}

// TestSetup_ChatgptPredefinedModelsContent pins the concrete seed entries so
// accidental edits to either side are caught.
func TestSetup_ChatgptPredefinedModelsContent(t *testing.T) {
	want := []string{
		"gpt-5.1",
		"gpt-5.1-codex",
		"gpt-5.1-codex-max",
		"gpt-5.1-codex-mini",
		"gpt-5",
		"gpt-5-codex",
		"gpt-5-codex-mini",
	}
	if len(service.CodexFallbackModels) != len(want) {
		t.Fatalf("CodexFallbackModels length = %d, want %d", len(service.CodexFallbackModels), len(want))
	}
	for i, name := range want {
		if service.CodexFallbackModels[i] != name {
			t.Errorf("CodexFallbackModels[%d] = %q, want %q", i, service.CodexFallbackModels[i], name)
		}
	}
}

// TestSetup_UnknownProviderPredefinedModelsNoop keeps the existing behavior:
// providers without a predefined list (e.g. custom OpenAI) create nothing.
func TestSetup_UnknownProviderPredefinedModelsNoop(t *testing.T) {
	f := newSetupModelFixture(t)
	defer f.Close()
	ctx := context.Background()

	provider := f.seedProvider(t, "my-api", models.APITypeOpenAI)
	createPredefinedModels(ctx, f.models, f.tags, f.globals, provider, "my-api", testLogger())

	if got := f.listModels(t, provider.ID); len(got) != 0 {
		t.Errorf("expected no models for unknown provider, got %v", got)
	}
}

// TestSetup_ClaudeCodePredefinedModelsUnchanged guards the claude-code list.
func TestSetup_ClaudeCodePredefinedModelsUnchanged(t *testing.T) {
	f := newSetupModelFixture(t)
	defer f.Close()
	ctx := context.Background()

	provider := f.seedProvider(t, "claude-code", models.APITypeAnthropic)
	createPredefinedModels(ctx, f.models, f.tags, f.globals, provider, "claude-code", testLogger())

	got := f.listModels(t, provider.ID)
	want := []string{
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-sonnet-5",
		"claude-opus-4-7",
		"claude-opus-4-8",
	}
	if len(got) != len(want) {
		t.Fatalf("claude-code models = %v, want %v", got, want)
	}
	gotSet := map[string]bool{}
	for _, name := range got {
		gotSet[name] = true
	}
	for _, name := range want {
		if !gotSet[name] {
			t.Errorf("missing claude-code model %q", name)
		}
	}
}

// --- Usage / help text -----------------------------------------------------------

// TestSetup_HelpTextMentionsChatgpt keeps the OAuth section of
// printSupportedProviders accurate (stdout-captured to stay quiet).
func TestSetup_HelpTextMentionsChatgpt(t *testing.T) {
	output := captureStdout(t, func() {
		printSupportedProviders()
	})

	for _, want := range []string{
		"chatgpt",
		"ChatGPT Plus/Pro subscription (OAuth",
		"alias: codex",
		"llm-router setup --provider chatgpt",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("help text should mention %q", want)
		}
	}
}

// --- Provider creation path (setup → Create) ---------------------------------------

// TestSetup_CreateChatgptProviderMatchesConnectSeed verifies the row setup
// creates from the chatgpt entry is identical to what connect's
// ensureChatgptProvider would create (api_type codex, canonical URL, empty
// key) — the two entry points must converge on the same provider.
func TestSetup_CreateChatgptProviderMatchesConnectSeed(t *testing.T) {
	f := newSetupModelFixture(t)
	defer f.Close()
	ctx := context.Background()

	providerSvc := service.NewProviderService(f.providers, f.metadata, testEncryptionKey)
	created, err := providerSvc.Create(ctx, models.CreateProviderRequest{
		Name:    supportedProviders["chatgpt"].name,
		APIType: models.APIType(supportedProviders["chatgpt"].apiType),
		BaseURL: supportedProviders["chatgpt"].baseURL,
		APIKey:  "",
	})
	if err != nil {
		t.Fatalf("create chatgpt provider: %v", err)
	}

	seed, err := ensureChatgptProvider(ctx, f.providers, providerSvc)
	if err != nil {
		t.Fatalf("ensureChatgptProvider: %v", err)
	}
	if created.ID != seed.ID {
		t.Errorf("setup-created provider (id %d) and connect seed (id %d) differ — both must target the same canonical row", created.ID, seed.ID)
	}
	if created.APIType != models.APITypeCodex {
		t.Errorf("APIType = %q, want codex", created.APIType)
	}
}
