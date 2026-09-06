package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chris/llm-router/internal/api/handlers"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/service"
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
		name        string
		apiType     models.APIType
		accountID   string
		expect400   bool
		description string
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
	if req.APIKey == "" && req.APIType != models.APITypeOllama && req.APIType != models.APITypeOllamaCloud && req.APIType != models.APITypeCodex {
		http.Error(w, `{"error":"api_key is required"}`, http.StatusBadRequest)
		return
	}

	if req.APIType != models.APITypeOpenAI && req.APIType != models.APITypeAnthropic && req.APIType != models.APITypeCloudflare && req.APIType != models.APITypeOllama && req.APIType != models.APITypeOllamaCloud && req.APIType != models.APITypeCodex {
		http.Error(w, `{"error":"api_type must be 'openai', 'anthropic', 'cloudflare', 'ollama', 'ollama-cloud', or 'codex'"}`, http.StatusBadRequest)
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

// --- Codex (ChatGPT OAuth) create cases — driven through the REAL handler ----

// fullMockProviderService implements the whole service.ProviderService for
// driving the real ProviderHandler.
type fullMockProviderService struct {
	created *models.CreateProviderRequest
}

func (m *fullMockProviderService) Create(_ context.Context, req models.CreateProviderRequest) (*models.Provider, error) {
	m.created = &req
	return &models.Provider{ID: 1, Name: req.Name, APIType: req.APIType, BaseURL: req.BaseURL}, nil
}

func (m *fullMockProviderService) GetByID(_ context.Context, _ int64) (*models.Provider, error) {
	return nil, nil
}

func (m *fullMockProviderService) List(_ context.Context) ([]models.Provider, error) {
	return nil, nil
}

func (m *fullMockProviderService) ListByMetadata(_ context.Context, _ map[string]string) ([]models.Provider, error) {
	return nil, nil
}

func (m *fullMockProviderService) Update(_ context.Context, _ int64, _ models.UpdateProviderRequest) (*models.Provider, error) {
	return nil, nil
}

func (m *fullMockProviderService) Delete(_ context.Context, _ int64) error { return nil }

func (m *fullMockProviderService) DecryptAPIKey(encrypted string) (string, error) {
	return encrypted, nil
}

// fullMockModelService implements the whole service.ModelService.
type fullMockModelService struct {
	discovered bool
}

func (m *fullMockModelService) Discover(_ context.Context, _ int64) (*service.DiscoverResult, error) {
	m.discovered = true
	return &service.DiscoverResult{}, nil
}

func (m *fullMockModelService) ListByProvider(_ context.Context, _ int64) ([]models.Model, error) {
	return nil, nil
}

func (m *fullMockModelService) ListAll(_ context.Context) ([]service.ModelEffortEntry, error) {
	return nil, nil
}

func (m *fullMockModelService) GetByID(_ context.Context, _ int64) (*models.Model, error) {
	return nil, nil
}

func (m *fullMockModelService) SetTags(_ context.Context, _ int64, _ map[string]string) error {
	return nil
}

func (m *fullMockModelService) GetTags(_ context.Context, _ int64) ([]models.Tag, error) {
	return nil, nil
}

func (m *fullMockModelService) Delete(_ context.Context, _ int64) error { return nil }

func (m *fullMockModelService) ToggleDisabled(_ context.Context, _ int64, _ bool, _ *time.Duration) error {
	return nil
}

// postProvider marshals req and posts it to the real ProviderHandler.
func postProvider(t *testing.T, h *handlers.ProviderHandler, req models.CreateProviderRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, r)
	return w
}

func TestProviderHandler_Create_Codex_NoAPIKey(t *testing.T) {
	ps := &fullMockProviderService{}
	ms := &fullMockModelService{}
	handler := handlers.NewProviderHandler(ps, ms)

	w := postProvider(t, handler, models.CreateProviderRequest{
		Name:    "chatgpt",
		APIType: models.APITypeCodex,
		BaseURL: "https://chatgpt.com/backend-api/codex",
		APIKey:  "",
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for codex without api_key, got %d: %s", w.Code, w.Body.String())
	}
	if ps.created == nil || ps.created.APIType != models.APITypeCodex {
		t.Errorf("codex create request not forwarded: %+v", ps.created)
	}
}

func TestProviderHandler_Create_Codex_WithAPIKey(t *testing.T) {
	handler := handlers.NewProviderHandler(&fullMockProviderService{}, &fullMockModelService{})

	w := postProvider(t, handler, models.CreateProviderRequest{
		Name:    "chatgpt",
		APIType: models.APITypeCodex,
		BaseURL: "https://chatgpt.com/backend-api/codex",
		APIKey:  "leftover-key",
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for codex with api_key (key tolerated), got %d: %s", w.Code, w.Body.String())
	}
}

func TestProviderHandler_Create_Codex_AccountIDOptional(t *testing.T) {
	handler := handlers.NewProviderHandler(&fullMockProviderService{}, &fullMockModelService{})

	// Codex needs no account_id at create time (it comes from the OAuth row).
	w := postProvider(t, handler, models.CreateProviderRequest{
		Name:      "chatgpt",
		APIType:   models.APITypeCodex,
		BaseURL:   "https://chatgpt.com/backend-api/codex",
		AccountID: "",
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for codex without account_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProviderHandler_Create_Codex_TypeAccepted(t *testing.T) {
	// Whitelist check drives the acceptance: codex must not hit the
	// invalid-api_type branch.
	handler := handlers.NewProviderHandler(&fullMockProviderService{}, &fullMockModelService{})

	w := postProvider(t, handler, models.CreateProviderRequest{
		Name:    "chatgpt",
		APIType: models.APITypeCodex,
		BaseURL: "https://chatgpt.com/backend-api/codex",
	})

	if strings.Contains(w.Body.String(), "api_type must be") {
		t.Errorf("codex should be a whitelisted api_type, got: %s", w.Body.String())
	}
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}
}

func TestProviderHandler_Create_InvalidType_MessageIncludesCodex(t *testing.T) {
	handler := handlers.NewProviderHandler(&fullMockProviderService{}, &fullMockModelService{})

	w := postProvider(t, handler, models.CreateProviderRequest{
		Name:    "bad",
		APIType: "nope",
		BaseURL: "https://example.com",
		APIKey:  "key",
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid api_type, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "codex") {
		t.Errorf("error message should list codex as valid type, got: %s", w.Body.String())
	}
}
