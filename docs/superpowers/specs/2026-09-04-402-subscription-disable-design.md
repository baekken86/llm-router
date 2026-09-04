# Design: Permanent Model Disable on 402 (Subscription Required)

**Date:** 2026-09-04
**Status:** Approved
**Scope:** `internal/proxy/engine.go`, `internal/proxy/circuit_breaker.go`, `internal/proxy/openai_client.go`, `internal/proxy/anthropic_client.go`, `internal/proxy/ollama_cloud_client.go`, `internal/proxy/engine_test.go`

---

## Problem

Ollama Cloud models that require a paid plan return HTTP 402 with
`{"error":"this model requires a subscription or extra usage..."}` on **every**
request. The circuit breaker only records `StatusCode >= 500`
(`engine.go:340`), so a 402 retries, fails, and recurs forever. The user must
manually disable the model.

## Decision

| Question | Decision |
|---|---|
| Disable style | **Permanent until manual re-enable** (`disabled=1, disabled_until=NULL`) |
| Re-enable | Manual only (`toggle-model --enable`, TUI `d`) |
| Respect circuit-breaker `Enabled` setting | **No** — 402 is a deterministic billing mismatch, not transient error noise |
| Scope | All provider types (any client can return 402), not just ollama-cloud |
| Refactor | Typed `SubscriptionRequiredError` + shared engine error-processing helper (Approach C) |

## Architecture

```
provider HTTP response (any client: openai / anthropic / ollama-cloud)
        │  status != 200
        ▼
newProviderError(resp, body)            [openai_client.go]
        │
        ├── 402 → SubscriptionRequiredError{ProviderError{402, msg, RawBody}}
        └── else → ProviderError{status, msg, RetryAfter, RawBody}
        │
        ▼
engine error path (4 sites: non-stream/stream × OpenAI/Anthropic format)
        │
        ▼
e.processProviderError(ctx, err, rm)    [engine.go — replaces duplicated blocks]
        │
        ├── SubscriptionRequiredError → cb.DisableModelPermanent(...)
        ├── ProviderError 429        → rateLimits.MarkLimited (existing logic)
        └── ProviderError >=500      → circuitBreaker.Record5xx (existing logic)
        │
        ▼
resolved model list excludes disabled models on next request
(ResolveModels → ListEnabled → WHERE disabled = 0)
```

## Components

### 1. `SubscriptionRequiredError` (openai_client.go, beside `ProviderError`)

```go
// SubscriptionRequiredError signals a permanent, billing-related failure
// (HTTP 402). Wraps the underlying ProviderError.
type SubscriptionRequiredError struct {
    ProviderError *ProviderError
}

func (e *SubscriptionRequiredError) Error() string { return e.ProviderError.Error() }
func (e *SubscriptionRequiredError) Unwrap() error { return e.ProviderError }
```

### 2. `newProviderError` shared constructor (openai_client.go)

Replaces the 6 duplicated error-block sites:

- `openai_client.go` ~163 (non-stream), ~205 (stream)
- `anthropic_client.go` ~148 (non-stream), ~204 (stream)
- `ollama_cloud_client.go` ~259 (non-stream), ~300 (stream)

```go
// newProviderError builds the appropriate error for a failed provider HTTP
// response: *SubscriptionRequiredError for 402, *ProviderError otherwise.
// Preserves Retry-After parsing and raw body (used by classifyRateLimit).
func newProviderError(resp *http.Response, body []byte) error {
    pe := &ProviderError{
        StatusCode: resp.StatusCode,
        Message:    string(body),
        RetryAfter: parseRetryAfter(resp),
        RawBody:    body,
    }
    if resp.StatusCode == http.StatusPaymentRequired {
        return &SubscriptionRequiredError{ProviderError: pe}
    }
    return pe
}
```

Return type is `error` (a Go function cannot vary its concrete return type by
branch), so callers assign directly to their existing `error` return. The
`processProviderError` helper (section 4) classifies via type switch on
`*SubscriptionRequiredError`, falling back to `errors.As(&*ProviderError)`.

### 3. `CircuitBreaker.DisableModelPermanent` (circuit_breaker.go)

```go
// DisableModelPermanent disables a model with no expiry (402/billing).
// It bypasses the circuit-breaker Enabled setting and registers no cooldown,
// so the re-enable loop will never auto re-enable it. Returns true if the
// model transitioned disabled=0 → disabled=1 (first time only).
func (cb *CircuitBreaker) DisableModelPermanent(ctx context.Context, providerID int64, modelName string) bool
```

Behavior:

| Case | Behavior |
|---|---|
| Model not found / repo error | Log `Error`, return `false` |
| Already `Disabled` | No-op (no second log), return `false` |
| First disable | `ToggleDisabled(id, true, nil)` → `disabled=1, disabled_until=NULL`, log `Warn`, return `true` |
| **No cooldown registered** | `modelCooldowns` untouched → `reenableExpired()` never re-enables |
| **No `modelErrors` window** | Never counted toward provider-level trip (`checkProvider`) |

### 4. `Engine.processProviderError` (engine.go)

The 4 engine error paths (engine.go:332, 578, 1142, 1440) duplicate:
normalize error → 429 handling → 5xx circuit-breaker → retry decision.
Extract the *reaction* (side effects + normalization) into one method:

```go
// processProviderError normalizes err to *ProviderError via errors.As
// (unknown errors → 500) and applies all side effects:
//   - 402 (SubscriptionRequiredError) → circuitBreaker.DisableModelPermanent
//   - 429 → rateLimits.MarkLimited (RetryAfter, then classifyRateLimit fallback)
//   - >=500 → circuitBreaker.Record5xx
// Returns the normalized error for the caller's logging/retry logic.
func (e *Engine) processProviderError(ctx context.Context, err error, rm service.ResolvedModel) *ProviderError
```

Call sites keep their own `RequestLog` and `shouldRetry` mechanics (retry-loop
shapes differ per path: re-`i--`, `break`, `lastErr` accumulation) and only
replace the duplicated side-effect blocks.

Detection order in helper: `SubscriptionRequiredError` (type switch) first,
then `errors.As(*ProviderError)` for 429/5xx. Engine call sites switch their
`err.(*ProviderError)` assertions to the helper (which uses `errors.As`), so
embedded/typed errors are handled uniformly.

### 5. Existing wiring that makes this work with zero changes

- `ResolveModels` → `ListEnabled` filters `disabled = 1`
  (`model_repo.go:118`) — disabled models vanish from routing on the next
  request.
- `ReenableExpirer` (`reenable_expirer.go`) only re-enables rows with expired
  `disabled_until` — `disabled_until=NULL` rows are never touched.
- TUI already renders `DISABLED (perm)` (`status.go:107`).
- `toggle-model --enable` / TUI `d` re-enables manually.

## Error Handling

| Case | Behavior |
|---|---|
| Repo failure inside `DisableModelPermanent` | Logged, not fatal; request continues with normal failure handling |
| 402 retried? | No — 402 is not in default `retryOnStatus` (`engine.go:924`), falls through to next fallback model (unchanged) |
| Circuit breaker disabled via settings | 402 disable still fires (bypasses `Enabled`); 429/5xx behavior unchanged |
| Legacy callers | `errors.As` handles wrapped `ProviderError` |

## Tests

### Engine-level (new, `engine_test.go` style — mock model repo + failing server)

| Test | Assert |
|---|---|
| 402 non-stream (OpenAI fmt) → model disabled | `ToggleDisabled(id, true, nil)` called; no cooldown |
| 402 stream (OpenAI fmt) → model disabled | same |
| 402 non-stream (Anthropic fmt) → model disabled | same |
| 402 stream (Anthropic fmt) → model disabled | same |
| 402 with circuit breaker disabled via settings | disable still fires |
| 5xx regression | `Record5xx` unchanged; 402 does NOT count toward 5xx windows |

### CircuitBreaker unit (`circuit_breaker_test.go` — new file)

| Test | Assert |
|---|---|
| First disable | returns `true`, model disabled, no cooldown entry |
| Second disable (already disabled) | no-op, returns `false`, no duplicate log |
| Repo error | returns `false`, logged, no panic |
| No re-enable | `reenableExpired` never flips the row |

### Client-level

| Test | Assert |
|---|---|
| `newProviderError` with 402 | returns `*SubscriptionRequiredError` |
| `newProviderError` with 500 | returns `*ProviderError` (not typed as subscription) |

## Out of Scope

- Disabling the whole **provider** on 402 (models are per-provider; a billing
  mismatch is model-specific)
- Configurable cooldown-style re-check ("retry daily to see if credits were
  added")
- Body-keyword classification for 402 (status code alone is deterministic)
- Provider subscription-state UI (existing `DISABLED (perm)` label covers it)
