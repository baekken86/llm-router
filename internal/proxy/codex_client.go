package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// CodexClient talks to the ChatGPT subscription backend's Responses API
// (POST {baseURL}/responses, e.g. baseURL =
// https://chatgpt.com/backend-api/codex). The backend only speaks streaming
// Responses SSE; both methods force stream=true upstream. Non-streaming
// client requests are satisfied by aggregating the stream (§4.5 of the
// codex spec).
type CodexClient struct {
	httpClient *http.Client
}

func NewCodexClient() *CodexClient {
	return &CodexClient{
		httpClient: &http.Client{Transport: newTTFTTransport()},
	}
}

// codexClientVersionDefault mirrors the latest official codex_cli_rs release
// at implementation time. ChatGPT's backend gates the /models catalog
// SERVER-SIDE by the `client_version` query param (and the User-Agent), so a
// stale version silently hides newer models: e.g. 0.136.0 hid gpt-5.6-*
// (min 0.144.0) and gpt-6-astra (min 0.153.0). When new codex models stop
// appearing in discovery, bump this default to the current official CLI
// version (design §3.4; migrations note on model minimums; check
// github.com/openai/codex releases).
const codexClientVersionDefault = "0.153.4"

// codexClientVersion is resolved once at init: LLM_ROUTER_CODEX_CLIENT_VERSION
// (when it looks like a version) wins, otherwise the default above. It feeds
// BOTH the User-Agent and the client_version query param — the two must stay
// in sync because the server may cross-check them.
var codexClientVersion = codexClientVersionFromEnv()

// codexClientVersionFromEnv reads LLM_ROUTER_CODEX_CLIENT_VERSION and returns
// it when it looks like a version (digits/dots only, e.g. "0.153.4" or "1.2");
// anything else (empty, "v1.2.3", suffixed, padded) falls back to the default.
// Strict, no trimming — we present the version, we never parse it for
// comparisons: the server does the actual gating (per-model
// minimum_version filtering).
func codexClientVersionFromEnv() string {
	v := os.Getenv("LLM_ROUTER_CODEX_CLIENT_VERSION")
	if v == "" {
		return codexClientVersionDefault
	}
	for _, r := range v {
		if (r < '0' || r > '9') && r != '.' {
			return codexClientVersionDefault
		}
	}
	return v
}

// setCodexHeaders applies the shared header set for Codex backend requests
// (§3.2/§3.4). ChatGPT-Account-ID is only sent when the account id is known.
func setCodexHeaders(httpReq *http.Request, accessToken, accountID, sessionID string) {
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Originator", "codex_cli_rs")
	httpReq.Header.Set("User-Agent", "codex_cli_rs/"+codexClientVersion)
	if sessionID != "" {
		httpReq.Header.Set("session_id", headerValue(sessionID))
	}
	if accountID != "" {
		httpReq.Header.Set("ChatGPT-Account-ID", accountID)
	}
}

// headerValue guards the session id header against CRLF injection.
func headerValue(v string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, v)
}

// buildCodexResponsesRequest translates the chat request and crafts the
// outgoing POST. Effort/model-suffix resolution follows the translator
// contract: ChatToCodexResponses resolves via codexResolveEffort (explicit
// req.ReasoningEffort wins over the model's -low/-medium/-high/-xhigh/-max
// suffix; default "medium") and strips the suffix from the model it sends.
// The stripped model is what the backend catalog lists, so it is also used
// for the stream state below.
func (c *CodexClient) buildCodexResponsesRequest(ctx context.Context, baseURL, accessToken, accountID, sessionID string, req ChatCompletionRequest) (*http.Request, error) {
	body, err := ChatToCodexResponses(req, sessionID)
	if err != nil {
		return nil, fmt.Errorf("codex: translate chat request: %w", err)
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("codex: marshal responses body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(baseURL, "/")+"/responses", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("codex: create request: %w", err)
	}
	setCodexHeaders(httpReq, accessToken, accountID, sessionID)
	httpReq.Header.Set("Accept", "text/event-stream")
	return httpReq, nil
}

// ChatCompletion performs a non-streaming chat completion. The Codex backend
// only streams, so the SSE wire is read fully and aggregated into a single
// chat-completions response.
func (c *CodexClient) ChatCompletion(ctx context.Context, baseURL, accessToken, accountID, sessionID string, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	httpReq, err := c.buildCodexResponsesRequest(ctx, baseURL, accessToken, accountID, sessionID, req)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("codex: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, mapCodexHTTPError(resp, respBody)
	}

	state := NewCodexStreamState(codexStrippedModel(req.Model), sessionID)
	chunks, err := collectCodexStream(resp.Body, state)
	if err != nil {
		return nil, err
	}
	result := AggregateCodexStream(chunks)
	return &result, nil
}

// codexStrippedModel returns the model name without a trailing effort suffix
// (-low/-medium/-high/-xhigh/-max), matching what the translator sends
// upstream; it is the canonical catalog name.
func codexStrippedModel(model string) string {
	for suffix := range codexEffortSuffixes {
		if strings.HasSuffix(model, suffix) {
			return strings.TrimSuffix(model, suffix)
		}
	}
	return model
}

// ChatCompletionStream performs a streaming chat completion. The upstream
// Responses SSE wire is translated to OpenAI chat-completions SSE over an
// io.Pipe (mirrors ollama_cloud_client.go since the wire format differs from
// the client-facing one).
//
// 401 mapping: on HTTP 401 the client returns
// ProviderError{StatusCode:401, Message:"codex token expired"} and does NOT
// attempt a refresh itself — the engine/oauth layer (task T006) owns
// refresh-and-retry so the client stays a pure transport.
func (c *CodexClient) ChatCompletionStream(ctx context.Context, baseURL, accessToken, accountID, sessionID string, req ChatCompletionRequest) (io.ReadCloser, *http.Response, error) {
	httpReq, err := c.buildCodexResponsesRequest(ctx, baseURL, accessToken, accountID, sessionID, req)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("codex: send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, nil, mapCodexHTTPError(resp, respBody)
	}

	pr, pw := io.Pipe()
	go func() {
		err := pumpCodexSSEToPipe(resp.Body, pw, codexStrippedModel(req.Model), sessionID)
		if err != nil {
			pw.CloseWithError(err)
		} else {
			pw.Close()
		}
		resp.Body.Close()
	}()

	return pr, resp, nil
}

// codexCapacityMarkers are in-stream error messages the Codex backend sends
// inside a 200-OK SSE body (design §3.2): capacity/overload events must map
// to a retryable 503 so the engine's shouldRetry set (429/5xx) picks them up.
// Matches both the machine error-code form (model_at_capacity) and the human
// message form ("Selected model is at capacity …").
var codexCapacityMarkers = []string{
	"model_at_capacity",
	"at capacity",
	"server_is_overloaded",
	"service_unavailable_error",
}

// codexStreamErrorToProviderError classifies an error returned by
// CodexStreamState.ProcessEvent. Capacity-class messages become a 503 with a
// short RetryAfter; everything else is a 502 carrying the stream error text.
func codexStreamErrorToProviderError(err error) error {
	msg := err.Error()
	for _, marker := range codexCapacityMarkers {
		if strings.Contains(msg, marker) {
			return &ProviderError{
				StatusCode: http.StatusServiceUnavailable,
				Message:    msg,
				RetryAfter: 30 * time.Second,
			}
		}
	}
	return &ProviderError{StatusCode: http.StatusBadGateway, Message: msg}
}

// pumpCodexSSEToPipe reads Responses SSE from r and writes OpenAI
// chat-completions SSE to pw, ending with a final "data: [DONE]".
func pumpCodexSSEToPipe(r io.Reader, pw *io.PipeWriter, model, sessionID string) error {
	state := NewCodexStreamState(model, sessionID)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			// Responses SSE is data-only; tolerate but ignore other
			// lines (e.g. "event:" comments).
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		chunks, err := state.ProcessEvent([]byte(data))
		if err != nil {
			return codexStreamErrorToProviderError(err)
		}
		for _, chunk := range chunks {
			out, err := json.Marshal(chunk)
			if err != nil {
				return fmt.Errorf("codex: marshal chunk: %w", err)
			}
			if _, err := pw.Write([]byte("data: " + string(out) + "\n\n")); err != nil {
				return err
			}
		}
		if state.FinishDone {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("codex: read stream: %w", err)
	}
	_, err := pw.Write([]byte("data: [DONE]\n\n"))
	return err
}

// collectCodexStream reads a Responses SSE wire fully, feeding each parsed
// event to the stream state, and returns every emitted chunk (used by the
// non-streaming aggregation path).
func collectCodexStream(r io.Reader, state *CodexStreamState) ([]StreamChunk, error) {
	var chunks []StreamChunk
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		emitted, err := state.ProcessEvent([]byte(data))
		if err != nil {
			return nil, codexStreamErrorToProviderError(err)
		}
		chunks = append(chunks, emitted...)
		if state.FinishDone {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("codex: read stream: %w", err)
	}
	return chunks, nil
}

// mapCodexHTTPError converts a non-200 response into the engine-facing error.
func mapCodexHTTPError(resp *http.Response, body []byte) error {
	if resp.StatusCode == http.StatusUnauthorized {
		// 401 is always the OAuth token: the caller (engine/oauth layer)
		// refreshes and retries; the client stays a pure transport.
		return &ProviderError{StatusCode: http.StatusUnauthorized, Message: "codex token expired", RawBody: body}
	}

	// usage_limit_reached (429): parse resets_at (unix seconds) or
	// resets_in_seconds and surface it as RetryAfter so the engine's
	// rate-limit cooldown uses the backend's reset time.
	if resp.StatusCode == http.StatusTooManyRequests {
		if retryAfter, ok := parseCodexUsageLimit(body); ok && retryAfter > 0 {
			pe := &ProviderError{
				StatusCode: resp.StatusCode,
				Message:    string(body),
				RetryAfter: retryAfter,
				RawBody:    body,
			}
			return pe
		}
	}

	return newProviderError(resp, body)
}

// isCodexUnauthorized reports whether err is the Codex client's "OAuth token
// rejected upstream" signal (ProviderError 401) — the trigger for the
// engine's refresh-and-retry-once flow.
func isCodexUnauthorized(err error) bool {
	var pe *ProviderError
	return errors.As(err, &pe) && pe.StatusCode == http.StatusUnauthorized
}

// parseCodexUsageLimit extracts the cooldown from a usage_limit_reached body:
//
//	{"error":{"type":"usage_limit_reached","resets_at":<unix-sec>}} or
//	{"error":{"type":"usage_limit_reached","resets_in_seconds":<sec>}}.
func parseCodexUsageLimit(body []byte) (time.Duration, bool) {
	var parsed struct {
		Error struct {
			Type            string `json:"type"`
			ResetsAt        int64  `json:"resets_at"`
			ResetsInSeconds int64  `json:"resets_in_seconds"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, false
	}
	if parsed.Error.Type != "" && parsed.Error.Type != "usage_limit_reached" {
		return 0, false
	}

	now := time.Now()
	switch {
	case parsed.Error.ResetsAt > 0:
		d := time.Unix(parsed.Error.ResetsAt, 0).Sub(now)
		if d < 0 {
			return 0, false
		}
		return d, true
	case parsed.Error.ResetsInSeconds > 0:
		return time.Duration(parsed.Error.ResetsInSeconds) * time.Second, true
	}
	return 0, false
}

// --- Model discovery (§3.4) ---------------------------------------------------

// CodexModelInfo is one entry of the ChatGPT codex model catalog. Filtering
// (visibility / supported_in_api) is the model service's job, not the
// client's. SupportedInAPI is a pointer so an OMITTED catalog field stays nil
// (not false) — the model service's "supported_in_api != false" rule (§3.4)
// treats an omitted field as supported.
type CodexModelInfo struct {
	Slug                     string                `json:"slug"`
	DisplayName              string                `json:"display_name"`
	Description              string                `json:"description"`
	DefaultReasoningLevel    string                `json:"default_reasoning_level"`
	SupportedReasoningLevels []CodexReasoningLevel `json:"supported_reasoning_levels"`
	Visibility               string                `json:"visibility"`
	SupportedInAPI           *bool                 `json:"supported_in_api"`
	ContextWindow            int                   `json:"context_window"`
	Priority                 int                   `json:"priority"`
}

type CodexReasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

// ListModels fetches the live model catalog for ChatGPT-OAuth sessions
// (GET {baseURL}/models?client_version=<resolved codexClientVersion>). On 401 the caller refreshes
// the OAuth token and retries.
func (c *CodexClient) ListModels(ctx context.Context, baseURL, accessToken, accountID string) ([]CodexModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(baseURL, "/")+"/models?client_version="+codexClientVersion, nil)
	if err != nil {
		return nil, fmt.Errorf("codex: create request: %w", err)
	}
	setCodexHeaders(httpReq, accessToken, accountID, "")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("codex: list models: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("codex: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, &ProviderError{StatusCode: http.StatusUnauthorized, Message: "codex token expired", RawBody: respBody}
		}
		return nil, fmt.Errorf("codex: list models failed: %d %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Models []CodexModelInfo `json:"models"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("codex: decode models: %w", err)
	}
	return parsed.Models, nil
}
