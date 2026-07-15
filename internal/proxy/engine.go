package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/service"
)

type RequestLog struct {
	Timestamp      time.Time
	RequestID      string
	VirtualModel   string
	ClientKeyID    int64
	ProviderName   string
	ModelName      string
	StatusCode     int
	Latency        time.Duration
	InputTokens    int
	OutputTokens   int
	CachedTokens   int
	ReasoningTokens int
	ErrorMessage   string
	RetryCount     int
	FallbackCount  int
}

type Engine struct {
	vmService      service.VirtualModelService
	providerService service.ProviderService
	openaiClient   *OpenAIClient
	anthropicClient *AnthropicClient
	logger         *slog.Logger
	logChan        chan<- RequestLog
}

func NewEngine(
	vmService service.VirtualModelService,
	providerService service.ProviderService,
	logger *slog.Logger,
	logChan chan<- RequestLog,
) *Engine {
	return &Engine{
		vmService:       vmService,
		providerService: providerService,
		openaiClient:    NewOpenAIClient(),
		anthropicClient: NewAnthropicClient(),
		logger:          logger,
		logChan:         logChan,
	}
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

	vm, err := e.vmService.GetByName(r.Context(), req.Model)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if vm == nil {
		http.Error(w, fmt.Sprintf(`{"error":"model not found: %s"}`, req.Model), http.StatusNotFound)
		return
	}

	resolvedModels, err := e.vmService.ResolveModels(r.Context(), vm)
	if err != nil {
		http.Error(w, `{"error":"failed to resolve models"}`, http.StatusInternalServerError)
		return
	}

	if len(resolvedModels) == 0 {
		http.Error(w, `{"error":"no matching models found"}`, http.StatusNotFound)
		return
	}

	var failures []map[string]interface{}
	var retryOnStatus []int
	if len(vm.RetryOnStatus) > 0 {
		json.Unmarshal(vm.RetryOnStatus, &retryOnStatus)
	}

	for i, rm := range resolvedModels {
		apiKey, err := e.providerService.DecryptAPIKey(rm.Provider.APIKeyEncrypted)
		if err != nil {
			e.logger.Error("decrypt api key", "error", err, "provider", rm.Provider.Name)
			failures = append(failures, map[string]interface{}{
				"model":    rm.Model.Name,
				"provider": rm.Provider.Name,
				"status":   500,
				"message":  "internal error: failed to decrypt api key",
			})
			continue
		}

		for retry := 0; retry <= vm.MaxRetries; retry++ {
			if retry > 0 {
				time.Sleep(time.Duration(retry) * time.Second)
			}

			resp, err := e.sendRequest(r, rm, apiKey, req)
			if err == nil {
				e.logRequest(RequestLog{
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
					FallbackCount: i,
					RetryCount:    retry,
				})

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-ID", requestID)
				w.Header().Set("X-Provider", rm.Provider.Name)
				w.Header().Set("X-Model", rm.Model.Name)
				json.NewEncoder(w).Encode(resp)
				return
			}

			providerErr, ok := err.(*ProviderError)
			if !ok {
				providerErr = &ProviderError{StatusCode: 500, Message: err.Error()}
			}

			e.logRequest(RequestLog{
				Timestamp:    start,
				RequestID:    requestID,
				VirtualModel: req.Model,
				ProviderName: rm.Provider.Name,
				ModelName:    rm.Model.Name,
				StatusCode:   providerErr.StatusCode,
				Latency:      time.Since(start),
				ErrorMessage: providerErr.Message,
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

	e.logRequest(RequestLog{
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

	vm, err := e.vmService.GetByName(r.Context(), req.Model)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if vm == nil {
		http.Error(w, fmt.Sprintf(`{"error":"model not found: %s"}`, req.Model), http.StatusNotFound)
		return
	}

	resolvedModels, err := e.vmService.ResolveModels(r.Context(), vm)
	if err != nil {
		http.Error(w, `{"error":"failed to resolve models"}`, http.StatusInternalServerError)
		return
	}

	if len(resolvedModels) == 0 {
		http.Error(w, `{"error":"no matching models found"}`, http.StatusNotFound)
		return
	}

	var retryOnStatus []int
	if len(vm.RetryOnStatus) > 0 {
		json.Unmarshal(vm.RetryOnStatus, &retryOnStatus)
	}

	for i, rm := range resolvedModels {
		apiKey, err := e.providerService.DecryptAPIKey(rm.Provider.APIKeyEncrypted)
		if err != nil {
			continue
		}

		for retry := 0; retry <= vm.MaxRetries; retry++ {
			if retry > 0 {
				time.Sleep(time.Duration(retry) * time.Second)
			}

			streamResp, httpResp, err := e.sendStreamRequest(r, rm, apiKey, req)
			if err == nil {
				e.logRequest(RequestLog{
					Timestamp:    start,
					RequestID:    requestID,
					VirtualModel: req.Model,
					ProviderName: rm.Provider.Name,
					ModelName:    rm.Model.Name,
					StatusCode:   http.StatusOK,
					Latency:      time.Since(start),
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

				if rm.Provider.APIType == models.APITypeAnthropic {
					e.streamAnthropicToOpenAI(w, flusher, streamResp, rm.Model.Name, requestID)
				} else {
					e.streamPassthrough(w, flusher, streamResp, httpResp)
				}
				return
			}

			providerErr, ok := err.(*ProviderError)
			if !ok {
				providerErr = &ProviderError{StatusCode: 500, Message: err.Error()}
			}

			if !shouldRetry(providerErr.StatusCode, retryOnStatus) {
				break
			}
		}
	}

	http.Error(w, `{"error":"all models failed"}`, http.StatusBadGateway)
}

func (e *Engine) sendRequest(r *http.Request, rm service.ResolvedModel, apiKey string, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	if rm.Provider.APIType == models.APITypeAnthropic {
		anthReq := OpenAIToAnthropic(req)
		anthReq.Model = rm.Model.Name
		resp, err := e.anthropicClient.ChatCompletion(rm.Provider.BaseURL, apiKey, anthReq)
		if err != nil {
			return nil, err
		}
		result := AnthropicToOpenAI(resp, rm.Model.Name)
		return &result, nil
	}

	req.Model = rm.Model.Name
	return e.openaiClient.ChatCompletion(rm.Provider.BaseURL, apiKey, req)
}

func (e *Engine) sendStreamRequest(r *http.Request, rm service.ResolvedModel, apiKey string, req ChatCompletionRequest) (io.ReadCloser, *http.Response, error) {
	if rm.Provider.APIType == models.APITypeAnthropic {
		anthReq := OpenAIToAnthropic(req)
		anthReq.Model = rm.Model.Name
		return e.anthropicClient.ChatCompletionStream(rm.Provider.BaseURL, apiKey, anthReq)
	}

	req.Model = rm.Model.Name
	return e.openaiClient.ChatCompletionStream(rm.Provider.BaseURL, apiKey, req)
}

func (e *Engine) streamPassthrough(w http.ResponseWriter, flusher http.Flusher, body io.ReadCloser, httpResp *http.Response) {
	defer body.Close()

	for key, values := range httpResp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}

	buf := make([]byte, 4096)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			flusher.Flush()
		}
		if err != nil {
			break
		}
	}
}

func (e *Engine) streamAnthropicToOpenAI(w http.ResponseWriter, flusher http.Flusher, body io.ReadCloser, model string, requestID string) {
	events := ParseAnthropicSSEStream(body)

	for event := range events {
		chunk := AnthropicStreamToOpenAIChunk(event, model, requestID)
		if chunk == nil {
			continue
		}

		data, err := json.Marshal(chunk)
		if err != nil {
			continue
		}

		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
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

	if log.ErrorMessage != "" {
		attrs = append(attrs, "error", log.ErrorMessage)
	}

	if log.StatusCode >= 200 && log.StatusCode < 300 {
		e.logger.Info("request completed", attrs...)
	} else {
		e.logger.Error("request failed", attrs...)
	}

	if e.logChan != nil {
		e.logChan <- log
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
