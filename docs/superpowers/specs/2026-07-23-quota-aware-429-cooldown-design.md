# Design: Quota-Aware 429 Cooldown

**Date:** 2026-07-23  
**Status:** Approved  
**Scope:** `internal/proxy/rate_limit.go`, `internal/proxy/openai_client.go`, `internal/proxy/ollama_cloud_client.go`, `internal/proxy/anthropic_client.go`, `internal/proxy/engine.go`, `internal/proxy/rate_limit_test.go`

---

## Problem

Current `defaultCooldown` after a 429 is 60 seconds. For quota-style 429s (daily/weekly limits), this is useless — the router retries 1 minute later, gets another 429, and loops indefinitely.

Observed in production logs:
- Cloudflare `kimi-k2.6`: `"you have used up your daily free allocation of 10,000 neurons"` → needs 12h cooldown
- Ollama cloud: `"you have reached your weekly usage limit"` → needs 72h cooldown

---

## Decisions

| Question | Decision |
|---|---|
| Daily quota cooldown | 12h fixed |
| Weekly quota cooldown | 72h fixed |
| Matching strategy | Generic keyword scan on raw response body |
| Body source | Raw bytes (`respBody`) already available at `ProviderError` construction sites |

---

## Architecture

```
429 response body bytes
        │
        ▼
classifyRateLimit(body []byte) time.Duration
        │
   lowercase scan
        ├── "daily" OR "allocation" OR "neurons"  → 12h
        ├── "weekly"                               → 72h
        └── no match                               → 0  (defer to Retry-After or 60s default)
        │
        ▼
MarkLimited(providerID, duration)
```

Priority: if both daily and weekly keywords match, daily wins (longer cooldown).

---

## Components

### 1. `rate_limit.go` — new function

```go
const (
    quotaCooldownDaily  = 12 * time.Hour
    quotaCooldownWeekly = 72 * time.Hour
)

// classifyRateLimit inspects the raw 429 response body for quota exhaustion
// keywords and returns an appropriate extended cooldown duration.
// Returns 0 if no quota keywords found (caller uses Retry-After or default).
func classifyRateLimit(body []byte) time.Duration {
    if len(body) == 0 {
        return 0
    }
    lower := strings.ToLower(string(body))
    // Daily quota keywords (checked first — longer cooldown wins)
    for _, kw := range []string{"daily", "allocation", "neurons"} {
        if strings.Contains(lower, kw) {
            return quotaCooldownDaily
        }
    }
    // Weekly quota keywords
    if strings.Contains(lower, "weekly") {
        return quotaCooldownWeekly
    }
    return 0
}
```

### 2. `ProviderError` struct — add `RawBody`

Add field to `ProviderError` in `openai_client.go`:

```go
type ProviderError struct {
    StatusCode int
    Message    string
    RetryAfter time.Duration
    RawBody    []byte   // raw response body for quota keyword classification
}
```

Set at every construction site where `respBody` already exists:
- `openai_client.go` line ~164 (non-stream)
- `openai_client.go` line ~206 (stream)
- `ollama_cloud_client.go` line ~259 (non-stream)
- `ollama_cloud_client.go` line ~299 (stream)
- `anthropic_client.go` line ~147 (non-stream)
- `anthropic_client.go` line ~202 (stream)

### 3. `engine.go` — updated `MarkLimited` call sites (4 locations)

Replace every:
```go
if providerErr.StatusCode == http.StatusTooManyRequests {
    e.rateLimits.MarkLimited(rm.Provider.ID, providerErr.RetryAfter)
}
```

With:
```go
if providerErr.StatusCode == http.StatusTooManyRequests {
    cooldown := providerErr.RetryAfter
    if cooldown == 0 {
        cooldown = classifyRateLimit(providerErr.RawBody)
    }
    e.rateLimits.MarkLimited(rm.Provider.ID, cooldown)
}
```

`classifyRateLimit` is only called when `RetryAfter == 0` (no header), so explicit `Retry-After` headers always take precedence.

---

## Error Handling

| Case | Behavior |
|---|---|
| `nil` / empty body | `classifyRateLimit` returns 0 → existing 60s default unchanged |
| Body too large | Keyword scan still works (string ops, no JSON parse required) |
| Non-429 status | `classifyRateLimit` never called |
| Both daily+weekly keywords in body | Daily wins (checked first, returns 12h) |
| Provider sends `Retry-After` header | Header value used; `classifyRateLimit` skipped |

---

## Tests (`rate_limit_test.go`)

New test cases for `classifyRateLimit`:

| Input | Expected |
|---|---|
| Cloudflare body with `"daily free allocation"` | 12h |
| Body with `"neurons"` | 12h |
| Body with `"daily"` only | 12h |
| Ollama body with `"weekly usage limit"` | 72h |
| Body with `"weekly"` only | 72h |
| Generic error body (no quota keywords) | 0 |
| Empty body `[]byte{}` | 0 |
| `nil` body | 0 |
| Mixed-case body `"Daily FREE Allocation"` | 12h (lowercased before scan) |
| Body with both `"daily"` and `"weekly"` | 12h (daily wins) |

---

## Out of Scope

- Per-provider configurable cooldown durations
- Cloudflare error code `4006` parsing (keyword match covers the same case)
- UI for viewing quota cooldown state (existing rate limit status endpoint unchanged)
- Persistent cooldown across restarts (in-memory only, existing behavior)
