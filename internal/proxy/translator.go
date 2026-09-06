package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
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

		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			content := []AnthropicContent{}
			if s, ok := msg.Content.(string); ok && s != "" {
				content = append(content, AnthropicContent{Type: "text", Text: s})
			}
			for _, tc := range msg.ToolCalls {
				content = append(content, AnthropicContent{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: json.RawMessage(tc.Function.Arguments),
				})
			}
			anthReq.Messages = append(anthReq.Messages, AnthropicMessage{
				Role:    "assistant",
				Content: content,
			})
			continue
		}

		if msg.Role == "tool" {
			content := []AnthropicContent{
				{
					Type:      "tool_result",
					ToolUseID: msg.ToolCallID,
					Content:   fmt.Sprintf("%v", msg.Content),
				},
			}
			anthReq.Messages = append(anthReq.Messages, AnthropicMessage{
				Role:    "user",
				Content: content,
			})
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
		desc := tool.Function.Description
		if desc == "" {
			desc = tool.Function.Name
		}
		anthReq.Tools = append(anthReq.Tools, AnthropicTool{
			Name:        tool.Function.Name,
			Description: desc,
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

	var reasoningParts []string
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			contentParts = append(contentParts, block.Text)
		case "thinking":
			reasoningParts = append(reasoningParts, block.Thinking)
		case "tool_use":
			tc := ToolCall{
				ID:   block.ID,
				Type: "function",
			}
			tc.Function.Name = decloakToolName(block.Name)
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

func anthropicThinkingToEffort(thinking *AnthropicThinking) string {
	if thinking == nil || thinking.Type == "disabled" {
		return "none"
	}
	tokens := thinking.BudgetTokens
	switch {
	case tokens <= 1024:
		return "low"
	case tokens <= 4096:
		return "medium"
	case tokens <= 8192:
		return "high"
	default:
		return "max"
	}
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

type ClaudeStreamState struct {
	Model             string
	RequestID         string
	MessageID         string
	InThinkingBlock   bool
	CurrentBlockIndex int
	ServerToolIndex   int
	TextBlockStarted  bool
	ToolCallIndex     int
	ToolCalls         map[int]*ToolCallState
	Usage             *Usage
	usageParts        anthropicUsageParts
	FinishReasonSent  bool
}

type ToolCallState struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

func (s *ClaudeStreamState) createChunk(delta StreamDelta, finishReason *string, usage *Usage) *StreamChunk {
	chunk := &StreamChunk{
		ID:      fmt.Sprintf("chatcmpl-%s", s.MessageID),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   s.Model,
		Choices: []StreamChoice{{Index: 0, Delta: delta, FinishReason: finishReason}},
	}
	if usage != nil {
		chunk.Usage = usage
	}
	return chunk
}

func (s *ClaudeStreamState) ProcessEvent(event AnthropicStreamEvent) []*StreamChunk {
	var results []*StreamChunk

	switch event.Type {
	case "message_start":
		var msg struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		}
		if err := json.Unmarshal(event.Message, &msg); err == nil {
			s.MessageID = msg.ID
			if msg.Model != "" {
				s.Model = msg.Model
			}
			// message.usage is the initial usage snapshot (real Anthropic:
			// input/cache counts with output 0; zai: everything zeroed).
			// Decode presence-aware so a later cumulative delta overwrites
			// per-field and OpenAI-style names are accepted.
			if raw := extractUsageRaw(event.Message); len(raw) > 0 {
				if s.usageParts.applyRaw(raw) {
					s.usageParts.seen = true // usage key present → non-nil Usage
				}
				s.Usage = s.usageParts.build()
			}
		}
		results = append(results, s.createChunk(StreamDelta{Role: "assistant"}, nil, nil))

	case "content_block_start":
		rawBlock := event.ContentBlock
		if rawBlock == nil {
			rawBlock = event.Delta
		}
		var block struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if rawBlock != nil {
			json.Unmarshal(rawBlock, &block)
		}

		switch block.Type {
		case "thinking":
			s.InThinkingBlock = true
			s.CurrentBlockIndex = event.Index
			results = append(results, s.createChunk(StreamDelta{Content: ""}, nil, nil))
		case "tool_use":
			tc := &ToolCallState{
				Index: s.ToolCallIndex,
				ID:    block.ID,
				Name:  decloakToolName(block.Name),
			}
			s.ToolCallIndex++
			s.ToolCalls[event.Index] = tc
			results = append(results, s.createChunk(StreamDelta{
				ToolCalls: []StreamToolCall{{
					Index: tc.Index,
					ID:    tc.ID,
					Type:  "function",
					Function: struct {
						Name      string `json:"name,omitempty"`
						Arguments string `json:"arguments"`
					}{Name: tc.Name, Arguments: ""},
				}},
			}, nil, nil))
		case "text":
			s.TextBlockStarted = true
		}

	case "content_block_delta":
		if event.Index == s.ServerToolIndex {
			break
		}
		var delta struct {
			Type         string `json:"type"`
			Text         string `json:"text"`
			Thinking     string `json:"thinking"`
			PartialJSON  string `json:"partial_json"`
		}
		if err := json.Unmarshal(event.Delta, &delta); err != nil {
			break
		}

		switch delta.Type {
		case "text_delta":
			if delta.Text != "" {
				results = append(results, s.createChunk(StreamDelta{Content: delta.Text}, nil, nil))
			}
		case "thinking_delta":
			results = append(results, s.createChunk(StreamDelta{ReasoningContent: delta.Thinking}, nil, nil))
		case "input_json_delta":
			if tc, ok := s.ToolCalls[event.Index]; ok {
				tc.Arguments += delta.PartialJSON
				results = append(results, s.createChunk(StreamDelta{
					ToolCalls: []StreamToolCall{{
						Index: tc.Index,
						ID:    tc.ID,
						Function: struct {
							Name      string `json:"name,omitempty"`
							Arguments string `json:"arguments"`
						}{Arguments: delta.PartialJSON},
					}},
				}, nil, nil))
			}
		}

	case "content_block_stop":
		if event.Index == s.ServerToolIndex {
			s.ServerToolIndex = -1
			break
		}
		if s.InThinkingBlock && event.Index == s.CurrentBlockIndex {
			results = append(results, s.createChunk(StreamDelta{Content: ""}, nil, nil))
			s.InThinkingBlock = false
		}
		s.TextBlockStarted = false

	case "message_delta":
		// Usage on message_delta is CUMULATIVE (Anthropic contract). Some
		// providers (zai) only send the real totals here — top-level or nested
		// inside delta — with message_start's usage zeroed. Read both shapes;
		// present fields overwrite the running totals, absent fields are
		// retained.
		if nested := extractNestedDeltaUsage(event.Delta); len(nested) > 0 {
			s.usageParts.applyRaw(nested)
		}
		s.usageParts.applyRaw(event.UsageRaw)
		if merged := s.usageParts.build(); merged != nil {
			s.Usage = merged
		}
		if event.Delta != nil {
			var delta struct {
				StopReason string `json:"stop_reason"`
			}
			if err := json.Unmarshal(event.Delta, &delta); err == nil && delta.StopReason != "" {
				finishReason := mapStopReason(delta.StopReason)
				chunk := s.createChunk(StreamDelta{}, &finishReason, s.Usage)
				results = append(results, chunk)
				s.FinishReasonSent = true
			}
		}

	case "message_stop":
		if !s.FinishReasonSent {
			finishReason := "stop"
			if len(s.ToolCalls) > 0 {
				finishReason = "tool_calls"
			}
			chunk := s.createChunk(StreamDelta{}, &finishReason, s.Usage)
			results = append(results, chunk)
			s.FinishReasonSent = true
		}
	}

	return results
}

// --- Usage accounting (Anthropic SSE) -----------------------------------------

// usageFieldSet decodes one usage payload with per-field presence (a pointer
// per field) so an explicit 0 (zai) is distinguishable from an absent field
// (canonical Anthropic output-only message_delta). Unknown extras
// (server_tool_use, service_tier, ...) are ignored. OpenAI-style names
// (prompt_tokens / completion_tokens / prompt_tokens_details.cached_tokens)
// are accepted as fallbacks when the Anthropic name is absent.
type usageFieldSet struct {
	Input         *int `json:"input_tokens"`
	Output        *int `json:"output_tokens"`
	CacheRead     *int `json:"cache_read_input_tokens"`
	CacheCreation *int `json:"cache_creation_input_tokens"`

	// OpenAI-style fallbacks, consulted only when the Anthropic name is absent.
	Prompt        *int `json:"prompt_tokens"`
	Completion    *int `json:"completion_tokens"`
	OpenAIDetails *struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// anthropicUsageParts tracks the Anthropic usage components across a stream.
// message_delta usage is CUMULATIVE per the Anthropic contract, so present
// fields OVERWRITE the running values — never sum. Providers like zai send a
// zeroed message_start usage and the real totals only in the final
// message_delta (top-level or nested inside delta); overwrite semantics make
// both layouts converge on the correct totals.
type anthropicUsageParts struct {
	input         int
	output        int
	cacheRead     int
	cacheCreation int
	seen          bool
}

// apply folds decoded usage payloads into the running parts with field-level
// overwrite. Later payloads win per field.
func (p *anthropicUsageParts) apply(f usageFieldSet) {
	if f.Input != nil {
		p.input = *f.Input
		p.seen = true
	} else if f.Prompt != nil {
		p.input = *f.Prompt
		p.seen = true
	}
	if f.Output != nil {
		p.output = *f.Output
		p.seen = true
	} else if f.Completion != nil {
		p.output = *f.Completion
		p.seen = true
	}
	if f.CacheRead != nil {
		p.cacheRead = *f.CacheRead
		p.seen = true
	} else if f.OpenAIDetails != nil && f.OpenAIDetails.CachedTokens != nil {
		p.cacheRead = *f.OpenAIDetails.CachedTokens
		p.seen = true
	}
	if f.CacheCreation != nil {
		p.cacheCreation = *f.CacheCreation
		p.seen = true
	}
}

// applyRaw decodes raw usage-object bytes and folds them in. Used when the
// field presence itself matters (an explicit 0 must overwrite a nonzero
// running value; an absent field must not). Returns whether a JSON object was
// decoded; null / malformed payloads are rejected.
func (p *anthropicUsageParts) applyRaw(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil || probe == nil {
		return false
	}
	var f usageFieldSet
	if err := json.Unmarshal(raw, &f); err != nil {
		return false
	}
	p.apply(f)
	return true
}

// build returns the OpenAI-style Usage snapshot, or nil when no usage payload
// has been seen anywhere in the stream.
func (p *anthropicUsageParts) build() *Usage {
	if !p.seen {
		return nil
	}
	promptTokens := p.input + p.cacheRead + p.cacheCreation
	return &Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: p.output,
		TotalTokens:      promptTokens + p.output,
		PromptTokensDetails: &PromptTokensDetails{
			CachedTokens: p.cacheRead,
		},
	}
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

	if req.Effort != nil {
		openReq.ReasoningEffort = req.Effort
	} else if req.Thinking != nil {
		effort := anthropicThinkingToEffort(req.Thinking)
		openReq.ReasoningEffort = &effort
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
