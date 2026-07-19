package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/service"
)

func TestOpenAIToAnthropic(t *testing.T) {
	maxTokens := 1024
	temp := 0.7

	req := ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
		MaxTokens:   &maxTokens,
		Temperature: &temp,
	}

	anthReq := OpenAIToAnthropic(req)

	if anthReq.Model != "gpt-4" {
		t.Errorf("expected model gpt-4, got %s", anthReq.Model)
	}
	if anthReq.MaxTokens != 1024 {
		t.Errorf("expected max_tokens 1024, got %d", anthReq.MaxTokens)
	}
	if anthReq.System != "You are helpful." {
		t.Errorf("expected system message, got %s", anthReq.System)
	}
	if len(anthReq.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(anthReq.Messages))
	}
	if anthReq.Messages[0].Role != "user" {
		t.Errorf("expected role user, got %s", anthReq.Messages[0].Role)
	}
}

func TestAnthropicToOpenAI(t *testing.T) {
	resp := &AnthropicResponse{
		ID:   "msg_123",
		Type: "message",
		Role: "assistant",
		Content: []AnthropicContent{
			{Type: "text", Text: "Hello there!"},
		},
		Model:      "claude-3-sonnet",
		StopReason: "end_turn",
		Usage: AnthropicUsage{
			InputTokens:  100,
			OutputTokens: 50,
			CacheReadInputTokens: 20,
		},
	}

	openResp := AnthropicToOpenAI(resp, "claude-3-sonnet")

	if openResp.ID != "msg_123" {
		t.Errorf("expected ID msg_123, got %s", openResp.ID)
	}
	if len(openResp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(openResp.Choices))
	}
	if openResp.Choices[0].Message.Content != "Hello there!" {
		t.Errorf("expected content 'Hello there!', got %v", openResp.Choices[0].Message.Content)
	}
	if openResp.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason stop, got %s", openResp.Choices[0].FinishReason)
	}
	if openResp.Usage.PromptTokens != 100 {
		t.Errorf("expected prompt_tokens 100, got %d", openResp.Usage.PromptTokens)
	}
	if openResp.Usage.CompletionTokens != 50 {
		t.Errorf("expected completion_tokens 50, got %d", openResp.Usage.CompletionTokens)
	}
	if openResp.Usage.PromptTokensDetails.CachedTokens != 20 {
		t.Errorf("expected cached_tokens 20, got %d", openResp.Usage.PromptTokensDetails.CachedTokens)
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"hello", 1},
		{"this is a test", 3},
		{"", 0},
	}

	for _, tt := range tests {
		result := EstimateTokens(tt.input)
		if result != tt.expected {
			t.Errorf("EstimateTokens(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestCalculateHeadroom(t *testing.T) {
	headroom := CalculateHeadroom(128000, 1000, 4096)
	if headroom != 122904 {
		t.Errorf("expected headroom 122904, got %d", headroom)
	}
}

func TestEstimateCost(t *testing.T) {
	cost := EstimateCost(1000, 500, 200, 2.50, 10.00, 1.25)
	expected := (800.0/1000.0*2.50) + (200.0/1000.0*1.25) + (500.0/1000.0*10.00)
	if cost != expected {
		t.Errorf("expected cost %f, got %f", expected, cost)
	}
}

func TestMapStopReason(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"end_turn", "stop"},
		{"max_tokens", "length"},
		{"tool_use", "tool_calls"},
		{"stop_sequence", "stop"},
		{"unknown", "stop"},
	}

	for _, tt := range tests {
		result := mapStopReason(tt.input)
		if result != tt.expected {
			t.Errorf("mapStopReason(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		status   int
		codes    []int
		expected bool
	}{
		{429, nil, true},
		{500, nil, true},
		{200, nil, false},
		{400, nil, false},
		{429, []int{429, 500}, true},
		{500, []int{429, 500}, true},
		{503, []int{429, 500}, false},
	}

	for _, tt := range tests {
		result := shouldRetry(tt.status, tt.codes)
		if result != tt.expected {
			t.Errorf("shouldRetry(%d, %v) = %v, want %v", tt.status, tt.codes, result, tt.expected)
		}
	}
}

// --- Mock service implementations for streaming handler tests ---

type mockVMService struct {
	getByNameFn func(ctx context.Context, name string) (*models.VirtualModel, error)
	resolveFn   func(ctx context.Context, vm *models.VirtualModel) ([]service.ResolvedModel, error)
}

func (m *mockVMService) GetByName(ctx context.Context, name string) (*models.VirtualModel, error) {
	return m.getByNameFn(ctx, name)
}

func (m *mockVMService) ResolveModels(ctx context.Context, vm *models.VirtualModel) ([]service.ResolvedModel, error) {
	return m.resolveFn(ctx, vm)
}

func (m *mockVMService) Create(_ context.Context, _ models.CreateVirtualModelRequest) (*models.VirtualModel, error) {
	panic("not used")
}
func (m *mockVMService) GetByID(_ context.Context, _ int64) (*models.VirtualModel, error) {
	panic("not used")
}
func (m *mockVMService) List(_ context.Context) ([]models.VirtualModel, error) {
	panic("not used")
}
func (m *mockVMService) Update(_ context.Context, _ int64, _ models.UpdateVirtualModelRequest) (*models.VirtualModel, error) {
	panic("not used")
}
func (m *mockVMService) Delete(_ context.Context, _ int64) error {
	panic("not used")
}
func (m *mockVMService) PreviewResolve(_ context.Context, _ json.RawMessage, _ json.RawMessage, _ json.RawMessage, _ *models.CompositionNode) ([]service.ResolvedModel, error) {
	panic("not used")
}
func (m *mockVMService) GetDependencies(_ context.Context) (map[string][]string, error) {
	panic("not used")
}

type mockProviderService struct {
	decryptKeyFn func(encrypted string) (string, error)
}

func (m *mockProviderService) DecryptAPIKey(encrypted string) (string, error) {
	return m.decryptKeyFn(encrypted)
}

func (m *mockProviderService) Create(_ context.Context, _ models.CreateProviderRequest) (*models.Provider, error) {
	panic("not used")
}
func (m *mockProviderService) GetByID(_ context.Context, _ int64) (*models.Provider, error) {
	panic("not used")
}
func (m *mockProviderService) List(_ context.Context) ([]models.Provider, error) {
	panic("not used")
}
func (m *mockProviderService) ListByMetadata(_ context.Context, _ map[string]string) ([]models.Provider, error) {
	panic("not used")
}
func (m *mockProviderService) Update(_ context.Context, _ int64, _ models.UpdateProviderRequest) (*models.Provider, error) {
	panic("not used")
}
func (m *mockProviderService) Delete(_ context.Context, _ int64) error {
	panic("not used")
}

type mockOAuthService struct {
	getTokenFn func(ctx context.Context, providerID int64) (string, error)
}

func (m *mockOAuthService) GetValidToken(ctx context.Context, providerID int64) (string, error) {
	return m.getTokenFn(ctx, providerID)
}

func (m *mockOAuthService) StartAuthFlow(_ context.Context, _ int64) (string, string, error) {
	panic("not used")
}
func (m *mockOAuthService) StartAuthFlowWithCallback(_ context.Context, _ int64, _ string) (string, string, error) {
	panic("not used")
}
func (m *mockOAuthService) HandleCallback(_ context.Context, _ string, _ string) (*models.OAuthToken, error) {
	panic("not used")
}
func (m *mockOAuthService) Disconnect(_ context.Context, _ int64) error {
	panic("not used")
}
func (m *mockOAuthService) IsConnected(_ context.Context, _ int64) (bool, *models.OAuthToken, error) {
	panic("not used")
}
func (m *mockOAuthService) StartCallbackServer(_ context.Context, _ string, _ int64) (*models.OAuthToken, error) {
	panic("not used")
}
func (m *mockOAuthService) StartCallbackServerOnAddr(_ context.Context, _ string, _ int64, _ string) (*models.OAuthToken, error) {
	panic("not used")
}

// --- Test helpers ---

// newTestEngine creates an Engine with mocks and a buffered log channel.
func newTestEngine(vmSvc service.VirtualModelService, provSvc service.ProviderService, oauthSvc service.OAuthService) (*Engine, chan RequestLog) {
	logChan := make(chan RequestLog, 20)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := NewEngine(vmSvc, provSvc, oauthSvc, logger, logChan)
	e.ApplySettings(0, 300, 8192) // maxRetries=0 to avoid sleep delays
	return e, logChan
}

// drainLogs reads all pending logs from the channel with a short timeout.
func drainLogs(t *testing.T, ch chan RequestLog) []RequestLog {
	t.Helper()
	var logs []RequestLog
	timeout := time.After(2 * time.Second)
	for {
		select {
		case log := <-ch:
			logs = append(logs, log)
		case <-timeout:
			return logs
		}
		// Non-blocking: if channel is empty and we got at least one, check once more
		if len(logs) > 0 {
			select {
			case log := <-ch:
				logs = append(logs, log)
			default:
				return logs
			}
		}
	}
}

// failingServer returns an httptest server that always returns the given status code.
func failingServer(t *testing.T, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(statusCode)
		w.Write([]byte(`{"error":"server error"}`))
	}))
}

// --- Streaming handler tests: PR #5 ---

func TestHandleChatCompletionStream_AllProvidersFailed_LogsProxyEntry(t *testing.T) {
	// Set up a failing HTTP server (OpenAI type)
	ts := failingServer(t, http.StatusInternalServerError)
	defer ts.Close()

	vmName := "test-vm"
物理Model := "gpt-4-test"

	vmSvc := &mockVMService{
		getByNameFn: func(_ context.Context, name string) (*models.VirtualModel, error) {
			if name != vmName {
				return nil, nil
			}
			return &models.VirtualModel{
				ID:         1,
				Name:       vmName,
				MaxRetries: 0,
			}, nil
		},
		resolveFn: func(_ context.Context, _ *models.VirtualModel) ([]service.ResolvedModel, error) {
			return []service.ResolvedModel{
				{
					Model:    models.Model{ID: 1, ProviderID: 1, Name: 物理Model},
					Provider: models.Provider{ID: 1, Name: "openai-test", APIType: models.APITypeOpenAI, BaseURL: ts.URL},
				},
			}, nil
		},
	}

	provSvc := &mockProviderService{
		decryptKeyFn: func(_ string) (string, error) { return "test-api-key", nil },
	}

	oauthSvc := &mockOAuthService{
		getTokenFn: func(_ context.Context, _ int64) (string, error) { return "", errors.New("no token") },
	}

	engine, logChan := newTestEngine(vmSvc, provSvc, oauthSvc)

	// Build request
	body := `{"model":"test-vm","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.HandleChatCompletionStream(w, req)

	logs := drainLogs(t, logChan)

	// Must have at least incoming + proxy entries
	if len(logs) < 2 {
		t.Fatalf("expected at least 2 log entries, got %d", len(logs))
	}

	// First entry: incoming
	if logs[0].Type != "incoming" {
		t.Errorf("first log type = %q, want %q", logs[0].Type, "incoming")
	}
	if logs[0].VirtualModel != vmName {
		t.Errorf("incoming log VirtualModel = %q, want %q", logs[0].VirtualModel, vmName)
	}

	// Last entry: proxy (all models failed)
	proxyLog := logs[len(logs)-1]
	if proxyLog.Type != "proxy" {
		t.Errorf("last log type = %q, want %q", proxyLog.Type, "proxy")
	}
	if proxyLog.StatusCode != http.StatusBadGateway {
		t.Errorf("proxy log StatusCode = %d, want %d", proxyLog.StatusCode, http.StatusBadGateway)
	}
	if proxyLog.ErrorMessage != "all models failed" {
		t.Errorf("proxy log ErrorMessage = %q, want %q", proxyLog.ErrorMessage, "all models failed")
	}
	if proxyLog.VirtualModel != vmName {
		t.Errorf("proxy log VirtualModel = %q, want %q (must not be mutated to physical model)", proxyLog.VirtualModel, vmName)
	}
	if proxyLog.Latency <= 0 {
		t.Errorf("proxy log Latency = %v, want > 0", proxyLog.Latency)
	}

	// HTTP response must be 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("HTTP status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestHandleAnthropicMessagesStream_AllProvidersFailed_LogsProxyEntry(t *testing.T) {
	// Set up a failing HTTP server (Anthropic type)
	ts := failingServer(t, http.StatusInternalServerError)
	defer ts.Close()

	vmName := "test-anthropic-vm"
	物理Model1 := "claude-3-opus"
	物理Model2 := "claude-3-sonnet"

	vmSvc := &mockVMService{
		getByNameFn: func(_ context.Context, name string) (*models.VirtualModel, error) {
			if name != vmName {
				return nil, nil
			}
			return &models.VirtualModel{
				ID:         2,
				Name:       vmName,
				MaxRetries: 0,
			}, nil
		},
		resolveFn: func(_ context.Context, _ *models.VirtualModel) ([]service.ResolvedModel, error) {
			return []service.ResolvedModel{
				{
					Model:    models.Model{ID: 1, ProviderID: 1, Name: 物理Model1},
					Provider: models.Provider{ID: 1, Name: "anthropic-1", APIType: models.APITypeAnthropic, BaseURL: ts.URL},
				},
				{
					Model:    models.Model{ID: 2, ProviderID: 2, Name: 物理Model2},
					Provider: models.Provider{ID: 2, Name: "anthropic-2", APIType: models.APITypeAnthropic, BaseURL: ts.URL},
				},
			}, nil
		},
	}

	provSvc := &mockProviderService{
		decryptKeyFn: func(_ string) (string, error) { return "test-api-key", nil },
	}

	oauthSvc := &mockOAuthService{
		getTokenFn: func(_ context.Context, _ int64) (string, error) { return "", errors.New("no token") },
	}

	engine, logChan := newTestEngine(vmSvc, provSvc, oauthSvc)

	// Build Anthropic-format request
	body := `{"model":"test-anthropic-vm","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.HandleAnthropicMessagesStream(w, req)

	logs := drainLogs(t, logChan)

	if len(logs) < 2 {
		t.Fatalf("expected at least 2 log entries, got %d", len(logs))
	}

	// First entry: incoming
	if logs[0].Type != "incoming" {
		t.Errorf("first log type = %q, want %q", logs[0].Type, "incoming")
	}
	if logs[0].VirtualModel != vmName {
		t.Errorf("incoming log VirtualModel = %q, want %q", logs[0].VirtualModel, vmName)
	}

	// CRITICAL: verify anthReq.Model mutation fix.
	// Without the fix, anthReq.Model would be mutated to the last physical model name
	// inside the loop. The fix captures virtualModel before the loop.
	proxyLog := logs[len(logs)-1]
	if proxyLog.Type != "proxy" {
		t.Errorf("last log type = %q, want %q", proxyLog.Type, "proxy")
	}
	if proxyLog.StatusCode != http.StatusBadGateway {
		t.Errorf("proxy log StatusCode = %d, want %d", proxyLog.StatusCode, http.StatusBadGateway)
	}
	if proxyLog.ErrorMessage != "all models failed" {
		t.Errorf("proxy log ErrorMessage = %q, want %q", proxyLog.ErrorMessage, "all models failed")
	}
	// This is the key assertion: VirtualModel must be the VM name, not the physical model.
	// The mutation anthReq.Model = rm.Model.Name overwrites the original.
	if proxyLog.VirtualModel != vmName {
		t.Errorf("proxy log VirtualModel = %q, want %q — anthReq.Model mutation not captured before loop", proxyLog.VirtualModel, vmName)
	}
	if proxyLog.Latency <= 0 {
		t.Errorf("proxy log Latency = %v, want > 0", proxyLog.Latency)
	}

	if w.Code != http.StatusBadGateway {
		t.Errorf("HTTP status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestHandleChatCompletionStream_AllProvidersFailed_ApiKeySkipped(t *testing.T) {
	// All providers fail on API key retrieval — simpler path, no HTTP calls made.
	vmName := "test-vm"

	vmSvc := &mockVMService{
		getByNameFn: func(_ context.Context, name string) (*models.VirtualModel, error) {
			if name != vmName {
				return nil, nil
			}
			return &models.VirtualModel{
				ID:         3,
				Name:       vmName,
				MaxRetries: 0,
			}, nil
		},
		resolveFn: func(_ context.Context, _ *models.VirtualModel) ([]service.ResolvedModel, error) {
			return []service.ResolvedModel{
				{
					Model:    models.Model{ID: 1, ProviderID: 1, Name: "gpt-4"},
					Provider: models.Provider{ID: 1, Name: "openai-nokey", APIType: models.APITypeOpenAI, BaseURL: "http://unused"},
				},
			}, nil
		},
	}

	provSvc := &mockProviderService{
		decryptKeyFn: func(_ string) (string, error) {
			return "", errors.New("decrypt failed")
		},
	}

	oauthSvc := &mockOAuthService{
		getTokenFn: func(_ context.Context, _ int64) (string, error) { return "", errors.New("no token") },
	}

	engine, logChan := newTestEngine(vmSvc, provSvc, oauthSvc)

	body := `{"model":"test-vm","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.HandleChatCompletionStream(w, req)

	logs := drainLogs(t, logChan)

	if len(logs) < 2 {
		t.Fatalf("expected at least 2 log entries, got %d", len(logs))
	}

	// Verify proxy all-failed log exists
	proxyLog := logs[len(logs)-1]
	if proxyLog.Type != "proxy" {
		t.Errorf("last log type = %q, want %q", proxyLog.Type, "proxy")
	}
	if proxyLog.StatusCode != http.StatusBadGateway {
		t.Errorf("proxy log StatusCode = %d, want %d", proxyLog.StatusCode, http.StatusBadGateway)
	}
	if proxyLog.ErrorMessage != "all models failed" {
		t.Errorf("proxy log ErrorMessage = %q, want %q", proxyLog.ErrorMessage, "all models failed")
	}
	if proxyLog.VirtualModel != vmName {
		t.Errorf("proxy log VirtualModel = %q, want %q", proxyLog.VirtualModel, vmName)
	}
}

func TestHandleAnthropicMessagesStream_AllProvidersFailed_ApiKeySkipped(t *testing.T) {
	vmName := "test-anth-vm"

	vmSvc := &mockVMService{
		getByNameFn: func(_ context.Context, name string) (*models.VirtualModel, error) {
			if name != vmName {
				return nil, nil
			}
			return &models.VirtualModel{
				ID:         4,
				Name:       vmName,
				MaxRetries: 0,
			}, nil
		},
		resolveFn: func(_ context.Context, _ *models.VirtualModel) ([]service.ResolvedModel, error) {
			return []service.ResolvedModel{
				{
					Model:    models.Model{ID: 1, ProviderID: 1, Name: "claude-3-haiku"},
					Provider: models.Provider{ID: 1, Name: "anth-nokey", APIType: models.APITypeAnthropic, BaseURL: "http://unused"},
				},
			}, nil
		},
	}

	provSvc := &mockProviderService{
		decryptKeyFn: func(_ string) (string, error) {
			return "", errors.New("no key")
		},
	}

	oauthSvc := &mockOAuthService{
		getTokenFn: func(_ context.Context, _ int64) (string, error) { return "", errors.New("no token") },
	}

	engine, logChan := newTestEngine(vmSvc, provSvc, oauthSvc)

	body := `{"model":"test-anth-vm","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.HandleAnthropicMessagesStream(w, req)

	logs := drainLogs(t, logChan)

	if len(logs) < 2 {
		t.Fatalf("expected at least 2 log entries, got %d", len(logs))
	}

	proxyLog := logs[len(logs)-1]
	if proxyLog.Type != "proxy" {
		t.Errorf("last log type = %q, want %q", proxyLog.Type, "proxy")
	}
	if proxyLog.StatusCode != http.StatusBadGateway {
		t.Errorf("proxy log StatusCode = %d, want %d", proxyLog.StatusCode, http.StatusBadGateway)
	}
	if proxyLog.ErrorMessage != "all models failed" {
		t.Errorf("proxy log ErrorMessage = %q, want %q", proxyLog.ErrorMessage, "all models failed")
	}
	if proxyLog.VirtualModel != vmName {
		t.Errorf("proxy log VirtualModel = %q, want %q", proxyLog.VirtualModel, vmName)
	}
}

func TestSendRequest_Cloudflare_ExplicitBranch(t *testing.T) {
	// Mock test server that records request details
	var gotPath, gotMethod, gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"cf-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer ts.Close()

	engine, _ := newTestEngine(&mockVMService{}, &mockProviderService{}, &mockOAuthService{})

	rm := service.ResolvedModel{
		Model:    models.Model{ID: 1, ProviderID: 1, Name: "@cf/meta/llama-3-8b-instruct"},
		Provider: models.Provider{ID: 1, Name: "cloudflare", APIType: models.APITypeCloudflare, BaseURL: ts.URL},
	}

	chatReq := ChatCompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	}

	resp, result, err := engine.sendRequest(nil, rm, "test-key", chatReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert request hit the OpenAI-compatible endpoint via cloudflare branch
	if gotPath != "/v1/chat/completions" {
		t.Errorf("request path = %q, want /v1/chat/completions", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("request method = %q, want POST", gotMethod)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth header = %q, want %q", gotAuth, "Bearer test-key")
	}

	// Assert response parsed correctly
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestSendRequest_Ollama_ExplicitBranch(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"ollama-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer ts.Close()

	engine, _ := newTestEngine(&mockVMService{}, &mockProviderService{}, &mockOAuthService{})

	rm := service.ResolvedModel{
		Model:    models.Model{ID: 1, ProviderID: 1, Name: "llama3.2:latest"},
		Provider: models.Provider{ID: 1, Name: "ollama-local", APIType: models.APITypeOllama, BaseURL: ts.URL},
	}

	chatReq := ChatCompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	}

	resp, result, err := engine.sendRequest(nil, rm, "", chatReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert request hit the OpenAI-compatible endpoint via ollama branch
	if gotPath != "/v1/chat/completions" {
		t.Errorf("request path = %q, want /v1/chat/completions", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("request method = %q, want POST", gotMethod)
	}
	// openaiClient sends Bearer + apiKey; trailing space trimmed by http header canonicalization
	if !strings.HasPrefix(gotAuth, "Bearer") {
		t.Errorf("auth header = %q, want Bearer prefix", gotAuth)
	}

	// Assert response parsed correctly
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestSendStreamRequest_Ollama_ExplicitBranch(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {\"id\":\"ollama-s1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprintf(w, "data: {\"id\":\"ollama-s2\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprintf(w, "data: {\"id\":\"ollama-s3\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer ts.Close()

	engine, _ := newTestEngine(&mockVMService{}, &mockProviderService{}, &mockOAuthService{})

	rm := service.ResolvedModel{
		Model:    models.Model{ID: 1, ProviderID: 1, Name: "llama3.2:latest"},
		Provider: models.Provider{ID: 1, Name: "ollama-local", APIType: models.APITypeOllama, BaseURL: ts.URL},
	}

	chatReq := ChatCompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	}

	body, httpResp, err := engine.sendStreamRequest(nil, rm, "", chatReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer body.Close()

	// Assert request hit the OpenAI-compatible endpoint via ollama branch
	if gotPath != "/v1/chat/completions" {
		t.Errorf("request path = %q, want /v1/chat/completions", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("request method = %q, want POST", gotMethod)
	}
	// openaiClient sends Bearer + apiKey; trailing space trimmed by http header canonicalization
	if !strings.HasPrefix(gotAuth, "Bearer") {
		t.Errorf("auth header = %q, want Bearer prefix", gotAuth)
	}

	if httpResp == nil {
		t.Fatal("expected non-nil HTTP response")
	}
	if httpResp.StatusCode != http.StatusOK {
		t.Errorf("HTTP status = %d, want %d", httpResp.StatusCode, http.StatusOK)
	}

	// Read the SSE stream and verify we get data
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("failed to read stream: %v", err)
	}
	streamContent := string(data)
	if !strings.Contains(streamContent, "[DONE]") {
		t.Errorf("stream missing [DONE] marker, got: %s", streamContent)
	}
	if !strings.Contains(streamContent, "chat.completion.chunk") {
		t.Errorf("stream missing chunk objects, got: %s", streamContent)
	}
}

func TestSendStreamRequest_Cloudflare_ExplicitBranch(t *testing.T) {
	// Mock test server that returns an SSE stream
	var gotPath, gotMethod, gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {\"id\":\"cf-s1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprintf(w, "data: {\"id\":\"cf-s2\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprintf(w, "data: {\"id\":\"cf-s3\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer ts.Close()

	engine, _ := newTestEngine(&mockVMService{}, &mockProviderService{}, &mockOAuthService{})

	rm := service.ResolvedModel{
		Model:    models.Model{ID: 1, ProviderID: 1, Name: "@cf/meta/llama-3-8b-instruct"},
		Provider: models.Provider{ID: 1, Name: "cloudflare", APIType: models.APITypeCloudflare, BaseURL: ts.URL},
	}

	chatReq := ChatCompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	}

	body, httpResp, err := engine.sendStreamRequest(nil, rm, "test-key", chatReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer body.Close()

	// Assert request hit the OpenAI-compatible endpoint via cloudflare branch
	if gotPath != "/v1/chat/completions" {
		t.Errorf("request path = %q, want /v1/chat/completions", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("request method = %q, want POST", gotMethod)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth header = %q, want %q", gotAuth, "Bearer test-key")
	}

	if httpResp == nil {
		t.Fatal("expected non-nil HTTP response")
	}
	if httpResp.StatusCode != http.StatusOK {
		t.Errorf("HTTP status = %d, want %d", httpResp.StatusCode, http.StatusOK)
	}

	// Read the SSE stream and verify we get data
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("failed to read stream: %v", err)
	}
	streamContent := string(data)
	if !strings.Contains(streamContent, "[DONE]") {
		t.Errorf("stream missing [DONE] marker, got: %s", streamContent)
	}
	if !strings.Contains(streamContent, "chat.completion.chunk") {
		t.Errorf("stream missing chunk objects, got: %s", streamContent)
	}
}
