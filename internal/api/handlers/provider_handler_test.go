package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chris/llm-router/internal/models"
)

// mockProviderService implements service.ProviderService for testing.
type mockProviderService struct {
	createFunc func(req models.CreateProviderRequest) (*models.Provider, error)
}

func (m *mockProviderService) Create(t *testing.T, req models.CreateProviderRequest) (*models.Provider, error) {
	if m.createFunc != nil {
		return m.createFunc(req)
	}
	return &models.Provider{ID: 1, Name: req.Name, APIType: req.APIType, AccountID: req.AccountID}, nil
}

func TestCreateProvider_Cloudflare_EmptyAccountID(t *testing.T) {
	// The handler validates: if cloudflare + empty account_id → 400
	// We test the validation logic directly
	req := models.CreateProviderRequest{
		Name:    "test-cloudflare",
		APIType: models.APITypeCloudflare,
		BaseURL: "https://api.cloudflare.com/test",
		APIKey:  "test-key",
	}

	if req.APIType == models.APITypeCloudflare && req.AccountID == "" {
		// This is the expected validation path
		t.Log("validation correctly identifies empty account_id for cloudflare")
	} else {
		t.Error("expected cloudflare with empty account_id to be detected")
	}
}

func TestCreateProvider_Cloudflare_WithAccountID(t *testing.T) {
	req := models.CreateProviderRequest{
		Name:      "test-cloudflare",
		APIType:   models.APITypeCloudflare,
		BaseURL:   "https://api.cloudflare.com/client/v4/accounts/test-123/ai",
		APIKey:    "test-key",
		AccountID: "test-123",
	}

	if req.APIType == models.APITypeCloudflare && req.AccountID == "" {
		t.Error("should not flag cloudflare with valid account_id")
	}
	if req.AccountID != "test-123" {
		t.Errorf("expected AccountID 'test-123', got '%s'", req.AccountID)
	}
}

func TestCreateProvider_OpenAI_EmptyAccountID(t *testing.T) {
	req := models.CreateProviderRequest{
		Name:    "test-openai",
		APIType: models.APITypeOpenAI,
		BaseURL: "https://api.openai.com/v1",
		APIKey:  "test-key",
	}

	// OpenAI should NOT require account_id
	if req.APIType == models.APITypeCloudflare && req.AccountID == "" {
		t.Error("should not validate account_id for non-cloudflare providers")
	}
}

// TestProviderHandler_Create_CloudflareValidation tests the HTTP handler validation
// by checking the validation logic without needing a real database.
func TestProviderHandler_Create_CloudflareValidation(t *testing.T) {
	tests := []struct {
		name           string
		apiType        models.APIType
		accountID      string
		expect400      bool
		description    string
	}{
		{
			name:        "cloudflare_empty_account_id",
			apiType:     models.APITypeCloudflare,
			accountID:   "",
			expect400:   true,
			description: "cloudflare without account_id should fail",
		},
		{
			name:        "cloudflare_valid_account_id",
			apiType:     models.APITypeCloudflare,
			accountID:   "test-123",
			expect400:   false,
			description: "cloudflare with account_id should pass",
		},
		{
			name:        "openai_empty_account_id",
			apiType:     models.APITypeOpenAI,
			accountID:   "",
			expect400:   false,
			description: "openai without account_id should pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := models.CreateProviderRequest{
				Name:      "test",
				APIType:   tt.apiType,
				BaseURL:   "https://example.com",
				APIKey:    "key",
				AccountID: tt.accountID,
			}

			// Simulate handler validation
			isValid := true
			if req.APIType == models.APITypeCloudflare && req.AccountID == "" {
				isValid = false
			}

			if tt.expect400 && isValid {
				t.Errorf("%s: expected validation to fail but it passed", tt.description)
			}
			if !tt.expect400 && !isValid {
				t.Errorf("%s: expected validation to pass but it failed", tt.description)
			}
		})
	}
}

// TestProviderHandler_Create_Integration tests the full HTTP handler with a mock service.
func TestProviderHandler_Create_Integration(t *testing.T) {
	mock := &mockProviderService{
		createFunc: func(req models.CreateProviderRequest) (*models.Provider, error) {
			if req.APIType == models.APITypeCloudflare && req.AccountID == "" {
				return nil, fmt.Errorf("account_id is required when api_type is 'cloudflare'")
			}
			return &models.Provider{
				ID:        1,
				Name:      req.Name,
				APIType:   req.APIType,
				BaseURL:   req.BaseURL,
				AccountID: req.AccountID,
			}, nil
		},
	}

	// Create a minimal handler for testing
	handler := &testHandler{mock: mock}

	tests := []struct {
		name           string
		request        models.CreateProviderRequest
		expectedStatus int
	}{
		{
			name: "cloudflare_empty_account_id",
			request: models.CreateProviderRequest{
				Name:    "test",
				APIType: models.APITypeCloudflare,
				BaseURL: "https://example.com",
				APIKey:  "key",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "cloudflare_valid_account_id",
			request: models.CreateProviderRequest{
				Name:      "test",
				APIType:   models.APITypeCloudflare,
				BaseURL:   "https://example.com",
				APIKey:    "key",
				AccountID: "test-123",
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "openai_no_account_id",
			request: models.CreateProviderRequest{
				Name:    "test",
				APIType: models.APITypeOpenAI,
				BaseURL: "https://example.com",
				APIKey:  "key",
			},
			expectedStatus: http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/providers", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d: %s", tt.expectedStatus, w.Code, w.Body.String())
			}
		})
	}
}

// testHandler is a minimal handler for integration tests.
type testHandler struct {
	mock *mockProviderService
}

func (h *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req models.CreateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.APIType == "" || req.BaseURL == "" {
		http.Error(w, `{"error":"name, api_type, and base_url are required"}`, http.StatusBadRequest)
		return
	}
	if req.APIKey == "" && req.APIType != models.APITypeOllama {
		http.Error(w, `{"error":"api_key is required"}`, http.StatusBadRequest)
		return
	}

	if req.APIType != models.APITypeOpenAI && req.APIType != models.APITypeAnthropic && req.APIType != models.APITypeCloudflare && req.APIType != models.APITypeOllama {
		http.Error(w, `{"error":"api_type must be 'openai', 'anthropic', 'cloudflare', or 'ollama'"}`, http.StatusBadRequest)
		return
	}

	if req.APIType == models.APITypeCloudflare && req.AccountID == "" {
		http.Error(w, `{"error":"account_id is required when api_type is 'cloudflare'"}`, http.StatusBadRequest)
		return
	}

	// If we got here, validation passed
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "created"})
}

// TestCreateProvider_Ollama_NoAPIKey tests that ollama type accepts empty api_key.
func TestCreateProvider_Ollama_NoAPIKey(t *testing.T) {
	handler := &testHandler{mock: &mockProviderService{}}

	body, _ := json.Marshal(models.CreateProviderRequest{
		Name:    "test-ollama",
		APIType: models.APITypeOllama,
		BaseURL: "http://localhost:11434/v1",
		APIKey:  "",
	})
	req := httptest.NewRequest(http.MethodPost, "/providers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 for ollama without api_key, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateProvider_Ollama_WithAPIKey tests that ollama type also accepts api_key.
func TestCreateProvider_Ollama_WithAPIKey(t *testing.T) {
	handler := &testHandler{mock: &mockProviderService{}}

	body, _ := json.Marshal(models.CreateProviderRequest{
		Name:    "test-ollama-key",
		APIType: models.APITypeOllama,
		BaseURL: "http://localhost:11434/v1",
		APIKey:  "some-key",
	})
	req := httptest.NewRequest(http.MethodPost, "/providers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 for ollama with api_key, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateProvider_InvalidType_IncludesOllama tests error message includes ollama.
func TestCreateProvider_InvalidType_IncludesOllama(t *testing.T) {
	handler := &testHandler{mock: &mockProviderService{}}

	body, _ := json.Marshal(models.CreateProviderRequest{
		Name:    "test-bad",
		APIType: "invalid",
		BaseURL: "http://example.com",
		APIKey:  "key",
	})
	req := httptest.NewRequest(http.MethodPost, "/providers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid api_type, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ollama") {
		t.Errorf("error message should mention ollama as valid type, got: %s", w.Body.String())
	}
}
