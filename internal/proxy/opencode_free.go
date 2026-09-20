package proxy

import "strings"

// OpenCode zen free-tier gate (providers opencode-go / opencode-zen, models
// with a "-free" suffix). Verified live 2026-09-19 against
// https://opencode.ai/zen (mimo-v2.5-free, nemotron-3-ultra-free): the
// Console upstream only serves -free models when the request
//
//  1. has "stream": true in the body (non-stream -> 403 FreeTierError),
//  2. carries OpenCode's own CLI system prompt as a system message — the
//     gate is a CONTENT FINGERPRINT (the first ~975 chars of the CLI's
//     title-generator prompt pass; arbitrary text of the same length does
//     not), and
//  3. identifies as the OpenCode CLI (see opencodeUserAgent) and uses the
//     anonymous key "Bearer public" — free quota is per-IP, the user's real
//     key must NOT be sent (403/429 otherwise).
//
// Non-free models bypass the gate entirely. This gate is opencode.ai's
// client-side behavior and may change on their side.
const opencodeSystemPromptMarker = `You are a title generator. You output ONLY a thread title. Nothing else.

<task>
Generate a brief title that would help the user find this conversation later.

Follow all rules in <rules>
Use the <examples> so you know what a good title looks like.
Your output must be:
- A single line
- ≤50 characters
- No explanations
</task>

<rules>
- you MUST use the same language as the user message you are summarizing
- Title must be grammatically correct and read naturally - no word salad
- Never include tool names in the title (e.g. "read tool", "bash tool", "edit tool")
- Focus on the main topic or question the user needs to retrieve
- Vary your phrasing - avoid repetitive patterns like always starting with "Analyzing"
- When a file is mentioned, focus on WHAT the user wants to do WITH the file, not just that they shared it
- Keep exact: technical terms, numbers, filenames, HTTP codes
- Remove: the, this, my, a, an
- Never assume tech stack
- Never use tools
- NEVER re`

// opencodeFreeAuthKey is the anonymous bearer token the OpenCode CLI sends
// for -free models (free quota is per-IP, not per-key).
const opencodeFreeAuthKey = "public"

// isOpencodeFreeModel reports whether a model name targets OpenCode zen's
// free tier (-free suffix, case-insensitive).
func isOpencodeFreeModel(modelName string) bool {
	return strings.HasSuffix(strings.ToLower(modelName), "-free")
}

// opencodeFreeRequest builds the outgoing request for a zen -free model:
// the content-marker system message is prepended (outgoing copy only, the
// caller's struct and message slice are never mutated) and the request is
// upgraded to stream:true + include_usage, which the free-tier gate
// requires. The upstream then replies with SSE even for callers that asked
// for a non-stream response; aggregateFreeStreamResponse folds those chunks
// back into a normal ChatCompletionResponse.
func opencodeFreeRequest(req ChatCompletionRequest) ChatCompletionRequest {
	out := req
	out.Messages = append([]Message{{Role: "system", Content: opencodeSystemPromptMarker}}, req.Messages...)
	out.Stream = true
	out.StreamOptions = &StreamOptions{IncludeUsage: true}
	return out
}

// toolCallAccumulator collects streamed tool-call deltas for one index.
type toolCallAccumulator struct {
	id   string
	name string
	args strings.Builder
}

// aggregateFreeStreamResponse folds zen-free SSE chunks into a standard
// ChatCompletionResponse: delta.content is concatenated into one assistant
// message, tool-call deltas are merged by index (id/name overwrite,
// arguments concatenated), usage is taken from the last chunk carrying it.
// Returns nil if the stream produced no chunks.
func aggregateFreeStreamResponse(chunks <-chan StreamChunk) *ChatCompletionResponse {
	var last *StreamChunk
	var lastUsage *Usage
	var content strings.Builder
	var reasoning strings.Builder
	var finishReason string
	accs := map[int]*toolCallAccumulator{}
	var order []int

	for chunk := range chunks {
		chunk := chunk
		last = &chunk
		if chunk.Usage != nil {
			lastUsage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		content.WriteString(delta.Content)
		if delta.ReasoningContent != "" {
			reasoning.WriteString(delta.ReasoningContent)
		}
		for _, tc := range delta.ToolCalls {
			acc, ok := accs[tc.Index]
			if !ok {
				acc = &toolCallAccumulator{}
				accs[tc.Index] = acc
				order = append(order, tc.Index)
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			acc.args.WriteString(tc.Function.Arguments)
		}
		if chunk.Choices[0].FinishReason != nil && *chunk.Choices[0].FinishReason != "" {
			finishReason = *chunk.Choices[0].FinishReason
		}
	}
	if last == nil {
		return nil
	}

	msg := Message{Role: "assistant", Content: content.String()}
	msg.ReasoningContent = reasoning.String()
	for _, idx := range order {
		acc := accs[idx]
		tc := ToolCall{ID: acc.id, Type: "function"}
		tc.Function.Name = acc.name
		tc.Function.Arguments = acc.args.String()
		msg.ToolCalls = append(msg.ToolCalls, tc)
	}
	if finishReason == "" {
		finishReason = "stop"
	}

	id := last.ID
	if id == "" {
		id = "zen-free-agg"
	}
	out := &ChatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: last.Created,
		Model:   last.Model,
		Choices: []Choice{{Index: 0, Message: msg, FinishReason: finishReason}},
	}
	if lastUsage != nil {
		out.Usage = *lastUsage
	}
	return out
}
