package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/service"
)

// --- Test-case event JSON builders -------------------------------------------

func mustAnthropicEvent(t *testing.T, v map[string]interface{}) AnthropicStreamEvent {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	var ev AnthropicStreamEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	// Mirror ParseAnthropicSSEStream's raw-usage surfacing so tests exercise
	// the same decode path as production.
	ev.UsageRaw = extractUsageRaw(data)
	if len(ev.UsageRaw) == 0 && len(ev.Delta) > 0 {
		ev.UsageRaw = extractNestedDeltaUsage(ev.Delta)
	}
	return ev
}

func messageStartEvent(t *testing.T, usageJSON string) AnthropicStreamEvent {
	t.Helper()
	message := map[string]interface{}{
		"id":    "msg_123",
		"model": "claude-test",
	}
	if usageJSON != "" {
		message["usage"] = json.RawMessage(usageJSON)
	}
	return mustAnthropicEvent(t, map[string]interface{}{
		"type":    "message_start",
		"message": message,
	})
}

// --- Table-driven ProcessEvent usage tests -----------------------------------

// TestProcessEvent_UsageExtraction covers usage accumulation across the
// message_start → message_delta → message_stop sequence for both canonical
// Anthropic streams and zai-style streams (zeroed message_start usage, real
// totals only in the final message_delta, optionally nested inside delta).
func TestProcessEvent_UsageExtraction(t *testing.T) {
	tests := []struct {
		name      string
		events    []AnthropicStreamEvent
		wantUsage *Usage
	}{
		{
			name: "canonical message_start usage + output-only delta",
			events: []AnthropicStreamEvent{
				messageStartEvent(t, `{"input_tokens":10,"cache_read_input_tokens":3}`),
				mustAnthropicEvent(t, map[string]interface{}{
					"type":  "message_delta",
					"delta": map[string]interface{}{"stop_reason": "end_turn"},
					"usage": map[string]interface{}{"output_tokens": 42},
				}),
			},
			wantUsage: &Usage{
				PromptTokens:     13,
				CompletionTokens: 42,
				TotalTokens:      55,
				PromptTokensDetails: &PromptTokensDetails{
					CachedTokens: 3,
				},
			},
		},
		{
			name: "zai: zeroed message_start usage overwritten by cumulative delta",
			events: []AnthropicStreamEvent{
				messageStartEvent(t, `{"input_tokens":0,"output_tokens":0}`),
				mustAnthropicEvent(t, map[string]interface{}{
					"type":  "message_delta",
					"delta": map[string]interface{}{"stop_reason": "end_turn"},
					"usage": map[string]interface{}{
						"input_tokens":            16,
						"output_tokens":           31,
						"cache_read_input_tokens": 0,
						"server_tool_use":         map[string]interface{}{"web_search_requests": 0},
						"service_tier":            "standard",
					},
				}),
			},
			wantUsage: &Usage{
				PromptTokens:     16,
				CompletionTokens: 31,
				TotalTokens:      47,
				PromptTokensDetails: &PromptTokensDetails{
					CachedTokens: 0,
				},
			},
		},
		{
			name: "zai variant: message_start without usage key",
			events: []AnthropicStreamEvent{
				messageStartEvent(t, ""),
				mustAnthropicEvent(t, map[string]interface{}{
					"type":  "message_delta",
					"delta": map[string]interface{}{"stop_reason": "end_turn"},
					"usage": map[string]interface{}{
						"input_tokens":  14,
						"output_tokens": 32,
					},
				}),
			},
			wantUsage: &Usage{
				PromptTokens:     14,
				CompletionTokens: 32,
				TotalTokens:      46,
				PromptTokensDetails: &PromptTokensDetails{
					CachedTokens: 0,
				},
			},
		},
		{
			name: "usage nested inside delta object",
			events: []AnthropicStreamEvent{
				messageStartEvent(t, `{"input_tokens":0,"output_tokens":0}`),
				mustAnthropicEvent(t, map[string]interface{}{
					"type": "message_delta",
					"delta": map[string]interface{}{
						"stop_reason": "end_turn",
						"usage": map[string]interface{}{
							"input_tokens":  16,
							"output_tokens": 31,
						},
					},
				}),
			},
			wantUsage: &Usage{
				PromptTokens:     16,
				CompletionTokens: 31,
				TotalTokens:      47,
				PromptTokensDetails: &PromptTokensDetails{
					CachedTokens: 0,
				},
			},
		},
		{
			name: "OpenAI-style field names mapped as fallback",
			events: []AnthropicStreamEvent{
				messageStartEvent(t, ""),
				mustAnthropicEvent(t, map[string]interface{}{
					"type":  "message_delta",
					"delta": map[string]interface{}{"stop_reason": "end_turn"},
					"usage": map[string]interface{}{
						"prompt_tokens":     20,
						"completion_tokens": 11,
						"prompt_tokens_details": map[string]interface{}{
							"cached_tokens": 5,
						},
					},
				}),
			},
			wantUsage: &Usage{
				PromptTokens:     25,
				CompletionTokens: 11,
				TotalTokens:      36,
				PromptTokensDetails: &PromptTokensDetails{
					CachedTokens: 5,
				},
			},
		},
		{
			name: "unknown usage extras do not break parsing",
			events: []AnthropicStreamEvent{
				messageStartEvent(t, `{"input_tokens":7,"output_tokens":0,"server_tool_use":{"web_search_requests":2},"service_tier":"standard","prompt_tokens_details":{"cached_tokens":4}}`),
				mustAnthropicEvent(t, map[string]interface{}{
					"type":  "message_delta",
					"delta": map[string]interface{}{"stop_reason": "end_turn"},
					"usage": map[string]interface{}{
						"input_tokens":    9,
						"output_tokens":   21,
						"service_tier":    "standard",
						"some_new_future": map[string]interface{}{"x": 1},
					},
				}),
			},
			wantUsage: &Usage{
				PromptTokens:     13,
				CompletionTokens: 21,
				TotalTokens:      34,
				PromptTokensDetails: &PromptTokensDetails{
					CachedTokens: 4,
				},
			},
		},
		{
			name: "no usage anywhere: nil usage, stream completes",
			events: []AnthropicStreamEvent{
				messageStartEvent(t, ""),
				mustAnthropicEvent(t, map[string]interface{}{
					"type":  "message_delta",
					"delta": map[string]interface{}{"stop_reason": "end_turn"},
				}),
				mustAnthropicEvent(t, map[string]interface{}{"type": "message_stop"}),
			},
			wantUsage: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &ClaudeStreamState{
				Model:           "claude-test",
				ServerToolIndex: -1,
				ToolCalls:       make(map[int]*ToolCallState),
			}

			var lastChunk *StreamChunk
			for _, ev := range tt.events {
				for _, chunk := range state.ProcessEvent(ev) {
					lastChunk = chunk
				}
			}
			finalUsage := state.Usage

			if (tt.wantUsage == nil) != (finalUsage == nil) {
				t.Fatalf("state.Usage = %+v, want %+v", finalUsage, tt.wantUsage)
			}
			if tt.wantUsage == nil {
				// Regression: stream must still complete with a finish reason.
				if lastChunk == nil || lastChunk.Choices == nil || len(lastChunk.Choices) == 0 || lastChunk.Choices[0].FinishReason == nil {
					t.Fatalf("expected a final chunk with a finish reason, got %+v", lastChunk)
				}
				return
			}
			got := *finalUsage
			want := *tt.wantUsage
			if got.PromptTokens != want.PromptTokens ||
				got.CompletionTokens != want.CompletionTokens ||
				got.TotalTokens != want.TotalTokens {
				t.Errorf("usage = {prompt:%d completion:%d total:%d}, want {prompt:%d completion:%d total:%d}",
					got.PromptTokens, got.CompletionTokens, got.TotalTokens,
					want.PromptTokens, want.CompletionTokens, want.TotalTokens)
			}
			gotCached := 0
			if got.PromptTokensDetails != nil {
				gotCached = got.PromptTokensDetails.CachedTokens
			}
			wantCached := 0
			if want.PromptTokensDetails != nil {
				wantCached = want.PromptTokensDetails.CachedTokens
			}
			if gotCached != wantCached {
				t.Errorf("cached_tokens = %d, want %d", gotCached, wantCached)
			}
		})
	}
}

// --- AnthropicUsage tolerant decode -------------------------------------------

// TestAnthropicUsage_UnmarshalJSON_Tolerant covers OpenAI-style fallback
// naming and unknown-extra tolerance on the wire type itself.
func TestAnthropicUsage_UnmarshalJSON_Tolerant(t *testing.T) {
	tests := []struct {
		name string
		json string
		want AnthropicUsage
	}{
		{
			name: "anthropic canonical",
			json: `{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":2}`,
			want: AnthropicUsage{InputTokens: 10, OutputTokens: 5, CacheReadInputTokens: 2},
		},
		{
			name: "openai style mapped",
			json: `{"prompt_tokens":30,"completion_tokens":12,"prompt_tokens_details":{"cached_tokens":8}}`,
			want: AnthropicUsage{InputTokens: 30, OutputTokens: 12, CacheReadInputTokens: 8},
		},
		{
			name: "anthropic names win over openai names",
			json: `{"input_tokens":10,"prompt_tokens":99,"output_tokens":4,"completion_tokens":77}`,
			want: AnthropicUsage{InputTokens: 10, OutputTokens: 4},
		},
		{
			name: "unknown extras ignored",
			json: `{"input_tokens":3,"output_tokens":1,"server_tool_use":{"web_search_requests":9},"service_tier":"standard","future_field":{"nested":true}}`,
			want: AnthropicUsage{InputTokens: 3, OutputTokens: 1},
		},
		{
			name: "empty object",
			json: `{}`,
			want: AnthropicUsage{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got AnthropicUsage
			if err := json.Unmarshal([]byte(tt.json), &got); err != nil {
				t.Fatalf("unmarshal %s: %v", tt.json, err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// --- Passthrough-path coverage (engine_test harness) ---------------------------

// TestHandleAnthropicMessagesStream_UsagePassthrough drives the real
// HandleAnthropicMessagesStream → streamAnthropicPassthrough path with a fake
// Anthropic upstream for both canonical and zai-style usage layouts.
func TestHandleAnthropicMessagesStream_UsagePassthrough(t *testing.T) {
	tests := []struct {
		name           string
		sse            string
		wantPrompt     int
		wantCompletion int
		wantCached     int
	}{
		{
			name: "canonical",
			sse: "event: message_start\n" +
				`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":10,"cache_read_input_tokens":3}}}` + "\n\n" +
				"event: content_block_delta\n" +
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}` + "\n\n" +
				"event: message_delta\n" +
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42}}` + "\n\n" +
				"event: message_stop\n" +
				`data: {"type":"message_stop"}` + "\n\n",
			wantPrompt:     13,
			wantCompletion: 42,
			wantCached:     3,
		},
		{
			name: "zai cumulative final delta",
			sse: "event: message_start\n" +
				`data: {"type":"message_start","message":{"id":"msg_2","model":"glm-5.3","usage":{"input_tokens":0,"output_tokens":0}}}` + "\n\n" +
				"event: content_block_delta\n" +
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}` + "\n\n" +
				"event: message_delta\n" +
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":16,"output_tokens":31,"cache_read_input_tokens":0,"server_tool_use":{"web_search_requests":0},"service_tier":"standard"}}` + "\n\n" +
				"event: message_stop\n" +
				`data: {"type":"message_stop"}` + "\n\n",
			wantPrompt:     16,
			wantCompletion: 31,
			wantCached:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Write([]byte(tt.sse))
			}))
			defer upstream.Close()

			vmName := "test-anthropic-usage-vm"
			vmSvc := &mockVMService{
				getByNameFn: func(_ context.Context, name string) (*models.VirtualModel, error) {
					if name != vmName {
						return nil, nil
					}
					return &models.VirtualModel{ID: 1, Name: vmName, MaxRetries: 0}, nil
				},
				resolveFn: func(_ context.Context, _ *models.VirtualModel) ([]service.ResolvedModel, error) {
					return []service.ResolvedModel{
						{
							Model: models.Model{ID: 1, ProviderID: 1, Name: "claude-test"},
							Provider: models.Provider{
								ID:              1,
								Name:            "anth-test",
								APIType:         models.APITypeAnthropic,
								BaseURL:         upstream.URL,
								APIKeyEncrypted: "encrypted-test-key",
							},
						},
					}, nil
				},
			}
			provSvc := &mockProviderService{
				decryptKeyFn: func(_ string) (string, error) { return "test-api-key", nil },
			}
			oauthSvc := &mockOAuthService{
				getTokenFn: func(_ context.Context, _ int64) (string, error) { return "", nil },
			}

			engine, logChan := newTestEngine(vmSvc, provSvc, oauthSvc)

			body := `{"model":"` + vmName + `","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
			req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			engine.HandleAnthropicMessagesStream(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("HTTP status = %d, want 200; body: %s", w.Code, w.Body.String())
			}

			// Usage is logged on the final "proxy completed" entry (drainLogs
			// collects everything the handler emitted).
			var completed *RequestLog
			for i, log := range drainLogs(t, logChan) {
				_ = i
				if log.Type == "proxy" && log.Status == "completed" && log.StatusCode == http.StatusOK {
					l := log
					completed = &l
				}
			}
			if completed == nil {
				t.Fatalf("no completed proxy log; handler output:\n%s", w.Body.String())
			}
			if completed.InputTokens != tt.wantPrompt {
				t.Errorf("InputTokens = %d, want %d", completed.InputTokens, tt.wantPrompt)
			}
			if completed.OutputTokens != tt.wantCompletion {
				t.Errorf("OutputTokens = %d, want %d", completed.OutputTokens, tt.wantCompletion)
			}
			if completed.CachedTokens != tt.wantCached {
				t.Errorf("CachedTokens = %d, want %d", completed.CachedTokens, tt.wantCached)
			}
		})
	}
}
