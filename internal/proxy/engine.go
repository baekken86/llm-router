package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/service"
)

type RequestLog struct {
	Type               string // "incoming" or "proxy"
	Status             string // "streaming", "completed", "failed", or "" for non-streaming
	Timestamp          time.Time
	RequestID          string
	VirtualModel       string
	ClientKeyID        int64
	ProviderName       string
	ModelName          string
	StatusCode         int
	Latency            time.Duration
	InputTokens        int
	OutputTokens       int
	CachedTokens       int
	ReasoningTokens    int
	ErrorMessage       string
	RetryCount         int
	FallbackCount      int
	RTKIntercepted     bool
	RTKSavedTokens     int
	CavemanIntercepted bool
	CavemanSavedTokens int
}

type StreamProgress struct {
	InputTokens     int
	OutputTokens    int
	CachedTokens    int
	ReasoningTokens int
}

type SendRequestResult struct {
	Response           *ChatCompletionResponse
	CavemanIntercepted bool
	CavemanSavedTokens int
}

type InterceptResult struct {
	Messages       []Message
	RTKIntercepted bool
	RTKSavedTokens int
}

type Engine struct {
	vmService         service.VirtualModelService
	providerService   service.ProviderService
	oauthService      service.OAuthService
	openaiClient      *OpenAIClient
	anthropicClient   *AnthropicClient
	ollamaCloudClient *OllamaCloudClient
	codexClient       *CodexClient
	logger            *slog.Logger
	logChan           chan<- RequestLog
	rtk               *RTKInterceptor
	caveman           *CavemanInterceptor
	rateLimits        *RateLimitTracker
	circuitBreaker    *CircuitBreaker
	maxRetries        int
	timeoutSeconds    int
	maxTokens         int
}

func NewEngine(
	vmService service.VirtualModelService,
	providerService service.ProviderService,
	oauthService service.OAuthService,
	logger *slog.Logger,
	logChan chan<- RequestLog,
) *Engine {
	return &Engine{
		vmService:         vmService,
		providerService:   providerService,
		oauthService:      oauthService,
		openaiClient:      NewOpenAIClient(),
		anthropicClient:   NewAnthropicClient(),
		ollamaCloudClient: NewOllamaCloudClient(),
		codexClient:       NewCodexClient(),
		logger:            logger,
		logChan:           logChan,
		rtk:               NewRTKInterceptor(logger),
		caveman:           NewCavemanInterceptor(logger),
		rateLimits:        &RateLimitTracker{},
		maxRetries:        2,
		timeoutSeconds:    300,
		maxTokens:         8192,
	}
}

func (e *Engine) GetRTK() *RTKInterceptor {
	return e.rtk
}

func (e *Engine) GetCaveman() *CavemanInterceptor {
	return e.caveman
}

func (e *Engine) GetRateLimitStatus() []ProviderRateLimitStatus {
	return e.rateLimits.GetStatus()
}

func (e *Engine) ClearRateLimit(providerID int64) {
	e.rateLimits.Clear(providerID)
}

func (e *Engine) SetCircuitBreaker(cb *CircuitBreaker) {
	e.circuitBreaker = cb
}

func (e *Engine) GetCircuitBreaker() *CircuitBreaker {
	return e.circuitBreaker
}

func (e *Engine) getAPIKey(ctx context.Context, provider models.Provider) (string, error) {
	// Check for OAuth token first (handles refresh automatically)
	token, err := e.oauthService.GetValidToken(ctx, provider.ID)
	if err == nil && token != "" {
		return token, nil
	}
	// OAuth token unavailable — check if provider has an API key fallback
	if provider.APIKeyEncrypted == "" {
		// Allow empty key for providers that don't need auth (e.g. local Ollama)
		if provider.APIType == models.APITypeOllama || provider.APIType == models.APITypeOllamaCloud {
			return "", nil
		}
		// No API key and no valid OAuth token — this is an OAuth-only provider
		// that can't authenticate. Return a clear error instead of silently
		// sending an empty bearer token which causes confusing 401s.
		if err != nil {
			return "", fmt.Errorf("oauth token unavailable (re-run 'llm-router setup --provider %s' to re-authenticate): %w", provider.Name, err)
		}
		return "", fmt.Errorf("no credentials available for provider %s (re-run 'llm-router setup --provider %s')", provider.Name, provider.Name)
	}
	return e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
}

// getCodexCredentials resolves the ChatGPT OAuth access token and account id
// for a codex provider. It mirrors getAPIKey's OAuth-first flow but returns
// the full token row because the codex wire needs BOTH the access token
// (Authorization: Bearer) and the account id (ChatGPT-Account-ID header).
// getAPIKey's signature is intentionally left untouched — only the codex
// branch needs the row.
func (e *Engine) getCodexCredentials(ctx context.Context, provider models.Provider) (*models.OAuthToken, error) {
	token, err := e.oauthService.GetValidToken(ctx, provider.ID)
	if err != nil {
		return nil, fmt.Errorf("oauth token unavailable (re-run 'llm-router connect --provider %s' to re-authenticate): %w", provider.Name, err)
	}
	if token == "" {
		// Codex providers are OAuth-only (no API key fallback is created for
		// them), so this mirrors getAPIKey's no-credentials case.
		return nil, fmt.Errorf("no credentials available for provider %s (re-run 'llm-router connect --provider %s')", provider.Name, provider.Name)
	}

	row, err := e.oauthService.GetTokenRow(ctx, provider.ID)
	if err != nil {
		return nil, fmt.Errorf("oauth token unavailable (re-run 'llm-router connect --provider %s' to re-authenticate): %w", provider.Name, err)
	}
	if row == nil {
		// GetValidToken succeeded but the row vanished (e.g. concurrent
		// invalidation) — treat as re-auth required.
		return nil, fmt.Errorf("oauth token unavailable (re-run 'llm-router connect --provider %s' to re-authenticate)", provider.Name)
	}
	return row, nil
}

// codexAuthError builds the actionable provider error surfaced when the Codex
// backend rejects the OAuth token and a refresh could not recover it. Auth
// failure is NOT quota: no cooldown is registered and the model is not
// disabled — the user just needs to re-run the connect flow.
func codexAuthError(providerName string, cause error) *ProviderError {
	return &ProviderError{
		StatusCode: http.StatusUnauthorized,
		Message: fmt.Sprintf(
			"chatgpt session invalid — re-run 'llm-router connect --provider %s' to reconnect: %v",
			providerName, cause,
		),
	}
}

func (e *Engine) ApplySettings(maxRetries, timeoutSeconds, maxTokens int) {
	e.maxRetries = maxRetries
	e.timeoutSeconds = timeoutSeconds
	e.maxTokens = maxTokens
}

// fallbackSessionID is a process-wide stable ID used when an incoming request
// carries no X-Request-ID. Generated once per process.
var (
	fallbackSessionIDOnce sync.Once
	fallbackSessionID     string
)

// sessionIDForRequest returns a stable per-conversation ID for provider
// session headers (e.g. X-Opencode-Session). Uses the incoming X-Request-ID
// (opencode sends its session ID there) when present, otherwise a
// process-wide stable UUID so all conversation-less traffic shares one ID.
func sessionIDForRequest(r *http.Request) string {
	if r != nil {
		if id := r.Header.Get("X-Request-ID"); id != "" {
			return id
		}
	}
	fallbackSessionIDOnce.Do(func() {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			fallbackSessionID = fmt.Sprintf("router-%d", time.Now().UnixNano())
			return
		}
		fallbackSessionID = hex.EncodeToString(b)
	})
	return fallbackSessionID
}

func (e *Engine) HandleChatCompletionRoute(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var peek struct {
		Stream bool `json:"stream"`
	}
	json.Unmarshal(body, &peek)
	r.Body = io.NopCloser(bytes.NewReader(body))

	if peek.Stream {
		e.HandleChatCompletionStream(w, r)
		return
	}
	e.HandleChatCompletion(w, r)
}

// resolveRequestRoute resolves the incoming model name to either a virtual
// model or a single directly-addressed provider model. For provider models it
// synthesizes a single-entry resolution (built by RouteModel) so the
// downstream retry/failover loop is unchanged. A nil vm with nil resolved
// models (and nil error) means "model not found" — callers keep their
// existing 404 path. Note: for provider routes the returned vm is nil but the
// request is still valid; handlers treat non-nil resolvedModels as success.
func (e *Engine) resolveRequestRoute(ctx context.Context, name string) (*models.VirtualModel, []service.ResolvedModel, error) {
	route, err := e.vmService.RouteModel(ctx, name)
	if err != nil {
		return nil, nil, err
	}

	if route.Kind == service.RouteKindProvider {
		if route.NotFound {
			return nil, nil, nil // caller 404s
		}
		// No virtual model object backs a direct provider model, but the
		// retry/failover loop reads vm.MaxRetries/vm.RetryOnStatus —
		// synthesize a minimal VM with the same defaults a created VM gets
		// (see virtual_model_repo.Create).
		vm := &models.VirtualModel{
			Name:          name,
			MaxRetries:    1,
			RetryOnStatus: []byte(`[429,500,502,503,504]`),
		}
		return vm, route.Resolved, nil
	}

	// Virtual route: resolve like before.
	vm, err := e.vmService.GetByName(ctx, route.VirtualName)
	if err != nil {
		return nil, nil, err
	}
	if vm == nil {
		return nil, nil, nil // caller 404s
	}
	resolved, err := e.vmService.ResolveModels(ctx, vm)
	if err != nil {
		return nil, nil, err
	}
	return vm, resolved, nil
}

func (e *Engine) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = fmt.Sprintf("req_%d", time.Now().UnixNano())
	}

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	var rtkIntercepted bool
	var rtkSavedTokens int

	if e.rtk.IsEnabled() {
		interceptResult := e.interceptMessages(req.Messages)
		req.Messages = interceptResult.Messages
		rtkIntercepted = interceptResult.RTKIntercepted
		rtkSavedTokens = interceptResult.RTKSavedTokens
	}

	// RequestLog carries the client-sent model name; for direct provider
	// addressing that's "<provider-key>/<model>" (e.g. "openai/gpt-4o").
	vm, resolvedModels, err := e.resolveRequestRoute(r.Context(), req.Model)
	if err != nil {
		e.logger.Error("resolve model route", "error", err, "model", req.Model)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if vm == nil && resolvedModels == nil {
		http.Error(w, fmt.Sprintf(`{"error":"model not found: %s"}`, req.Model), http.StatusNotFound)
		return
	}

	if len(resolvedModels) == 0 {
		http.Error(w, `{"error":"no matching models found"}`, http.StatusNotFound)
		return
	}

	e.logRequest(RequestLog{
		Type:         "incoming",
		Timestamp:    start,
		RequestID:    requestID,
		VirtualModel: req.Model,
		StatusCode:   http.StatusOK,
	})

	var failures []map[string]interface{}
	var retryOnStatus []int
	if len(vm.RetryOnStatus) > 0 {
		json.Unmarshal(vm.RetryOnStatus, &retryOnStatus)
	}

	type providerRetryState struct {
		retries int
	}
	providerRetries := make(map[int64]*providerRetryState)

	for i := 0; i < len(resolvedModels); i++ {
		rm := resolvedModels[i]
		apiKey, err := e.getAPIKey(r.Context(), rm.Provider)
		if err != nil {
			e.logger.Error("get api key", "error", err, "provider", rm.Provider.Name)
			failures = append(failures, map[string]interface{}{
				"model":    rm.Model.Name,
				"provider": rm.Provider.Name,
				"status":   500,
				"message":  "internal error: failed to get api key",
			})
			continue
		}

		prs, exists := providerRetries[rm.Provider.ID]
		if !exists {
			prs = &providerRetryState{}
			providerRetries[rm.Provider.ID] = prs
		}

		if prs.retries > vm.MaxRetries {
			continue
		}

		if limited, remaining := e.rateLimits.IsLimited(rm.Provider.ID); limited {
			failures = append(failures, map[string]interface{}{
				"model":    rm.Model.Name,
				"provider": rm.Provider.Name,
				"status":   429,
				"message":  fmt.Sprintf("rate limited, retry in %s", remaining.Round(time.Second)),
			})
			continue
		}

		if rm.Provider.Disabled {
			failures = append(failures, map[string]interface{}{
				"model":    rm.Model.Name,
				"provider": rm.Provider.Name,
				"status":   503,
				"message":  "provider disabled",
			})
			continue
		}

		resp, reqResult, err := e.sendRequest(r, rm, apiKey, req)
		if reqResult == nil {
			reqResult = &SendRequestResult{}
		}
		if resp != nil {
			e.logRequest(RequestLog{
				Type:         "proxy",
				Timestamp:    start,
				RequestID:    requestID,
				VirtualModel: req.Model,
				ProviderName: rm.Provider.Name,
				ModelName:    rm.Model.Name,
				StatusCode:   http.StatusOK,
				Latency:      time.Since(start),
				InputTokens:  resp.Usage.PromptTokens,
				OutputTokens: resp.Usage.CompletionTokens,
				CachedTokens: func() int {
					if resp.Usage.PromptTokensDetails != nil {
						return resp.Usage.PromptTokensDetails.CachedTokens
					}
					return 0
				}(),
				ReasoningTokens:    extractReasoningTokens(&resp.Usage),
				RTKIntercepted:     rtkIntercepted,
				RTKSavedTokens:     rtkSavedTokens,
				CavemanIntercepted: reqResult.CavemanIntercepted,
				CavemanSavedTokens: reqResult.CavemanSavedTokens,
				FallbackCount:      i,
				RetryCount:         prs.retries,
			})

			e.recordModelSuccess(rm)

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-ID", requestID)
			w.Header().Set("X-Provider", rm.Provider.Name)
			w.Header().Set("X-Model", rm.Model.Name)
			json.NewEncoder(w).Encode(resp)
			return
		}

		providerErr := e.processProviderError(r.Context(), err, rm)

		e.logRequest(RequestLog{
			Type:          "proxy",
			Timestamp:     start,
			RequestID:     requestID,
			VirtualModel:  req.Model,
			ProviderName:  rm.Provider.Name,
			ModelName:     rm.Model.Name,
			StatusCode:    providerErr.StatusCode,
			Latency:       time.Since(start),
			ErrorMessage:  providerErr.Message,
			FallbackCount: i,
			RetryCount:    prs.retries,
		})

		if shouldRetry(providerErr.StatusCode, retryOnStatus) && prs.retries < vm.MaxRetries {
			prs.retries++
			time.Sleep(time.Duration(prs.retries) * time.Second)
			i--
			continue
		}

		failures = append(failures, map[string]interface{}{
			"model":    rm.Model.Name,
			"provider": rm.Provider.Name,
			"status":   providerErr.StatusCode,
			"message":  providerErr.Message,
		})
	}

	e.logRequest(RequestLog{
		Type:         "proxy",
		Timestamp:    start,
		RequestID:    requestID,
		VirtualModel: req.Model,
		StatusCode:   502,
		Latency:      time.Since(start),
		ErrorMessage: "all models failed",
	})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", requestID)
	w.WriteHeader(http.StatusBadGateway)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":    "all models failed",
		"failures": failures,
	})
}

func (e *Engine) HandleChatCompletionStream(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = fmt.Sprintf("req_%d", time.Now().UnixNano())
	}

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	var rtkIntercepted bool
	var rtkSavedTokens int
	if e.rtk.IsEnabled() {
		interceptResult := e.interceptMessages(req.Messages)
		req.Messages = interceptResult.Messages
		rtkIntercepted = interceptResult.RTKIntercepted
		rtkSavedTokens = interceptResult.RTKSavedTokens
	}

	vm, resolvedModels, err := e.resolveRequestRoute(r.Context(), req.Model)
	if err != nil {
		e.logger.Error("resolve model route", "error", err, "model", req.Model)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if vm == nil && resolvedModels == nil {
		http.Error(w, fmt.Sprintf(`{"error":"model not found: %s"}`, req.Model), http.StatusNotFound)
		return
	}

	if len(resolvedModels) == 0 {
		http.Error(w, `{"error":"no matching models found"}`, http.StatusNotFound)
		return
	}

	e.logRequest(RequestLog{
		Type:         "incoming",
		Timestamp:    start,
		RequestID:    requestID,
		VirtualModel: req.Model,
		StatusCode:   http.StatusOK,
	})

	var retryOnStatus []int
	if len(vm.RetryOnStatus) > 0 {
		json.Unmarshal(vm.RetryOnStatus, &retryOnStatus)
	}

	var failures []map[string]interface{}

	for i, rm := range resolvedModels {
		apiKey, err := e.getAPIKey(r.Context(), rm.Provider)
		if err != nil {
			e.logger.Warn("skipping provider: no api key", "provider", rm.Provider.Name, "model", rm.Model.Name, "error", err)
			failures = append(failures, map[string]interface{}{
				"model":    rm.Model.Name,
				"provider": rm.Provider.Name,
				"status":   500,
				"message":  "internal error: failed to get api key",
			})
			continue
		}

		if limited, remaining := e.rateLimits.IsLimited(rm.Provider.ID); limited {
			e.logger.Warn("skipping provider: rate limited", "provider", rm.Provider.Name, "model", rm.Model.Name, "remaining", remaining.Round(time.Second))
			failures = append(failures, map[string]interface{}{
				"model":    rm.Model.Name,
				"provider": rm.Provider.Name,
				"status":   429,
				"message":  fmt.Sprintf("rate limited, retry in %s", remaining.Round(time.Second)),
			})
			continue
		}

		if rm.Provider.Disabled {
			e.logger.Warn("skipping provider: disabled", "provider", rm.Provider.Name, "model", rm.Model.Name)
			failures = append(failures, map[string]interface{}{
				"model":    rm.Model.Name,
				"provider": rm.Provider.Name,
				"status":   503,
				"message":  "provider disabled",
			})
			continue
		}

		for retry := 0; retry <= vm.MaxRetries; retry++ {
			if retry > 0 {
				time.Sleep(time.Duration(retry) * time.Second)
			}

			streamResp, httpResp, err := e.sendStreamRequest(r, rm, apiKey, req)
			if err == nil {
				e.recordModelSuccess(rm)

				e.logRequest(RequestLog{
					Type:          "proxy",
					Status:        "streaming",
					Timestamp:     start,
					RequestID:     requestID,
					VirtualModel:  req.Model,
					ProviderName:  rm.Provider.Name,
					ModelName:     rm.Model.Name,
					StatusCode:    0,
					FallbackCount: i,
					RetryCount:    retry,
				})

				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")
				w.Header().Set("X-Request-ID", requestID)
				w.Header().Set("X-Provider", rm.Provider.Name)
				w.Header().Set("X-Model", rm.Model.Name)

				flusher, ok := w.(http.Flusher)
				if !ok {
					http.Error(w, "streaming not supported", http.StatusInternalServerError)
					return
				}

				onProgress := func(p StreamProgress) {
					e.logRequest(RequestLog{
						Type:            "proxy",
						Status:          "streaming",
						Timestamp:       start,
						RequestID:       requestID,
						VirtualModel:    req.Model,
						ProviderName:    rm.Provider.Name,
						ModelName:       rm.Model.Name,
						StatusCode:      0,
						InputTokens:     p.InputTokens,
						OutputTokens:    p.OutputTokens,
						CachedTokens:    p.CachedTokens,
						ReasoningTokens: p.ReasoningTokens,
						FallbackCount:   i,
						RetryCount:      retry,
					})
				}

				var usage *Usage
				if rm.Provider.APIType == models.APITypeAnthropic {
					usage = e.streamAnthropicToOpenAI(w, flusher, streamResp, rm.Model.Name, requestID, onProgress)
				} else {
					usage = e.streamPassthrough(w, flusher, streamResp, httpResp, onProgress)
				}

				log := RequestLog{
					Type:           "proxy",
					Status:         "completed",
					Timestamp:      start,
					RequestID:      requestID,
					VirtualModel:   req.Model,
					ProviderName:   rm.Provider.Name,
					ModelName:      rm.Model.Name,
					StatusCode:     http.StatusOK,
					Latency:        time.Since(start),
					RTKIntercepted: rtkIntercepted,
					RTKSavedTokens: rtkSavedTokens,
					FallbackCount:  i,
					RetryCount:     retry,
				}
				if usage != nil {
					log.InputTokens = usage.PromptTokens
					log.OutputTokens = usage.CompletionTokens
					if usage.PromptTokensDetails != nil {
						log.CachedTokens = usage.PromptTokensDetails.CachedTokens
					}
					log.ReasoningTokens = extractReasoningTokens(usage)
				}
				e.logRequest(log)
				return
			}

			providerErr := e.processProviderError(r.Context(), err, rm)

			e.logRequest(RequestLog{
				Type:          "proxy",
				Timestamp:     start,
				RequestID:     requestID,
				VirtualModel:  req.Model,
				ProviderName:  rm.Provider.Name,
				ModelName:     rm.Model.Name,
				StatusCode:    providerErr.StatusCode,
				ErrorMessage:  providerErr.Message,
				FallbackCount: i,
				RetryCount:    retry,
			})

			if !shouldRetry(providerErr.StatusCode, retryOnStatus) {
				break
			}
		}

		failures = append(failures, map[string]interface{}{
			"model":    rm.Model.Name,
			"provider": rm.Provider.Name,
			"status":   502,
			"message":  "request failed",
		})
	}

	e.logRequest(RequestLog{
		Type:         "proxy",
		Status:       "failed",
		Timestamp:    start,
		RequestID:    requestID,
		VirtualModel: req.Model,
		StatusCode:   http.StatusBadGateway,
		Latency:      time.Since(start),
		ErrorMessage: "all models failed",
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": "all models failed",
			"type":    "bad_gateway",
		},
		"failures": failures,
	})
}

func (e *Engine) sendRequest(r *http.Request, rm service.ResolvedModel, apiKey string, req ChatCompletionRequest) (*ChatCompletionResponse, *SendRequestResult, error) {
	var resp *ChatCompletionResponse
	var err error
	sessionID := sessionIDForRequest(r)

	if rm.Provider.APIType == models.APITypeAnthropic {
		anthReq := OpenAIToAnthropic(req)
		anthReq.Model = rm.Model.Name
		applyReasoningEffortToAnthropic(&anthReq, rm.ReasoningEffort)
		anthResp, err2 := e.anthropicClient.ChatCompletion(rm.Provider.BaseURL, apiKey, anthReq)
		if err2 != nil {
			return nil, nil, err2
		}
		result := AnthropicToOpenAI(anthResp, rm.Model.Name)
		resp = &result
	} else if rm.Provider.APIType == models.APITypeCloudflare {
		// Cloudflare Workers AI: OpenAI-compatible wire format.
		// Explicit branch (not generic else) to allow per-provider hooks
		// without re-plumbing the engine: rate limit format, error normalization,
		// streaming diffs, custom retry-after parsing. Body identical to OpenAI
		// else branch today; differences will land here as needed.
		req.Model = rm.Model.Name
		if rm.ReasoningEffort != "" {
			req.ReasoningEffort = &rm.ReasoningEffort
		}
		resp, err = e.openaiClient.ChatCompletion(rm.Provider.BaseURL, apiKey, sessionID, req)
		if err != nil {
			return nil, nil, err
		}
	} else if rm.Provider.APIType == models.APITypeOllama {
		// Ollama local: OpenAI-compatible wire format.
		// Explicit branch (not generic else) to allow per-provider hooks
		// without re-plumbing the engine: tool calling parity,
		// context-length differences, model-not-found fallback to tag-pull.
		req.Model = rm.Model.Name
		if rm.ReasoningEffort != "" {
			req.ReasoningEffort = &rm.ReasoningEffort
		}
		resp, err = e.openaiClient.ChatCompletion(rm.Provider.BaseURL, apiKey, sessionID, req)
		if err != nil {
			return nil, nil, err
		}
	} else if rm.Provider.APIType == models.APITypeOllamaCloud {
		// Ollama Cloud (ollama.com): native Ollama format at /api/chat.
		// Different from local Ollama which supports OpenAI-compatible /v1.
		req.Model = rm.Model.Name
		if rm.ReasoningEffort != "" {
			req.ReasoningEffort = &rm.ReasoningEffort
		}
		resp, err = e.ollamaCloudClient.ChatCompletion(rm.Provider.BaseURL, apiKey, req)
		if err != nil {
			return nil, nil, err
		}
	} else if rm.Provider.APIType == models.APITypeCodex {
		// ChatGPT subscription backend (codex): Responses API over OAuth.
		// Explicit branch (not generic else) to allow per-provider hooks
		// without re-plumbing the engine: OAuth bearer + ChatGPT-Account-ID
		// headers, 401 → RefreshNow → single retry (auth failure is not
		// quota: no cooldown, no model disable), effort-suffix models.
		// apiKey is ignored here; the OAuth access token is the credential.
		ctx := context.Background()
		if r != nil {
			ctx = r.Context()
		}
		row, credErr := e.getCodexCredentials(ctx, rm.Provider)
		if credErr != nil {
			return nil, nil, &ProviderError{StatusCode: http.StatusUnauthorized, Message: credErr.Error()}
		}
		req.Model = rm.Model.Name
		req.ReasoningEffort = nil // translator resolves effort itself (suffix or explicit field)
		if rm.ReasoningEffort != "" {
			req.ReasoningEffort = &rm.ReasoningEffort
		}
		sessionID := sessionIDForRequest(r)
		resp, err = e.codexClient.ChatCompletion(ctx, rm.Provider.BaseURL, row.AccessToken, row.AccountID, sessionID, req)
		if isCodexUnauthorized(err) {
			// One unconditional token refresh → single retry (§4.5). The
			// access token can die server-side before its policy expiry.
			fresh, refreshErr := e.oauthService.RefreshNow(ctx, &rm.Provider)
			if refreshErr != nil {
				return nil, nil, codexAuthError(rm.Provider.Name, refreshErr)
			}
			if fresh == nil || fresh.AccessToken == "" {
				return nil, nil, codexAuthError(rm.Provider.Name, errors.New("token refresh returned no access token"))
			}
			resp, err = e.codexClient.ChatCompletion(ctx, rm.Provider.BaseURL, fresh.AccessToken, fresh.AccountID, sessionID, req)
		}
		if err != nil {
			return nil, nil, err
		}
	} else {
		req.Model = rm.Model.Name
		if rm.ReasoningEffort != "" {
			req.ReasoningEffort = &rm.ReasoningEffort
		}
		resp, err = e.openaiClient.ChatCompletion(rm.Provider.BaseURL, apiKey, sessionID, req)
		if err != nil {
			return nil, nil, err
		}
	}

	result := &SendRequestResult{Response: resp}

	if e.caveman.IsEnabled() && resp != nil && len(resp.Choices) > 0 {
		if content, ok := resp.Choices[0].Message.Content.(string); ok && content != "" {
			compressed, intercepted := e.caveman.InterceptOutput(content)
			if intercepted {
				resp.Choices[0].Message.Content = compressed
				result.CavemanIntercepted = true
				result.CavemanSavedTokens = estimateTokens(content) - estimateTokens(compressed)
			}
		}
	}

	return resp, result, nil
}

func (e *Engine) sendStreamRequest(r *http.Request, rm service.ResolvedModel, apiKey string, req ChatCompletionRequest) (io.ReadCloser, *http.Response, error) {
	sessionID := sessionIDForRequest(r)
	if rm.Provider.APIType == models.APITypeAnthropic {
		anthReq := OpenAIToAnthropic(req)
		anthReq.Model = rm.Model.Name
		applyReasoningEffortToAnthropic(&anthReq, rm.ReasoningEffort)
		return e.anthropicClient.ChatCompletionStream(rm.Provider.BaseURL, apiKey, anthReq)
	} else if rm.Provider.APIType == models.APITypeCloudflare {
		// Cloudflare Workers AI: OpenAI-compatible wire format.
		// Explicit branch (not generic else) to allow per-provider hooks
		// without re-plumbing the engine: rate limit format, error normalization,
		// streaming diffs, custom retry-after parsing. Body identical to OpenAI
		// else branch today; differences will land here as needed.
		req.Model = rm.Model.Name
		if rm.ReasoningEffort != "" {
			req.ReasoningEffort = &rm.ReasoningEffort
		}
		return e.openaiClient.ChatCompletionStream(rm.Provider.BaseURL, apiKey, sessionID, req)
	} else if rm.Provider.APIType == models.APITypeOllama {
		// Ollama local: OpenAI-compatible wire format.
		// Explicit branch (not generic else) to allow per-provider hooks
		// without re-plumbing the engine: tool calling parity,
		// context-length differences, model-not-found fallback to tag-pull.
		req.Model = rm.Model.Name
		if rm.ReasoningEffort != "" {
			req.ReasoningEffort = &rm.ReasoningEffort
		}
		return e.openaiClient.ChatCompletionStream(rm.Provider.BaseURL, apiKey, sessionID, req)
	} else if rm.Provider.APIType == models.APITypeOllamaCloud {
		// Ollama Cloud (ollama.com): native Ollama format at /api/chat.
		req.Model = rm.Model.Name
		if rm.ReasoningEffort != "" {
			req.ReasoningEffort = &rm.ReasoningEffort
		}
		return e.ollamaCloudClient.ChatCompletionStream(rm.Provider.BaseURL, apiKey, req)
	} else if rm.Provider.APIType == models.APITypeCodex {
		// ChatGPT subscription backend (codex): Responses API over OAuth.
		// Explicit branch (not generic else) to allow per-provider hooks
		// without re-plumbing the engine (§4.6). ChatCompletionStream returns
		// an io.Pipe already translated to OpenAI chat-completions SSE, so
		// the engine's streamPassthrough parser handles the body unchanged —
		// the branch only differs in request dispatch. 401 → RefreshNow →
		// single retry; no cooldown / model disable for auth failures.
		// apiKey is ignored here; the OAuth access token is the credential.
		ctx := context.Background()
		if r != nil {
			ctx = r.Context()
		}
		row, credErr := e.getCodexCredentials(ctx, rm.Provider)
		if credErr != nil {
			return nil, nil, &ProviderError{StatusCode: http.StatusUnauthorized, Message: credErr.Error()}
		}
		req.Model = rm.Model.Name
		req.ReasoningEffort = nil // translator resolves effort itself (suffix or explicit field)
		if rm.ReasoningEffort != "" {
			req.ReasoningEffort = &rm.ReasoningEffort
		}
		sessionID := sessionIDForRequest(r) // same per-conversation id as the opencode header; feeds prompt_cache_key
		body, httpResp, err := e.codexClient.ChatCompletionStream(ctx, rm.Provider.BaseURL, row.AccessToken, row.AccountID, sessionID, req)
		if isCodexUnauthorized(err) {
			// One unconditional token refresh → single retry (§4.5).
			fresh, refreshErr := e.oauthService.RefreshNow(ctx, &rm.Provider)
			if refreshErr != nil {
				return nil, nil, codexAuthError(rm.Provider.Name, refreshErr)
			}
			if fresh == nil || fresh.AccessToken == "" {
				return nil, nil, codexAuthError(rm.Provider.Name, errors.New("token refresh returned no access token"))
			}
			body, httpResp, err = e.codexClient.ChatCompletionStream(ctx, rm.Provider.BaseURL, fresh.AccessToken, fresh.AccountID, sessionID, req)
		}
		return body, httpResp, err
	}

	req.Model = rm.Model.Name
	if rm.ReasoningEffort != "" {
		req.ReasoningEffort = &rm.ReasoningEffort
	}
	return e.openaiClient.ChatCompletionStream(rm.Provider.BaseURL, apiKey, sessionID, req)
}

func (e *Engine) streamPassthrough(w http.ResponseWriter, flusher http.Flusher, body io.ReadCloser, httpResp *http.Response, onProgress func(StreamProgress)) *Usage {
	defer body.Close()

	for key, values := range httpResp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}

	var usage *Usage
	var bytesTotal int
	reader := bufio.NewReader(body)

	if onProgress != nil {
		done := make(chan struct{})
		defer close(done)
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					estimated := bytesTotal / 4
					onProgress(StreamProgress{OutputTokens: estimated})
				case <-done:
					return
				}
			}
		}()
	}

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			bytesTotal += len(line)
			w.Write([]byte(line))
			flusher.Flush()

			trimmed := strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(trimmed, "data: ") {
				data := strings.TrimPrefix(trimmed, "data: ")
				if data != "[DONE]" {
					var chunk StreamChunk
					if jsonErr := json.Unmarshal([]byte(data), &chunk); jsonErr == nil && chunk.Usage != nil {
						usage = chunk.Usage
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	return usage
}

func (e *Engine) streamAnthropicToOpenAI(w http.ResponseWriter, flusher http.Flusher, body io.ReadCloser, model string, requestID string, onProgress func(StreamProgress)) *Usage {
	state := &ClaudeStreamState{
		Model:           model,
		RequestID:       requestID,
		ServerToolIndex: -1,
		ToolCalls:       make(map[int]*ToolCallState),
	}
	events := ParseAnthropicSSEStream(body)

	if onProgress != nil {
		done := make(chan struct{})
		defer close(done)
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					var p StreamProgress
					if state.Usage != nil {
						p.InputTokens = state.Usage.PromptTokens
						p.OutputTokens = state.Usage.CompletionTokens
						p.CachedTokens = 0
						if state.Usage.PromptTokensDetails != nil {
							p.CachedTokens = state.Usage.PromptTokensDetails.CachedTokens
						}
						p.ReasoningTokens = extractReasoningTokens(state.Usage)
					}
					onProgress(p)
				case <-done:
					return
				}
			}
		}()
	}

	for event := range events {
		chunks := state.ProcessEvent(event)
		for _, chunk := range chunks {
			data, err := json.Marshal(chunk)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
	return state.Usage
}

func extractReasoningTokens(usage *Usage) int {
	if usage == nil {
		return 0
	}
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens > 0 {
		return usage.CompletionTokensDetails.ReasoningTokens
	}
	r := usage.TotalTokens - usage.PromptTokens - usage.CompletionTokens
	if r < 0 {
		return 0
	}
	return r
}

func (e *Engine) logRequest(log RequestLog) {
	attrs := []any{
		"request_id", log.RequestID,
		"virtual_model", log.VirtualModel,
		"provider", log.ProviderName,
		"model", log.ModelName,
		"status", log.StatusCode,
		"latency", log.Latency.String(),
		"input_tokens", log.InputTokens,
		"output_tokens", log.OutputTokens,
		"cached_tokens", log.CachedTokens,
		"fallback", log.FallbackCount,
		"retry", log.RetryCount,
	}

	if log.Status != "" {
		attrs = append(attrs, "stream_status", log.Status)
	}

	if log.ErrorMessage != "" {
		attrs = append(attrs, "error", log.ErrorMessage)
	}

	if log.StatusCode >= 200 && log.StatusCode < 300 {
		e.logger.Info("request completed", attrs...)
	} else if log.Status == "streaming" {
		e.logger.Info("streaming", attrs...)
	} else {
		e.logger.Error("request failed", attrs...)
	}

	if e.logChan != nil {
		select {
		case e.logChan <- log:
		default:
		}
	}
}

// unavailableModelPatterns matches provider-side model-existence/availability
// complaints. Deliberately conservative: these errors mean OUR catalog entry is
// stale (model names sent upstream always come from the DB, never raw client
// input), not that the client sent a bad request.
var unavailableModelPatterns = []string{
	"model is unavailable",
	"model unavailable",
	"model not found",
	"model_not_found",
	"no such model",
	"unknown model",
	"model does not exist",
}

// isModelUnavailableError reports whether a provider error is an upstream
// complaint that the model itself is unavailable/gone (not a generic client
// error). Only 400/404 qualify.
func isModelUnavailableError(pe *ProviderError) bool {
	if pe.StatusCode != http.StatusBadRequest && pe.StatusCode != http.StatusNotFound {
		return false
	}
	haystack := strings.ToLower(pe.Message + " " + string(pe.RawBody))
	for _, p := range unavailableModelPatterns {
		if strings.Contains(haystack, p) {
			return true
		}
	}
	return false
}

// processProviderError normalizes err to *ProviderError (via errors.As,
// unknown errors → 500) and applies all side effects:
//   - 402 (SubscriptionRequiredError) → circuitBreaker.DisableModelPermanent
//   - 429 → rateLimits.MarkLimited (RetryAfter, then classifyRateLimit fallback)
//   - >=500 → circuitBreaker.Record5xx
//   - 400/404 "model unavailable" → circuitBreaker.RecordUnavailable
//
// Returns the normalized error for the caller's logging/retry logic.
func (e *Engine) processProviderError(ctx context.Context, err error, rm service.ResolvedModel) *ProviderError {
	var subErr *SubscriptionRequiredError
	if errors.As(err, &subErr) {
		if cb := e.circuitBreaker; cb != nil {
			cb.DisableModelPermanent(ctx, rm.Provider.ID, rm.Model.Name)
		}
		return subErr.ProviderError
	}

	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		providerErr = &ProviderError{StatusCode: 500, Message: err.Error()}
	}

	if providerErr.StatusCode == http.StatusTooManyRequests {
		cooldown := providerErr.RetryAfter
		if cooldown == 0 {
			cooldown = classifyRateLimit(providerErr.RawBody)
		}
		e.rateLimits.MarkLimited(rm.Provider.ID, cooldown)
	}

	if e.circuitBreaker != nil && providerErr.StatusCode >= 500 {
		e.circuitBreaker.Record5xx(ctx, rm.Provider.ID, rm.Model.Name)
	}

	if e.circuitBreaker != nil && isModelUnavailableError(providerErr) {
		e.circuitBreaker.RecordUnavailable(ctx, rm.Provider.ID, rm.Model.Name)
	}

	return providerErr
}

// recordModelSuccess notifies the circuit breaker that an upstream model
// responded successfully, clearing any escalation strikes for it.
func (e *Engine) recordModelSuccess(rm service.ResolvedModel) {
	if cb := e.circuitBreaker; cb != nil {
		cb.RecordSuccess(rm.Provider.ID, rm.Model.Name)
	}
}

func shouldRetry(statusCode int, retryOnStatus []int) bool {
	if len(retryOnStatus) == 0 {
		retryOnStatus = []int{429, 500, 502, 503, 504}
	}
	for _, s := range retryOnStatus {
		if s == statusCode {
			return true
		}
	}
	return false
}

func (e *Engine) interceptMessages(messages []Message) InterceptResult {
	result := make([]Message, len(messages))
	copy(result, messages)

	var intercepted bool
	var savedTokens int

	for i, msg := range result {
		if msg.Role == "tool" {
			toolName := ""
			content := ""

			if msg.ToolCallID != "" {
				toolName = "bash"
			}

			switch c := msg.Content.(type) {
			case string:
				content = c
			case []interface{}:
				for _, block := range c {
					if m, ok := block.(map[string]interface{}); ok {
						if t, ok := m["type"].(string); ok && t == "text" {
							if text, ok := m["text"].(string); ok {
								content = text
							}
						}
					}
				}
			}

			if content != "" && toolName != "" {
				compressed, ok := e.rtk.InterceptToolResult(toolName, "", content)
				if ok {
					result[i].Content = compressed
					intercepted = true
					savedTokens += estimateTokens(content) - estimateTokens(compressed)
				}
			}
		}
	}

	return InterceptResult{
		Messages:       result,
		RTKIntercepted: intercepted,
		RTKSavedTokens: savedTokens,
	}
}

func ForwardHeaders(dst http.ResponseWriter, src *http.Response) {
	for key, values := range src.Header {
		keyLower := strings.ToLower(key)
		if keyLower == "transfer-encoding" || keyLower == "connection" {
			continue
		}
		for _, v := range values {
			dst.Header().Add(key, v)
		}
	}
}

func CopyRequestHeaders(dst *http.Request, src *http.Request) {
	for key, values := range src.Header {
		keyLower := strings.ToLower(key)
		if keyLower == "authorization" || keyLower == "host" || keyLower == "connection" {
			continue
		}
		for _, v := range values {
			dst.Header.Add(key, v)
		}
	}
}

func (e *Engine) HandleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = fmt.Sprintf("req_%d", time.Now().UnixNano())
	}

	var anthReq AnthropicRequest
	if err := json.NewDecoder(r.Body).Decode(&anthReq); err != nil {
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"invalid request body"}}`, http.StatusBadRequest)
		return
	}

	vm, resolvedModels, err := e.resolveRequestRoute(r.Context(), anthReq.Model)
	if err != nil {
		e.logger.Error("resolve model route", "error", err, "model", anthReq.Model)
		http.Error(w, `{"type":"error","error":{"type":"api_error","message":"internal error"}}`, http.StatusInternalServerError)
		return
	}
	if vm == nil && resolvedModels == nil {
		http.Error(w, fmt.Sprintf(`{"type":"error","error":{"type":"not_found_error","message":"model not found: %s"}}`, anthReq.Model), http.StatusNotFound)
		return
	}

	if len(resolvedModels) == 0 {
		http.Error(w, `{"type":"error","error":{"type":"not_found_error","message":"no matching models found"}}`, http.StatusNotFound)
		return
	}

	e.logRequest(RequestLog{
		Type:         "incoming",
		Timestamp:    start,
		RequestID:    requestID,
		VirtualModel: anthReq.Model,
		StatusCode:   http.StatusOK,
	})

	openReq := AnthropicRequestToOpenAI(anthReq)

	var retryOnStatus []int
	if len(vm.RetryOnStatus) > 0 {
		json.Unmarshal(vm.RetryOnStatus, &retryOnStatus)
	}

	var failures []map[string]interface{}

	for i, rm := range resolvedModels {
		apiKey, err := e.getAPIKey(r.Context(), rm.Provider)
		if err != nil {
			e.logger.Warn("skipping provider: no api key", "provider", rm.Provider.Name, "model", rm.Model.Name, "error", err)
			failures = append(failures, map[string]interface{}{
				"model":    rm.Model.Name,
				"provider": rm.Provider.Name,
				"status":   500,
				"message":  "internal error: failed to get api key",
			})
			continue
		}

		if limited, remaining := e.rateLimits.IsLimited(rm.Provider.ID); limited {
			e.logger.Warn("skipping provider: rate limited", "provider", rm.Provider.Name, "model", rm.Model.Name, "remaining", remaining.Round(time.Second))
			failures = append(failures, map[string]interface{}{
				"model":    rm.Model.Name,
				"provider": rm.Provider.Name,
				"status":   429,
				"message":  fmt.Sprintf("rate limited, retry in %s", remaining.Round(time.Second)),
			})
			continue
		}

		if rm.Provider.Disabled {
			e.logger.Warn("skipping provider: disabled", "provider", rm.Provider.Name, "model", rm.Model.Name)
			failures = append(failures, map[string]interface{}{
				"model":    rm.Model.Name,
				"provider": rm.Provider.Name,
				"status":   503,
				"message":  "provider disabled",
			})
			continue
		}

		for retry := 0; retry <= vm.MaxRetries; retry++ {
			if retry > 0 {
				time.Sleep(time.Duration(retry) * time.Second)
			}

			openResp, reqResult, err := e.sendRequest(r, rm, apiKey, openReq)
			if reqResult == nil {
				reqResult = &SendRequestResult{}
			}
			if err == nil {
				e.recordModelSuccess(rm)

				anthResp := OpenAIResponseToAnthropic(*openResp, rm.Model.Name)

				e.logRequest(RequestLog{
					Type:         "proxy",
					Timestamp:    start,
					RequestID:    requestID,
					VirtualModel: anthReq.Model,
					ProviderName: rm.Provider.Name,
					ModelName:    rm.Model.Name,
					StatusCode:   http.StatusOK,
					Latency:      time.Since(start),
					InputTokens:  openResp.Usage.PromptTokens,
					OutputTokens: openResp.Usage.CompletionTokens,
					CachedTokens: func() int {
						if openResp.Usage.PromptTokensDetails != nil {
							return openResp.Usage.PromptTokensDetails.CachedTokens
						}
						return 0
					}(),
					ReasoningTokens:    extractReasoningTokens(&openResp.Usage),
					CavemanIntercepted: reqResult.CavemanIntercepted,
					CavemanSavedTokens: reqResult.CavemanSavedTokens,
					FallbackCount:      i,
					RetryCount:         retry,
				})

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-ID", requestID)
				json.NewEncoder(w).Encode(anthResp)
				return
			}

			providerErr := e.processProviderError(r.Context(), err, rm)

			e.logRequest(RequestLog{
				Type:          "proxy",
				Timestamp:     start,
				RequestID:     requestID,
				VirtualModel:  anthReq.Model,
				ProviderName:  rm.Provider.Name,
				ModelName:     rm.Model.Name,
				StatusCode:    providerErr.StatusCode,
				Latency:       time.Since(start),
				ErrorMessage:  providerErr.Message,
				FallbackCount: i,
				RetryCount:    retry,
			})

			if !shouldRetry(providerErr.StatusCode, retryOnStatus) {
				break
			}
		}

		failures = append(failures, map[string]interface{}{
			"model":    rm.Model.Name,
			"provider": rm.Provider.Name,
			"status":   500,
			"message":  "request failed after retries",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "api_error",
			"message": "all models failed",
		},
		"failures": failures,
	})
}

func (e *Engine) HandleAnthropicMessagesStream(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = fmt.Sprintf("req_%d", time.Now().UnixNano())
	}

	var anthReq AnthropicRequest
	if err := json.NewDecoder(r.Body).Decode(&anthReq); err != nil {
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"invalid request body"}}`, http.StatusBadRequest)
		return
	}

	vm, resolvedModels, err := e.resolveRequestRoute(r.Context(), anthReq.Model)
	if err != nil {
		e.logger.Error("resolve model route", "error", err, "model", anthReq.Model)
		http.Error(w, `{"type":"error","error":{"type":"api_error","message":"internal error"}}`, http.StatusInternalServerError)
		return
	}
	if vm == nil && resolvedModels == nil {
		http.Error(w, `{"type":"error","error":{"type":"not_found_error","message":"model not found"}}`, http.StatusNotFound)
		return
	}

	if len(resolvedModels) == 0 {
		http.Error(w, `{"type":"error","error":{"type":"not_found_error","message":"no matching models found"}}`, http.StatusNotFound)
		return
	}

	e.logRequest(RequestLog{
		Type:         "incoming",
		Timestamp:    start,
		RequestID:    requestID,
		VirtualModel: anthReq.Model,
		StatusCode:   http.StatusOK,
	})

	virtualModel := anthReq.Model
	openReq := AnthropicRequestToOpenAI(anthReq)
	openReq.Stream = true

	var retryOnStatus []int
	if len(vm.RetryOnStatus) > 0 {
		json.Unmarshal(vm.RetryOnStatus, &retryOnStatus)
	}

	var failures []map[string]interface{}

	for i, rm := range resolvedModels {
		apiKey, err := e.getAPIKey(r.Context(), rm.Provider)
		if err != nil {
			failures = append(failures, map[string]interface{}{
				"model":    rm.Model.Name,
				"provider": rm.Provider.Name,
				"status":   500,
				"message":  "internal error: failed to get api key",
			})
			continue
		}

		if limited, _ := e.rateLimits.IsLimited(rm.Provider.ID); limited {
			failures = append(failures, map[string]interface{}{
				"model":    rm.Model.Name,
				"provider": rm.Provider.Name,
				"status":   429,
				"message":  "rate limited",
			})
			continue
		}

		if rm.Provider.Disabled {
			e.logger.Warn("skipping provider: disabled", "provider", rm.Provider.Name, "model", rm.Model.Name)
			failures = append(failures, map[string]interface{}{
				"model":    rm.Model.Name,
				"provider": rm.Provider.Name,
				"status":   503,
				"message":  "provider disabled",
			})
			continue
		}

		for retry := 0; retry <= vm.MaxRetries; retry++ {
			if retry > 0 {
				time.Sleep(time.Duration(retry) * time.Second)
			}

			var lastErr error
			if rm.Provider.APIType == models.APITypeAnthropic {
				anthReq.Model = rm.Model.Name
				applyReasoningEffortToAnthropic(&anthReq, rm.ReasoningEffort)
				streamBody, _, err := e.anthropicClient.ChatCompletionStream(rm.Provider.BaseURL, apiKey, anthReq)
				if err == nil {
					e.recordModelSuccess(rm)

					e.logRequest(RequestLog{
						Type:          "proxy",
						Status:        "streaming",
						Timestamp:     start,
						RequestID:     requestID,
						VirtualModel:  anthReq.Model,
						ProviderName:  rm.Provider.Name,
						ModelName:     rm.Model.Name,
						StatusCode:    0,
						FallbackCount: i,
						RetryCount:    retry,
					})

					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set("Cache-Control", "no-cache")
					w.Header().Set("Connection", "keep-alive")

					flusher, ok := w.(http.Flusher)
					if !ok {
						http.Error(w, "streaming not supported", http.StatusInternalServerError)
						return
					}

					onProgress := func(p StreamProgress) {
						e.logRequest(RequestLog{
							Type:            "proxy",
							Status:          "streaming",
							Timestamp:       start,
							RequestID:       requestID,
							VirtualModel:    anthReq.Model,
							ProviderName:    rm.Provider.Name,
							ModelName:       rm.Model.Name,
							StatusCode:      0,
							InputTokens:     p.InputTokens,
							OutputTokens:    p.OutputTokens,
							CachedTokens:    p.CachedTokens,
							ReasoningTokens: p.ReasoningTokens,
							FallbackCount:   i,
							RetryCount:      retry,
						})
					}

					usage := e.streamAnthropicPassthrough(w, flusher, streamBody, onProgress)

					log := RequestLog{
						Type:          "proxy",
						Status:        "completed",
						Timestamp:     start,
						RequestID:     requestID,
						VirtualModel:  anthReq.Model,
						ProviderName:  rm.Provider.Name,
						ModelName:     rm.Model.Name,
						StatusCode:    http.StatusOK,
						Latency:       time.Since(start),
						FallbackCount: i,
						RetryCount:    retry,
					}
					if usage != nil {
						log.InputTokens = usage.PromptTokens
						log.OutputTokens = usage.CompletionTokens
						if usage.PromptTokensDetails != nil {
							log.CachedTokens = usage.PromptTokensDetails.CachedTokens
						}
						log.ReasoningTokens = extractReasoningTokens(usage)
					}
					e.logRequest(log)
					return
				}
				lastErr = err
			} else {
				openReq.Model = rm.Model.Name
				streamBody, _, err := e.openaiClient.ChatCompletionStream(rm.Provider.BaseURL, apiKey, requestID, openReq)
				if err == nil {
					e.recordModelSuccess(rm)

					e.logRequest(RequestLog{
						Type:          "proxy",
						Status:        "streaming",
						Timestamp:     start,
						RequestID:     requestID,
						VirtualModel:  anthReq.Model,
						ProviderName:  rm.Provider.Name,
						ModelName:     rm.Model.Name,
						StatusCode:    0,
						FallbackCount: i,
						RetryCount:    retry,
					})

					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set("Cache-Control", "no-cache")
					w.Header().Set("Connection", "keep-alive")

					flusher, ok := w.(http.Flusher)
					if !ok {
						http.Error(w, "streaming not supported", http.StatusInternalServerError)
						return
					}

					onProgress := func(p StreamProgress) {
						e.logRequest(RequestLog{
							Type:            "proxy",
							Status:          "streaming",
							Timestamp:       start,
							RequestID:       requestID,
							VirtualModel:    anthReq.Model,
							ProviderName:    rm.Provider.Name,
							ModelName:       rm.Model.Name,
							StatusCode:      0,
							InputTokens:     p.InputTokens,
							OutputTokens:    p.OutputTokens,
							CachedTokens:    p.CachedTokens,
							ReasoningTokens: p.ReasoningTokens,
							FallbackCount:   i,
							RetryCount:      retry,
						})
					}

					usage := e.streamOpenAIToAnthropic(w, flusher, streamBody, rm.Model.Name, requestID, onProgress)

					log := RequestLog{
						Type:          "proxy",
						Status:        "completed",
						Timestamp:     start,
						RequestID:     requestID,
						VirtualModel:  anthReq.Model,
						ProviderName:  rm.Provider.Name,
						ModelName:     rm.Model.Name,
						StatusCode:    http.StatusOK,
						Latency:       time.Since(start),
						FallbackCount: i,
						RetryCount:    retry,
					}
					if usage != nil {
						log.InputTokens = usage.PromptTokens
						log.OutputTokens = usage.CompletionTokens
						if usage.PromptTokensDetails != nil {
							log.CachedTokens = usage.PromptTokensDetails.CachedTokens
						}
						log.ReasoningTokens = extractReasoningTokens(usage)
					}
					e.logRequest(log)
					return
				}
				lastErr = err
			}

			providerErr := e.processProviderError(r.Context(), lastErr, rm)

			e.logRequest(RequestLog{
				Type:          "proxy",
				Timestamp:     start,
				RequestID:     requestID,
				VirtualModel:  anthReq.Model,
				ProviderName:  rm.Provider.Name,
				ModelName:     rm.Model.Name,
				StatusCode:    providerErr.StatusCode,
				ErrorMessage:  providerErr.Message,
				FallbackCount: i,
				RetryCount:    retry,
			})

			if !shouldRetry(providerErr.StatusCode, retryOnStatus) {
				break
			}
		}

		failures = append(failures, map[string]interface{}{
			"model":    rm.Model.Name,
			"provider": rm.Provider.Name,
			"status":   502,
			"message":  "request failed",
		})
	}

	e.logRequest(RequestLog{
		Type:         "proxy",
		Status:       "failed",
		Timestamp:    start,
		RequestID:    requestID,
		VirtualModel: virtualModel,
		StatusCode:   http.StatusBadGateway,
		Latency:      time.Since(start),
		ErrorMessage: "all models failed",
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "api_error",
			"message": "all models failed",
		},
		"failures": failures,
	})
}

func (e *Engine) streamAnthropicPassthrough(w http.ResponseWriter, flusher http.Flusher, body io.ReadCloser, onProgress func(StreamProgress)) *Usage {
	defer body.Close()

	parts := anthropicUsageParts{}
	reader := bufio.NewReader(body)

	if onProgress != nil {
		done := make(chan struct{})
		defer close(done)
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					usage := parts.build()
					var progress StreamProgress
					if usage != nil {
						progress.InputTokens = usage.PromptTokens
						progress.OutputTokens = usage.CompletionTokens
						if usage.PromptTokensDetails != nil {
							progress.CachedTokens = usage.PromptTokensDetails.CachedTokens
						}
					}
					onProgress(progress)
				case <-done:
					return
				}
			}
		}()
	}

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			w.Write([]byte(line))
			flusher.Flush()

			trimmed := strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(trimmed, "data: ") {
				data := strings.TrimPrefix(trimmed, "data: ")
				var event AnthropicStreamEvent
				if jsonErr := json.Unmarshal([]byte(data), &event); jsonErr == nil {
					// Usage accounting mirrors ClaudeStreamState's translator
					// path: presence-aware cumulative overwrite from the
					// top-level usage or one nested inside delta.
					if nested := extractNestedDeltaUsage(event.Delta); len(nested) > 0 {
						parts.applyRaw(nested)
					}
					if usageRaw := extractUsageRaw([]byte(data)); len(usageRaw) > 0 {
						parts.applyRaw(usageRaw)
					}
					if event.Type == "message_start" {
						if msgUsage := extractUsageRaw(event.Message); len(msgUsage) > 0 {
							if parts.applyRaw(msgUsage) {
								parts.seen = true
							}
						}
					}
				}
			}
		}
		if err != nil {
			break
		}
	}

	return parts.build()
}

func (e *Engine) streamOpenAIToAnthropic(w http.ResponseWriter, flusher http.Flusher, body io.ReadCloser, model string, requestID string, onProgress func(StreamProgress)) *Usage {
	defer body.Close()
	chunks := ParseSSEStream(body)
	var usage *Usage
	var bytesTotal int

	if onProgress != nil {
		done := make(chan struct{})
		defer close(done)
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					estimated := bytesTotal / 4
					onProgress(StreamProgress{OutputTokens: estimated})
				case <-done:
					return
				}
			}
		}()
	}

	for chunk := range chunks {
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		events := OpenAIStreamToAnthropicEvent(chunk, requestID)
		for _, event := range events {
			data, _ := json.Marshal(event)
			bytesTotal += len(data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
		}
	}
	return usage
}

func applyReasoningEffortToAnthropic(req *AnthropicRequest, effort string) {
	switch effort {
	case "none":
		req.Thinking = &AnthropicThinking{Type: "disabled"}
	case "":
	case "low":
		req.Thinking = &AnthropicThinking{Type: "enabled", BudgetTokens: 1024}
	case "medium":
		req.Thinking = &AnthropicThinking{Type: "enabled", BudgetTokens: 4096}
	case "high":
		req.Thinking = &AnthropicThinking{Type: "enabled", BudgetTokens: 8192}
	case "max":
		req.Thinking = &AnthropicThinking{Type: "enabled", BudgetTokens: 16384}
	default:
		req.Effort = &effort
	}
}
