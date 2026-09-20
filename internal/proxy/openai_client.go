package proxy

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// newTTFTTransport returns an HTTP transport with no TTFT cap.
// Reasoning models can hold the response for tens of seconds before
// the first body byte arrives, and enforcing a 15s ResponseHeaderTimeout
// was breaking long-running/reasoning requests. The overall request
// timeout is enforced by the http.Server's WriteTimeout (5m) instead.
func newTTFTTransport() *http.Transport {
	return &http.Transport{}
}

type OpenAIClient struct {
	httpClient *http.Client
}

func NewOpenAIClient() *OpenAIClient {
	return &OpenAIClient{
		httpClient: &http.Client{Transport: newTTFTTransport()},
	}
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type ChatCompletionRequest struct {
	Model          string         `json:"model"`
	Messages       []Message      `json:"messages"`
	MaxTokens      *int           `json:"max_tokens,omitempty"`
	Temperature    *float64       `json:"temperature,omitempty"`
	TopP           *float64       `json:"top_p,omitempty"`
	Stream         bool           `json:"stream,omitempty"`
	StreamOptions  *StreamOptions `json:"stream_options,omitempty"`
	Tools          []Tool         `json:"tools,omitempty"`
	ToolChoice     interface{}    `json:"tool_choice,omitempty"`
	Stop           []string       `json:"stop,omitempty"`
	ReasoningEffort *string       `json:"reasoning_effort,omitempty"`
}

type Message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`

	// ReasoningContent carries DeepSeek-style thinking traces. Thinking-mode
	// providers (e.g. DeepSeek via OpenRouter) require assistant messages to
	// echo reasoning_content back on follow-up turns; dropping it made them
	// fail with 400 "The reasoning_content in the thinking mode must be
	// passed back to the API." on multi-turn conversations.
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type StreamToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens             int                      `json:"prompt_tokens"`
	CompletionTokens         int                      `json:"completion_tokens"`
	TotalTokens              int                      `json:"total_tokens"`
	PromptTokensDetails      *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails  *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type StreamDelta struct {
	Role             string           `json:"role,omitempty"`
	Content          string           `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []StreamToolCall `json:"tool_calls,omitempty"`
}

type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type StreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
	Usage   *Usage         `json:"usage,omitempty"`
}

// isOpencodeHost reports whether the provider host is an OpenCode host.
// Package var so tests can override it (test servers run on localhost).
var isOpencodeHost = func(host string) bool {
	return host == "opencode.ai" || strings.HasSuffix(host, ".opencode.ai")
}

// opencodeUserAgent mimics the OpenCode CLI's User-Agent. OpenCode Go/zen
// free tier (Console upstream) only unlocks capacity for requests that
// identify as the OpenCode CLI: the upstream validates the "opencode/"
// UA prefix (see anomalyco/opencode packages/opencode/src/session/llm/
// request.ts; issues #42029/#42500 — the x-opencode-* headers alone do not
// unlock free-tier models). Without it, -free models return FreeTierError.
// Package var so tests can pin it to a fixed value.
var opencodeUserAgent = "opencode/1.18.31 ai-sdk/provider-utils/4.0.23 runtime/bun/1.3.14"

// randomRequestID returns a random 16-byte hex ID for per-request headers.
func randomRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// isOpencodeBaseURL reports whether the provider base URL points at an
// opencode.ai host. Cheap re-check for call sites that need to know the
// host matched (the httpReq headers alone don't tell them).
func isOpencodeBaseURL(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return isOpencodeHost(u.Hostname())
}

// applyOpencodeHeaders sets the headers OpenCode Go/zen requires for its
// free tier (Console upstream): requests must identify as the OpenCode CLI
// via the "opencode/..." User-Agent plus the x-opencode-* headers, or -free
// models fail with FreeTierError ("OpenCode's free tier can only be used
// from within OpenCode"). The host check gates ALL opencode headers; the
// session header itself is only set when sessionID is non-empty. Other
// providers are completely unaffected.
func applyOpencodeHeaders(httpReq *http.Request, baseURL, sessionID string) {
	if !isOpencodeBaseURL(baseURL) {
		return
	}
	httpReq.Header.Set("User-Agent", opencodeUserAgent)
	httpReq.Header.Set("X-Opencode-Client", "cli")
	httpReq.Header.Set("X-Opencode-Request", randomRequestID())
	if sessionID != "" {
		httpReq.Header.Set("X-Opencode-Session", sessionID)
	}
}

// normalizeOpenAIBaseURL returns a base URL that always ends right before
// "/v1", so "/v1/chat/completions" is appended exactly once. Stored provider
// base URLs may already include "/v1" (e.g. https://openrouter.ai/api/v1),
// which previously produced "/v1/v1/chat/completions" and upstream 404s.
func normalizeOpenAIBaseURL(baseURL string) string {
	return strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1") + "/v1"
}

func (c *OpenAIClient) ChatCompletion(baseURL, apiKey, sessionID string, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	// Zen free-tier gate (see opencode_free.go): -free models require
	// stream:true, the CLI system-prompt marker and anonymous auth, so the
	// non-stream caller gets a server-side SSE aggregation instead of a
	// direct JSON response. The caller's req struct is never mutated.
	if isOpencodeBaseURL(baseURL) && isOpencodeFreeModel(req.Model) {
		return c.chatCompletionOpencodeFree(baseURL, apiKey, sessionID, req)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", normalizeOpenAIBaseURL(baseURL)+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	applyOpencodeHeaders(httpReq, baseURL, sessionID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, newProviderError(resp, respBody)
	}

	var result ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

func (c *OpenAIClient) ChatCompletionStream(baseURL, apiKey, sessionID string, req ChatCompletionRequest) (io.ReadCloser, *http.Response, error) {
	// Zen free-tier gate (see opencode_free.go): anonymous auth and the
	// content-marker system message. The request is already streamed here;
	// only the outgoing copy is modified, the caller's struct is untouched.
	if isOpencodeBaseURL(baseURL) && isOpencodeFreeModel(req.Model) {
		req = opencodeFreeRequest(req)
		apiKey = opencodeFreeAuthKey
	}

	req.Stream = true
	req.StreamOptions = &StreamOptions{IncludeUsage: true}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", normalizeOpenAIBaseURL(baseURL)+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")
	applyOpencodeHeaders(httpReq, baseURL, sessionID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, nil, newProviderError(resp, respBody)
	}

	return resp.Body, resp, nil
}

// chatCompletionOpencodeFree serves a non-stream ChatCompletion for a zen
// -free model. The free-tier gate rejects non-stream bodies, so the request
// is sent as stream:true (with the marker system message and anonymous
// "Bearer public" auth) and the SSE reply is aggregated server-side into a
// normal ChatCompletionResponse — invisible to the caller.
func (c *OpenAIClient) chatCompletionOpencodeFree(baseURL, apiKey, sessionID string, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	out := opencodeFreeRequest(req)

	body, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", normalizeOpenAIBaseURL(baseURL)+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+opencodeFreeAuthKey)
	httpReq.Header.Set("Accept", "text/event-stream")
	applyOpencodeHeaders(httpReq, baseURL, sessionID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, newProviderError(resp, respBody)
	}

	result := aggregateFreeStreamResponse(ParseSSEStream(resp.Body))
	if result == nil {
		return nil, fmt.Errorf("opencode zen free stream produced no chunks")
	}
	return result, nil
}

func ParseSSEStream(reader io.ReadCloser) <-chan StreamChunk {
	ch := make(chan StreamChunk)

	go func() {
		defer close(ch)
		defer reader.Close()

		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}

			var chunk StreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			ch <- chunk
		}
	}()

	return ch
}

type ProviderError struct {
	StatusCode int
	Message    string
	RetryAfter time.Duration
	RawBody    []byte
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider error %d: %s", e.StatusCode, e.Message)
}

// SubscriptionRequiredError signals a permanent, billing-related provider
// failure (HTTP 402). Wraps the underlying ProviderError.
type SubscriptionRequiredError struct {
	ProviderError *ProviderError
}

func (e *SubscriptionRequiredError) Error() string { return e.ProviderError.Error() }
func (e *SubscriptionRequiredError) Unwrap() error { return e.ProviderError }

// newProviderError builds the appropriate error for a failed provider HTTP
// response: *SubscriptionRequiredError for 402, *ProviderError otherwise.
// Preserves Retry-After parsing and raw body (used by classifyRateLimit).
func newProviderError(resp *http.Response, body []byte) error {
	pe := &ProviderError{
		StatusCode: resp.StatusCode,
		Message:    string(body),
		RetryAfter: parseRetryAfter(resp),
		RawBody:    body,
	}
	if resp.StatusCode == http.StatusPaymentRequired {
		return &SubscriptionRequiredError{ProviderError: pe}
	}
	return pe
}

func parseRetryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	raw := resp.Header.Get("Retry-After")
	if raw == "" {
		return 0
	}
	var seconds int
	if _, err := fmt.Sscanf(raw, "%d", &seconds); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 0
}
