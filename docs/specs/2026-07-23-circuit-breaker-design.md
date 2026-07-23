# Circuit Breaker for 5xx Errors

## Summary

Implement a circuit breaker that temporarily disables provider models and providers when they return too many 5xx errors (500, 502, 503, 504) within a configurable time window. Disabled entities are stored in the database and auto-re-enabled after a cooldown period via a background goroutine.

## Motivation

When a provider's model starts returning persistent 5xx errors, the router wastes time retrying and failing over through broken backends. A circuit breaker detects this pattern and temporarily removes the broken model/provider from rotation, letting traffic flow to healthy alternatives.

## Two Levels

1. **Model-level**: A specific model on a provider (e.g., `gpt-4o` on OpenAI) gets circuit-broken independently
2. **Provider-level**: If enough models on a provider are5xx-ing, the entire provider gets circuit-broken

## Settings

All settings are in `config.go` `Settings` struct, configurable via `PUT /api/v1/settings`:

| Field | JSON key | Default | Description |
|-------|----------|---------|-------------|
| `CircuitBreakerEnabled` | `circuit_breaker_enabled` | `true` | Master switch |
| `CircuitBreakerModelThreshold` | `circuit_breaker_model_threshold` | `5` | 5xx count to trigger model CB |
| `CircuitBreakerModelWindowSec` | `circuit_breaker_model_window_sec` | `300` | Sliding window (seconds) for model CB |
| `CircuitBreakerModelCooldownSec` | `circuit_breaker_model_cooldown_sec` | `600` | How long model stays disabled (seconds) |
| `CircuitBreakerProviderThreshold` | `circuit_breaker_provider_threshold` | `10` | 5xx count to trigger provider CB |
| `CircuitBreakerProviderWindowSec` | `circuit_breaker_provider_window_sec` | `300` | Sliding window (seconds) for provider CB |
| `CircuitBreakerProviderCooldownSec` | `circuit_breaker_provider_cooldown_sec` | `600` | How long provider stays disabled (seconds) |
| `CircuitBreakerProviderMinModels` | `circuit_breaker_provider_min_models` | `2` | Min distinct models affected to trigger provider CB |

## Data Model

### In-Memory Tracking

```
type CircuitBreaker struct {
    // Sliding window of 5xx timestamps per model
    // Key: "providerID:modelName" -> []time.Time
    modelErrors sync.Map

    // Cooldown tracking (set when CB trips, checked by background re-enabler)
    // Key: providerID -> time.Time (cooldown expiry)
    providerCooldowns sync.Map
    // Key: "providerID:modelName" -> time.Time
    modelCooldowns sync.Map

    settings    CircuitBreakerSettings
    mu          sync.RWMutex  // protects settings
    modelRepo   repository.ModelRepository
    providerRepo repository.ProviderRepository
    logger      *slog.Logger
    stopCh      chan struct{}
}
```

### CircuitBreakerSettings (mirrors config, applied to engine)

```go
type CircuitBreakerSettings struct {
    Enabled                bool
    ModelThreshold         int
    ModelWindowSec         int
    ModelCooldownSec       int
    ProviderThreshold      int
    ProviderWindowSec      int
    ProviderCooldownSec    int
    ProviderMinModels      int
}
```

## Logic

### Recording Errors

Engine calls `Record5xx(providerID, modelName)` after each 5xx response:
1. Append `time.Now()` to the sliding window for `providerID:modelName`
2. Purge entries older than the window
3. **Model check**: if count in window `>= modelThreshold`:
   - Set `Disabled=true` on model in DB (`modelRepo.ToggleDisabled`)
   - Store cooldown expiry in `modelCooldowns`
   - Log the event
4. **Provider check**: if total5xx across all models in window `>= providerThreshold` AND distinct models affected `>= providerMinModels`:
   - Set `Disabled=true` on provider in DB (`providerRepo.Update`)
   - Store cooldown expiry in `providerCooldowns`
   - Log the event

### Background Re-Enable

A goroutine runs every 60 seconds:
1. Check `modelCooldowns`: for each entry where `time.Now() >= expiry`:
   - Set `Disabled=false` on model in DB
   - Delete from `modelCooldowns`
   - Delete from `modelErrors`
2. Check `providerCooldowns`: for each entry where `time.Now() >= expiry`:
   - Set `Disabled=false` on provider in DB
   - Delete from `providerCooldowns`
   - Log re-enable

### Engine Integration

The engine already checks `rm.Provider.Disabled` and model disabled status during routing. No routing logic changes needed. The circuit breaker only sets these DB fields; the engine's existing checks handle the rest.

Error recording happens at the same points where the engine currently logs 5xx failures:
- Non-streaming: `engine.go` ~line 339 (after `failures = append(...)`)
- Streaming: `engine.go` ~line 552

## Files

### New
- `internal/proxy/circuit_breaker.go` — CircuitBreaker struct, Record5xx, background goroutine

### Modified
- `internal/config/config.go` — 8 new settings fields + defaults
- `internal/proxy/engine.go` — add `circuitBreaker` field, call `Record5xx` on 5xx
- `internal/api/handlers/settings_handler.go` — add fields to response/update structs + clamping
- `internal/api/handlers/status_handler.go` — expose CB status
- `cmd/llm-router/main.go` — wire CircuitBreaker with repos, pass to engine

### No migration needed
Reuses existing `disabled` column on `providers` and `models` tables.

## API Changes

### GET /api/v1/settings

Response adds:
```json
{
  "circuit_breaker_enabled": true,
  "circuit_breaker_model_threshold": 5,
  "circuit_breaker_model_window_sec": 300,
  "circuit_breaker_model_cooldown_sec": 600,
  "circuit_breaker_provider_threshold": 10,
  "circuit_breaker_provider_window_sec": 300,
  "circuit_breaker_provider_cooldown_sec": 600,
  "circuit_breaker_provider_min_models": 2
}
```

### PUT /api/v1/settings

Accepts same fields as optional parameters.

### GET /api/v1/status

Provider response adds:
```json
{
  "circuit_broken": false,
  "circuit_breaker_remaining": ""
}
```
