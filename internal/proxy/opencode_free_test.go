package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The marker must be the exact 975-rune prefix of OpenCode's CLI system
// prompt — the free-tier gate is a content fingerprint, so any drift breaks
// -free models.
func TestOpencodeSystemPromptMarker_LengthAndPrefix(t *testing.T) {
	runes := []rune(opencodeSystemPromptMarker)
	if len(runes) != 975 {
		t.Fatalf("marker length = %d runes, want 975", len(runes))
	}
	if !strings.HasPrefix(opencodeSystemPromptMarker, "You are a title generator.") {
		t.Errorf("marker must start with the CLI title-generator prompt, got %q", firstRunes(opencodeSystemPromptMarker, 40))
	}
}

func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func TestIsOpencodeFreeModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"mimo-v2.5-free", true},
		{"nemotron-3-ultra-free", true},
		{"MUSE-FREE", true},
		{"glm-5.3-flash", false},
		{"free", false},
		{"-free", true},
		{"", false},
	}
	for _, tt := range tests {
		if got := isOpencodeFreeModel(tt.model); got != tt.want {
			t.Errorf("isOpencodeFreeModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

// opencodeFreeSSRServer captures the outgoing request and replies with a
// minimal OpenAI-compatible SSE stream (content delta + usage + [DONE]).
func opencodeFreeSSRServer(t *testing.T, captured *struct {
	auth       string
	userAgent  string
	clientHdr  string
	bodyString string
}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured.auth = r.Header.Get("Authorization")
		captured.userAgent = r.Header.Get("User-Agent")
		captured.clientHdr = r.Header.Get("X-Opencode-Client")
		captured.bodyString = string(body)

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"mimo-v2.5-free","choices":[{"index":0,"delta":{"role":"assistant","content":"he"},"finish_reason":null}]}`)
		fmt.Fprintln(w, `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"mimo-v2.5-free","choices":[{"index":0,"delta":{"content":"llo"},"finish_reason":null}]}`)
		fmt.Fprintln(w, `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"mimo-v2.5-free","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
}

func TestChatCompletion_OpencodeFree_Gate(t *testing.T) {
	origHostCheck := isOpencodeHost
	isOpencodeHost = func(string) bool { return true }
	defer func() { isOpencodeHost = origHostCheck }()

	origUA := opencodeUserAgent
	opencodeUserAgent = "opencode/test-fixed"
	defer func() { opencodeUserAgent = origUA }()

	var captured struct {
		auth       string
		userAgent  string
		clientHdr  string
		bodyString string
	}
	srv := opencodeFreeSSRServer(t, &captured)
	defer srv.Close()

	client := NewOpenAIClient()
	req := ChatCompletionRequest{
		Model:  "mimo-v2.5-free",
		Stream: false, // caller asked for non-stream; gate forces stream:true
		Messages: []Message{
			{Role: "system", Content: "be helpful"},
			{Role: "user", Content: "say ok"},
		},
	}
	resp, err := client.ChatCompletion(srv.URL, "real-secret-key", "sess_abc", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wire assertions: anonymous auth, CLI identity, stream forced on.
	if captured.auth != "Bearer public" {
		t.Errorf("Authorization = %q, want %q (real key must not be sent for -free models)", captured.auth, "Bearer public")
	}
	if captured.userAgent != "opencode/test-fixed" {
		t.Errorf("User-Agent = %q, want %q", captured.userAgent, "opencode/test-fixed")
	}
	if captured.clientHdr != "cli" {
		t.Errorf("X-Opencode-Client = %q, want cli", captured.clientHdr)
	}

	var outgoing ChatCompletionRequest
	if err := json.Unmarshal([]byte(captured.bodyString), &outgoing); err != nil {
		t.Fatalf("decode outgoing body: %v", err)
	}
	if !outgoing.Stream {
		t.Error("outgoing body must have stream:true")
	}
	if len(outgoing.Messages) != 3 {
		t.Fatalf("outgoing messages = %d, want 3 (marker + caller's 2)", len(outgoing.Messages))
	}
	if outgoing.Messages[0].Role != "system" || outgoing.Messages[0].Content != opencodeSystemPromptMarker {
		t.Errorf("first outgoing message must be the marker system message, got role=%q content-prefix=%q",
			outgoing.Messages[0].Role, firstRunes(fmt.Sprint(outgoing.Messages[0].Content), 40))
	}
	if outgoing.Messages[1].Content != "be helpful" || outgoing.Messages[2].Content != "say ok" {
		t.Error("caller's messages must follow the marker untouched")
	}

	// Aggregation assertions: SSE folded into a normal completion response.
	if resp.ID != "chatcmpl-1" || resp.Object != "chat.completion" {
		t.Errorf("resp ID/Object = %q/%q, want chatcmpl-1/chat.completion", resp.ID, resp.Object)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	if got := resp.Choices[0].Message.Content; got != "hello" {
		t.Errorf("aggregated content = %q, want %q", got, "hello")
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", resp.Choices[0].FinishReason)
	}
	if resp.Usage.PromptTokens != 7 || resp.Usage.CompletionTokens != 2 || resp.Usage.TotalTokens != 9 {
		t.Errorf("usage = %+v, want 7/2/9", resp.Usage)
	}

	// Caller's struct must not be mutated.
	if req.Stream != false {
		t.Error("caller's request Stream flag must remain false")
	}
	if len(req.Messages) != 2 {
		t.Errorf("caller's message count = %d, want 2 (no marker injected)", len(req.Messages))
	}
}

func TestChatCompletion_OpencodeNonFree_Unchanged(t *testing.T) {
	origHostCheck := isOpencodeHost
	isOpencodeHost = func(string) bool { return true }
	defer func() { isOpencodeHost = origHostCheck }()

	var captured struct {
		auth       string
		userAgent  string
		clientHdr  string
		bodyString string
	}
	srv := opencodeFreeSSRServer(t, &captured)
	defer srv.Close()

	// The non-free path returns JSON, not SSE — swap in a JSON responder.
	srv.Close()
	jsonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured.auth = r.Header.Get("Authorization")
		captured.userAgent = r.Header.Get("User-Agent")
		captured.bodyString = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer jsonSrv.Close()

	client := NewOpenAIClient()
	req := ChatCompletionRequest{
		Model: "glm-5.3-flash",
		Messages: []Message{
			{Role: "user", Content: "say ok"},
		},
	}
	resp, err := client.ChatCompletion(jsonSrv.URL, "real-secret-key", "sess_abc", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if captured.auth != "Bearer real-secret-key" {
		t.Errorf("Authorization = %q, want provider key", captured.auth)
	}
	if captured.userAgent != opencodeUserAgent {
		t.Errorf("User-Agent = %q, want %q", captured.userAgent, opencodeUserAgent)
	}

	var outgoing ChatCompletionRequest
	if err := json.Unmarshal([]byte(captured.bodyString), &outgoing); err != nil {
		t.Fatalf("decode outgoing body: %v", err)
	}
	if outgoing.Stream {
		t.Error("non-free model must not have stream forced")
	}
	if len(outgoing.Messages) != 1 || outgoing.Messages[0].Content == opencodeSystemPromptMarker {
		t.Error("non-free model must not get the marker system message")
	}
	if resp.Choices[0].Message.Content != "hi" {
		t.Errorf("content = %q, want hi", resp.Choices[0].Message.Content)
	}
}

func TestAggregateFreeStreamResponse_ToolCallDeltas(t *testing.T) {
	chunks := []StreamChunk{
		{ID: "c1", Choices: []StreamChoice{{Index: 0, Delta: StreamDelta{Role: "assistant", ToolCalls: []StreamToolCall{{Index: 0, ID: "call_1", Type: "function", Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments"`
		}{Name: "get_weather", Arguments: `{"cit`}}}}}}},
		{ID: "c1", Choices: []StreamChoice{{Index: 0, Delta: StreamDelta{ToolCalls: []StreamToolCall{{Index: 0, Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments"`
		}{Arguments: `y":"paris"}`}}}}}}},
		{ID: "c1", Choices: []StreamChoice{{Index: 0, Delta: StreamDelta{}, FinishReason: strPtr("tool_calls")}}},
		{ID: "c1", Usage: &Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7}},
	}

	ch := make(chan StreamChunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)

	resp := aggregateFreeStreamResponse(ch)
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	msg := resp.Choices[0].Message
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Type != "function" || tc.Function.Name != "get_weather" {
		t.Errorf("tool call = %+v, want id=call_1 name=get_weather", tc)
	}
	if tc.Function.Arguments != `{"city":"paris"}` {
		t.Errorf("concatenated arguments = %q, want %q", tc.Function.Arguments, `{"city":"paris"}`)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", resp.Choices[0].FinishReason)
	}
	if resp.Usage.TotalTokens != 7 {
		t.Errorf("usage total = %d, want 7", resp.Usage.TotalTokens)
	}
}

func strPtr(s string) *string { return &s }

// The opencode free-tier SSE aggregation must fold reasoning deltas into the
// aggregated assistant message (thinking-mode providers require them passed
// back on follow-up turns).
func TestAggregateFreeStreamResponseReasoning(t *testing.T) {
	ch := make(chan StreamChunk, 2)
	ch <- StreamChunk{Choices: []StreamChoice{{Index: 0, Delta: StreamDelta{ReasoningContent: "thought"}}}}
	ch <- StreamChunk{Choices: []StreamChoice{{Index: 0, Delta: StreamDelta{Content: "answer"}, FinishReason: strPtr("stop")}}}
	close(ch)

	resp := aggregateFreeStreamResponse(ch)
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if got := resp.Choices[0].Message.ReasoningContent; got != "thought" {
		t.Errorf("reasoning_content = %q, want %q", got, "thought")
	}
}
