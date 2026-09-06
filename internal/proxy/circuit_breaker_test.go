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
	strikeCalls    []strikeCall
	failToggle     bool
	ByProviderName map[string]int64 // providerID:name -> model ID
	cbStrikes      map[int64]int    // model ID -> strikes
}

type toggleCall struct {
	id       int64
	disabled bool
	duration *time.Duration
}

type strikeCall struct {
	id      int64
	strikes int
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

func (m *mockModelRepo) GetCBStrikes(_ context.Context, modelID int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cbStrikes == nil {
		return 0, nil
	}
	return m.cbStrikes[modelID], nil
}

func (m *mockModelRepo) SetCBStrikes(_ context.Context, modelID int64, strikes int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cbStrikes == nil {
		m.cbStrikes = map[int64]int{}
	}
	m.cbStrikes[modelID] = strikes
	m.strikeCalls = append(m.strikeCalls, strikeCall{id: modelID, strikes: strikes})
	return nil
}

var _ repository.ModelRepository = (*mockModelRepo)(nil)

// --- Mock provider repository (for provider-level breaker assertions) ---

type mockProviderRepo struct {
	mu        sync.Mutex
	providers map[int64]*models.Provider
}

func newMockProviderRepo() *mockProviderRepo {
	return &mockProviderRepo{providers: map[int64]*models.Provider{}}
}

func (m *mockProviderRepo) add(id int64, name string, disabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers[id] = &models.Provider{ID: id, Name: name, Disabled: disabled}
}

func (m *mockProviderRepo) Create(_ context.Context, _ *models.Provider) error { return nil }

func (m *mockProviderRepo) GetByID(_ context.Context, id int64) (*models.Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.providers[id]
	if p == nil {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (m *mockProviderRepo) GetByName(_ context.Context, _ string) (*models.Provider, error) {
	return nil, nil
}

func (m *mockProviderRepo) GetByKey(_ context.Context, _ string) (*models.Provider, error) {
	return nil, nil
}

func (m *mockProviderRepo) List(_ context.Context) ([]models.Provider, error) {
	return nil, nil
}

func (m *mockProviderRepo) ListByMetadata(_ context.Context, _ map[string]string) ([]models.Provider, error) {
	return nil, nil
}

func (m *mockProviderRepo) Update(_ context.Context, p *models.Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *p
	m.providers[p.ID] = &cp
	return nil
}

func (m *mockProviderRepo) Delete(_ context.Context, _ int64) error { return nil }

func (m *mockProviderRepo) ListExpiredDisabled(_ context.Context, _ time.Time) ([]int64, error) {
	return nil, nil
}

var _ repository.ProviderRepository = (*mockProviderRepo)(nil)

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

// --- Escalating cooldown (cb_strikes) tests ---

// TestEscalatingCooldownMath disables the same model repeatedly (simulating
// trips after auto/manual re-enable) and asserts the persisted cooldown
// duration escalates 600s → 1200s → 2400s … capped at 24h.
func TestEscalatingCooldownMath(t *testing.T) {
	repo := newMockModelRepo()
	repo.add(1, 42, "gpt-x", false)
	cb := newTestCircuitBreaker(repo)

	expected := []int{600, 1200, 2400, 4800, 9600, 19200, 38400, 76800, 86400, 86400}

	// FreshCB returns a breaker that starts clean (no in-memory cooldown),
	// backed by the same repo so persisted strikes drive escalation.
	freshCB := func() *CircuitBreaker {
		cb2 := newTestCircuitBreaker(repo)
		cb2.Stop() // don't leak reenableLoop tickers
		return cb2
	}
	cb.Stop() // stop the original loop

	// First strike happens via disableModel directly (strike=1).
	cb.disableModel(context.Background(), 1, "gpt-x", 600)

	for i, want := range expected {
		repo.mu.Lock()
		var call *toggleCall
		for j := range repo.toggleCalls {
			if repo.toggleCalls[j].disabled {
				c := repo.toggleCalls[j]
				call = &c
			}
		}
		strikes := repo.cbStrikes[42]
		repo.mu.Unlock()
		if call == nil {
			t.Fatalf("strike %d: expected a disable toggle call", i+1)
		}
		if call.duration == nil {
			t.Fatalf("strike %d: expected non-nil duration, got nil", i+1)
		}
		if got := int(call.duration.Seconds()); got != want {
			t.Errorf("strike %d: expected cooldown %ds, got %ds", i+1, want, got)
		}
		if strikes != i+1 {
			t.Errorf("strike %d: expected persisted strikes %d, got %d", i+1, i+1, strikes)
		}

		if i == len(expected)-1 {
			break
		}

		// Simulate model coming back and failing again: reset disabled flag,
		// clear the in-memory cooldown, then trip again.
		repo.mu.Lock()
		repo.models[100000+42].Disabled = false
		repo.mu.Unlock()
		next := freshCB()
		next.disableModel(context.Background(), 1, "gpt-x", 600)
	}
}

// TestRecordUnavailable_DisablesModelAtThreshold feeds threshold many
// "model unavailable" errors and asserts the model (not provider) is disabled
// with a persisted strike count.
func TestRecordUnavailable_DisablesModelAtThreshold(t *testing.T) {
	repo := newMockModelRepo()
	repo.add(1, 42, "stale-model", false)
	cb := newTestCircuitBreaker(repo)
	defer cb.Stop()

	s := cb.GetSettings()
	s.ModelThreshold = 2
	s.ModelCooldownSec = 600
	cb.ApplySettings(s)

	ctx := context.Background()
	cb.RecordUnavailable(ctx, 1, "stale-model")
	cb.RecordUnavailable(ctx, 1, "stale-model")

	repo.mu.Lock()
	calls := append([]toggleCall{}, repo.toggleCalls...)
	strikes := append([]strikeCall{}, repo.strikeCalls...)
	repo.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 ToggleDisabled call, got %d", len(calls))
	}
	if calls[0].id != 42 || !calls[0].disabled {
		t.Errorf("expected ToggleDisabled(42, true), got %+v", calls[0])
	}
	if calls[0].duration == nil || int(calls[0].duration.Seconds()) != 600 {
		t.Errorf("expected first-strike cooldown 600s, got %+v", calls[0].duration)
	}
	if len(strikes) != 1 || strikes[0].id != 42 || strikes[0].strikes != 1 {
		t.Errorf("expected SetCBStrikes(42, 1), got %+v", strikes)
	}

	// Model-level cooldown registered
	if _, ok := cb.modelCooldowns.Load(modelKey(1, "stale-model")); !ok {
		t.Error("expected model cooldown entry after threshold reached")
	}
}

// TestRecordUnavailable_DoesNotDisableProvider asserts stale-model errors
// never trip the provider-level breaker, even across many models.
func TestRecordUnavailable_DoesNotDisableProvider(t *testing.T) {
	repo := newMockModelRepo()
	provRepo := newMockProviderRepo()
	provRepo.add(1, "prov-1", false)
	repo.add(1, 42, "stale-a", false)
	repo.add(1, 43, "stale-b", false)
	cb := NewCircuitBreaker(repo, provRepo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer cb.Stop()

	s := cb.GetSettings()
	s.ModelThreshold = 2
	s.ProviderThreshold = 4 // would trip if RecordUnavailable counted at provider level
	s.ProviderMinModels = 2
	cb.ApplySettings(s)

	ctx := context.Background()
	// Far exceed the provider threshold: 3 failures × 2 models = 6 events.
	for i := 0; i < 3; i++ {
		cb.RecordUnavailable(ctx, 1, "stale-a")
		cb.RecordUnavailable(ctx, 1, "stale-b")
	}

	if _, ok := cb.providerCooldowns.Load(int64(1)); ok {
		t.Error("provider must not enter cooldown from model-unavailable errors")
	}
	if p, _ := provRepo.GetByID(ctx, 1); p != nil && p.Disabled {
		t.Error("provider must not be disabled by RecordUnavailable")
	}
	if _, ok := cb.modelCooldowns.Load(modelKey(1, "stale-a")); !ok {
		t.Error("expected model stale-a to be disabled")
	}
}

// TestRecordSuccess_ResetsStrikes asserts a successful response clears
// strikes so the next failure uses the base cooldown again.
func TestRecordSuccess_ResetsStrikes(t *testing.T) {
	repo := newMockModelRepo()
	repo.add(1, 42, "gpt-x", false)
	cb := newTestCircuitBreaker(repo)
	defer cb.Stop()

	// First disable: strike 1, base cooldown.
	cb.disableModel(context.Background(), 1, "gpt-x", 600)

	repo.mu.Lock()
	repo.models[100000+42].Disabled = false
	repo.mu.Unlock()

	// A success arrives → strikes cleared.
	cb.RecordSuccess(1, "gpt-x")

	repo.mu.Lock()
	strikes := repo.cbStrikes[42]
	_, memStrike := cb.modelStrikesMem.Load(modelKey(1, "gpt-x"))
	repo.mu.Unlock()
	if strikes != 0 {
		t.Errorf("expected persisted strikes reset to 0, got %d", strikes)
	}
	if memStrike {
		t.Error("expected in-memory strike marker cleared")
	}

	// Next failure must be back at strike 1 → base cooldown.
	next := newTestCircuitBreaker(repo)
	defer next.Stop()
	next.disableModel(context.Background(), 1, "gpt-x", 600)

	repo.mu.Lock()
	calls := append([]toggleCall{}, repo.toggleCalls...)
	repo.mu.Unlock()
	var last *toggleCall
	for i := range calls {
		if calls[i].disabled {
			c := calls[i]
			last = &c
		}
	}
	if last == nil || last.duration == nil || int(last.duration.Seconds()) != 600 {
		t.Errorf("expected base cooldown 600s after RecordSuccess, got %+v", last)
	}
}

// TestIsModelUnavailableError covers the engine-side classification helper.
func TestIsModelUnavailableError(t *testing.T) {
	cases := []struct {
		name string
		pe   *ProviderError
		want bool
	}{
		{"400 model unavailable", &ProviderError{StatusCode: 400, Message: `{"error":{"message":"Model is unavailable."}}`}, true},
		{"404 model_not_found", &ProviderError{StatusCode: 404, Message: `{"error":{"type":"model_not_found"}}`}, true},
		{"400 invalid request", &ProviderError{StatusCode: 400, Message: `{"error":{"message":"invalid request"}}`}, false},
		{"400 bad auth", &ProviderError{StatusCode: 400, Message: "api key invalid"}, false},
		{"500 model not found text", &ProviderError{StatusCode: 500, Message: "model not found"}, false},
		{"402 model unavailable", &ProviderError{StatusCode: 402, Message: "model is unavailable"}, false},
	}
	for _, tc := range cases {
		if got := isModelUnavailableError(tc.pe); got != tc.want {
			t.Errorf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}

// --- Engine 400 "model unavailable" → breaker disable integration tests ---

// newEngineStatusFixture is newEngine402Fixture generalized: the provider
// server returns statusCode with the given body for every request.
func newEngineStatusFixture(t *testing.T, statusCode int, body string) *engine402Fixture {
	t.Helper()

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(statusCode)
		w.Write([]byte(body))
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
					Provider: models.Provider{ID: 1, Name: "ollama-cloud-test", APIType: models.APITypeOpenAI, BaseURL: failSrv.URL, APIKeyEncrypted: "enc:test"},
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

// TestHandleChatCompletion_400ModelUnavailable_DisablesModel: upstream
// "Model is unavailable." 400s count against the model breaker and disable it
// once the threshold is reached.
func TestHandleChatCompletion_400ModelUnavailable_DisablesModel(t *testing.T) {
	f := newEngineStatusFixture(t, http.StatusBadRequest, `{"error":{"message":"Model is unavailable."}}`)

	settings := f.engine.GetCircuitBreaker().GetSettings()
	settings.ModelThreshold = 2
	f.engine.GetCircuitBreaker().ApplySettings(settings)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(f.bodyJson))
		w := httptest.NewRecorder()
		f.engine.HandleChatCompletion(w, req)
	}

	f.repo.mu.Lock()
	calls := append([]toggleCall{}, f.repo.toggleCalls...)
	strikes := append([]strikeCall{}, f.repo.strikeCalls...)
	model := f.repo.models[100000+42]
	f.repo.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 ToggleDisabled call, got %d", len(calls))
	}
	if calls[0].id != 42 || !calls[0].disabled {
		t.Errorf("expected ToggleDisabled(42, true), got %+v", calls[0])
	}
	if calls[0].duration == nil || int(calls[0].duration.Seconds()) != 600 {
		t.Errorf("expected first-strike cooldown 600s, got %+v", calls[0].duration)
	}
	if len(strikes) != 1 || strikes[0].strikes != 1 {
		t.Errorf("expected SetCBStrikes(42, 1), got %+v", strikes)
	}
	if model == nil || !model.Disabled {
		t.Error("expected model state Disabled=true")
	}
}

// TestHandleChatCompletion_400Generic_NotDisabled: generic client errors
// (e.g. invalid request) must never count toward the model breaker.
func TestHandleChatCompletion_400Generic_NotDisabled(t *testing.T) {
	f := newEngineStatusFixture(t, http.StatusBadRequest, `{"error":{"message":"invalid request"}}`)

	settings := f.engine.GetCircuitBreaker().GetSettings()
	settings.ModelThreshold = 2
	f.engine.GetCircuitBreaker().ApplySettings(settings)

	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(f.bodyJson))
		w := httptest.NewRecorder()
		f.engine.HandleChatCompletion(w, req)
	}

	f.repo.mu.Lock()
	calls := len(f.repo.toggleCalls)
	model := f.repo.models[100000+42]
	f.repo.mu.Unlock()

	if calls != 0 {
		t.Errorf("generic 400 must not disable model; got %d ToggleDisabled calls", calls)
	}
	if model != nil && model.Disabled {
		t.Error("expected model to remain enabled")
	}
}

// --- X-Opencode-Session header tests ---

// captureServer records request headers and returns 200 with a minimal completion.
func captureServer(t *testing.T, captured *http.Header) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*captured = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
}

func TestApplySessionHeader_OpencodeHost(t *testing.T) {
	for _, baseURL := range []string{
		"https://opencode.ai/zen/go",
		"https://opencode.ai/zen",
		"https://api.opencode.ai/v1",
	} {
		req, _ := http.NewRequest("POST", baseURL+"/v1/chat/completions", nil)
		applySessionHeader(req, baseURL, "sess-123")
		if got := req.Header.Get("X-Opencode-Session"); got != "sess-123" {
			t.Errorf("baseURL %q: expected header sess-123, got %q", baseURL, got)
		}
	}
}

func TestApplySessionHeader_OtherHosts(t *testing.T) {
	for _, baseURL := range []string{
		"https://api.openai.com/v1",
		"https://notopencode.ai/v1",
		"https://evil-opencode.ai.com/v1",
		"https://localhost:11434/v1",
	} {
		req, _ := http.NewRequest("POST", baseURL+"/v1/chat/completions", nil)
		applySessionHeader(req, baseURL, "sess-123")
		if got := req.Header.Get("X-Opencode-Session"); got != "" {
			t.Errorf("baseURL %q: expected no header, got %q", baseURL, got)
		}
	}
}

func TestApplySessionHeader_EmptySessionID(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://opencode.ai/zen/go", nil)
	applySessionHeader(req, "https://opencode.ai/zen/go", "")
	if req.Header.Get("X-Opencode-Session") != "" {
		t.Error("empty sessionID must not set header")
	}
}

func TestSessionIDForRequest_UsesXRequestID(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("X-Request-ID", "my-session-id")
	if got := sessionIDForRequest(r); got != "my-session-id" {
		t.Errorf("expected my-session-id, got %q", got)
	}
}

func TestSessionIDForRequest_FallbackStable(t *testing.T) {
	r1 := httptest.NewRequest(http.MethodPost, "/", nil)
	r2 := httptest.NewRequest(http.MethodPost, "/", nil)
	id1 := sessionIDForRequest(r1)
	id2 := sessionIDForRequest(r2)
	if id1 == "" {
		t.Fatal("fallback ID must not be empty")
	}
	if id1 != id2 {
		t.Errorf("fallback ID must be process-stable: %q != %q", id1, id2)
	}
}

func TestOpenCodeGo_SessionHeaderForwarded(t *testing.T) {
	origHostCheck := isOpencodeHost
	isOpencodeHost = func(string) bool { return true }
	defer func() { isOpencodeHost = origHostCheck }()

	var captured http.Header
	srv := captureServer(t, &captured)
	defer srv.Close()

	vmSvc := &mockVMService{
		getByNameFn: func(_ context.Context, _ string) (*models.VirtualModel, error) {
			return &models.VirtualModel{ID: 1, Name: "test-vm", MaxRetries: 0}, nil
		},
		resolveFn: func(_ context.Context, _ *models.VirtualModel) ([]service.ResolvedModel, error) {
			return []service.ResolvedModel{
				{
					Model:    models.Model{ID: 42, ProviderID: 1, Name: "go-model"},
					Provider: models.Provider{ID: 1, Name: "opencode-go", APIType: models.APITypeOpenAI, BaseURL: srv.URL + "/go", APIKeyEncrypted: "enc:test"},
				},
			}, nil
		},
	}

	engine, _ := newTestEngine(vmSvc, &mockProviderService{
		decryptKeyFn: func(_ string) (string, error) { return "test-key", nil },
	}, &mockOAuthService{
		getTokenFn: func(_ context.Context, _ int64) (string, error) { return "", io.EOF },
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test-vm","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("X-Request-ID", "sess_abc123")
	w := httptest.NewRecorder()
	engine.HandleChatCompletion(w, req)

	if captured.Get("X-Opencode-Session") == "" {
		t.Errorf("expected X-Opencode-Session to be set, headers: %v", captured)
	}
}
