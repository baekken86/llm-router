# Design: Disable Duration for Models and Providers

**Date:** 2026-07-23
**Status:** Proposed

---

## Overview

Add duration-aware disable to both models and providers. When disabling, the user specifies how long: indefinite, 10 minutes, 1 hour, or 24 hours. A background goroutine auto-re-enables expired entries. Remaining time is displayed in the web UI and TUI.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Database (SQLite)                        │
│  models/providers: ADD COLUMN disabled_until TIMESTAMP NULL     │
│  NULL = indefinite, non-NULL = auto re-enable at that time      │
└──────────────────────────────┬──────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                      ReenableExpirer                             │
│  Background goroutine, polls every 30s                          │
│  SELECT id FROM {table} WHERE disabled=1                        │
│    AND disabled_until IS NOT NULL AND disabled_until <= now      │
│  → sets disabled=0, disabled_until=NULL                          │
└──────────────────────────────┬──────────────────────────────────┘
                               │
                ┌──────────────┼──────────────┐
                ▼              ▼              ▼
┌──────────────────┐ ┌──────────────┐ ┌──────────────────┐
│   API Layer      │ │     CLI      │ │     TUI          │
│  PUT disable     │ │  --duration  │ │  Duration picker │
│  accepts duration│ │  flag on     │ │  on keypress     │
│  Duration string │ │  toggle-     │ │  Remaining time  │
│  "10m"/"1h"/"24h"│ │  provider &  │ │  inline display  │
│                  │ │  toggle-model│ │                  │
└──────────────────┘ └──────────────┘ └──────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Web UI                                    │
│  Modal dialog on disable click with duration dropdown           │
│  Remaining time countdown badge next to disable/enable button   │
└─────────────────────────────────────────────────────────────────┘
```

---

## Component Design

### 1. Database Migration

**File:** `internal/db/migrations/027_add_disabled_until.sql`

```sql
-- +goose Up
ALTER TABLE models ADD COLUMN disabled_until TIMESTAMP NULL;
ALTER TABLE providers ADD COLUMN disabled_until TIMESTAMP NULL;

-- +goose Down
ALTER TABLE models DROP COLUMN disabled_until;
ALTER TABLE providers DROP COLUMN disabled_until;
```

- `NULL` = indefinite (permanent disable, same as today's behavior)
- Non-NULL = auto re-enable at that timestamp
- Default NULL preserves existing behavior for all current disabled entries

---

### 2. Model Changes

**File:** `internal/models/model.go`

```go
type Model struct {
    // ... existing fields ...
    DisabledUntil *time.Time `json:"disabled_until,omitempty"` // NEW
}
```

**File:** `internal/models/provider.go`

```go
type Provider struct {
    // ... existing fields ...
    DisabledUntil *time.Time `json:"disabled_until,omitempty"` // NEW
}

type UpdateProviderRequest struct {
    // ... existing fields ...
    Duration *string `json:"duration,omitempty"` // NEW: "10m", "1h", "24h", nil=indefinite
}
```

---

### 3. Duration Type

**File:** `internal/models/duration.go` (new)

```go
package models

import (
    "fmt"
    "time"
)

type DisableDuration string

const (
    DurationIndefinite DisableDuration = ""
    Duration10Min      DisableDuration = "10m"
    Duration1Hour      DisableDuration = "1h"
    Duration24Hours    DisableDuration = "24h"
)

var AllowedDurations = map[DisableDuration]time.Duration{
    Duration10Min:   10 * time.Minute,
    Duration1Hour:   1 * time.Hour,
    Duration24Hours: 24 * time.Hour,
}

func ParseDuration(d string) (time.Duration, error) {
    if d == "" {
        return 0, nil // indefinite
    }
    dur, ok := AllowedDurations[DisableDuration(d)]
    if !ok {
        return 0, fmt.Errorf("invalid duration %q: allowed values are 10m, 1h, 24h, or empty for indefinite", d)
    }
    return dur, nil
}

func DisabledUntil(d DisableDuration) *time.Time {
    if d == DurationIndefinite {
        return nil
    }
    dur, err := ParseDuration(string(d))
    if err != nil {
        return nil
    }
    t := time.Now().Add(dur)
    return &t
}
```

---

### 4. Repository Changes

**File:** `internal/repository/model_repo.go`

Update `ToggleDisabled` signature and implementation:

```go
// Interface
ToggleDisabled(ctx context.Context, id int64, disabled bool, duration *time.Duration) error

// Implementation
func (r *modelRepository) ToggleDisabled(ctx context.Context, id int64, disabled bool, duration *time.Duration) error {
    var disabledUntil *time.Time
    if disabled && duration != nil {
        t := time.Now().Add(*duration)
        disabledUntil = &t
    }
    _, err := r.db.ExecContext(ctx,
        `UPDATE models SET disabled = ?, disabled_until = ? WHERE id = ?`,
        disabled, disabledUntil, id)
    return err
}
```

New query for the expirer:

```go
// Interface (on model_repo and provider_repo separately)
ListExpiredDisabled(ctx context.Context, now time.Time) ([]int64, error)
```

Implementation:

```go
func (r *modelRepository) ListExpiredDisabled(ctx context.Context, now time.Time) ([]int64, error) {
    rows, err := r.db.QueryContext(ctx,
        `SELECT id FROM models WHERE disabled = 1 AND disabled_until IS NOT NULL AND disabled_until <= ?`, now)
    // ... scan and return ids
}
```

Same pattern for `provider_repo.go`.

All SELECT queries need to scan `disabled_until` column (add to GetByID, GetByName, List, ListByMetadata, ListAll).

---

### 5. Service Changes

**File:** `internal/service/model_service.go`

```go
// Interface
ToggleDisabled(ctx context.Context, id int64, disabled bool, duration *time.Duration) error
```

Implementation delegates to repo with the parsed duration.

**File:** `internal/service/provider_service.go`

In `Update()`, parse `req.Duration` and compute `disabled_until`:

```go
if req.Disabled != nil {
    p.Disabled = *req.Disabled
    if *req.Disabled && req.Duration != nil {
        dur, err := models.ParseDuration(*req.Duration)
        if err != nil { return nil, err }
        p.DisabledUntil = models.DisabledUntil(models.DisableDuration(*req.Duration))
    } else if !*req.Disabled {
        p.DisabledUntil = nil // clear on re-enable
    }
}
```

---

### 6. ReenableExpirer

**File:** `internal/proxy/reenable_expirer.go` (new)

```go
package proxy

import (
    "context"
    "log/slog"
    "time"
)

type ModelRepo interface {
    ListExpiredDisabled(ctx context.Context, now time.Time) ([]int64, error)
    ToggleDisabled(ctx context.Context, id int64, disabled bool, duration *time.Duration) error
}

type ProviderRepo interface {
    ListExpiredDisabled(ctx context.Context, now time.Time) ([]int64, error)
    Update(ctx context.Context, provider *models.Provider) error
}

type ReenableExpirer struct {
    modelRepo    ModelRepo
    providerRepo ProviderRepo
    tickInterval time.Duration
    logger       *slog.Logger
}

func NewReenableExpirer(modelRepo ModelRepo, providerRepo ProviderRepo, logger *slog.Logger) *ReenableExpirer {
    return &ReenableExpirer{
        modelRepo:    modelRepo,
        providerRepo: providerRepo,
        tickInterval: 30 * time.Second,
        logger:       logger,
    }
}

func (e *ReenableExpirer) Start(ctx context.Context) {
    ticker := time.NewTicker(e.tickInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case now := <-ticker.C:
            e.reenableExpired(ctx, now)
        }
    }
}

func (e *ReenableExpirer) reenableExpired(ctx context.Context, now time.Time) {
    // Models
    modelIDs, err := e.modelRepo.ListExpiredDisabled(ctx, now)
    if err != nil {
        e.logger.Error("failed to list expired models", "error", err)
    }
    for _, id := range modelIDs {
        if err := e.modelRepo.ToggleDisabled(ctx, id, false, nil); err != nil {
            e.logger.Error("failed to re-enable model", "id", id, "error", err)
        } else {
            e.logger.Info("auto re-enabled model", "id", id)
        }
    }

    // Providers
    providerIDs, err := e.providerRepo.ListExpiredDisabled(ctx, now)
    if err != nil {
        e.logger.Error("failed to list expired providers", "error", err)
    }
    for _, id := range providerIDs {
        // Fetch provider, set disabled=false, update
        p, err := e.providerRepo.GetByID(ctx, id)
        if err != nil { continue }
        p.Disabled = false
        p.DisabledUntil = nil
        if err := e.providerRepo.Update(ctx, p); err != nil {
            e.logger.Error("failed to re-enable provider", "id", id, "error", err)
        } else {
            e.logger.Info("auto re-enabled provider", "id", id)
        }
    }
}
```

Started in `main.go` alongside the circuit breaker:

```go
expirer := proxy.NewReenableExpirer(modelRepo, providerRepo, logger)
go expirer.Start(ctx)
```

**Key distinction from circuit breaker:** The circuit breaker sets `disabled=1` WITHOUT setting `disabled_until`. The expirer only acts on entries WHERE `disabled_until IS NOT NULL`. They never collide.

---

### 7. API Changes

**File:** `internal/api/handlers/model_handler.go`

Update `ToggleDisabled` handler to parse duration from request body:

```go
type ToggleDisabledRequest struct {
    Disabled bool    `json:"disabled"`
    Duration *string `json:"duration,omitempty"` // "10m", "1h", "24h", nil=indefinite
}
```

Parse duration, pass to service:

```go
var duration *time.Duration
if req.Duration != nil {
    d, err := models.ParseDuration(*req.Duration)
    if err != nil { /* 400 error */ }
    duration = &d
}
err = h.modelService.ToggleDisabled(r.Context(), id, req.Disabled, duration)
```

**File:** `internal/api/handlers/provider_handler.go`

Update `Update` handler — `UpdateProviderRequest` already has `Duration *string`. Parse and pass through.

**File:** `internal/api/handlers/status_handler.go`

Add `DisabledUntil` to `ProviderStatus`:

```go
type ProviderStatus struct {
    // ... existing fields ...
    Disabled      bool       `json:"disabled"`
    DisabledUntil *time.Time `json:"disabled_until,omitempty"`
}
```

Copy from provider in `GetStatus`.

Model list endpoints (`GET /api/v1/models`) already return model structs — they'll automatically include `disabled_until` once the model struct has the field.

---

### 8. Web UI Changes

**File:** `web/src/components/RawModelList.svelte`

#### Disable Modal

Replace the inline toggle with a modal:

```svelte
<script>
  let showDisableModal = false;
  let disableTarget = null;
  let disableDuration = '10m';

  function openDisableModal(model) {
    disableTarget = model;
    disableDuration = '10m';
    showDisableModal = true;
  }

  async function confirmDisable() {
    if (!disableTarget) return;
    const duration = disableDuration === 'indefinite' ? null : disableDuration;
    await toggleDisabled(disableTarget.id, true, duration);
    showDisableModal = false;
    disableTarget = null;
  }
</script>

<!-- Modal -->
{#if showDisableModal}
  <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50"
       on:click|self={() => showDisableModal = false}>
    <div class="bg-gray-900 rounded-lg border border-gray-700 p-6 w-80">
      <h3 class="text-white font-medium mb-4">
        Disable {disableTarget?.name}
      </h3>
      <label class="block text-gray-400 text-sm mb-2">Duration</label>
      <select bind:value={disableDuration}
              class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm">
        <option value="indefinite">Indefinite</option>
        <option value="10m">10 minutes</option>
        <option value="1h">1 hour</option>
        <option value="24h">24 hours</option>
      </select>
      <div class="flex justify-end gap-2 mt-6">
        <button class="px-3 py-1.5 text-sm text-gray-400 hover:text-white"
                on:click={() => showDisableModal = false}>Cancel</button>
        <button class="px-3 py-1.5 text-sm bg-amber-600 text-white rounded hover:bg-amber-500"
                on:click={confirmDisable}>Disable</button>
      </div>
    </div>
  </div>
{/if}
```

#### Remaining Time Display

Add a countdown component next to each disabled model:

```svelte
{#if model.disabled}
  <span class="text-xs text-amber-400/70 ml-2">
    {#if model.disabled_until}
      {formatRemaining(model.disabled_until)}
    {:else}
      permanent
    {/if}
  </span>
{/if}
```

`formatRemaining` computes and displays time left:

```js
function formatRemaining(disabledUntil) {
  const remaining = new Date(disabledUntil) - new Date();
  if (remaining <= 0) return 're-enabling...';
  const mins = Math.floor(remaining / 60000);
  const secs = Math.floor((remaining % 60000) / 1000);
  if (mins >= 60) {
    const hrs = Math.floor(mins / 60);
    return `${hrs}h ${mins % 60}m`;
  }
  return `${mins}m ${secs}s`;
}
```

**File:** `web/src/components/StatusView.svelte`

Same pattern — modal on disable click, countdown badge next to disabled providers.

---

### 9. CLI Changes

**File:** `cmd/llm-router/toggleprovider.go`

Add `--duration` flag:

```go
duration := fs.String("duration", "", "Disable duration: 10m, 1h, 24h (default: indefinite)")
```

Validate and parse:

```go
var dur *time.Duration
if *duration != "" {
    d, err := models.ParseDuration(*duration)
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
    dur = &d
}
```

Pass to service: `providerService.Update(ctx, provider.ID, models.UpdateProviderRequest{Disabled: &newDisabled, Duration: duration})`

**File:** `cmd/llm-router/togglemodel.go` (new)

```go
package main

func runToggleModel(args []string) {
    fs := flag.NewFlagSet("toggle-model", flag.ExitOnError)
    dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
    name := fs.String("name", "", "Model name (required)")
    enable := fs.Bool("enable", false, "Enable the model")
    disable := fs.Bool("disable", false, "Disable the model")
    duration := fs.String("duration", "", "Disable duration: 10m, 1h, 24h (default: indefinite)")
    fs.Parse(args)

    // ... validate flags, open DB, find model by name ...
    // ... parse duration, call modelService.ToggleDisabled(ctx, model.ID, newDisabled, dur) ...
}
```

Register in `main.go`:

```go
case "toggle-model":
    runToggleModel(os.Args[2:])
    return
```

Update usage text.

---

### 10. TUI Changes

**File:** `internal/tui/tui.go`

#### Duration Picker State

Add to the main `Model` struct:

```go
type Model struct {
    // ... existing fields ...
    showDurationPicker bool
    durationCursor     int
    durationTarget     string // "model" or "provider"
    durationOptions    []string
}
```

Duration options: `[]string{"indefinite", "10 min", "1 hour", "24 hours"}`

Map to API values: `"indefinite"→"", "10 min"→"10m", "1 hour"→"1h", "24 hours"→"24h"`

#### Key Handling

When `x` (model) or `d` (provider) is pressed, instead of immediately toggling:

```go
case "x":
    if !m.showDurationPicker && /* on models raw tab */ {
        m.showDurationPicker = true
        m.durationTarget = "model"
        m.durationCursor = 1 // default to "10 min"
        return m, nil
    }
```

When `showDurationPicker` is true, arrow keys cycle, Enter confirms, Esc cancels:

```go
case "left", "right":
    if m.showDurationPicker {
        if msg.String() == "left" {
            m.durationCursor--
            if m.durationCursor < 0 { m.durationCursor = len(m.durationOptions)-1 }
        } else {
            m.durationCursor++
            if m.durationCursor >= len(m.durationOptions) { m.durationCursor = 0 }
        }
        return m, nil
    }
case "enter":
    if m.showDurationPicker {
        m.showDurationPicker = false
        dur := durationToAPI(m.durationOptions[m.durationCursor])
        // dispatch toggle command with duration
        return m, toggleCmd(dur)
    }
case "esc":
    if m.showDurationPicker {
        m.showDurationPicker = false
        return m, nil
    }
```

#### Duration Picker View

Render at bottom of screen when active:

```go
func (m Model) View() string {
    // ... existing view ...
    if m.showDurationPicker {
        b.WriteString("\n")
        b.WriteString(MutedStyle.Render("  Duration: "))
        for i, opt := range m.durationOptions {
            if i == m.durationCursor {
                b.WriteString(SelectedStyle.Render(opt))
            } else {
                b.WriteString(MutedStyle.Render(opt))
            }
            b.WriteString("  ")
        }
        b.WriteString(MutedStyle.Render("  [Enter] confirm  [Esc] cancel"))
    }
    return b.String()
}
```

#### Remaining Time Display

In `vmview.go` and `status.go`, when rendering disabled models/providers, append remaining time:

```go
if rm.Disabled {
    modelDisplay = MutedStyle.Render(modelDisplay)
    if rm.DisabledUntil != nil {
        remaining := time.Until(*rm.DisabledUntil)
        if remaining > 0 {
            modelDisplay += MutedStyle.Render(" " + formatRemaining(remaining))
        }
    } else {
        modelDisplay += MutedStyle.Render(" (perm)")
    }
}
```

`RawModelInfo` and `ProviderStatusResponse` need `DisabledUntil *time.Time` fields.

---

## Data Flow Summary

```
User Action                    Data Flow
─────────────────────────────────────────────────────────────────
CLI: toggle-model --name X --disable --duration 10m
  → modelService.ToggleDisabled(id, true, &10m)
    → modelRepo.ToggleDisabled(id, true, &10m)
      → UPDATE models SET disabled=1, disabled_until=now+10m WHERE id=X
        → ReenableExpirer picks up after 10m → auto re-enables

Web UI: Click "Disable" → Modal → Select "1 hour" → Click "Disable"
  → PUT /api/v1/models/{id} {"disabled": true, "duration": "1h"}
    → modelHandler.ToggleDisabled → service → repo
      → Same DB update as CLI
        → Countdown badge shows "59m 30s" and ticks down

TUI: Press 'x' → Duration picker → Select "10 min" → Enter
  → modelRepo.ToggleDisabled(id, true, &10m)
    → Same DB update
      → View refreshes, shows "10m" inline
```

---

## Edge Cases

1. **Manual disable while circuit-broken**: Circuit breaker sets `disabled=1, disabled_until=NULL`. Manual disable with duration sets `disabled=1, disabled_until=now+dur`. The expirer only touches entries with `disabled_until IS NOT NULL`, so it re-enables the manual disable. Circuit breaker can later re-disable if errors continue. No conflict.

2. **Re-enable while circuit-broken**: User clicks "Enable" → sets `disabled=0, disabled_until=NULL`. Circuit breaker may re-disable on next 5xx. Expected behavior.

3. **Process restart during timed disable**: `disabled_until` is persisted in DB. The expirer starts on boot and will find expired entries. No data loss.

4. **Expired entry not yet picked up**: Between expiry and next 30s tick, the model is still marked disabled. The UI countdown shows "re-enabling...". Acceptable latency.

5. **All models disabled**: Same as today — returns 502. Duration doesn't change this.

6. **Double disable with different duration**: Second disable overwrites `disabled_until`. Latest wins.

---

## Testing

1. **Unit tests**:
   - `ParseDuration`: valid values, invalid values, empty string
   - `DisabledUntil`: computes correct timestamp
   - `ReenableExpirer`: mock repos, verify expired entries are re-enabled
   - `ToggleDisabled` (repo): verify `disabled_until` is set correctly for each duration

2. **Integration tests**:
   - CLI: `toggle-model --disable --duration 10m` sets `disabled_until`, `toggle-model --enable` clears it
   - API: PUT with duration returns updated model with `disabled_until`
   - Expirer: insert expired entry, run reenable cycle, verify re-enabled

3. **Manual verification**:
   - Disable model for 10 min via web UI → see countdown → wait → auto re-enables
   - Disable provider for 1h via CLI → check TUI shows remaining time
   - Restart process during timed disable → verify it still re-enables

---

## Rollback Plan

1. Revert migration: `DROP COLUMN disabled_until` from both tables
2. Revert code changes
3. Or keep column, clear all: `UPDATE models SET disabled_until = NULL; UPDATE providers SET disabled_until = NULL;`
