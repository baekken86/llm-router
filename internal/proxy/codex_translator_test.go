package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- ChatToCodexResponses ---------------------------------------------------

func TestChatToCodexResponses_FullParamStripping(t *testing.T) {
	temp := 0.7
	topP := 0.9
	maxTokens := 1024
	effort := "high"

	req := ChatCompletionRequest{
		Model:           "gpt-5.1-codex",
		Messages:        []Message{{Role: "user", Content: "Hello"}},
		Temperature:     &temp,
		TopP:            &topP,
		MaxTokens:       &maxTokens,
		Stop:            []string{"END"},
		ReasoningEffort: &effort,
	}

	body, err := ChatToCodexResponses(req, "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the allowed key set may appear.
	allowed := map[string]bool{
		"model": true, "instructions": true, "input": true, "tools": true,
		"tool_choice": true, "parallel_tool_calls": true, "stream": true,
		"store": true, "reasoning": true, "include": true,
		"service_tier": true, "prompt_cache_key": true,
	}
	for k := range body {
		if !allowed[k] {
			t.Errorf("unexpected key %q in translated body", k)
		}
	}
	for _, k := range []string{"temperature", "top_p", "frequency_penalty", "presence_penalty",
		"logprobs", "top_logprobs", "n", "seed", "max_tokens", "max_completion_tokens",
		"max_output_tokens", "user", "metadata", "stream_options", "safety_identifier",
		"response_format", "stop", "reasoning_effort"} {
		if _, ok := body[k]; ok {
			t.Errorf("dropped param %q leaked into body", k)
		}
	}
}

func TestChatToCodexResponses_ForcedStreamStoreAndCacheKey(t *testing.T) {
	body, err := ChatToCodexResponses(ChatCompletionRequest{
		Model:    "gpt-5.1",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, "sess-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["stream"] != true {
		t.Errorf("stream must be forced true, got %v", body["stream"])
	}
	if body["store"] != false {
		t.Errorf("store must be forced false, got %v", body["store"])
	}
	if body["prompt_cache_key"] != "sess-abc" {
		t.Errorf("prompt_cache_key = sessionID, got %v", body["prompt_cache_key"])
	}
}

func TestChatToCodexResponses_SystemToInstructions(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "gpt-5.1-codex",
		Messages: []Message{
			{Role: "system", Content: "Be terse."},
			{Role: "system", Content: "Prefer Go."},
			{Role: "user", Content: "Write a test."},
		},
	}
	body, err := ChatToCodexResponses(req, "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	instructions, _ := body["instructions"].(string)
	if !strings.Contains(instructions, "Be terse.") || !strings.Contains(instructions, "Prefer Go.") {
		t.Errorf("system messages must fold into instructions, got %q", instructions)
	}

	input, _ := body["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("system messages must be removed from input, got %d items", len(input))
	}
	item, _ := input[0].(map[string]any)
	if item["role"] != "user" {
		t.Errorf("expected user message, got %v", item["role"])
	}
}

func TestChatToCodexResponses_DefaultInstructionsWhenNoSystem(t *testing.T) {
	req := ChatCompletionRequest{
		Model:    "gpt-5.1-codex",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}
	body, err := ChatToCodexResponses(req, "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["instructions"] != CodexDefaultInstructions {
		t.Errorf("expected default instructions, got %v", body["instructions"])
	}
	if !strings.Contains(CodexDefaultInstructions, "You are Codex") {
		t.Errorf("default instructions should be the Codex preamble, got %q", CodexDefaultInstructions)
	}
}

func TestChatToCodexResponses_ToolFlattening(t *testing.T) {
	req := ChatCompletionRequest{
		Model:    "gpt-5.1-codex",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools: []Tool{{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
			},
		}},
	}
	body, err := ChatToCodexResponses(req, "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 flattened tool, got %v", body["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Errorf("tool type must be function, got %v", tool["type"])
	}
	if tool["name"] != "get_weather" {
		t.Errorf("tool must be flattened: name at top level, got %v", tool)
	}
	if _, hasNested := tool["function"]; hasNested {
		t.Error("chat-shape nested function object must not survive")
	}
	if tool["description"] != "Get weather" {
		t.Errorf("description lost, got %v", tool["description"])
	}
	params, ok := tool["parameters"].(map[string]any)
	if !ok || params["type"] != "object" {
		t.Errorf("parameters lost, got %v", tool["parameters"])
	}
}

func TestChatToCodexResponses_ToolCallHistory(t *testing.T) {
	tc := ToolCall{ID: "call_1", Type: "function"}
	tc.Function.Name = "get_weather"
	tc.Function.Arguments = `{"city":"SF"}`

	req := ChatCompletionRequest{
		Model: "gpt-5.1-codex",
		Messages: []Message{
			{Role: "user", Content: "Weather?"},
			{Role: "assistant", ToolCalls: []ToolCall{tc}},
			{Role: "tool", ToolCallID: "call_1", Content: "72F and sunny"},
		},
	}
	body, err := ChatToCodexResponses(req, "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	input, _ := body["input"].([]any)
	if len(input) != 3 {
		t.Fatalf("expected 3 input items, got %d: %v", len(input), body["input"])
	}

	call, _ := input[1].(map[string]any)
	if call["type"] != "function_call" {
		t.Errorf("expected function_call item, got %v", call)
	}
	if call["call_id"] != "call_1" || call["name"] != "get_weather" {
		t.Errorf("function_call fields wrong: %v", call)
	}
	if call["arguments"] != `{"city":"SF"}` {
		t.Errorf("arguments lost, got %v", call["arguments"])
	}

	out, _ := input[2].(map[string]any)
	if out["type"] != "function_call_output" {
		t.Errorf("expected function_call_output item, got %v", out)
	}
	if out["call_id"] != "call_1" {
		t.Errorf("call_id lost, got %v", out["call_id"])
	}
	if out["output"] != "72F and sunny" {
		t.Errorf("output lost, got %v", out["output"])
	}
}

func TestChatToCodexResponses_AssistantContentBecomesOutputText(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "gpt-5.1-codex",
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello there"},
		},
	}
	body, err := ChatToCodexResponses(req, "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	input, _ := body["input"].([]any)
	if len(input) != 2 {
		t.Fatalf("expected 2 items, got %d", len(input))
	}
	msg, _ := input[1].(map[string]any)
	if msg["role"] != "assistant" {
		t.Errorf("expected assistant role, got %v", msg["role"])
	}
	content, _ := msg["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 content part, got %v", msg["content"])
	}
	part, _ := content[0].(map[string]any)
	if part["type"] != "output_text" || part["text"] != "hello there" {
		t.Errorf("expected output_text part, got %v", part)
	}
}

func TestChatToCodexResponses_ImageContentPassThrough(t *testing.T) {
	dataURI := "data:image/png;base64,AAAA"
	req := ChatCompletionRequest{
		Model: "gpt-5.1-codex",
		Messages: []Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "What is this?"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": dataURI}},
				map[string]any{"type": "image_url", "image_url": "https://example.com/cat.png"},
			},
		}},
	}
	body, err := ChatToCodexResponses(req, "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	input, _ := body["input"].([]any)
	msg, _ := input[0].(map[string]any)
	content, _ := msg["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("expected 3 content parts, got %v", msg["content"])
	}
	text, _ := content[0].(map[string]any)
	if text["type"] != "input_text" || text["text"] != "What is this?" {
		t.Errorf("text part wrong: %v", text)
	}
	embed, _ := content[1].(map[string]any)
	if embed["type"] != "input_image" || embed["image_url"] != dataURI {
		t.Errorf("data URI image part wrong: %v", embed)
	}
	url, _ := content[2].(map[string]any)
	if url["type"] != "input_image" || url["image_url"] != "https://example.com/cat.png" {
		t.Errorf("URL image part wrong: %v", url)
	}
}

func TestChatToCodexResponses_ReasoningFromReqEffort(t *testing.T) {
	effort := "high"
	body, err := ChatToCodexResponses(ChatCompletionRequest{
		Model:           "gpt-5.1-codex",
		Messages:        []Message{{Role: "user", Content: "hi"}},
		ReasoningEffort: &effort,
	}, "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	reasoning, _ := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Errorf("expected effort high, got %v", reasoning)
	}
	if reasoning["summary"] != "auto" {
		t.Errorf("expected summary auto, got %v", reasoning["summary"])
	}
	include, _ := body["include"].([]any)
	if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Errorf("expected include reasoning.encrypted_content, got %v", body["include"])
	}
}

func TestChatToCodexResponses_ReasoningFromModelSuffix(t *testing.T) {
	cases := []struct {
		model     string
		effort    string
		wantModel string
	}{
		{"gpt-5.1-codex-high", "high", "gpt-5.1-codex"},
		{"gpt-5.1-codex-low", "low", "gpt-5.1-codex"},
		{"gpt-5.1-codex-xhigh", "xhigh", "gpt-5.1-codex"},
		{"gpt-5.1-codex-max", "xhigh", "gpt-5.1-codex"}, // -max → xhigh
		{"gpt-5.1-codex", "medium", "gpt-5.1-codex"},    // default
	}
	for _, c := range cases {
		body, err := ChatToCodexResponses(ChatCompletionRequest{
			Model:    c.model,
			Messages: []Message{{Role: "user", Content: "hi"}},
		}, "sess-1")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.model, err)
		}
		if body["model"] != c.wantModel {
			t.Errorf("%s: expected stripped model %q, got %v", c.model, c.wantModel, body["model"])
		}
		reasoning, _ := body["reasoning"].(map[string]any)
		if reasoning["effort"] != c.effort {
			t.Errorf("%s: expected effort %q, got %v", c.model, c.effort, reasoning["effort"])
		}
	}
}

func TestChatToCodexResponses_ReqEffortOverridesSuffix(t *testing.T) {
	effort := "low"
	body, err := ChatToCodexResponses(ChatCompletionRequest{
		Model:           "gpt-5.1-codex-high",
		Messages:        []Message{{Role: "user", Content: "hi"}},
		ReasoningEffort: &effort,
	}, "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	reasoning, _ := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "low" {
		t.Errorf("req effort should win over suffix, got %v", reasoning["effort"])
	}
}

func TestChatToCodexResponses_EffortNoneOmitsReasoningAndInclude(t *testing.T) {
	effort := "none"
	body, err := ChatToCodexResponses(ChatCompletionRequest{
		Model:           "gpt-5.1-codex",
		Messages:        []Message{{Role: "user", Content: "hi"}},
		ReasoningEffort: &effort,
	}, "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := body["reasoning"]; ok {
		t.Errorf("reasoning must be omitted for effort none, got %v", body["reasoning"])
	}
	if _, ok := body["include"]; ok {
		t.Errorf("include must be omitted for effort none, got %v", body["include"])
	}
}

func TestChatToCodexResponses_ServiceTier(t *testing.T) {
	body, err := ChatToCodexResponses(ChatCompletionRequest{
		Model:    "gpt-5.1-codex",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := body["service_tier"]; ok {
		t.Errorf("service_tier must be dropped when unset (and unknown values never forwarded), got %v", body["service_tier"])
	}

	// Mapping table: fast→priority, anything else dropped. The internal
	// ChatCompletionRequest carries no service_tier field, so the mapping
	// helper is what the raw-request path (T005) applies.
	if got := codexMapServiceTier("fast"); got != "priority" {
		t.Errorf("fast must map to priority, got %q", got)
	}
	for _, tier := range []string{"", "auto", "default", "flex", "priority", "scale"} {
		if got := codexMapServiceTier(tier); got != "" {
			t.Errorf("tier %q must be dropped, got %q", tier, got)
		}
	}
}

func TestCodexResolveEffort_SuffixEdgeCases(t *testing.T) {
	cases := []struct {
		model      string
		wantEffort string
		wantModel  string
	}{
		{"gpt-5.1", "medium", "gpt-5.1"}, // no suffix → default
		{"gpt-5.1-codex", "medium", "gpt-5.1-codex"},
		{"gpt-5.1-codex-max", "xhigh", "gpt-5.1-codex"}, // -max → xhigh on non-max base name
		{"gpt-5.1-codex-mini-high", "high", "gpt-5.1-codex-mini"},
		{"gpt-5.1-turbo", "medium", "gpt-5.1-turbo"}, // unknown suffix untouched
	}
	for _, c := range cases {
		effort, model := codexResolveEffort(c.model, ChatCompletionRequest{})
		if effort != c.wantEffort {
			t.Errorf("%s: expected effort %q, got %q", c.model, c.wantEffort, effort)
		}
		if model != c.wantModel {
			t.Errorf("%s: expected model %q, got %q", c.model, c.wantModel, model)
		}
	}

	// Explicit effort wins over everything.
	effort := "none"
	got, model := codexResolveEffort("gpt-5.1-codex-max", ChatCompletionRequest{ReasoningEffort: &effort})
	if got != "none" || model != "gpt-5.1-codex" {
		t.Errorf("explicit effort must win: got effort=%q model=%q", got, model)
	}

	// Empty explicit effort falls back to suffix resolution.
	empty := ""
	got, _ = codexResolveEffort("gpt-5.1-codex-high", ChatCompletionRequest{ReasoningEffort: &empty})
	if got != "high" {
		t.Errorf("empty explicit effort must fall back to suffix, got %q", got)
	}
}

// --- ProcessEvent -----------------------------------------------------------

func mustEvent(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return data
}

func TestCodexProcessEvent_TextDelta(t *testing.T) {
	s := NewCodexStreamState("gpt-5.1-codex", "req-1")
	chunks, err := s.ProcessEvent(mustEvent(t, map[string]any{
		"type": "response.output_text.delta", "delta": "Hello",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Choices[0].Delta.Content != "Hello" {
		t.Errorf("expected content delta, got %v", chunks[0].Choices[0].Delta)
	}
	if chunks[0].Object != "chat.completion.chunk" {
		t.Errorf("expected chat.completion.chunk object, got %s", chunks[0].Object)
	}
	if !strings.HasPrefix(chunks[0].ID, "chatcmpl-") {
		t.Errorf("expected chatcmpl- prefixed ID, got %s", chunks[0].ID)
	}
}

func TestCodexProcessEvent_ReasoningSummaryDelta(t *testing.T) {
	s := NewCodexStreamState("gpt-5.1-codex", "req-1")
	chunks, err := s.ProcessEvent(mustEvent(t, map[string]any{
		"type": "response.reasoning_summary_text.delta", "delta": "thinking…",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	// Must match the anthropic translator convention (delta.reasoning_content).
	if chunks[0].Choices[0].Delta.ReasoningContent != "thinking…" {
		t.Errorf("expected reasoning_content delta, got %v", chunks[0].Choices[0].Delta)
	}
}

func TestCodexProcessEvent_ToolCallAccumulation(t *testing.T) {
	s := NewCodexStreamState("gpt-5.1-codex", "req-1")

	// First delta creates the tool call with id+name.
	chunks, err := s.ProcessEvent(mustEvent(t, map[string]any{
		"type":    "response.function_call_arguments.delta",
		"item_id": "fc_1", "call_id": "call_9", "name": "get_weather", "delta": `{"ci`,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	stc := chunks[0].Choices[0].Delta.ToolCalls
	if len(stc) != 1 {
		t.Fatalf("expected tool_calls delta, got %v", chunks[0].Choices[0].Delta)
	}
	if stc[0].Index != 0 || stc[0].ID != "call_9" || stc[0].Type != "function" {
		t.Errorf("first tool delta wrong: %+v", stc[0])
	}
	if stc[0].Function.Arguments != `{"ci` {
		t.Errorf("arguments delta lost: %q", stc[0].Function.Arguments)
	}

	// Second delta appends without re-sending id/name.
	chunks, err = s.ProcessEvent(mustEvent(t, map[string]any{
		"type":    "response.function_call_arguments.delta",
		"item_id": "fc_1", "call_id": "call_9", "delta": `ty":"SF"}`,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stc = chunks[0].Choices[0].Delta.ToolCalls
	if len(stc) != 1 || stc[0].Index != 0 || stc[0].Function.Arguments != `ty":"SF"}` {
		t.Errorf("second tool delta wrong: %+v", stc)
	}

	// Internal state accumulated the full arguments.
	if s.ToolCalls["call_9"].Arguments != `{"city":"SF"}` {
		t.Errorf("arguments must accumulate, got %q", s.ToolCalls["call_9"].Arguments)
	}
	if s.ToolCalls["call_9"].Name != "get_weather" {
		t.Errorf("tool name must be tracked, got %q", s.ToolCalls["call_9"].Name)
	}
}

func TestCodexProcessEvent_InterleavedTextAndToolCall(t *testing.T) {
	s := NewCodexStreamState("gpt-5.1-codex", "req-1")

	events := [][]byte{
		mustEvent(t, map[string]any{"type": "response.output_text.delta", "delta": "Let me check "}),
		mustEvent(t, map[string]any{"type": "response.output_text.delta", "delta": "the weather."}),
		mustEvent(t, map[string]any{
			"type":    "response.function_call_arguments.delta",
			"item_id": "fc_1", "call_id": "call_a", "name": "get_weather", "delta": `{"city":"SF"}`,
		}),
		mustEvent(t, map[string]any{
			"type":    "response.function_call_arguments.delta",
			"item_id": "fc_2", "call_id": "call_b", "name": "get_time", "delta": `{"tz":"PT"}`,
		}),
		mustEvent(t, map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id": "resp_1",
				"usage": map[string]any{
					"input_tokens": 120, "output_tokens": 45,
					"output_tokens_details": map[string]any{"reasoning_tokens": 20},
				},
			},
		}),
	}

	var all []StreamChunk
	for _, ev := range events {
		chunks, err := s.ProcessEvent(ev)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		all = append(all, chunks...)
	}

	if len(all) != 5 {
		t.Fatalf("expected 5 chunks total, got %d", len(all))
	}
	if all[0].Choices[0].Delta.Content != "Let me check " {
		t.Errorf("text delta 1 wrong: %v", all[0].Choices[0].Delta)
	}
	if all[1].Choices[0].Delta.Content != "the weather." {
		t.Errorf("text delta 2 wrong: %v", all[1].Choices[0].Delta)
	}
	if all[2].Choices[0].Delta.ToolCalls[0].ID != "call_a" {
		t.Errorf("tool call a wrong: %+v", all[2].Choices[0].Delta.ToolCalls)
	}
	if all[3].Choices[0].Delta.ToolCalls[0].Index != 1 || all[3].Choices[0].Delta.ToolCalls[0].ID != "call_b" {
		t.Errorf("tool call b wrong (index must advance): %+v", all[3].Choices[0].Delta.ToolCalls)
	}

	final := all[4]
	if final.Choices[0].FinishReason == nil || *final.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason must be tool_calls, got %v", final.Choices[0].FinishReason)
	}
	if final.Usage == nil {
		t.Fatal("final chunk must carry usage")
	}
	if final.Usage.PromptTokens != 120 || final.Usage.CompletionTokens != 45 {
		t.Errorf("usage mapping wrong: %+v", final.Usage)
	}
	if final.Usage.TotalTokens != 165 {
		t.Errorf("total tokens wrong: %d", final.Usage.TotalTokens)
	}
	if final.Usage.CompletionTokensDetails == nil || final.Usage.CompletionTokensDetails.ReasoningTokens != 20 {
		t.Errorf("reasoning tokens wrong: %+v", final.Usage.CompletionTokensDetails)
	}
	if s.FinishDone != true {
		t.Error("FinishDone must be set after response.completed")
	}
}

func TestCodexProcessEvent_CompletedStopFinish(t *testing.T) {
	s := NewCodexStreamState("gpt-5.1-codex", "req-1")
	s.ProcessEvent(mustEvent(t, map[string]any{"type": "response.output_text.delta", "delta": "done"}))

	chunks, err := s.ProcessEvent(mustEvent(t, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 5},
		},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if *chunks[0].Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason must be stop, got %v", chunks[0].Choices[0].FinishReason)
	}
	if chunks[0].Usage == nil || chunks[0].Usage.PromptTokens != 10 || chunks[0].Usage.CompletionTokens != 5 {
		t.Errorf("usage wrong: %+v", chunks[0].Usage)
	}
	if chunks[0].Usage.CompletionTokensDetails != nil {
		t.Errorf("no reasoning tokens expected, got %+v", chunks[0].Usage.CompletionTokensDetails)
	}
}

func TestCodexProcessEvent_ErrorEvent(t *testing.T) {
	s := NewCodexStreamState("gpt-5.1-codex", "req-1")
	_, err := s.ProcessEvent(mustEvent(t, map[string]any{
		"type":  "error",
		"error": map[string]any{"code": "server_is_overloaded", "message": "Server is overloaded"},
	}))
	if err == nil {
		t.Fatal("error event must return an error")
	}
	if !strings.Contains(err.Error(), "Server is overloaded") {
		t.Errorf("error message must be included, got %v", err)
	}
}

func TestCodexProcessEvent_UnknownEventIgnored(t *testing.T) {
	s := NewCodexStreamState("gpt-5.1-codex", "req-1")
	for _, typ := range []string{
		"response.created", "response.in_progress",
		"response.output_item.added", "response.output_item.done",
		"response.content_part.added", "response.content_part.done",
		"response.output_text.done", "response.function_call_arguments.done",
		"response.reasoning_summary_part.added", "ping",
	} {
		chunks, err := s.ProcessEvent(mustEvent(t, map[string]any{"type": typ}))
		if err != nil {
			t.Fatalf("%s: unknown events must be ignored, got error %v", typ, err)
		}
		if len(chunks) != 0 {
			t.Errorf("%s: expected no chunks, got %d", typ, len(chunks))
		}
	}
}

func TestCodexProcessEvent_MalformedJSON(t *testing.T) {
	s := NewCodexStreamState("gpt-5.1-codex", "req-1")
	if _, err := s.ProcessEvent([]byte("not json")); err == nil {
		t.Error("malformed event JSON must return an error")
	}
}

// --- AggregateCodexStream ---------------------------------------------------

func TestAggregateCodexStream_TextAndUsage(t *testing.T) {
	state := NewCodexStreamState("gpt-5.1-codex", "req-1")
	var chunks []StreamChunk
	for _, ev := range [][]byte{
		mustEvent(t, map[string]any{"type": "response.output_text.delta", "delta": "Hel"}),
		mustEvent(t, map[string]any{"type": "response.output_text.delta", "delta": "lo"}),
		mustEvent(t, map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"usage": map[string]any{"input_tokens": 11, "output_tokens": 7},
			},
		}),
	} {
		out, err := state.ProcessEvent(ev)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, out...)
	}

	resp := AggregateCodexStream(chunks)
	if resp.Object != "chat.completion" {
		t.Errorf("object must be chat.completion, got %s", resp.Object)
	}
	if content, ok := resp.Choices[0].Message.Content.(string); !ok || content != "Hello" {
		t.Errorf("aggregated content wrong: %v", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("role must be assistant, got %s", resp.Choices[0].Message.Role)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason wrong: %s", resp.Choices[0].FinishReason)
	}
	if resp.Usage.PromptTokens != 11 || resp.Usage.CompletionTokens != 7 {
		t.Errorf("usage wrong: %+v", resp.Usage)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 0 {
		t.Errorf("no tool calls expected: %+v", resp.Choices[0].Message.ToolCalls)
	}
}

func TestAggregateCodexStream_ToolCalls(t *testing.T) {
	state := NewCodexStreamState("gpt-5.1-codex", "req-1")
	var chunks []StreamChunk
	for _, ev := range [][]byte{
		mustEvent(t, map[string]any{"type": "response.output_text.delta", "delta": "checking"}),
		mustEvent(t, map[string]any{
			"type":    "response.function_call_arguments.delta",
			"item_id": "fc_1", "call_id": "call_x", "name": "get_weather", "delta": `{"city":`,
		}),
		mustEvent(t, map[string]any{
			"type":    "response.function_call_arguments.delta",
			"item_id": "fc_1", "call_id": "call_x", "delta": `"SF"}`,
		}),
		mustEvent(t, map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"usage": map[string]any{
					"input_tokens": 50, "output_tokens": 30,
					"output_tokens_details": map[string]any{"reasoning_tokens": 12},
				},
			},
		}),
	} {
		out, err := state.ProcessEvent(ev)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, out...)
	}

	resp := AggregateCodexStream(chunks)
	msg := resp.Choices[0].Message
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 aggregated tool call, got %+v", msg.ToolCalls)
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_x" || tc.Type != "function" {
		t.Errorf("tool call identity wrong: %+v", tc)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("tool name wrong: %q", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"city":"SF"}` {
		t.Errorf("arguments must be joined, got %q", tc.Function.Arguments)
	}
	if msg.Content != nil {
		t.Errorf("content must be nil when tool calls present (OpenAI convention), got %v", msg.Content)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason wrong: %s", resp.Choices[0].FinishReason)
	}
	if resp.Usage.CompletionTokensDetails == nil || resp.Usage.CompletionTokensDetails.ReasoningTokens != 12 {
		t.Errorf("reasoning tokens wrong: %+v", resp.Usage.CompletionTokensDetails)
	}
}
