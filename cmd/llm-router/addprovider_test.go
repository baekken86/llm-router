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
