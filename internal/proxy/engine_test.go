package proxy

import (
	"testing"
)

func TestOpenAIToAnthropic(t *testing.T) {
	maxTokens := 1024
	temp := 0.7

	req := ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
		MaxTokens:   &maxTokens,
		Temperature: &temp,
	}

	anthReq := OpenAIToAnthropic(req)

	if anthReq.Model != "gpt-4" {
		t.Errorf("expected model gpt-4, got %s", anthReq.Model)
	}
	if anthReq.MaxTokens != 1024 {
		t.Errorf("expected max_tokens 1024, got %d", anthReq.MaxTokens)
	}
	if anthReq.System != "You are helpful." {
		t.Errorf("expected system message, got %s", anthReq.System)
	}
	if len(anthReq.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(anthReq.Messages))
	}
	if anthReq.Messages[0].Role != "user" {
		t.Errorf("expected role user, got %s", anthReq.Messages[0].Role)
	}
}

func TestAnthropicToOpenAI(t *testing.T) {
	resp := &AnthropicResponse{
		ID:   "msg_123",
		Type: "message",
		Role: "assistant",
		Content: []AnthropicContent{
			{Type: "text", Text: "Hello there!"},
		},
		Model:      "claude-3-sonnet",
		StopReason: "end_turn",
		Usage: AnthropicUsage{
			InputTokens:  100,
			OutputTokens: 50,
			CacheReadInputTokens: 20,
		},
	}

	openResp := AnthropicToOpenAI(resp, "claude-3-sonnet")

	if openResp.ID != "msg_123" {
		t.Errorf("expected ID msg_123, got %s", openResp.ID)
	}
	if len(openResp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(openResp.Choices))
	}
	if openResp.Choices[0].Message.Content != "Hello there!" {
		t.Errorf("expected content 'Hello there!', got %v", openResp.Choices[0].Message.Content)
	}
	if openResp.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason stop, got %s", openResp.Choices[0].FinishReason)
	}
	if openResp.Usage.PromptTokens != 100 {
		t.Errorf("expected prompt_tokens 100, got %d", openResp.Usage.PromptTokens)
	}
	if openResp.Usage.CompletionTokens != 50 {
		t.Errorf("expected completion_tokens 50, got %d", openResp.Usage.CompletionTokens)
	}
	if openResp.Usage.PromptTokensDetails.CachedTokens != 20 {
		t.Errorf("expected cached_tokens 20, got %d", openResp.Usage.PromptTokensDetails.CachedTokens)
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"hello", 1},
		{"this is a test", 3},
		{"", 0},
	}

	for _, tt := range tests {
		result := EstimateTokens(tt.input)
		if result != tt.expected {
			t.Errorf("EstimateTokens(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestCalculateHeadroom(t *testing.T) {
	headroom := CalculateHeadroom(128000, 1000, 4096)
	if headroom != 122904 {
		t.Errorf("expected headroom 122904, got %d", headroom)
	}
}

func TestEstimateCost(t *testing.T) {
	cost := EstimateCost(1000, 500, 200, 2.50, 10.00, 1.25)
	expected := (800.0/1000.0*2.50) + (200.0/1000.0*1.25) + (500.0/1000.0*10.00)
	if cost != expected {
		t.Errorf("expected cost %f, got %f", expected, cost)
	}
}

func TestMapStopReason(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"end_turn", "stop"},
		{"max_tokens", "length"},
		{"tool_use", "tool_calls"},
		{"stop_sequence", "stop"},
		{"unknown", "stop"},
	}

	for _, tt := range tests {
		result := mapStopReason(tt.input)
		if result != tt.expected {
			t.Errorf("mapStopReason(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		status   int
		codes    []int
		expected bool
	}{
		{429, nil, true},
		{500, nil, true},
		{200, nil, false},
		{400, nil, false},
		{429, []int{429, 500}, true},
		{500, []int{429, 500}, true},
		{503, []int{429, 500}, false},
	}

	for _, tt := range tests {
		result := shouldRetry(tt.status, tt.codes)
		if result != tt.expected {
			t.Errorf("shouldRetry(%d, %v) = %v, want %v", tt.status, tt.codes, result, tt.expected)
		}
	}
}
