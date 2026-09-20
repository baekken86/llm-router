package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

// Thinking-mode providers (DeepSeek via OpenRouter) require assistant
// messages to carry reasoning_content back on follow-up turns. The proxy
// re-encodes every request through ChatCompletionRequest/Message, so the
// field must survive a decode→marshal roundtrip.
func TestReasoningContentPassthrough(t *testing.T) {
	raw := `{"model":"deepseek/deepseek-v4.1-flash","messages":[
		{"role":"user","content":"hi"},
		{"role":"assistant","content":"answer","reasoning_content":"thinking trace"},
		{"role":"user","content":"continue"}
	]}`

	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(req.Messages))
	}
	if got := req.Messages[1].ReasoningContent; got != "thinking trace" {
		t.Fatalf("reasoning_content dropped on decode: %q", got)
	}

	out, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"reasoning_content":"thinking trace"`) {
		t.Fatalf("reasoning_content dropped on marshal: %s", out)
	}

	// User messages must not gain the field.
	if strings.Count(string(out), "reasoning_content") != 1 {
		t.Fatalf("unexpected reasoning_content occurrences: %s", out)
	}
}

// Anthropic→OpenAI translation must surface thinking blocks as
// reasoning_content so follow-up turns stay valid.
func TestAnthropicToOpenAIPreservesReasoning(t *testing.T) {
	resp := &AnthropicResponse{
		ID:      "msg_1",
		Content: []AnthropicContent{{Type: "thinking", Thinking: "chain"}, {Type: "text", Text: "hi"}},
	}
	out := AnthropicToOpenAI(resp, "m")
	if got := out.Choices[0].Message.ReasoningContent; got != "chain" {
		t.Fatalf("expected reasoning_content 'chain', got %q", got)
	}
	data, err := json.Marshal(out.Choices[0].Message)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"reasoning_content":"chain"`) {
		t.Fatalf("marshaled message missing reasoning_content: %s", data)
	}
}


