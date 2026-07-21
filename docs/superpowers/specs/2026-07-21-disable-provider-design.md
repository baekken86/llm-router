# Design: Disable Provider Feature

**Date:** 2026-07-21  
**Status:** Proposed

---

## Overview

Add a persisted `disabled` flag to providers that:
- Survives restarts (stored in database)
- Causes the routing engine to skip disabled providers during fallback
- Is toggleable via CLI (`llm-router toggle-provider <name>`), Web UI, and TUI
- Displays as a "Disabled" badge in both UIs alongside rate limit / auth status

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Database (SQLite)                        │
│  providers table: ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0│
└──────────────────────────────┬──────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Provider Model                             │
│  struct Provider { ..., Disabled bool `json:"disabled"` }       │
│  struct UpdateProviderRequest { ..., Disabled *bool }           │
└──────────────────────────────┬──────────────────────────────────┘
                               │
               ┌───────────────┼───────────────┐
               ▼               ▼               ▼
┌──────────────────┐ ┌──────────────┐ ┌──────────────────┐
│   Repository     │ │   Service    │ │   Engine         │
│  - SELECT adds   │ │  - Update    │ │  - In all 4      │
│    disabled col  │ │    handles   │ │    handler loops │
│  - INSERT adds   │ │    Disabled  │ │    add check:    │
│    DEFAULT 0     │ │              │ │    if rm.Provider│
│  - UPDATE adds   │ │              │ │    .Disabled {   │
│    disabled col  │ │              │ │      continue    │
└──────────────────┘ └──────────────┘ └──────────────────┘
               │               │               │
               ▼               ▼               ▼
┌──────────────────┐ ┌──────────────┐ ┌──────────────────┐
│   Status API     │ │     TUI      │ │    Web UI        │
│  - ProviderStatus│ │  - Add       │ │  - Add disabled  │
│    adds Disabled │ │    "DISABLED"│ │    badge +       │
│    field         │ │    badge     │ │    toggle button │
│                  │ │  - Add 'd'   │ │                  │
│                  │ │    keybind   │ │                  │
└──────────────────┘ └──────────────┘ └──────────────────┘
               │
               ▼
┌──────────────────┐
│      CLI         │
│  - New command:  │
│    toggle-provider│
└──────────────────┘
```

---

## Component Design

### 1. Database Migration

**File:** `internal/db/migrations/024_add_disabled_to_providers.sql`

```sql
-- +goose Up
ALTER TABLE providers ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE providers DROP COLUMN disabled;
```

- Uses SQLite boolean convention: `0` = enabled, `1` = disabled
- Default `0` preserves existing behavior (all current providers enabled)
- No data migration needed

---

### 2. Model Changes

**File:** `internal/models/provider.go`

```go
// Provider struct - add Disabled field
type Provider struct {
    // ... existing fields ...
    Disabled        bool   `json:"disabled"`    // NEW
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

// UpdateProviderRequest - add Disabled pointer for partial updates
type UpdateProviderRequest struct {
    // ... existing fields ...
    Disabled        *bool  `json:"disabled,omitempty"`    // NEW
}
```

---

### 3. Repository Changes

**File:** `internal/repository/provider_repo.go`

All SELECT queries must include `disabled` column. Since they use explicit column lists (not `SELECT *`), update each:

- **Create**: INSERT doesn't need `disabled` (DEFAULT 0 handles it)
- **GetByID**: Add `disabled` to SELECT and scan
- **GetByName**: Add `disabled` to SELECT and scan
- **List**: Add `disabled` to SELECT and scan
- **ListByMetadata**: Add `disabled` to SELECT and scan
- **Update**: Add `disabled = ?` to SET clause, scan `p.Disabled`

Example for `GetByID`:
```go
row := s.db.QueryRowContext(ctx, `
    SELECT id, name, api_type, base_url, api_key_encrypted, account_id, disabled, created_at, updated_at
    FROM providers WHERE id = ?
`, id)
var p models.Provider
err := row.Scan(&p.ID, &p.Name, &p.APIType, &p.BaseURL, &p.APIKeyEncrypted, &p.AccountID, &p.Disabled, &p.CreatedAt, &p.UpdatedAt)
```

---

### 4. Service Changes

**File:** `internal/service/provider_service.go`

In `Update()` method, add handling for `Disabled` field (following existing pattern):

```go
if req.Disabled != nil {
    p.Disabled = *req.Disabled
}
```

No new service methods needed — the existing `Update` endpoint handles toggling.

---

### 5. Engine Routing Changes

**File:** `internal/proxy/engine.go`

Add disabled check in **all four** handler loops, right after rate-limit check (or before — either works since both are "skip" conditions):

```go
// In HandleChatCompletion (line ~244)
if limited, remaining := e.rateLimits.IsLimited(rm.Provider.ID); limited {
    // ... existing rate limit handling
    continue
}

// NEW: Skip disabled providers
if rm.Provider.Disabled {
    failures = append(failures, map[string]interface{}{
        "model":    rm.Model.Name,
        "provider": rm.Provider.Name,
        "status":   503,
        "message":  "provider disabled",
    })
    continue
}
```

Same pattern for:
- `HandleChatCompletionStream` (after line 417)
- `HandleAnthropicMessages` (after its rate-limit check)
- `HandleAnthropicMessagesStream` (after its rate-limit check)

**Logging:** Add a `DisabledCount` field to `RequestLog` if you want to track how many disabled providers were skipped per request (optional, for stats).

---

### 6. Status API Changes

**File:** `internal/api/handlers/status_handler.go`

**`ProviderStatus` struct** (line 35):
```go
type ProviderStatus struct {
    // ... existing fields ...
    Disabled bool `json:"disabled"`    // NEW
}
```

**`GetStatus` function**: When building response, copy `Disabled` from provider:

```go
providers, _ := h.providerService.List(r.Context())
for _, p := range providers {
    ps := ProviderStatus{
        // ... existing fields ...
        Disabled: p.Disabled,    // NEW
    }
    // ... rate limit, oauth, etc.
}
```

---

### 7. TUI Changes

**File:** `internal/tui/status.go`

**`ProviderStatusResponse`** (in `client.go`, line 117): Add `Disabled` field
```go
type ProviderStatusResponse struct {
    // ... existing fields ...
    Disabled bool `json:"disabled"`    // NEW
}
```

**`FetchStatusLocal`** (line 150): Pass through `Disabled` from provider:
```go
ps := ProviderStatusResponse{
    ID:   p.ID,
    Name: p.Name,
    Disabled: p.Disabled,    // NEW
    // ...
}
```

**`StatusModel.View()`** (line 64): Render disabled badge before rate limit:
```go
// NEW: Disabled badge
if p.Disabled {
    b.WriteString(fmt.Sprintf("    %s\n", MutedStyle.Render("DISABLED")))
    // Optionally skip rate limit/auth display for disabled providers
    if i < len(m.providers)-1 {
        b.WriteString("\n")
    }
    continue
}
```

**`StatusModel.Update()`** (line 34): Add key binding for `d` (toggle disable):
```go
case tea.KeyMsg:
    switch msg.String() {
    // ... existing cases ...
    case "d":
        if m.cursor >= 0 && m.cursor < len(m.providers) {
            if m.apiClient != nil {
                return m, ToggleProviderCmd(m.apiClient, m.providers[m.cursor].ID, !m.providers[m.cursor].Disabled)
            }
        }
```

**New command function** (in `tui.go` or `client.go`):
```go
func ToggleProviderCmd(apiClient *APIClient, providerID int64, disable bool) tea.Cmd {
    return func() tea.Msg {
        // Use existing PUT /api/v1/providers/{id} with {"disabled": true/false}
        // Then return StatusRefreshMsg to re-fetch
    }
}
```

**Footer help text** (line 551): Update to include `d: toggle disable`

---

### 8. Web UI Changes

**File:** `web/src/components/StatusView.svelte`

Add disabled state rendering and toggle button:

```svelte
{#each providers as p (p.id)}
  <div class="bg-gray-900 rounded-lg border border-gray-800 px-4 py-3 {p.disabled ? 'opacity-50' : ''}">
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-3">
        <span class="text-white font-medium">{p.name}</span>
        
        {#if p.disabled}
          <span class="inline-flex items-center gap-1.5 text-amber-400 text-sm">
            <span class="w-2 h-2 rounded-full bg-amber-400"></span>
            Disabled
          </span>
        {:else if p.rate_limited}
          <span class="inline-flex items-center gap-1.5 text-red-400 text-sm">
            <span class="w-2 h-2 rounded-full bg-red-400"></span>
            Rate Limited
            {#if p.retry_in}
              <span class="text-gray-500">({p.retry_in})</span>
            {/if}
          </span>
        {:else}
          <span class="inline-flex items-center gap-1.5 text-emerald-400 text-sm">
            <span class="w-2 h-2 rounded-full bg-emerald-400"></span>
            OK
          </span>
        {/if}
      </div>
      <div class="flex items-center gap-2">
        <button
          class="px-2 py-1 text-xs bg-gray-800 text-gray-300 rounded hover:bg-gray-700 hover:text-white transition"
          onclick={() => toggleDisabled(p.id, !p.disabled)}
        >
          {p.disabled ? 'Enable' : 'Disable'}
        </button>
        {#if p.rate_limited && !p.disabled}
          <button ... onclick={() => clearRateLimit(p.id)}>Clear</button>
        {/if}
      </div>
    </div>
    ...
  </div>
{/each}
```

Add `toggleDisabled` function:
```js
async function toggleDisabled(id, disabled) {
  try {
    await apiFetch(`/api/v1/providers/${id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ disabled })
    });
    providers = providers.map(p => p.id === id ? { ...p, disabled } : p);
    addToast(disabled ? 'Provider disabled' : 'Provider enabled', 'success');
  } catch (e) {
    addToast(e.message, 'error');
  }
}
```

---

### 9. CLI Changes

**New file:** `cmd/llm-router/toggleprovider.go` (or add to existing)

```go
package main

func runToggleProvider(args []string) {
    fs := flag.NewFlagSet("toggle-provider", flag.ExitOnError)
    dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
    name := fs.String("name", "", "Provider name (required)")
    enable := fs.Bool("enable", false, "Enable the provider (default: toggle)")
    disable := fs.Bool("disable", false, "Disable the provider (default: toggle)")
    fs.Parse(args)

    if *name == "" {
        fmt.Fprintln(os.Stderr, "Usage: llm-router toggle-provider --name <name> [--enable|--disable]")
        os.Exit(1)
    }
    if *enable && *disable {
        fmt.Fprintln(os.Stderr, "Error: --enable and --disable are mutually exclusive")
        os.Exit(1)
    }

    database, err := db.Open(*dbPath)
    if err != nil { /* handle */ }
    defer database.Close()

    providerRepo := repository.NewProviderRepository(database)
    providerMetadataRepo := repository.NewProviderMetadataRepository(database)
    providerService := service.NewProviderService(providerRepo, providerMetadataRepo, loadEncryptionKey())

    ctx := context.Background()
    provider, err := providerRepo.GetByName(ctx, *name)
    if err != nil || provider == nil {
        logger.Error("provider not found", "name", *name)
        os.Exit(1)
    }

    // Determine new state
    newDisabled := provider.Disabled
    if *enable {
        newDisabled = false
    } else if *disable {
        newDisabled = true
    } else {
        newDisabled = !provider.Disabled  // toggle
    }

    if newDisabled == provider.Disabled {
        fmt.Printf("Provider '%s' already %s\n", *name, map[bool]string{true: "disabled", false: "enabled"}[provider.Disabled])
        return
    }

    req := models.UpdateProviderRequest{Disabled: &newDisabled}
    _, err = providerService.Update(ctx, provider.ID, req)
    if err != nil {
        logger.Error("failed to toggle provider", "error", err)
        os.Exit(1)
    }

    fmt.Printf("✓ Provider '%s' %s\n", *name, map[bool]string{true: "disabled", false: "enabled"}[newDisabled])
}
```

**Register in `main.go`** (line 33-65):
```go
case "toggle-provider":
    runToggleProvider(os.Args[2:])
    return
```

**Update usage** (line 71-132): Add to help text:
```
  llm-router toggle-provider      Enable/disable a provider
```

---

## Data Flow Summary

```
User Action                    Data Flow
─────────────────────────────────────────────────────────────────
CLI: toggle-provider --name X
  → providerService.Update(id, {Disabled: true})
    → providerRepo.Update(p)  -- UPDATE providers SET disabled=1...
      → Engine reads Provider.Disabled on each request
        → Skips disabled providers in fallback loop

Web UI: Click "Disable" button
  → PUT /api/v1/providers/{id} {"disabled": true}
    → providerHandler.Update → providerService.Update → repo.Update
      → Status API returns Disabled=true
        → Web UI re-renders with badge

TUI: Press 'd' on Status tab
  → PUT /api/v1/providers/{id} {"disabled": true} (via APIClient)
    → Same as Web UI path
      → TUI refreshes status (5s auto-refresh or manual 'r')
```

---

## Edge Cases

1. **Provider disabled while request in flight**: No effect on current request; next request will skip it.

2. **All providers for a virtual model disabled**: Engine falls through to "all models failed" → returns 502. This is existing behavior when all providers fail.

3. **Disabled provider has rate limit**: Disabled check runs after rate-limit check. Order doesn't matter since both `continue`. Could swap order for slight optimization (check disabled first — cheaper).

4. **OAuth tokens on disabled provider**: Still tracked in status display but irrelevant since provider won't be used.

5. **Virtual model resolution**: No change — resolution happens before engine. Disabled check is in engine. Alternative: filter in `ResolveModels` to avoid resolving models from disabled providers entirely. Current approach is simpler and keeps resolution pure.

---

## Testing

1. **Unit tests**:
   - ProviderRepo: Create/Get/List/Update with Disabled field
   - ProviderService: Update handles Disabled pointer
   - Engine: Verify disabled providers are skipped in fallback loop (mock provider with Disabled=true)

2. **Integration tests**:
   - CLI: toggle-provider enables/disables, idempotent, --enable/--disable flags work
   - API: PUT /providers/{id} with disabled toggles state, returns updated provider
   - Web UI: Toggle button flips state, badge appears, persists on refresh
   - TUI: 'd' key toggles, badge shows, persists on refresh

3. **Manual verification**:
   - Configure 2 providers for same virtual model
   - Disable one → requests route to other
   - Re-enable → requests can route to either
   - Disable both → 502 error

---

## Rollback Plan

If issues arise:
1. Revert migration (goose down removes `disabled` column)
2. Revert code changes — engine will simply ignore missing field (scan will fail but that's caught at startup)
3. Or keep column, set all to 0 via SQL: `UPDATE providers SET disabled = 0;`

---

## Future Extensions (Not in Scope)

- `disabled_reason` text column + `disabled_at` timestamp (for audit)
- Scheduled re-enable (cron-like)
- Per-virtual-model disable (provider disabled only for specific VMs)
- Metrics/alerting on disabled providers
- Admin API to list disabled providers