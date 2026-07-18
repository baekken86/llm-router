# Thinking Loop Detection and Nudge

**Date:** 2026-07-18
**Status:** Draft

## Problem

Some models (both Anthropic and OpenAI with reasoning) produce identical thinking blocks in a loop during streaming. The model repeats the same reasoning sequence without converging on an answer, wasting tokens and time.

## Solution

Detect when 3+ consecutive thinking blocks are identical, interrupt the stream, and retry with a system message that explicitly tells the model it entered a thinking loop and should stop reasoning.

## Architecture

### Core: `ThinkingLoopDetector`

```go
type ThinkingLoopDetector struct {
    maxRepetitions int
    buffer         []string       // ring buffer of completed thinking block contents
    currentBlock   strings.Builder
    loopDetected   bool
}
```

**Methods:**
- `BeginBlock()` -- reset the accumulator for a new thinking block
- `AppendDelta(content string)` -- accumulate thinking content
- `EndBlock() bool` -- finalize block, compare to previous N-1 blocks, return true if all identical

**Comparison:** Full string equality of completed block content. Thinking blocks must be exact duplicates (same whitespace, same tokens).

### Anthropic Path (`ClaudeStreamState`)

Integrates into `ProcessEvent()`:

| Event | Action |
|-------|--------|
| `content_block_start` (type=thinking) | `detector.BeginBlock()` |
| `content_block_delta` (type=thinking_delta) | `detector.AppendDelta(delta.Thinking)` |
| `content_block_stop` while `InThinkingBlock` | `detector.EndBlock()` -- if true, signal loop |

When loop detected: return a sentinel value from `ProcessEvent()` that tells `streamAnthropicToOpenAI` to close the upstream body.

### OpenAI Path (`streamPassthrough`)

Parses each SSE chunk. When `reasoning_content` is non-empty, accumulate into a block buffer. When a chunk arrives without `reasoning_content` (text content), finalize and compare. Same loop detection logic.

### Nudge and Retry

When loop detected in either path:

1. **Interrupt:** Close the upstream `body` (stops generation)
2. **Log:** Warning with request ID, model, provider, repetition count
3. **Signal to engine:** Return control to `HandleChatCompletionStream`'s outer loop
4. **Nudge retry:** Before retrying the same model, modify the request:
   - **Inject system message:** Prepend `"IMPORTANT: You have entered a repetitive thinking loop. Stop reasoning immediately and provide your answer directly. Do not continue thinking."` to the system messages
   - **Reduce thinking budget:** For Anthropic, halve `thinking.budget_tokens`. For OpenAI, set `reasoning_effort` to `"low"`
5. **Retry once:** Max 1 nudge per request (tracked by a `nudged bool` flag). If the second attempt also loops, fail over to the next model normally.

### Client Experience

The client sees:
1. Partial thinking stream (first 3 identical blocks)
2. Stream interruption (may see truncated SSE)
3. Fresh stream from the same model with nudge applied
4. Model produces answer (nudge is effective in most cases)

### Configuration

- `ThinkingLoopDetector.maxRepetitions` defaults to 3 (hardcoded for now, can be made configurable per virtual model later)

### File Changes

| File | Change |
|------|--------|
| `internal/proxy/loop_detector.go` | **New file** -- `ThinkingLoopDetector` struct and methods |
| `internal/proxy/translator.go` | Add detector to `ClaudeStreamState`, integrate with `ProcessEvent()` |
| `internal/proxy/engine.go` | Add loop detection to `streamPassthrough()`, add nudge logic to `HandleChatCompletionStream` outer retry loop, add `nudged` flag |
| `internal/proxy/openai_client.go` | Add `ApplyNudge()` method to modify request with nudge system message + reduced thinking |

### Testing

- Unit tests for `ThinkingLoopDetector`: 3 identical blocks trigger, 2 identical + 1 different do not, empty blocks handled, single-block stream handled
- Integration test: mock provider that sends looping thinking, verify nudge retry occurs and response completes
