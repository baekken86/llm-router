package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CodexDefaultInstructions mirrors the codex_cli_rs default base
// instructions (abbreviated): it is injected as the Responses body
// `instructions` when the client request carries no system message.
// Kept minimal and neutral; see openai/codex codex-rs defaults.
const CodexDefaultInstructions = "You are Codex, based on GPT-5. You are running as a coding agent inside the Codex CLI, helping the user with software engineering tasks. Follow the user's instructions precisely and use the provided tools when they help."

// codexEffortSuffixes maps a trailing model-name suffix to the Responses
// reasoning effort it selects ("max" is an alias for "xhigh", mirroring
// codex_cli_rs reasoning presets).
var codexEffortSuffixes = map[string]string{
	"-low":    "low",
	"-medium": "medium",
	"-high":   "high",
	"-xhigh":  "xhigh",
	"-max":    "xhigh",
}

// codexResolveEffort resolves the reasoning effort for a request: an explicit
// req.ReasoningEffort wins; otherwise the model's trailing effort suffix
// (-low/-medium/-high/-xhigh/-max) selects the effort; default "medium".
// It returns the resolved effort and the model name stripped of any suffix.
func codexResolveEffort(model string, req ChatCompletionRequest) (effort, strippedModel string) {
	strippedModel = model
	for suffix, e := range codexEffortSuffixes {
		if strings.HasSuffix(model, suffix) {
			strippedModel = strings.TrimSuffix(model, suffix)
			effort = e
			break
		}
	}
	if req.ReasoningEffort != nil && *req.ReasoningEffort != "" {
		effort = *req.ReasoningEffort
	}
	if effort == "" {
		effort = "medium"
	}
	return effort, strippedModel
}

// codexMapServiceTier maps a client service tier to the ChatGPT backend
// spelling: "fast" → "priority". Any other value is dropped (empty string):
// the backend rejects unknown tiers.
func codexMapServiceTier(tier string) string {
	if tier == "fast" {
		return "priority"
	}
	return ""
}

// codexToolChoiceMap is the OpenAI chat request payload form used for named
// function tool_choice. The Responses API requires the flat form
// {type:"function", name:<fn-name>} — passing the chat form through verbatim
// makes the codex backend reject the request with "Missing required
// parameter: 'tool_choice.name'".
func translateCodexToolChoice(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	if t, _ := m["type"].(string); t != "function" {
		return v
	}
	fn, ok := m["function"].(map[string]any)
	if !ok {
		return v
	}
	name, _ := fn["name"].(string)
	if name == "" {
		return v
	}
	return map[string]any{"type": "function", "name": name}
}

// codexBody is the Responses API wire format (§3.2 of the spec). Kept as a
// typed struct so the allowed key set is closed: anything not represented
// here is dropped from the translated request.
type codexBody struct {
	Model             string           `json:"model"`
	Instructions      string           `json:"instructions"`
	Input             []map[string]any `json:"input"`
	Tools             []map[string]any `json:"tools,omitempty"`
	ToolChoice        any              `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool            `json:"parallel_tool_calls,omitempty"`
	Stream            bool             `json:"stream"`
	Store             bool             `json:"store"`
	Reasoning         *codexReasoning  `json:"reasoning,omitempty"`
	Include           []string         `json:"include,omitempty"`
	ServiceTier       string           `json:"service_tier,omitempty"`
	PromptCacheKey    string           `json:"prompt_cache_key,omitempty"`
}

type codexReasoning struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary,omitempty"`
}

// ChatToCodexResponses translates an internal OpenAI chat-completions request
// into the ChatGPT Codex backend's Responses API body (§3.2).
//
// Rules:
//   - stream=true, store=false forced; prompt_cache_key=sessionID.
//   - system messages are folded into `instructions` (joined with blank
//     lines) and removed from input; when the request has no system message,
//     CodexDefaultInstructions is used.
//   - reasoning effort resolved from req.ReasoningEffort or the model suffix
//     (default "medium"); reasoning + include:["reasoning.encrypted_content"]
//     are emitted only when effort != "none".
//   - chat-shape tools are flattened to Responses function tools.
//   - temperature/top_p/penalties/logprobs/n/seed/max_tokens/user/metadata/
//     stream_options/response_format/stop are dropped entirely.
//   - service_tier "fast" maps to "priority"; any other value is dropped.
func ChatToCodexResponses(req ChatCompletionRequest, sessionID string) (map[string]any, error) {
	effort, model := codexResolveEffort(req.Model, req)

	var systemParts []string
	var input []map[string]any

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemParts = append(systemParts, normalizeCodexContent(msg.Content))
			continue
		}

		// Tool calls in the assistant history → function_call items.
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			if text := normalizeCodexContent(msg.Content); text != "" {
				input = append(input, map[string]any{
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{{
						"type": "output_text",
						"text": text,
					}},
				})
			}
			for _, tc := range msg.ToolCalls {
				input = append(input, map[string]any{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				})
			}
			continue
		}

		// Tool results → function_call_output items.
		if msg.Role == "tool" {
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": msg.ToolCallID,
				"output":  normalizeCodexContent(msg.Content),
			})
			continue
		}

		role := "user"
		contentType := "input_text"
		if msg.Role == "assistant" {
			role = "assistant"
			contentType = "output_text"
		}

		parts := codexContentParts(msg.Content, contentType)
		if len(parts) == 0 {
			// Preserve empty turns as empty text parts.
			parts = []map[string]any{{"type": contentType, "text": ""}}
		}
		input = append(input, map[string]any{
			"type":    "message",
			"role":    role,
			"content": parts,
		})
	}

	instructions := strings.Join(systemParts, "\n\n")
	if instructions == "" {
		instructions = CodexDefaultInstructions
	}

	var tools []map[string]any
	for _, tool := range req.Tools {
		flat := map[string]any{
			"type": "function",
			"name": tool.Function.Name,
		}
		if tool.Function.Description != "" {
			flat["description"] = tool.Function.Description
		}
		if tool.Function.Parameters != nil {
			flat["parameters"] = tool.Function.Parameters
		}
		tools = append(tools, flat)
	}

	body := codexBody{
		Model:          model,
		Instructions:   instructions,
		Input:          input,
		Stream:         true,  // always forced: the Codex backend only streams
		Store:          false, // always forced: stateless history translation
		PromptCacheKey: sessionID,
	}

	if effort != "none" {
		body.Reasoning = &codexReasoning{Effort: effort, Summary: "auto"}
		body.Include = []string{"reasoning.encrypted_content"}
	}

	if len(tools) > 0 {
		body.Tools = tools
	}
	if req.ToolChoice != nil {
		body.ToolChoice = translateCodexToolChoice(req.ToolChoice)
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("codex: marshal responses body: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("codex: unmarshal responses body: %w", err)
	}
	return out, nil
}

// codexContentParts converts an internal message content (string or OpenAI
// content-part array) into Responses content parts. Images become
// {type:"input_image", image_url:...} (URL or data URI passed through
// as-is); text becomes {type:textType, text:...}.
func codexContentParts(content any, textType string) []map[string]any {
	var parts []map[string]any
	switch v := content.(type) {
	case string:
		if v != "" {
			parts = append(parts, map[string]any{"type": textType, "text": v})
		}
	case []any:
		for _, block := range v {
			m, ok := block.(map[string]any)
			if !ok {
				continue
			}
			switch m["type"] {
			case "text":
				if text, ok := m["text"].(string); ok && text != "" {
					parts = append(parts, map[string]any{"type": textType, "text": text})
				}
			case "image_url":
				var imgURL string
				if s, ok := m["image_url"].(string); ok {
					imgURL = s
				} else if iu, ok := m["image_url"].(map[string]any); ok {
					imgURL, _ = iu["url"].(string)
				}
				if imgURL != "" {
					parts = append(parts, map[string]any{"type": "input_image", "image_url": imgURL})
				}
			}
		}
	case nil:
	default:
		if s := fmt.Sprintf("%v", v); s != "" {
			parts = append(parts, map[string]any{"type": textType, "text": s})
		}
	}
	return parts
}

// normalizeCodexContent flattens message content to plain text (used for
// instructions, tool outputs, and assistant text).
func normalizeCodexContent(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		var parts []string
		for _, block := range v {
			m, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); t == "text" {
				if text, ok := m["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// codexToolCallState accumulates one streamed function call (mirrors
// ToolCallState in translator.go).
type codexToolCallState struct {
	Index     int
	ID        string
	Name      string
	Arguments string
	// NameEmitted tracks whether the name already rode an emitted chunk
	// (OpenAI convention: id+name on the first tool_call delta only).
	NameEmitted bool
}

// CodexStreamState is the streaming state machine for the ChatGPT Codex
// backend (Responses API). It consumes one parsed SSE JSON object per
// ProcessEvent call and emits zero or more internal OpenAI StreamChunks,
// mirroring the ClaudeStreamState.ProcessEvent pattern (translator.go).
type CodexStreamState struct {
	Model      string
	RequestID  string
	// ToolCalls indexes each tool-call state under BOTH of its wire keys —
	// the Responses item id (fc_…) and the call_id (call_…) — because
	// output_item.added and function_call_arguments.delta events may
	// correlate through either one (e.g. gpt-6-astra emits deltas carrying
	// only item_id). Aliased entries point at the same state; ToolOrder
	// stays 1:1 with actual states (one entry per call).
	ToolCalls  map[string]*codexToolCallState
	ToolOrder  []string
	ToolIndex  int
	Usage      *Usage
	FinishDone bool
}

// NewCodexStreamState builds a fresh state machine. model is the model name
// echoed on each emitted chunk; requestID seeds the chunk ID.
func NewCodexStreamState(model, requestID string) *CodexStreamState {
	return &CodexStreamState{
		Model:     model,
		RequestID: requestID,
		ToolCalls: make(map[string]*codexToolCallState),
	}
}

// lookupToolState returns the first existing tool-call state for any of the
// given keys (nil when none match; empty keys are skipped).
func (s *CodexStreamState) lookupToolState(keys ...string) *codexToolCallState {
	for _, key := range keys {
		if key == "" {
			continue
		}
		if tc, ok := s.ToolCalls[key]; ok {
			return tc
		}
	}
	return nil
}

// registerToolState aliases tc under every non-empty key. It does not touch
// ToolOrder/ToolIndex, which stay 1:1 with actual tool-call states.
func (s *CodexStreamState) registerToolState(tc *codexToolCallState, keys ...string) {
	for _, key := range keys {
		if key == "" {
			continue
		}
		s.ToolCalls[key] = tc
	}
}

// newChunk builds an internal OpenAI stream chunk (same shape as
// ClaudeStreamState.createChunk).
func (s *CodexStreamState) newChunk(delta StreamDelta, finishReason *string, usage *Usage) StreamChunk {
	chunk := StreamChunk{
		ID:      fmt.Sprintf("chatcmpl-%s", s.RequestID),
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

// ProcessEvent consumes one parsed SSE JSON object (a Responses API event:
// response.output_text.delta, response.function_call_arguments.delta,
// response.reasoning_summary_text.delta, response.completed, error) and
// returns zero or more internal OpenAI stream chunks.
//
// response.output_item.added announces a tool call (item.type ==
// "function_call"): it creates the state (aliased under both the item id and
// the call_id, since later deltas may correlate through either one) and
// emits id+name immediately when the item carries a name — newer models
// (e.g. gpt-6-astra) never repeat the name on the
// response.function_call_arguments.delta events. A nameless item waits for
// the deltas, which then carry the name on their first emitted chunk.
//
// Emission mapping:
//   - response.output_text.delta             → delta.content
//   - response.output_item.added             → delta.tool_calls (id+name,
//     arguments "") when item.name is known
//   - response.function_call_arguments.delta → delta.tool_calls (accumulated;
//     deltas with no ids fall back to the most recent tool call)
//   - response.reasoning_summary_text.delta  → delta.reasoning_content
//     (same convention the anthropic translator uses for reasoning deltas,
//     translator.go ClaudeStreamState thinking_delta)
//   - response.completed                     → finish_reason ("stop", or
//     "tool_calls" when tool calls were emitted) + usage
//     (input_tokens → prompt_tokens, output_tokens → completion_tokens,
//     output_tokens_details.reasoning_tokens → reasoning_tokens)
//
// Error events produce an error return; unknown event types are ignored
// silently.
func (s *CodexStreamState) ProcessEvent(event []byte) ([]StreamChunk, error) {
	var ev struct {
		Type     string          `json:"type"`
		Delta    string          `json:"delta"`
		ItemID   string          `json:"item_id"`
		CallID   string          `json:"call_id"`
		Name     string          `json:"name"`
		Item     json.RawMessage `json:"item"`
		Response json.RawMessage `json:"response"`
		Error    *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(event, &ev); err != nil {
		return nil, fmt.Errorf("codex: unmarshal event: %w", err)
	}

	switch ev.Type {
	case "response.output_text.delta":
		if ev.Delta == "" {
			return nil, nil
		}
		return []StreamChunk{s.newChunk(StreamDelta{Content: ev.Delta}, nil, nil)}, nil

	case "response.function_call_arguments.delta":
		if ev.Delta == "" {
			return nil, nil
		}
		tc := s.lookupToolState(ev.CallID, ev.ItemID)
		if tc == nil && ev.CallID == "" && ev.ItemID == "" {
			// A delta carrying neither id belongs to the call most recently
			// announced (matches the reference Responses translator's
			// fallback); only fail when nothing exists yet.
			if len(s.ToolOrder) == 0 {
				return nil, fmt.Errorf("codex: function_call_arguments.delta without call_id/item_id")
			}
			tc = s.ToolCalls[s.ToolOrder[len(s.ToolOrder)-1]]
		}
		if tc == nil {
			// First delta for this call: create the state and register it
			// under both wire keys so later events correlate regardless of
			// which id they carry.
			key := ev.CallID
			if key == "" {
				key = ev.ItemID
			}
			tc = &codexToolCallState{
				Index: s.ToolIndex,
				ID:    key,
				Name:  ev.Name,
			}
			s.ToolIndex++
			s.ToolOrder = append(s.ToolOrder, key)
			s.registerToolState(tc, ev.CallID, ev.ItemID)
		} else if ev.Name != "" && tc.Name == "" {
			tc.Name = ev.Name
		}
		tc.Arguments += ev.Delta
		// Emit the tool name on the first emitted delta of a call (OpenAI
		// chunk convention: id+name ride the initial tool_call delta,
		// subsequent deltas carry only argument fragments — mirrors the
		// anthropic tool_use content_block_start emission in
		// translator.go). The state may already exist when the first delta
		// arrives (created by output_item.added), so the guarantee is
		// tracked explicitly rather than by state creation.
		var nameField string
		if !tc.NameEmitted {
			nameField = tc.Name
			tc.NameEmitted = true
		}
		return []StreamChunk{s.newChunk(StreamDelta{
			ToolCalls: []StreamToolCall{{
				Index: tc.Index,
				ID:    tc.ID,
				Type:  "function",
				Function: struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments"`
				}{Name: nameField, Arguments: ev.Delta},
			}},
		}, nil, nil)}, nil

	case "response.reasoning_summary_text.delta":
		if ev.Delta == "" {
			return nil, nil
		}
		return []StreamChunk{s.newChunk(StreamDelta{ReasoningContent: ev.Delta}, nil, nil)}, nil

	case "response.completed":
		s.parseCompletedUsage(ev.Response)
		finishReason := "stop"
		if len(s.ToolOrder) > 0 {
			finishReason = "tool_calls"
		}
		s.FinishDone = true
		return []StreamChunk{s.newChunk(StreamDelta{}, &finishReason, s.Usage)}, nil

	case "error", "response.failed":
		msg := "unknown codex stream error"
		if ev.Error != nil && ev.Error.Message != "" {
			msg = ev.Error.Message
		}
		return nil, fmt.Errorf("codex stream error: %s", msg)

	case "response.output_item.added":
		// Announce the tool call up front: the item carries the function
		// name, while later function_call_arguments.delta events may carry
		// only item_id and no name (e.g. gpt-6-astra). This event must
		// never fail the stream, so unmarshal errors are tolerated
		// silently.
		var item struct {
			Type   string `json:"type"`
			ID     string `json:"id"`
			CallID string `json:"call_id"`
			Name   string `json:"name"`
		}
		if err := json.Unmarshal(ev.Item, &item); err != nil {
			return nil, nil
		}
		if item.Type != "function_call" || (item.ID == "" && item.CallID == "") {
			return nil, nil
		}
		if tc := s.lookupToolState(item.ID, item.CallID); tc != nil {
			// Deltas already arrived: backfill the name without emitting.
			if item.Name != "" && tc.Name == "" {
				tc.Name = item.Name
			}
			return nil, nil
		}
		// Create the state now so id-only deltas correlate through the
		// aliases; the name may still be unknown here.
		id := item.CallID
		if id == "" {
			id = item.ID
		}
		tc := &codexToolCallState{
			Index: s.ToolIndex,
			ID:    id,
			Name:  item.Name,
		}
		s.ToolIndex++
		s.ToolOrder = append(s.ToolOrder, tc.ID)
		s.registerToolState(tc, item.ID, item.CallID)
		if tc.Name == "" {
			// Without a name the added chunk would fail strict downstream
			// tool-call validation; wait for the deltas to carry it.
			return nil, nil
		}
		// Emit id+name immediately (arguments empty, matches the reference
		// Responses translator); the deltas then carry fragments only.
		tc.NameEmitted = true
		return []StreamChunk{s.newChunk(StreamDelta{
			ToolCalls: []StreamToolCall{{
				Index: tc.Index,
				ID:    tc.ID,
				Type:  "function",
				Function: struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments"`
				}{Name: tc.Name, Arguments: ""},
			}},
		}, nil, nil)}, nil

	default:
		// Unknown event types are ignored silently.
		return nil, nil
	}
}

// parseCompletedUsage extracts usage from the response.completed payload:
// input_tokens → prompt_tokens, output_tokens → completion_tokens,
// output_tokens_details.reasoning_tokens → completion_tokens_details.
func (s *CodexStreamState) parseCompletedUsage(response json.RawMessage) {
	if len(response) == 0 {
		return
	}
	var resp struct {
		Usage *struct {
			InputTokens         int `json:"input_tokens"`
			OutputTokens        int `json:"output_tokens"`
			OutputTokensDetails *struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(response, &resp); err != nil || resp.Usage == nil {
		return
	}
	u := &Usage{
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
		TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}
	if resp.Usage.OutputTokensDetails != nil {
		u.CompletionTokensDetails = &CompletionTokensDetails{
			ReasoningTokens: resp.Usage.OutputTokensDetails.ReasoningTokens,
		}
	}
	s.Usage = u
}

// AggregateCodexStream folds the chunks emitted by CodexStreamState into a
// single ChatCompletionResponse. The Codex backend always streams (even for
// non-streaming client requests), so the engine translates then aggregates.
func AggregateCodexStream(chunks []StreamChunk) ChatCompletionResponse {
	resp := ChatCompletionResponse{
		Object:  "chat.completion",
		Choices: []Choice{{Index: 0, Message: Message{Role: "assistant"}}},
	}

	var content strings.Builder
	var toolCalls []ToolCall
	var finishReason string

	for _, chunk := range chunks {
		resp.ID = chunk.ID
		resp.Model = chunk.Model
		resp.Created = chunk.Created
		if chunk.Usage != nil {
			resp.Usage = *chunk.Usage
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				content.WriteString(choice.Delta.Content)
			}
			if choice.Delta.ReasoningContent != "" {
				// Reasoning summary deltas are not folded into the final
				// content (matches the anthropic path, which keeps thinking
				// out of the aggregated message content).
				continue
			}
			for _, tc := range choice.Delta.ToolCalls {
				for len(toolCalls) <= tc.Index {
					toolCalls = append(toolCalls, ToolCall{Type: "function"})
				}
				if tc.ID != "" {
					toolCalls[tc.Index].ID = tc.ID
				}
				if tc.Type != "" {
					toolCalls[tc.Index].Type = tc.Type
				}
				if tc.Function.Name != "" {
					toolCalls[tc.Index].Function.Name += tc.Function.Name
				}
				toolCalls[tc.Index].Function.Arguments += tc.Function.Arguments
			}
			if choice.FinishReason != nil {
				finishReason = *choice.FinishReason
			}
		}
	}

	if content.Len() > 0 {
		resp.Choices[0].Message.Content = content.String()
	}
	if len(toolCalls) > 0 {
		resp.Choices[0].Message.ToolCalls = toolCalls
		resp.Choices[0].Message.Content = nil
	}
	if finishReason != "" {
		resp.Choices[0].FinishReason = finishReason
	}
	return resp
}
