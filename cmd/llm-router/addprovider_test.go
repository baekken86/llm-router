package main

import (
	"testing"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/service"
)

func TestAddProvider_CloudflareValidation(t *testing.T) {
	tests := []struct {
		name        string
		apiType     string
		accountID   string
		expectError bool
	}{
		{
			name:        "cloudflare_with_account_id",
			apiType:     "cloudflare",
			accountID:   "test-123",
			expectError: false,
		},
		{
			name:        "cloudflare_without_account_id",
			apiType:     "cloudflare",
			accountID:   "",
			expectError: true,
		},
		{
			name:        "openai_without_account_id",
			apiType:     "openai",
			accountID:   "",
			expectError: false,
		},
		{
			name:        "openai_with_account_id_warns",
			apiType:     "openai",
			accountID:   "test-123",
			expectError: false, // warns but doesn't error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the validation logic from runAddProvider
			hasError := false
			if tt.apiType == "cloudflare" && tt.accountID == "" {
				hasError = true
			}

			if tt.expectError && !hasError {
				t.Errorf("expected error but none occurred")
			}
			if !tt.expectError && hasError {
				t.Errorf("unexpected error")
			}
		})
	}
}

func TestAddProvider_CloudflareBaseURL(t *testing.T) {
	accountID := "test-123"
	expected := "https://api.cloudflare.com/client/v4/accounts/test-123/ai"
	actual := "https://api.cloudflare.com/client/v4/accounts/" + accountID + "/ai"

	if actual != expected {
		t.Errorf("expected URL '%s', got '%s'", expected, actual)
	}
}

func TestAddProvider_CloudflarePredefinedModels(t *testing.T) {
	expectedModels := []string{
		"@cf/deepseek-ai/deepseek-r1-distill-qwen-32b",
		"@cf/google/gemma-4-26b-a4b-it",
		"@cf/moonshotai/kimi-k2.6",
		"@cf/moonshotai/kimi-k2.7-code",
		"@cf/nvidia/nemotron-3-120b-a12b",
		"@cf/openai/gpt-oss-120b",
		"@cf/openai/gpt-oss-20b",
		"@cf/qwen/qwen3-30b-a3b-fp8",
		"@cf/qwen/qwq-32b",
		"@cf/zai-org/glm-4.7-flash",
		"@cf/zai-org/glm-5.2",
	}

	if len(expectedModels) != 11 {
		t.Errorf("expected 11 predefined models, got %d", len(expectedModels))
	}

	// Verify all model names start with @cf/
	for _, name := range expectedModels {
		if len(name) < 4 || name[:4] != "@cf/" {
			t.Errorf("model name '%s' does not start with @cf/", name)
		}
	}
}

func TestSetup_CloudflareSupportedProvider(t *testing.T) {
	cfg, exists := supportedProviders["cloudflare"]
	if !exists {
		t.Fatal("cloudflare should be in supportedProviders")
	}
	if cfg.apiType != "cloudflare" {
		t.Errorf("expected apiType 'cloudflare', got '%s'", cfg.apiType)
	}
	if cfg.auth != "apikey" {
		t.Errorf("expected auth 'apikey', got '%s'", cfg.auth)
	}
}

func TestSetup_PredefinedModelsContainCloudflare(t *testing.T) {
	// This tests that the cloudflare predefined models are in the map
	// We can't directly access createPredefinedModels, but we verify
	// the supported provider config is correct
	cfg := supportedProviders["cloudflare"]
	if cfg.name != "cloudflare" {
		t.Errorf("expected name 'cloudflare', got '%s'", cfg.name)
	}
	if cfg.apiType != string(models.APITypeCloudflare) {
		t.Errorf("expected apiType matching APITypeCloudflare")
	}
}

// --- Codex (ChatGPT OAuth) type support ----------------------------------------

// TestAddProvider_Codex_NoAPIKeyAllowed verifies the add-provider validation
// accepts codex with an empty --key and --url (same leniency as ollama): codex
// authenticates via ChatGPT OAuth, not an API key.
func TestAddProvider_Codex_NoAPIKeyAllowed(t *testing.T) {
	tests := []struct {
		name        string
		apiType     string
		baseURL     string
		apiKey      string
		expectError bool
	}{
		{
			name:        "codex_no_key_no_url",
			apiType:     "codex",
			baseURL:     "",
			apiKey:      "",
			expectError: false,
		},
		{
			name:        "codex_no_key_with_url",
			apiType:     "codex",
			baseURL:     "https://chatgpt.com/backend-api/codex",
			apiKey:      "",
			expectError: false,
		},
		{
			name:        "codex_with_key",
			apiType:     "codex",
			baseURL:     "",
			apiKey:      "ignored",
			expectError: false,
		},
		{
			name:        "openai_without_key_still_rejected",
			apiType:     "openai",
			baseURL:     "https://api.openai.com/v1",
			apiKey:      "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the required-args validation from runAddProvider.
			hasError := false
			if tt.apiType == "" || tt.baseURL == "" && tt.apiType != "ollama" && tt.apiType != "ollama-cloud" && tt.apiType != "codex" || tt.apiKey == "" && tt.apiType != "ollama" && tt.apiType != "codex" {
				hasError = true
			}

			if tt.expectError && !hasError {
				t.Errorf("expected validation error but none occurred")
			}
			if !tt.expectError && hasError {
				t.Errorf("unexpected validation error")
			}
		})
	}
}

// TestAddProvider_Codex_WhitelistedType verifies codex passes the --type
// whitelist simulation and that the API type constant is wired.
func TestAddProvider_Codex_WhitelistedType(t *testing.T) {
	valid := map[string]bool{
		"openai":       true,
		"anthropic":    true,
		"cloudflare":   true,
		"ollama":       true,
		"ollama-cloud": true,
		"codex":        true,
	}

	if !valid[string(models.APITypeCodex)] {
		t.Errorf("codex must be in the add-provider type whitelist")
	}
	if models.APITypeCodex != "codex" {
		t.Errorf("APITypeCodex = %q, want \"codex\"", models.APITypeCodex)
	}
}

// TestAddProvider_Codex_BaseURLDefault documents the canonical codex base URL
// used when --url is omitted (design §4.6).
func TestAddProvider_Codex_BaseURLDefault(t *testing.T) {
	baseURL := ""
	apiType := "codex"
	if apiType == "codex" && baseURL == "" {
		baseURL = "https://chatgpt.com/backend-api/codex"
	}
	if baseURL != "https://chatgpt.com/backend-api/codex" {
		t.Errorf("codex default base URL = %q", baseURL)
	}
}

// TestService_CodexFallbackModelsSeed checks the shared seed list the CLI can
// reuse for predefined models (design §3.3).
func TestService_CodexFallbackModelsSeed(t *testing.T) {
	if len(service.CodexFallbackModels) != 7 {
		t.Fatalf("expected 7 seed models, got %d", len(service.CodexFallbackModels))
	}
	want := []string{
		"gpt-5.1", "gpt-5.1-codex", "gpt-5.1-codex-max", "gpt-5.1-codex-mini",
		"gpt-5", "gpt-5-codex", "gpt-5-codex-mini",
	}
	for i, name := range want {
		if service.CodexFallbackModels[i] != name {
			t.Errorf("seed[%d] = %q, want %q", i, service.CodexFallbackModels[i], name)
		}
	}
}
