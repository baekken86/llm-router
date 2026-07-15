package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

type AnthropicRequest struct {
	Model     string            `json:"model"`
	MaxTokens int               `json:"max_tokens"`
	System    string            `json:"system,omitempty"`
	Messages  []AnthropicMessage `json:"messages"`
	Stream    bool              `json:"stream,omitempty"`
	Tools     []AnthropicTool   `json:"tools,omitempty"`
}

type AnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
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
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message,omitempty"`
	Index   int             `json:"index,omitempty"`
	Delta   json.RawMessage `json:"delta,omitempty"`
	Usage   *AnthropicUsage `json:"usage,omitempty"`
}

func (c *AnthropicClient) ChatCompletion(baseURL, apiKey string, req AnthropicRequest) (*AnthropicResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

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

	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
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
	}()

	return ch
}
