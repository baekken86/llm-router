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

func AnthropicRequestToOpenAI(req AnthropicRequest) ChatCompletionRequest {
	openReq := ChatCompletionRequest{
		Model: req.Model,
	}

	if req.MaxTokens > 0 {
		openReq.MaxTokens = &req.MaxTokens
	}

	if req.System != "" {
		openReq.Messages = append(openReq.Messages, Message{
			Role:    "system",
			Content: req.System,
		})
	}

	for _, msg := range req.Messages {
		openMsg := Message{
			Role: msg.Role,
		}

		switch content := msg.Content.(type) {
		case string:
			openMsg.Content = content
		case []interface{}:
			var parts []string
			var toolCalls []ToolCall
			for _, block := range content {
				b, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				switch b["type"] {
				case "text":
					if text, ok := b["text"].(string); ok {
						parts = append(parts, text)
					}
				case "tool_result":
					if toolUseID, ok := b["tool_use_id"].(string); ok {
						resultContent := ""
						if c, ok := b["content"].(string); ok {
							resultContent = c
						}
						openMsg.Role = "tool"
						openMsg.ToolCallID = toolUseID
						openMsg.Content = resultContent
					}
				case "tool_use":
					tc := ToolCall{
						ID:   b["id"].(string),
						Type: "function",
					}
					if name, ok := b["name"].(string); ok {
						tc.Function.Name = name
					}
					if input, ok := b["input"]; ok {
						inputJSON, _ := json.Marshal(input)
						tc.Function.Arguments = string(inputJSON)
					}
					toolCalls = append(toolCalls, tc)
				}
			}
			if len(parts) > 0 {
				openMsg.Content = strings.Join(parts, "")
			}
			if len(toolCalls) > 0 {
				openMsg.ToolCalls = toolCalls
				if openMsg.Role == "assistant" {
					openMsg.Content = nil
				}
			}
		default:
			openMsg.Content = content
		}

		openReq.Messages = append(openReq.Messages, openMsg)
	}

	for _, tool := range req.Tools {
		openReq.Tools = append(openReq.Tools, Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}

	return openReq
}

func OpenAIResponseToAnthropic(resp ChatCompletionResponse, model string) AnthropicResponse {
	anthResp := AnthropicResponse{
		ID:    resp.ID,
		Type:  "message",
		Role:  "assistant",
		Model: model,
		Usage: AnthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}

	if resp.Usage.PromptTokensDetails != nil {
		anthResp.Usage.CacheReadInputTokens = resp.Usage.PromptTokensDetails.CachedTokens
	}

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]

		if choice.Message.Content != nil {
			if content, ok := choice.Message.Content.(string); ok && content != "" {
				anthResp.Content = append(anthResp.Content, AnthropicContent{
					Type: "text",
					Text: content,
				})
			}
		}

		for _, tc := range choice.Message.ToolCalls {
			inputJSON := json.RawMessage(tc.Function.Arguments)
			anthResp.Content = append(anthResp.Content, AnthropicContent{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: inputJSON,
			})
		}

		anthResp.StopReason = reverseMapStopReason(choice.FinishReason)
	}

	return anthResp
}

func reverseMapStopReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return "end_turn"
	}
}

func OpenAIStreamToAnthropicEvent(chunk StreamChunk, requestID string) []AnthropicStreamEvent {
	var events []AnthropicStreamEvent

	if len(chunk.Choices) == 0 {
		return events
	}

	choice := chunk.Choices[0]

	if choice.Delta.Role == "assistant" {
		events = append(events, AnthropicStreamEvent{
			Type: "message_start",
			Message: mustMarshal(AnthropicResponse{
				ID:      requestID,
				Type:    "message",
				Role:    "assistant",
				Model:   chunk.Model,
				Content: []AnthropicContent{},
			}),
		})
		events = append(events, AnthropicStreamEvent{
			Type:  "content_block_start",
			Index: 0,
			Delta: mustMarshal(map[string]interface{}{
				"type": "text",
				"text": "",
			}),
		})
	}

	if choice.Delta.Content != "" {
		events = append(events, AnthropicStreamEvent{
			Type:  "content_block_delta",
			Index: 0,
			Delta: mustMarshal(map[string]interface{}{
				"type":         "text_delta",
				"text":         choice.Delta.Content,
			}),
		})
	}

	if choice.FinishReason != nil {
		events = append(events, AnthropicStreamEvent{
			Type:  "content_block_stop",
			Index: 0,
		})

		deltaData := map[string]interface{}{
			"stop_reason": reverseMapStopReason(*choice.FinishReason),
		}
		if chunk.Usage != nil {
			deltaData["usage"] = AnthropicUsage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			}
		}
		events = append(events, AnthropicStreamEvent{
			Type:  "message_delta",
			Delta: mustMarshal(deltaData),
		})
		events = append(events, AnthropicStreamEvent{
			Type: "message_stop",
		})
	}

	return events
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
