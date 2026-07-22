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

type OllamaCloudClient struct {
	httpClient *http.Client
}

func NewOllamaCloudClient() *OllamaCloudClient {
	return &OllamaCloudClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  *ollamaOptions  `json:"options,omitempty"`
	Tools    []Tool          `json:"tools,omitempty"`
	ToolChoice interface{}   `json:"tool_choice,omitempty"`
}

type ollamaMessage struct {
	Role      string      `json:"role"`
	Content   string      `json:"content"`
	Images    []string    `json:"images,omitempty"`
	ToolName  string      `json:"tool_name,omitempty"`
	ToolCalls interface{} `json:"tool_calls,omitempty"`
}

type ollamaOptions struct {
	Temperature *float64 `json:"temperature,omitempty"`
	NumPredict  *int     `json:"num_predict,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
}

type ollamaResponse struct {
	Model              string           `json:"model"`
	Message            *ollamaMessage   `json:"message"`
	Done               bool             `json:"done"`
	DoneReason         string           `json:"done_reason,omitempty"`
	PromptEvalCount    int              `json:"prompt_eval_count,omitempty"`
	EvalCount          int              `json:"eval_count,omitempty"`
	PromptEvalDuration int64            `json:"prompt_eval_duration,omitempty"`
	EvalDuration       int64            `json:"eval_duration,omitempty"`
}

func convertToOllamaRequest(req ChatCompletionRequest) ollamaRequest {
	messages := make([]ollamaMessage, 0, len(req.Messages))

	toolCallMap := make(map[string]string)
	for _, msg := range req.Messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" && tc.Function.Name != "" {
					toolCallMap[tc.ID] = tc.Function.Name
				}
			}
		}
	}

	for _, msg := range req.Messages {
		if msg.Role == "tool" {
			toolName := toolCallMap[msg.ToolCallID]
			if toolName == "" {
				toolName = "unknown_tool"
			}
			messages = append(messages, ollamaMessage{
				Role:     "tool",
				ToolName: toolName,
				Content:  normalizeContentToString(msg.Content),
			})
			continue
		}

		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			ollamaToolCalls := make([]interface{}, 0, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				var args interface{}
				if tc.Function.Arguments != "" {
					var parsed interface{}
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err == nil {
						args = parsed
					} else {
						args = tc.Function.Arguments
					}
				}
				ollamaToolCalls = append(ollamaToolCalls, map[string]interface{}{
					"type": "function",
					"function": map[string]interface{}{
						"index":     i,
						"name":      tc.Function.Name,
						"arguments": args,
					},
				})
			}
			messages = append(messages, ollamaMessage{
				Role:      "assistant",
				Content:   normalizeContentToString(msg.Content),
				ToolCalls: ollamaToolCalls,
			})
			continue
		}

		content, images := extractContentAndImages(msg.Content)
		out := ollamaMessage{
			Role:    msg.Role,
			Content: content,
		}
		if len(images) > 0 {
			out.Images = images
		}
		messages = append(messages, out)
	}

	result := ollamaRequest{
		Model:    req.Model,
		Messages: messages,
		Stream:   false,
	}

	if req.Temperature != nil || req.MaxTokens != nil || req.TopP != nil {
		opts := &ollamaOptions{}
		if req.Temperature != nil {
			opts.Temperature = req.Temperature
		}
		if req.MaxTokens != nil {
			opts.NumPredict = req.MaxTokens
		}
		if req.TopP != nil {
			opts.TopP = req.TopP
		}
		result.Options = opts
	}

	if len(req.Tools) > 0 {
		result.Tools = req.Tools
	}
	if req.ToolChoice != nil {
		result.ToolChoice = req.ToolChoice
	}

	return result
}

func normalizeContentToString(content interface{}) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, block := range v {
			if m, ok := block.(map[string]interface{}); ok {
				if t, ok := m["type"].(string); ok && t == "text" {
					if text, ok := m["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprintf("%v", content)
	}
}

// extractContentAndImages extracts text content and base64 images from OpenAI content array.
// Ollama expects raw base64 in message.images[] (no data: prefix).
func extractContentAndImages(content interface{}) (string, []string) {
	if content == nil {
		return "", nil
	}

	switch v := content.(type) {
	case string:
		return v, nil
	case []interface{}:
		var parts []string
		var images []string
		for _, block := range v {
			m, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			switch t {
			case "text":
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			case "image_url":
				var url string
				if imgURL, ok := m["image_url"].(string); ok {
					url = imgURL
				} else if iuMap, ok := m["image_url"].(map[string]interface{}); ok {
					url, _ = iuMap["url"].(string)
				}
				if base64 := extractBase64FromDataURL(url); base64 != "" {
					images = append(images, base64)
				}
			}
		}
		return strings.Join(parts, "\n"), images
	default:
		return fmt.Sprintf("%v", content), nil
	}
}

// extractBase64FromDataURL extracts raw base64 from "data:image/png;base64,..." URL
func extractBase64FromDataURL(url string) string {
	const prefix = "data:"
	if !strings.HasPrefix(url, prefix) {
		return ""
	}
	idx := strings.Index(url, ";base64,")
	if idx < 0 {
		return ""
	}
	return url[idx+len(";base64,"):]
}

func (c *OllamaCloudClient) ChatCompletion(baseURL, apiKey string, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	ollamaReq := convertToOllamaRequest(req)
	ollamaReq.Stream = false

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

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

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return convertOllamaResponseToOpenAI(ollamaResp), nil
}

func (c *OllamaCloudClient) ChatCompletionStream(baseURL, apiKey string, req ChatCompletionRequest) (io.ReadCloser, *http.Response, error) {
	ollamaReq := convertToOllamaRequest(req)
	ollamaReq.Stream = true

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
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

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		defer resp.Body.Close()

		state := &ollamaStreamState{}
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var ollamaChunk ollamaResponse
			if err := json.Unmarshal([]byte(line), &ollamaChunk); err != nil {
				continue
			}

			sseChunk := convertOllamaChunkToSSE(&ollamaChunk, state, req.Model)
			if sseChunk != "" {
				pw.Write([]byte("data: " + sseChunk + "\n\n"))
			}

			if ollamaChunk.Done {
				pw.Write([]byte("data: [DONE]\n\n"))
				return
			}
		}
	}()

	return pr, resp, nil
}

type ollamaStreamState struct {
	id            string
	created       int64
	hadToolCalls  bool
}

func convertOllamaChunkToSSE(chunk *ollamaResponse, state *ollamaStreamState, modelName string) string {
	if state.id == "" {
		state.id = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
		state.created = int64(time.Now().Unix())
	}

	if chunk.Done {
		usage := &Usage{
			PromptTokens:     chunk.PromptEvalCount,
			CompletionTokens: chunk.EvalCount,
			TotalTokens:      chunk.PromptEvalCount + chunk.EvalCount,
		}
		finishReason := "stop"
		if chunk.DoneReason == "tool_calls" || state.hadToolCalls {
			finishReason = "tool_calls"
		}
		finalChunk := StreamChunk{
			ID:      state.id,
			Object:  "chat.completion.chunk",
			Created: state.created,
			Model:   modelName,
			Choices: []StreamChoice{{
				Index:        0,
				Delta:        StreamDelta{},
				FinishReason: &finishReason,
			}},
			Usage: usage,
		}
		data, _ := json.Marshal(finalChunk)
		return string(data)
	}

	if chunk.Message == nil {
		return ""
	}

	delta := StreamDelta{}

	if chunk.Message.Content != "" {
		delta.Content = chunk.Message.Content
	}

	if chunk.Message.ToolCalls != nil {
		state.hadToolCalls = true
		delta.ToolCalls = convertOllamaResponseToolCalls(chunk.Message.ToolCalls)
	}

	if delta.Content == "" && delta.ToolCalls == nil {
		return ""
	}

	streamChunk := StreamChunk{
		ID:      state.id,
		Object:  "chat.completion.chunk",
		Created: state.created,
		Model:   modelName,
		Choices: []StreamChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: nil,
		}},
	}

	data, _ := json.Marshal(streamChunk)
	return string(data)
}

// convertOllamaResponseToolCalls converts Ollama tool_calls to OpenAI format
func convertOllamaResponseToolCalls(toolCalls interface{}) []StreamToolCall {
	raw, err := json.Marshal(toolCalls)
	if err != nil {
		return nil
	}

	var ollamaTCs []struct {
		Type     string `json:"type"`
		Function struct {
			Index     int         `json:"index"`
			Name      string      `json:"name"`
			Arguments interface{} `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &ollamaTCs); err != nil {
		return nil
	}

	result := make([]StreamToolCall, 0, len(ollamaTCs))
	for _, tc := range ollamaTCs {
		argsStr := ""
		if tc.Function.Arguments != nil {
			switch a := tc.Function.Arguments.(type) {
			case string:
				argsStr = a
			default:
				b, _ := json.Marshal(a)
				argsStr = string(b)
			}
		}
		id := fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), tc.Function.Index)
		stc := StreamToolCall{
			Index: tc.Function.Index,
			ID:    id,
			Type:  "function",
		}
		stc.Function.Name = tc.Function.Name
		stc.Function.Arguments = argsStr
		result = append(result, stc)
	}
	return result
}

func convertOllamaResponseToOpenAI(resp ollamaResponse) *ChatCompletionResponse {
	content := ""
	if resp.Message != nil {
		content = resp.Message.Content
	}

	finishReason := "stop"
	if resp.DoneReason == "tool_calls" {
		finishReason = "tool_calls"
	}

	msg := Message{
		Role:    "assistant",
		Content: content,
	}

	if resp.Message != nil && resp.Message.ToolCalls != nil {
		toolCalls := convertOllamaResponseToolCalls(resp.Message.ToolCalls)
		if len(toolCalls) > 0 {
			finishReason = "tool_calls"
			openaiTCs := make([]ToolCall, 0, len(toolCalls))
			for _, tc := range toolCalls {
				otc := ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
				}
				otc.Function.Name = tc.Function.Name
				otc.Function.Arguments = tc.Function.Arguments
				openaiTCs = append(openaiTCs, otc)
			}
			msg.ToolCalls = openaiTCs
		}
	}

	return &ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: int64(time.Now().Unix()),
		Model:   resp.Model,
		Choices: []Choice{{
			Index:        0,
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: Usage{
			PromptTokens:     resp.PromptEvalCount,
			CompletionTokens: resp.EvalCount,
			TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
		},
	}
}
