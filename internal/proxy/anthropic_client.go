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

type AnthropicStreamEvent struct {
	Type         string          `json:"type"`
	Message      json.RawMessage `json:"message,omitempty"`
	Index        int             `json:"index,omitempty"`
	Delta        json.RawMessage `json:"delta,omitempty"`
	ContentBlock json.RawMessage `json:"content_block,omitempty"`
	Usage        *AnthropicUsage `json:"usage,omitempty"`
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
		return nil, &ProviderError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
			RetryAfter: parseRetryAfter(resp),
		}
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
		return nil, nil, &ProviderError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
			RetryAfter: parseRetryAfter(resp),
		}
	}

	return resp.Body, resp, nil
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

			ch <- event
		}
		if err := scanner.Err(); err != nil {
			slog.Error("SSE scanner error", "error", err)
		}
	}()

	return ch
}
