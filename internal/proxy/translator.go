package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

func OpenAIToAnthropic(req ChatCompletionRequest) AnthropicRequest {
	anthReq := AnthropicRequest{
		Model:     req.Model,
		MaxTokens: 4096,
	}

	if req.MaxTokens != nil {
		anthReq.MaxTokens = *req.MaxTokens
	}

	var systemParts []string
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			if content, ok := msg.Content.(string); ok {
				systemParts = append(systemParts, content)
			}
			continue
		}

		anthMsg := AnthropicMessage{
			Role: msg.Role,
		}

		if content, ok := msg.Content.(string); ok {
			anthMsg.Content = content
		} else {
			anthMsg.Content = msg.Content
		}

		anthReq.Messages = append(anthReq.Messages, anthMsg)
	}

	if len(systemParts) > 0 {
		anthReq.System = strings.Join(systemParts, "\n\n")
	}

	for _, tool := range req.Tools {
		anthReq.Tools = append(anthReq.Tools, AnthropicTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		})
	}

	return anthReq
}

func AnthropicToOpenAI(resp *AnthropicResponse, model string) ChatCompletionResponse {
	openResp := ChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Model:   model,
		Choices: make([]Choice, 1),
	}

	var contentParts []string
	var toolCalls []ToolCall

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			contentParts = append(contentParts, block.Text)
		case "tool_use":
			tc := ToolCall{
				ID:   block.ID,
				Type: "function",
			}
			tc.Function.Name = block.Name
			tc.Function.Arguments = string(block.Input)
			toolCalls = append(toolCalls, tc)
		}
	}

	msg := Message{
		Role:    "assistant",
		Content: strings.Join(contentParts, ""),
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
		msg.Content = nil
	}

	openResp.Choices[0] = Choice{
		Index:        0,
		Message:      msg,
		FinishReason: mapStopReason(resp.StopReason),
	}

	cachedTokens := resp.Usage.CacheReadInputTokens
	openResp.Usage = Usage{
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
		TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		PromptTokensDetails: &PromptTokensDetails{
			CachedTokens: cachedTokens,
		},
	}

	return openResp
}

func mapStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "stop_sequence":
		return "stop"
	default:
		return "stop"
	}
}

func AnthropicStreamToOpenAIChunk(event AnthropicStreamEvent, model string, requestID string) *StreamChunk {
	switch event.Type {
	case "message_start":
		return &StreamChunk{
			ID:      requestID,
			Object:  "chat.completion.chunk",
			Model:   model,
			Choices: []StreamChoice{{Index: 0, Delta: StreamDelta{Role: "assistant"}}},
		}

	case "content_block_delta":
		var delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(event.Delta, &delta); err != nil {
			return nil
		}
		if delta.Type == "text_delta" {
			return &StreamChunk{
				ID:      requestID,
				Object:  "chat.completion.chunk",
				Model:   model,
				Choices: []StreamChoice{{Index: 0, Delta: StreamDelta{Content: delta.Text}}},
			}
		}

	case "message_delta":
		var delta struct {
			StopReason string `json:"stop_reason"`
		}
		if err := json.Unmarshal(event.Delta, &delta); err != nil {
			return nil
		}
		finishReason := mapStopReason(delta.StopReason)
		return &StreamChunk{
			ID:      requestID,
			Object:  "chat.completion.chunk",
			Model:   model,
			Choices: []StreamChoice{{Index: 0, Delta: StreamDelta{}, FinishReason: &finishReason}},
			Usage: func() *Usage {
				if event.Usage != nil {
					return &Usage{
						PromptTokens:     event.Usage.InputTokens,
						CompletionTokens: event.Usage.OutputTokens,
						TotalTokens:      event.Usage.InputTokens + event.Usage.OutputTokens,
						PromptTokensDetails: &PromptTokensDetails{
							CachedTokens: event.Usage.CacheReadInputTokens,
						},
					}
				}
				return nil
			}(),
		}
	}

	return nil
}

func ExtractTokenUsage(resp *ChatCompletionResponse) (input, output, cached, reasoning int) {
	input = resp.Usage.PromptTokens
	output = resp.Usage.CompletionTokens
	total := resp.Usage.TotalTokens

	if resp.Usage.PromptTokensDetails != nil {
		cached = resp.Usage.PromptTokensDetails.CachedTokens
	}

	reasoning = total - input - output
	if reasoning < 0 {
		reasoning = 0
	}

	return
}

func EstimateTokens(text string) int {
	return len(text) / 4
}

func CalculateHeadroom(contextWindow, inputTokens, maxTokens int) int {
	return contextWindow - inputTokens - maxTokens
}

func EstimateCost(input, output, cached int, costPer1kInput, costPer1kOutput, costPer1kCached float64) float64 {
	inputCost := float64(input-cached) / 1000.0 * costPer1kInput
	cachedCost := float64(cached) / 1000.0 * costPer1kCached
	outputCost := float64(output) / 1000.0 * costPer1kOutput
	return inputCost + cachedCost + outputCost
}

func FormatCost(cost float64) string {
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}
