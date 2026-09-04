package proxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
)

// --- Mock model repository ---

type mockModelRepo struct {
	mu             sync.Mutex
	models         map[int64]*models.Model // by providerID:name
	toggleCalls    []toggleCall
	failToggle     bool
	ByProviderName map[string]int64 // providerID:name -> model ID
}

type toggleCall struct {
	id       int64
	disabled bool
	duration *time.Duration
}

func newMockModelRepo() *mockModelRepo {
	return &mockModelRepo{models: map[int64]*models.Model{}}
}

func (m *mockModelRepo) add(providerID int64, id int64, name string, disabled bool) {
	m.models[providerID*100000+id] = &models.Model{ID: id, ProviderID: providerID, Name: name, Disabled: disabled}
}

func (m *mockModelRepo) Create(_ context.Context, _ *models.Model) error { return nil }

func (m *mockModelRepo) GetByID(_ context.Context, id int64) (*models.Model, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, model := range m.models {
		if model.ID == id {
			return model, nil
		}
	}
	return nil, nil
}

func (m *mockModelRepo) GetByProviderAndName(_ context.Context, providerID int64, name string) (*models.Model, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, model := range m.models {
		if model.ProviderID == providerID && model.Name == name {
			return model, nil
		}
	}
	return nil, nil
}

func (m *mockModelRepo) ListByProvider(_ context.Context, _ int64) ([]models.Model, error) {
	return nil, nil
}

func (m *mockModelRepo) ListAll(_ context.Context) ([]models.Model, error) {
	return nil, nil
}

func (m *mockModelRepo) ListEnabled(_ context.Context) ([]models.Model, error) {
	return nil, nil
}

func (m *mockModelRepo) Delete(_ context.Context, _ int64) error { return nil }

func (m *mockModelRepo) Upsert(_ context.Context, _ int64, _ string) (*models.Model, error) {
	return nil, nil
}

func (m *mockModelRepo) DisableByProviderExcept(_ context.Context, _ int64, _ []string) (int64, error) {
	return 0, nil
}

func (m *mockModelRepo) ToggleDisabled(_ context.Context, id int64, disabled bool, duration *time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toggleCalls = append(m.toggleCalls, toggleCall{id: id, disabled: disabled, duration: duration})
	if m.failToggle {
		return fmt.Errorf("injected repo failure")
	}
	for _, model := range m.models {
		if model.ID == id {
			model.Disabled = disabled
		}
	}
	return nil
}

func (m *mockModelRepo) ListExpiredDisabled(_ context.Context, _ time.Time) ([]int64, error) {
	return nil, nil
}

var _ repository.ModelRepository = (*mockModelRepo)(nil)

// --- CircuitBreaker.DisableModelPermanent tests ---

func newTestCircuitBreaker(repo repository.ModelRepository) *CircuitBreaker {
	return NewCircuitBreaker(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestDisableModelPermanent_FirstDisable(t *testing.T) {
	repo := newMockModelRepo()
	repo.add(1, 42, "gpt-x", false)
	cb := newTestCircuitBreaker(repo)

	if !cb.DisableModelPermanent(context.Background(), 1, "gpt-x") {
		t.Fatal("expected DisableModelPermanent to return true on first disable")
	}

	repo.mu.Lock()
	calls := append([]toggleCall{}, repo.toggleCalls...)
	repo.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 ToggleDisabled call, got %d", len(calls))
	}
	if calls[0].id != 42 || !calls[0].disabled || calls[0].duration != nil {
		t.Errorf("expected ToggleDisabled(42, true, nil), got %+v", calls[0])
	}

	// No cooldown registered — must never be auto re-enabled
	if _, ok := cb.modelCooldowns.Load(modelKey(1, "gpt-x")); ok {
		t.Error("expected no model cooldown entry for permanent disable")
	}
}

func TestDisableModelPermanent_AlreadyDisabled_NoOp(t *testing.T) {
	repo := newMockModelRepo()
	repo.add(1, 42, "gpt-x", true)
	cb := newTestCircuitBreaker(repo)

	if cb.DisableModelPermanent(context.Background(), 1, "gpt-x") {
		t.Error("expected false when model already disabled")
	}

	repo.mu.Lock()
	calls := len(repo.toggleCalls)
	repo.mu.Unlock()
	if calls != 0 {
		t.Errorf("expected 0 ToggleDisabled calls, got %d", calls)
	}
}

func TestDisableModelPermanent_ModelNotFound(t *testing.T) {
	repo := newMockModelRepo()
	cb := newTestCircuitBreaker(repo)

	if cb.DisableModelPermanent(context.Background(), 1, "missing") {
		t.Error("expected false when model not found")
	}
}

func TestDisableModelPermanent_RepoError(t *testing.T) {
	repo := newMockModelRepo()
	repo.add(1, 42, "gpt-x", false)
	repo.failToggle = true
	cb := newTestCircuitBreaker(repo)

	if cb.DisableModelPermanent(context.Background(), 1, "gpt-x") {
		t.Error("expected false on repo error")
	}
}

func TestDisableModelPermanent_NeverReEnabledByLoop(t *testing.T) {
	repo := newMockModelRepo()
	repo.add(1, 42, "gpt-x", false)
	cb := newTestCircuitBreaker(repo)

	cb.DisableModelPermanent(context.Background(), 1, "gpt-x")
	cb.reenableExpired()

	repo.mu.Lock()
	model := repo.models[100000+42]
	repo.mu.Unlock()
	if model == nil || !model.Disabled {
		t.Fatal("expected model to remain disabled after reenableExpired")
	}
}

// --- newProviderError classification tests ---

func TestNewProviderError_402Classified(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusPaymentRequired, Header: http.Header{}}
	err := newProviderError(resp, []byte(`{"error":"subscription required"}`))

	subErr, ok := err.(*SubscriptionRequiredError)
	if !ok {
		t.Fatalf("expected *SubscriptionRequiredError, got %T", err)
	}
	if subErr.ProviderError.StatusCode != http.StatusPaymentRequired {
		t.Errorf("expected embedded status 402, got %d", subErr.ProviderError.StatusCode)
	}

	// Unwrap must yield the ProviderError (errors.As compatibility)
	var pe *ProviderError
	if !asProviderError(err, &pe) {
		t.Error("expected errors.As to unwrap to *ProviderError")
	}
}

func TestNewProviderError_500Plain(t *testing.T) {
	resp := &http.Response{StatusCode: 500, Header: http.Header{}}
	err := newProviderError(resp, []byte(`{"error":"boom"}`))

	if _, ok := err.(*SubscriptionRequiredError); ok {
		t.Fatal("500 must NOT be classified as SubscriptionRequiredError")
	}
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected *ProviderError, got %T", err)
	}
	if pe.StatusCode != 500 {
		t.Errorf("expected status 500, got %d", pe.StatusCode)
	}
}

// asProviderError is a thin wrapper matching errors.As semantics.
func asProviderError(err error, target **ProviderError) bool {
	for err != nil {
		if pe, ok := err.(*ProviderError); ok {
			*target = pe
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// --- Engine 402 → permanent disable integration tests ---

const test402Body = `{"error":"this model requires a subscription or extra usage, upgrade for access at https://ollama.com/upgrade"}`

type engine402Fixture struct {
	engine   *Engine
	logChan  chan RequestLog
	repo     *mockModelRepo
	failSrv  *httptest.Server
	bodyJson string
}

// newEngine402Fixture wires an engine with a mock model repo and a provider
// server that always returns 402, resolving one model.
func newEngine402Fixture(t *testing.T, providerAPIType models.APIType) *engine402Fixture {
	t.Helper()

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(test402Body))
	}))
	t.Cleanup(failSrv.Close)

	repo := newMockModelRepo()
	repo.add(1, 42, "paid-model", false)

	vmName := "test-vm"
	vmSvc := &mockVMService{
		getByNameFn: func(_ context.Context, name string) (*models.VirtualModel, error) {
			if name != vmName {
				return nil, nil
			}
			return &models.VirtualModel{ID: 1, Name: vmName, MaxRetries: 0}, nil
		},
		resolveFn: func(_ context.Context, _ *models.VirtualModel) ([]service.ResolvedModel, error) {
			return []service.ResolvedModel{
				{
					Model:    models.Model{ID: 42, ProviderID: 1, Name: "paid-model"},
					Provider: models.Provider{ID: 1, Name: "ollama-cloud-test", APIType: providerAPIType, BaseURL: failSrv.URL, APIKeyEncrypted: "enc:test"},
				},
			}, nil
		},
	}

	engine, logChan := newTestEngine(vmSvc, &mockProviderService{
		decryptKeyFn: func(_ string) (string, error) { return "test-key", nil },
	}, &mockOAuthService{
		getTokenFn: func(_ context.Context, _ int64) (string, error) { return "", io.EOF },
	})

	cb := newTestCircuitBreaker(repo)
	engine.SetCircuitBreaker(cb)

	return &engine402Fixture{
		engine:   engine,
		logChan:  logChan,
		repo:     repo,
		failSrv:  failSrv,
		bodyJson: `{"model":"test-vm","messages":[{"role":"user","content":"hi"}]}`,
	}
}

func (f *engine402Fixture) assertModelPermanentlyDisabled(t *testing.T) {
	t.Helper()

	f.repo.mu.Lock()
	calls := append([]toggleCall{}, f.repo.toggleCalls...)
	model := f.repo.models[100000+42]
	f.repo.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 ToggleDisabled call, got %d", len(calls))
	}
	if calls[0].id != 42 || !calls[0].disabled || calls[0].duration != nil {
		t.Errorf("expected ToggleDisabled(42, true, nil), got %+v", calls[0])
	}
	if model == nil || !model.Disabled {
		t.Error("expected model state Disabled=true")
	}
}

func TestHandleChatCompletion_402_DisablesModel(t *testing.T) {
	f := newEngine402Fixture(t, models.APITypeOpenAI)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(f.bodyJson))
	w := httptest.NewRecorder()
	f.engine.HandleChatCompletion(w, req)

	f.assertModelPermanentlyDisabled(t)
}

func TestHandleChatCompletionStream_402_DisablesModel(t *testing.T) {
	f := newEngine402Fixture(t, models.APITypeOpenAI)

	body := `{"model":"test-vm","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	f.engine.HandleChatCompletionStream(w, req)

	f.assertModelPermanentlyDisabled(t)
}

func TestHandleAnthropicMessages_402_DisablesModel(t *testing.T) {
	f := newEngine402Fixture(t, models.APITypeAnthropic)

	body := `{"model":"test-vm","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	w := httptest.NewRecorder()
	f.engine.HandleAnthropicMessages(w, req)

	f.assertModelPermanentlyDisabled(t)
}

func TestHandleAnthropicMessagesStream_402_DisablesModel(t *testing.T) {
	f := newEngine402Fixture(t, models.APITypeAnthropic)

	body := `{"model":"test-vm","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	w := httptest.NewRecorder()
	f.engine.HandleAnthropicMessagesStream(w, req)

	f.assertModelPermanentlyDisabled(t)
}

func TestHandleChatCompletion_402_BypassesCBDisabledSetting(t *testing.T) {
	f := newEngine402Fixture(t, models.APITypeOpenAI)
	settings := f.engine.GetCircuitBreaker().GetSettings()
	settings.Enabled = false
	f.engine.GetCircuitBreaker().ApplySettings(settings)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(f.bodyJson))
	w := httptest.NewRecorder()
	f.engine.HandleChatCompletion(w, req)

	f.assertModelPermanentlyDisabled(t)
}

func TestHandleChatCompletion_5xx_DoesNotPermanentDisable(t *testing.T) {
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"boom"}`))
	}))
	defer failSrv.Close()

	repo := newMockModelRepo()
	repo.add(1, 42, "paid-model", false)

	vmSvc := &mockVMService{
		getByNameFn: func(_ context.Context, _ string) (*models.VirtualModel, error) {
			return &models.VirtualModel{ID: 1, Name: "test-vm", MaxRetries: 0}, nil
		},
		resolveFn: func(_ context.Context, _ *models.VirtualModel) ([]service.ResolvedModel, error) {
			return []service.ResolvedModel{
				{
					Model:    models.Model{ID: 42, ProviderID: 1, Name: "paid-model"},
					Provider: models.Provider{ID: 1, Name: "openai-test", APIType: models.APITypeOpenAI, BaseURL: failSrv.URL, APIKeyEncrypted: "enc:test"},
				},
			}, nil
		},
	}

	engine, _ := newTestEngine(vmSvc, &mockProviderService{
		decryptKeyFn: func(_ string) (string, error) { return "test-key", nil },
	}, &mockOAuthService{
		getTokenFn: func(_ context.Context, _ int64) (string, error) { return "", io.EOF },
	})
	engine.SetCircuitBreaker(newTestCircuitBreaker(repo))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test-vm","messages":[{"role":"user","content":"hi"}]}`))
	w := httptest.NewRecorder()
	engine.HandleChatCompletion(w, req)

	repo.mu.Lock()
	calls := len(repo.toggleCalls)
	repo.mu.Unlock()
	if calls != 0 {
		t.Errorf("5xx must not permanently disable; got %d ToggleDisabled calls", calls)
	}
}
