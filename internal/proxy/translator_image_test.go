package proxy

import (
	"encoding/json"
	"testing"
)

// --- OpenAIToAnthropic image/content-part conversion -------------------------

func TestOpenAIToAnthropic_DataURIImageBecomesBase64Source(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "claude-sonnet-4",
		Messages: []Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{
					"url": "data:image/jpeg;base64,QUJDREVG",
				}},
			},
		}},
	}

	anthReq := OpenAIToAnthropic(req)

	if len(anthReq.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(anthReq.Messages))
	}
	blocks, ok := anthReq.Messages[0].Content.([]AnthropicContent)
	if !ok {
		t.Fatalf("expected []AnthropicContent content, got %T", anthReq.Messages[0].Content)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d: %+v", len(blocks), blocks)
	}
	block := blocks[0]
	if block.Type != "image" {
		t.Errorf("block type must be image, got %q", block.Type)
	}
	if block.Source == nil {
		t.Fatal("image block must carry a source")
	}
	if block.Source.Type != "base64" {
		t.Errorf("source type must be base64, got %q", block.Source.Type)
	}
	if block.Source.MediaType != "image/jpeg" {
		t.Errorf("media type must be parsed from the data URI, got %q", block.Source.MediaType)
	}
	if block.Source.Data != "QUJDREVG" {
		t.Errorf("base64 payload must be everything after base64,, got %q", block.Source.Data)
	}
	if block.Source.URL != "" {
		t.Errorf("base64 source must not carry a url, got %q", block.Source.URL)
	}
}

func TestOpenAIToAnthropic_DataURIDefaultsMediaTypeToPNG(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "claude-sonnet-4",
		Messages: []Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{
					"url": "data:;base64,QUJD",
				}},
			},
		}},
	}

	anthReq := OpenAIToAnthropic(req)

	blocks, ok := anthReq.Messages[0].Content.([]AnthropicContent)
	if !ok {
		t.Fatalf("expected []AnthropicContent content, got %T", anthReq.Messages[0].Content)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	src := blocks[0].Source
	if src == nil || src.Type != "base64" {
		t.Fatalf("expected base64 source, got %+v", blocks[0].Source)
	}
	if src.MediaType != "image/png" {
		t.Errorf("absent mime must default to image/png, got %q", src.MediaType)
	}
}

func TestOpenAIToAnthropic_HTTPSImageURLBecomesURLSource(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "claude-sonnet-4",
		Messages: []Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{
					"url": "https://example.com/cat.png",
				}},
			},
		}},
	}

	anthReq := OpenAIToAnthropic(req)

	blocks, ok := anthReq.Messages[0].Content.([]AnthropicContent)
	if !ok {
		t.Fatalf("expected []AnthropicContent content, got %T", anthReq.Messages[0].Content)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d: %+v", len(blocks), blocks)
	}
	block := blocks[0]
	if block.Type != "image" {
		t.Errorf("block type must be image, got %q", block.Type)
	}
	if block.Source == nil || block.Source.Type != "url" {
		t.Fatalf("expected url source, got %+v", block.Source)
	}
	if block.Source.URL != "https://example.com/cat.png" {
		t.Errorf("url must be preserved verbatim, got %q", block.Source.URL)
	}
	if block.Source.Data != "" || block.Source.MediaType != "" {
		t.Errorf("url source must not carry base64 fields, got %+v", block.Source)
	}
}

func TestOpenAIToAnthropic_ImageURLStringShorthand(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "claude-sonnet-4",
		Messages: []Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": "data:image/webp;base64,WFZa"},
			},
		}},
	}

	anthReq := OpenAIToAnthropic(req)

	blocks, ok := anthReq.Messages[0].Content.([]AnthropicContent)
	if !ok {
		t.Fatalf("expected []AnthropicContent content, got %T", anthReq.Messages[0].Content)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	src := blocks[0].Source
	if src == nil || src.Type != "base64" || src.MediaType != "image/webp" || src.Data != "WFZa" {
		t.Errorf("string shorthand must convert like the object form, got %+v", src)
	}
}

func TestOpenAIToAnthropic_TextAndImagePartsMixed(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "claude-sonnet-4",
		Messages: []Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "What is in this image?"},
				map[string]any{"type": "image_url", "image_url": map[string]any{
					"url": "https://example.com/dog.jpg",
				}},
			},
		}},
	}

	anthReq := OpenAIToAnthropic(req)

	blocks, ok := anthReq.Messages[0].Content.([]AnthropicContent)
	if !ok {
		t.Fatalf("expected []AnthropicContent content, got %T", anthReq.Messages[0].Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Type != "text" || blocks[0].Text != "What is in this image?" {
		t.Errorf("text part must be preserved first, got %+v", blocks[0])
	}
	if blocks[1].Type != "image" || blocks[1].Source == nil || blocks[1].Source.URL != "https://example.com/dog.jpg" {
		t.Errorf("image part must follow the text part, got %+v", blocks[1])
	}
}

func TestOpenAIToAnthropic_SystemContentPartsJoinedIntoSystem(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "claude-sonnet-4",
		Messages: []Message{
			{
				Role: "system",
				Content: []any{
					map[string]any{"type": "text", "text": "Be terse."},
					map[string]any{"type": "text", "text": "Prefer Go."},
					map[string]any{"type": "image_url", "image_url": map[string]any{
						"url": "https://example.com/banner.png",
					}},
				},
			},
			{Role: "user", Content: "hi"},
		},
	}

	anthReq := OpenAIToAnthropic(req)

	if anthReq.System != "Be terse.\n\nPrefer Go." {
		t.Errorf("system text parts must be joined with blank lines, got %q", anthReq.System)
	}
	if len(anthReq.Messages) != 1 || anthReq.Messages[0].Role != "user" {
		t.Errorf("system message must be dropped from messages, got %+v", anthReq.Messages)
	}
}

func TestOpenAIToAnthropic_StringContentUnchanged(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "claude-sonnet-4",
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
	}

	anthReq := OpenAIToAnthropic(req)

	if anthReq.System != "You are helpful." {
		t.Errorf("string system content must pass through, got %v", anthReq.System)
	}
	if len(anthReq.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(anthReq.Messages))
	}
	if content, ok := anthReq.Messages[0].Content.(string); !ok || content != "Hello" {
		t.Errorf("string user content must stay a plain string, got %T %v",
			anthReq.Messages[0].Content, anthReq.Messages[0].Content)
	}
}

func TestOpenAIToAnthropic_MalformedImagePartsSkipped(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "claude-sonnet-4",
		Messages: []Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url"},                                             // no image_url value
				map[string]any{"type": "image_url", "image_url": map[string]any{}},              // no url key
				map[string]any{"type": "image_url", "image_url": "ftp://x/y.png"},               // unsupported scheme
				map[string]any{"type": "image_url", "image_url": "data:text/plain;base64,QQ=="}, // non-image mime
				map[string]any{"type": "audio", "data": "zzz"},                                  // unknown part type
				map[string]any{"type": "text", "text": "still here"},
			},
		}},
	}

	anthReq := OpenAIToAnthropic(req)

	blocks, ok := anthReq.Messages[0].Content.([]AnthropicContent)
	if !ok {
		t.Fatalf("expected []AnthropicContent content, got %T", anthReq.Messages[0].Content)
	}
	if len(blocks) != 1 || blocks[0].Type != "text" || blocks[0].Text != "still here" {
		t.Errorf("only the valid text part must survive, got %+v", blocks)
	}
}

func TestOpenAIToAnthropic_AllPartsSkippedOmitsMessage(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "claude-sonnet-4",
		Messages: []Message{
			{Role: "user", Content: []any{
				map[string]any{"type": "image_url", "image_url": "ftp://x/y.png"},
			}},
			{Role: "user", Content: "real question"},
		},
	}

	anthReq := OpenAIToAnthropic(req)

	if len(anthReq.Messages) != 1 {
		t.Fatalf("fully-skipped content must omit the message, got %d: %+v",
			len(anthReq.Messages), anthReq.Messages)
	}
	if content, ok := anthReq.Messages[0].Content.(string); !ok || content != "real question" {
		t.Errorf("remaining message wrong: %v", anthReq.Messages[0].Content)
	}
}

func TestOpenAIToAnthropic_ImageBlockSerializesToAnthropicShape(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "claude-sonnet-4",
		Messages: []Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{
					"url": "data:image/gif;base64,R0lGOD",
				}},
			},
		}},
	}

	anthReq := OpenAIToAnthropic(req)
	data, err := json.Marshal(anthReq)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var body struct {
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(body.Messages) == 0 || len(body.Messages[0].Content) == 0 {
		t.Fatalf("expected a message with one content block, got %s", data)
	}
	block := body.Messages[0].Content[0]
	if block["type"] != "image" {
		t.Fatalf("expected image block on the wire, got %v", block)
	}
	src, _ := block["source"].(map[string]any)
	if src["type"] != "base64" || src["media_type"] != "image/gif" || src["data"] != "R0lGOD" {
		t.Errorf("source must serialize as Anthropic base64 source, got %v", src)
	}
	if _, hasURL := src["url"]; hasURL {
		t.Error("base64 source must not serialize a url field")
	}
}
