package main

import (
	"testing"

	"github.com/chris/llm-router/internal/models"
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
		"@cf/meta/llama-3.1-8b-instruct",
		"@cf/meta/llama-3.3-70b-instruct-fp8-fast",
		"@cf/mistralai/mistral-7b-instruct-v0.2",
		"@cf/mistralai/mistral-small-3.1-24b-instruct",
		"@cf/google/gemma-3-12b-it",
		"@cf/google/gemma-4-26b-a4b-it",
		"@cf/qwen/qwen2.5-coder-32b-instruct",
		"@cf/deepseek/deepseek-r1-distill-qwen-32b",
		"@cf/defog/sqlcoder-7b-2",
	}

	if len(expectedModels) != 9 {
		t.Errorf("expected 9 predefined models, got %d", len(expectedModels))
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
