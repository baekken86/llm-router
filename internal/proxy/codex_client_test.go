package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- ChatCompletion (non-streaming aggregation) -------------------------------

func TestCodexClient_ChatCompletion_HeadersAndBody(t *testing.T) {
	var gotMethod, gotPath string
	var gotHeaders http.Header
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotHeaders = r.Header.Clone()
		gotBody, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: " + codexEventJSON(t, map[string]any{
			"type":  "response.output_text.delta",
			"delta": "Hello",
		}) + "\n\n"))
		w.Write([]byte("data: " + codexEventJSON(t, map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"usage": map[string]any{
					"input_tokens":  12,
					"output_tokens": 34,
				},
			},
		}) + "\n\n"))
	}))
	defer srv.Close()

	client := NewCodexClient()
	req := ChatCompletionRequest{
		Model:    "gpt-5.1-codex",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}
	resp, err := client.ChatCompletion(context.Background(), srv.URL, "tok-1", "acct-9", "sess-7", req)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp == nil {
		t.Fatal("resp should not be nil")
	}

	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/responses" {
		t.Errorf("path = %q, want /responses", gotPath)
	}

	wantHeaders := map[string]string{
		"Content-Type":       "application/json",
		"Authorization":      "Bearer tok-1",
		"Originator":         "codex_cli_rs",
		"User-Agent":         "codex_cli_rs/0.136.0",
		"Accept":             "text/event-stream",
		"session_id":         "sess-7",
		"ChatGPT-Account-Id": "acct-9",
	}
	for k, v := range wantHeaders {
		if gotHeaders.Get(k) != v {
			t.Errorf("header %q = %q, want %q", k, gotHeaders.Get(k), v)
		}
	}

	// Body must be the translated Responses shape (spot-check key contract
	// fields that ChatToCodexResponses guarantees).
	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("decode request body: %v (%q)", err, string(gotBody))
	}
	if body["stream"] != true {
		t.Errorf("body stream = %v, want true", body["stream"])
	}
	if body["store"] != false {
		t.Errorf("body store = %v, want false", body["store"])
	}
	if body["prompt_cache_key"] != "sess-7" {
		t.Errorf("body prompt_cache_key = %v, want sess-7", body["prompt_cache_key"])
	}
	if body["model"] != "gpt-5.1-codex" {
		t.Errorf("body model = %v, want gpt-5.1-codex", body["model"])
	}
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "medium" {
		t.Errorf("body reasoning effort = %v, want medium", body["reasoning"])
	}
}

func TestCodexClient_ChatCompletion_OmitsAccountIDWhenEmpty(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Write([]byte("data: " + codexEventJSON(t, map[string]any{"type": "response.completed", "response": map[string]any{}}) + "\n\n"))
	}))
	defer srv.Close()

	client := NewCodexClient()
	_, err := client.ChatCompletion(context.Background(), srv.URL, "tok-1", "", "sess-1", ChatCompletionRequest{
		Model:    "gpt-5.1",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if gotHeaders.Get("ChatGPT-Account-Id") != "" {
		t.Errorf("ChatGPT-Account-ID should be omitted when empty, got %q", gotHeaders.Get("ChatGPT-Account-Id"))
	}
}

func TestCodexClient_ChatCompletion_AggregatesTextAndUsage(t *testing.T) {
	srv := codexSSRServer(t, []string{
		`{"type":"response.output_text.delta","delta":"Hel"}`,
		`{"type":"response.output_text.delta","delta":"lo"}`,
		`{"type":"response.reasoning_summary_text.delta","delta":"thinking"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":7,"output_tokens_details":{"reasoning_tokens":2}}}}`,
	})
	defer srv.Close()

	client := NewCodexClient()
	resp, err := client.ChatCompletion(context.Background(), srv.URL, "tok", "acct", "sess", ChatCompletionRequest{
		Model:    "gpt-5.1",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	if got := resp.Choices[0].Message.Content; got != "Hello" {
		t.Errorf("content = %q, want %q", got, "Hello")
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", resp.Choices[0].FinishReason)
	}
	if resp.Usage.PromptTokens != 5 || resp.Usage.CompletionTokens != 7 || resp.Usage.TotalTokens != 12 {
		t.Errorf("usage = %+v, want prompt=5 completion=7 total=12", resp.Usage)
	}
	if resp.Usage.CompletionTokensDetails == nil || resp.Usage.CompletionTokensDetails.ReasoningTokens != 2 {
		t.Errorf("reasoning tokens not mapped: %+v", resp.Usage.CompletionTokensDetails)
	}
}

// --- Streaming translation over the pipe ---------------------------------------

func TestCodexClient_ChatCompletionStream_TranslatesSSE(t *testing.T) {
	srv := codexSSRServer(t, []string{
		`{"type":"response.output_text.delta","delta":"Hel"}`,
		`{"type":"response.output_text.delta","delta":"lo"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":2}}}`,
	})
	defer srv.Close()

	client := NewCodexClient()
	body, httpResp, err := client.ChatCompletionStream(context.Background(), srv.URL, "tok", "acct", "sess", ChatCompletionRequest{
		Model:    "gpt-5.1-codex",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletionStream: %v", err)
	}
	if httpResp == nil || httpResp.StatusCode != http.StatusOK {
		t.Fatalf("httpResp = %+v, want 200", httpResp)
	}
	defer body.Close()

	var chunks []StreamChunk
	var sawDone bool
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			sawDone = true
			break
		}
		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("decode chunk %q: %v", data, err)
		}
		chunks = append(chunks, chunk)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !sawDone {
		t.Error("final data: [DONE] missing")
	}
	if len(chunks) < 3 {
		t.Fatalf("chunks = %d, want >= 3", len(chunks))
	}

	var content strings.Builder
	var finish *string
	for _, c := range chunks {
		if c.Model != "gpt-5.1-codex" {
			t.Errorf("chunk model = %q, want gpt-5.1-codex", c.Model)
		}
		for _, choice := range c.Choices {
			content.WriteString(choice.Delta.Content)
			if choice.FinishReason != nil {
				finish = choice.FinishReason
			}
		}
	}
	if content.String() != "Hello" {
		t.Errorf("streamed content = %q, want %q", content.String(), "Hello")
	}
	if finish == nil || *finish != "stop" {
		t.Errorf("finish reason = %v, want stop", finish)
	}
	if chunks[len(chunks)-1].Usage == nil || chunks[len(chunks)-1].Usage.PromptTokens != 3 {
		t.Errorf("final chunk usage missing: %+v", chunks[len(chunks)-1].Usage)
	}
}

// --- Non-200 error mapping ------------------------------------------------------

func TestCodexClient_ChatCompletion_402MapsSubscriptionRequired(t *testing.T) {
	srv := codexStatusServer(t, http.StatusPaymentRequired, `{"error":"subscription required"}`)
	defer srv.Close()

	client := NewCodexClient()
	_, err := client.ChatCompletion(context.Background(), srv.URL, "tok", "", "sess", codexSimpleReq())
	var subErr *SubscriptionRequiredError
	if !errors.As(err, &subErr) {
		t.Fatalf("error %v (%T) should be *SubscriptionRequiredError", err, err)
	}
}

func TestCodexClient_ChatCompletion_429UsageLimitSetsRetryAfter(t *testing.T) {
	srv := codexStatusServer(t, http.StatusTooManyRequests, `{"error":{"type":"usage_limit_reached","resets_in_seconds":120}}`)
	defer srv.Close()

	client := NewCodexClient()
	_, err := client.ChatCompletion(context.Background(), srv.URL, "tok", "", "sess", codexSimpleReq())
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("error %v (%T) should be *ProviderError", err, err)
	}
	if pe.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", pe.StatusCode)
	}
	if pe.RetryAfter != 120*time.Second {
		t.Errorf("RetryAfter = %v, want 2m", pe.RetryAfter)
	}
}

func TestCodexClient_ChatCompletion_429ResetsAtSetsRetryAfter(t *testing.T) {
	srv := codexStatusServer(t, http.StatusTooManyRequests, fmt.Sprintf(`{"error":{"type":"usage_limit_reached","resets_at":%d}}`, time.Now().Add(5*time.Minute).Unix()))
	defer srv.Close()

	client := NewCodexClient()
	_, err := client.ChatCompletion(context.Background(), srv.URL, "tok", "", "sess", codexSimpleReq())
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("error %v (%T) should be *ProviderError", err, err)
	}
	if pe.RetryAfter <= 4*time.Minute || pe.RetryAfter > 5*time.Minute {
		t.Errorf("RetryAfter = %v, want ~5m", pe.RetryAfter)
	}
}

func TestCodexClient_ChatCompletion_500MapsProviderError(t *testing.T) {
	srv := codexStatusServer(t, http.StatusInternalServerError, `upstream exploded`)
	defer srv.Close()

	client := NewCodexClient()
	_, err := client.ChatCompletion(context.Background(), srv.URL, "tok", "", "sess", codexSimpleReq())
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("error %v (%T) should be *ProviderError", err, err)
	}
	if pe.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", pe.StatusCode)
	}
	if !strings.Contains(pe.Message, "upstream exploded") {
		t.Errorf("message should carry the body, got %q", pe.Message)
	}
}

func TestCodexClient_ChatCompletion_401CodexTokenExpired(t *testing.T) {
	srv := codexStatusServer(t, http.StatusUnauthorized, `{"error":"invalid_token"}`)
	defer srv.Close()

	client := NewCodexClient()
	_, err := client.ChatCompletion(context.Background(), srv.URL, "tok", "", "sess", codexSimpleReq())
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("error %v (%T) should be *ProviderError", err, err)
	}
	if pe.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", pe.StatusCode)
	}
	if !strings.Contains(pe.Error(), "codex token expired") {
		t.Errorf("message %q should contain 'codex token expired'", pe.Error())
	}
}

func TestCodexClient_ChatCompletionStream_401CodexTokenExpired(t *testing.T) {
	srv := codexStatusServer(t, http.StatusUnauthorized, `{"error":"invalid_token"}`)
	defer srv.Close()

	client := NewCodexClient()
	_, _, err := client.ChatCompletionStream(context.Background(), srv.URL, "tok", "", "sess", codexSimpleReq())
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("error %v (%T) should be *ProviderError", err, err)
	}
	if pe.StatusCode != http.StatusUnauthorized || !strings.Contains(pe.Error(), "codex token expired") {
		t.Errorf("got %+v, want 401 'codex token expired'", pe)
	}
}

// --- In-stream error events ------------------------------------------------------

func TestCodexClient_StreamErrorEventMapsTo502(t *testing.T) {
	srv := codexSSRServer(t, []string{
		`{"type":"error","error":{"code":"internal","message":"boom in stream"}}`,
	})
	defer srv.Close()

	client := NewCodexClient()
	_, err := client.ChatCompletion(context.Background(), srv.URL, "tok", "", "sess", codexSimpleReq())
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("error %v (%T) should be *ProviderError", err, err)
	}
	if pe.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", pe.StatusCode)
	}
	if !strings.Contains(pe.Message, "boom in stream") {
		t.Errorf("message = %q, want the stream error text", pe.Message)
	}
}

func TestCodexClient_StreamCapacityEventMapsTo503(t *testing.T) {
	cases := []struct {
		name    string
		message string
	}{
		{"model_at_capacity", "Selected model is at capacity, please try again later."},
		{"server_is_overloaded", "server_is_overloaded"},
		{"service_unavailable_error", "service_unavailable_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := codexSSRServer(t, []string{
				`{"type":"error","error":{"code":"capacity","message":"` + tc.message + `"}}`,
			})
			defer srv.Close()

			client := NewCodexClient()
			_, err := client.ChatCompletion(context.Background(), srv.URL, "tok", "", "sess", codexSimpleReq())
			pe, ok := err.(*ProviderError)
			if !ok {
				t.Fatalf("error %v (%T) should be *ProviderError", err, err)
			}
			if pe.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503", pe.StatusCode)
			}
			if pe.RetryAfter != 30*time.Second {
				t.Errorf("RetryAfter = %v, want 30s", pe.RetryAfter)
			}
			if !strings.Contains(pe.Message, tc.message) {
				t.Errorf("message = %q, want capacity text", pe.Message)
			}
		})
	}
}

func TestCodexClient_StreamErrorEvent_WhileStreaming(t *testing.T) {
	// Error arriving mid-stream on the piped (streaming) path must surface
	// via CloseWithError on the pipe.
	srv := codexSSRServer(t, []string{
		`{"type":"response.output_text.delta","delta":"partial"}`,
		`{"type":"error","error":{"code":"x","message":"mid-stream failure"}}`,
	})
	defer srv.Close()

	client := NewCodexClient()
	body, _, err := client.ChatCompletionStream(context.Background(), srv.URL, "tok", "", "sess", codexSimpleReq())
	if err != nil {
		t.Fatalf("ChatCompletionStream: %v", err)
	}
	defer body.Close()

	raw, readErr := io.ReadAll(body)
	if readErr == nil {
		t.Fatalf("expected read error from pipe, got body %q", string(raw))
	}
	if !strings.Contains(readErr.Error(), "mid-stream failure") {
		t.Errorf("pipe error = %v, want 'mid-stream failure'", readErr)
	}
}

// --- ListModels -------------------------------------------------------------------

func TestCodexClient_ListModels_ParsesCatalog(t *testing.T) {
	var gotPath, gotQuery, gotAccept, gotOriginator string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAccept = r.Header.Get("Accept")
		gotOriginator = r.Header.Get("Originator")
		w.Write([]byte(`{"models":[
			{"slug":"gpt-5.1-codex","display_name":"GPT-5.1 Codex","description":"d",
			 "default_reasoning_level":"medium",
			 "supported_reasoning_levels":[{"effort":"low","description":"low"},{"effort":"high","description":"high"}],
			 "visibility":"list","supported_in_api":true,"context_window":272000,"priority":1},
			{"slug":"gpt-5.1-codex-max","display_name":"GPT-5.1 Codex Max","visibility":"hidden","supported_in_api":false},
			{"slug":"gpt-5.1-codex-omit","display_name":"GPT-5.1 Codex Omit","visibility":"list","context_window":100000}
		]}`))
	}))
	defer srv.Close()

	client := NewCodexClient()
	models, err := client.ListModels(context.Background(), srv.URL, "tok", "acct-1")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}

	if gotPath != "/models" {
		t.Errorf("path = %q, want /models", gotPath)
	}
	if gotQuery != "client_version=0.136.0" {
		t.Errorf("query = %q, want client_version=0.136.0", gotQuery)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if gotOriginator != "codex_cli_rs" {
		t.Errorf("Originator = %q, want codex_cli_rs", gotOriginator)
	}

	if len(models) != 3 {
		t.Fatalf("models = %d, want 3 (no filtering here)", len(models))
	}
	m := models[0]
	if m.Slug != "gpt-5.1-codex" || m.DisplayName != "GPT-5.1 Codex" || m.Visibility != "list" {
		t.Errorf("first model = %+v", m)
	}
	if m.SupportedInAPI == nil || !*m.SupportedInAPI || m.ContextWindow != 272000 || m.DefaultReasoningLevel != "medium" {
		t.Errorf("first model fields = %+v (SupportedInAPI should be set true)", m)
	}
	if len(m.SupportedReasoningLevels) != 2 || m.SupportedReasoningLevels[0].Effort != "low" {
		t.Errorf("reasoning levels = %+v", m.SupportedReasoningLevels)
	}

	// Explicit false decodes to a set pointer; an omitted field to nil.
	mFalse := models[1]
	if mFalse.SupportedInAPI == nil || *mFalse.SupportedInAPI {
		t.Errorf("explicit supported_in_api=false = %v, want pointer to false", mFalse.SupportedInAPI)
	}

	// An omitted supported_in_api field must decode to nil (not false): the
	// model service's "supported_in_api != false" rule keeps omitted entries.
	m2 := models[2]
	if m2.SupportedInAPI != nil {
		t.Errorf("omitted supported_in_api = %v, want nil", m2.SupportedInAPI)
	}
}

func TestCodexClient_ListModels_SupportedInAPI_OmittedIsNil(t *testing.T) {
	// Catalog entry without the supported_in_api key at all.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models":[{"slug":"gpt-5.1-codex","visibility":"list","context_window":400000}]}`))
	}))
	defer srv.Close()

	client := NewCodexClient()
	models, err := client.ListModels(context.Background(), srv.URL, "tok", "")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %d, want 1", len(models))
	}
	if models[0].SupportedInAPI != nil {
		t.Errorf("SupportedInAPI = %v, want nil when the field is omitted", models[0].SupportedInAPI)
	}
}

func TestCodexClient_ListModels_401(t *testing.T) {
	srv := codexStatusServer(t, http.StatusUnauthorized, `{"error":"invalid_token"}`)
	defer srv.Close()

	client := NewCodexClient()
	_, err := client.ListModels(context.Background(), srv.URL, "tok", "")
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("error %v (%T) should be *ProviderError", err, err)
	}
	if pe.StatusCode != http.StatusUnauthorized || !strings.Contains(pe.Error(), "codex token expired") {
		t.Errorf("got %+v, want 401 'codex token expired'", pe)
	}
}

func TestCodexClient_ListModels_Non200WrapsBody(t *testing.T) {
	srv := codexStatusServer(t, http.StatusForbidden, `denied`)
	defer srv.Close()

	client := NewCodexClient()
	_, err := client.ListModels(context.Background(), srv.URL, "tok", "")
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Errorf("error = %v, want body wrapped", err)
	}
}

// --- Helpers ----------------------------------------------------------------------

// codexSimpleReq is a minimal valid request for error-path tests.
func codexSimpleReq() ChatCompletionRequest {
	return ChatCompletionRequest{
		Model:    "gpt-5.1",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}
}

// codexSSRServer replies 200 with the given SSE data lines.
func codexSSRServer(t *testing.T, events []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range events {
			w.Write([]byte("data: " + ev + "\n\n"))
		}
	}))
}

// codexStatusServer replies with a fixed status/body (no SSE).
func codexStatusServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

// codexEventJSON marshals an SSE event payload for test wires.
func codexEventJSON(t *testing.T, ev map[string]any) string {
	t.Helper()
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
