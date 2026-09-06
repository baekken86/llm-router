package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type AnthropicClient struct {
	httpClient *http.Client
}

func NewAnthropicClient() *AnthropicClient {
	return &AnthropicClient{
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

func setClaudeHeaders(httpReq *http.Request, apiKey, sessionId string) {
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Anthropic-Beta", "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24,structured-outputs-2025-12-15,fast-mode-2026-02-01,redact-thinking-2026-02-12,token-efficient-tools-2026-03-28")
	httpReq.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
	httpReq.Header.Set("User-Agent", "claude-cli/2.1.92 (external, sdk-cli)")
	httpReq.Header.Set("X-App", "cli")
	httpReq.Header.Set("X-Stainless-Helper-Method", "stream")
	httpReq.Header.Set("X-Stainless-Retry-Count", "0")
	httpReq.Header.Set("X-Stainless-Runtime-Version", "v24.14.0")
	httpReq.Header.Set("X-Stainless-Package-Version", "0.80.0")
	httpReq.Header.Set("X-Stainless-Runtime", "node")
	httpReq.Header.Set("X-Stainless-Lang", "js")
	httpReq.Header.Set("X-Stainless-Arch", "arm64")
	httpReq.Header.Set("X-Stainless-Os", "MacOS")
	httpReq.Header.Set("X-Stainless-Timeout", "600")
	httpReq.Header.Set("X-Claude-Code-Session-Id", sessionId)
}

type AnthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    interface{}        `json:"system,omitempty"`
	Messages  []AnthropicMessage `json:"messages"`
	Stream    bool               `json:"stream,omitempty"`
	Tools     []AnthropicTool    `json:"tools,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Thinking  *AnthropicThinking `json:"thinking,omitempty"`
	Effort    *string            `json:"effort,omitempty"`
}

type AnthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type AnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type AnthropicResponse struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"`
	Role         string              `json:"role"`
	Content      []AnthropicContent  `json:"content"`
	Model        string              `json:"model"`
	StopReason   string              `json:"stop_reason"`
	StopSequence *string             `json:"stop_sequence"`
	Usage        AnthropicUsage      `json:"usage"`
}

type AnthropicContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
}

type AnthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// UnmarshalJSON tolerates non-Anthropic usage payloads: OpenAI-style field
// names (prompt_tokens / completion_tokens / prompt_tokens_details.cached_tokens)
// are mapped in as fallbacks when the Anthropic names are absent, and unknown
// extras (server_tool_use, service_tier, prompt_tokens_details, ...) are
// ignored instead of failing the parse. Anthropic field names always win when
// present.
func (u *AnthropicUsage) UnmarshalJSON(data []byte) error {
	type plain AnthropicUsage
	var canonical plain
	if err := json.Unmarshal(data, &canonical); err != nil {
		return err
	}
	*u = AnthropicUsage(canonical)

	var loose struct {
		PromptTokens        *int `json:"prompt_tokens"`
		CompletionTokens    *int `json:"completion_tokens"`
		PromptTokensDetails *struct {
			CachedTokens *int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
	if err := json.Unmarshal(data, &loose); err != nil {
		// Canonical fields already parsed; OpenAI-name probing is best-effort.
		return nil
	}
	if u.InputTokens == 0 && loose.PromptTokens != nil {
		u.InputTokens = *loose.PromptTokens
	}
	if u.OutputTokens == 0 && loose.CompletionTokens != nil {
		u.OutputTokens = *loose.CompletionTokens
	}
	if u.CacheReadInputTokens == 0 && loose.PromptTokensDetails != nil && loose.PromptTokensDetails.CachedTokens != nil {
		u.CacheReadInputTokens = *loose.PromptTokensDetails.CachedTokens
	}
	return nil
}

type AnthropicStreamEvent struct {
	Type         string          `json:"type"`
	Message      json.RawMessage `json:"message,omitempty"`
	Index        int             `json:"index,omitempty"`
	Delta        json.RawMessage `json:"delta,omitempty"`
	ContentBlock json.RawMessage `json:"content_block,omitempty"`
	Usage        *AnthropicUsage `json:"usage,omitempty"`

	// UsageRaw preserves the raw usage object bytes (top-level; falls back to
	// a delta-nested usage) so usage accounting can distinguish an explicit 0
	// from an absent field and accept non-Anthropic field names. Set by
	// ParseAnthropicSSEStream; never serialized.
	UsageRaw json.RawMessage `json:"-"`
}

func (c *AnthropicClient) ChatCompletion(baseURL, apiKey string, req AnthropicRequest) (*AnthropicResponse, error) {
	jsonBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var bodyMap map[string]interface{}
	if err := json.Unmarshal(jsonBody, &bodyMap); err != nil {
		return nil, fmt.Errorf("unmarshal body: %w", err)
	}

	sessionId := randomUUID()
	bodyMap = applyCloaking(bodyMap, apiKey, sessionId)
	if strings.HasPrefix(apiKey, "sk-ant-oat") {
		bodyMap = cloakClaudeTools(bodyMap)
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("marshal cloaked body: %w", err)
	}

	httpReq, err := http.NewRequest("POST", baseURL+"/messages?beta=true", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	setClaudeHeaders(httpReq, apiKey, sessionId)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, newProviderError(resp, respBody)
	}

	var result AnthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

func (c *AnthropicClient) ChatCompletionStream(baseURL, apiKey string, req AnthropicRequest) (io.ReadCloser, *http.Response, error) {
	req.Stream = true

	jsonBody, err := json.Marshal(req)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	var bodyMap map[string]interface{}
	if err := json.Unmarshal(jsonBody, &bodyMap); err != nil {
		return nil, nil, fmt.Errorf("unmarshal body: %w", err)
	}

	sessionId := randomUUID()
	bodyMap = applyCloaking(bodyMap, apiKey, sessionId)
	if strings.HasPrefix(apiKey, "sk-ant-oat") {
		bodyMap = cloakClaudeTools(bodyMap)
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal cloaked body: %w", err)
	}

	httpReq, err := http.NewRequest("POST", baseURL+"/messages?beta=true", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}

	setClaudeHeaders(httpReq, apiKey, sessionId)
	httpReq.Header.Set("Accept", "text/event-stream")

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

// extractUsageRaw returns the raw bytes of the top-level "usage" object of an
// SSE data payload (or nil when absent).
func extractUsageRaw(data []byte) json.RawMessage {
	var probe struct {
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(data, &probe) == nil && len(probe.Usage) > 0 {
		return probe.Usage
	}
	return nil
}

// extractNestedDeltaUsage returns raw usage bytes nested inside a delta
// object: {"delta":{"usage":{...}}} — a layout some Anthropic-compatible
// providers (e.g. zai) use.
func extractNestedDeltaUsage(delta json.RawMessage) json.RawMessage {
	var probe struct {
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(delta, &probe) == nil && len(probe.Usage) > 0 {
		return probe.Usage
	}
	return nil
}

func ParseAnthropicSSEStream(reader io.ReadCloser) <-chan AnthropicStreamEvent {
	ch := make(chan AnthropicStreamEvent)

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

			var event AnthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			// Preserve raw usage bytes for presence-aware accounting; also
			// surface usage objects nested inside the delta object ({"delta":
			// {"usage":{...}}) which some providers emit.
			event.UsageRaw = extractUsageRaw([]byte(data))
			if len(event.UsageRaw) == 0 && len(event.Delta) > 0 {
				event.UsageRaw = extractNestedDeltaUsage(event.Delta)
			}

			ch <- event
		}
		if err := scanner.Err(); err != nil {
			slog.Error("SSE scanner error", "error", err)
		}
	}()

	return ch
}
