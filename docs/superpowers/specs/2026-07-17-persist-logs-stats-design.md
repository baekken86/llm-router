# Persist Logs, Syslogs, and Stats Across Restarts

**Date:** 2026-07-17
**Status:** Approved

## Problem

Logs (`LogViewModel`), syslogs (`SysLogViewModel`), and stats (`StatsModel` / `StatsHandler`) are all stored in-memory. On restart, all historical data is lost. Users lose visibility into past request activity and aggregate statistics.

## Solution

Persist request logs and syslog entries to SQLite. Compute stats from logs on startup (no separate stats table needed). Automatic 7-day retention cleanup.

## Database Schema

### Migration 013 — `request_logs`

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS request_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    log_type TEXT NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    request_id TEXT NOT NULL,
    virtual_model TEXT NOT NULL,
    client_key_id INTEGER,
    provider_name TEXT,
    model_name TEXT,
    status_code INTEGER,
    latency_ms INTEGER,
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    cached_tokens INTEGER DEFAULT 0,
    reasoning_tokens INTEGER DEFAULT 0,
    error_message TEXT,
    retry_count INTEGER DEFAULT 0,
    fallback_count INTEGER DEFAULT 0,
    rtk_intercepted BOOLEAN DEFAULT 0,
    rtk_saved_tokens INTEGER DEFAULT 0,
    caveman_intercepted BOOLEAN DEFAULT 0,
    caveman_saved_tokens INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_request_logs_timestamp ON request_logs(timestamp);
CREATE INDEX IF NOT EXISTS idx_request_logs_virtual_model ON request_logs(virtual_model);
CREATE INDEX IF NOT EXISTS idx_request_logs_type ON request_logs(log_type);
```

### Migration 014 — `syslog_entries`

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS syslog_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TIMESTAMP NOT NULL,
    level TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_syslog_entries_timestamp ON syslog_entries(timestamp);
```

## Repository

New file: `internal/repository/log_repo.go`

```go
type LogRepository struct {
    db *sql.DB
}

func NewLogRepository(db *sql.DB) *LogRepository
func (r *LogRepository) InsertRequestLog(ctx context.Context, log proxy.RequestLog) error
func (r *LogRepository) InsertSyslogEntry(ctx context.Context, entry SyslogEntry) error
func (r *LogRepository) ListRequestLogs(ctx context.Context, limit int) ([]proxy.RequestLog, error)
func (r *LogRepository) ListSyslogEntries(ctx context.Context, limit int) ([]SyslogEntry, error)
func (r *LogRepository) DeleteOlderThan(ctx context.Context, age time.Duration) error
func (r *LogRepository) ComputeStats(ctx context.Context) (*StatsData, error)
```

- `InsertRequestLog`: Converts `proxy.RequestLog` to SQL INSERT
- `InsertSyslogEntry`: Inserts timestamp + level + message
- `ListRequestLogs`: Returns last N logs ordered by timestamp DESC
- `ListSyslogEntries`: Returns last N entries ordered by timestamp DESC
- `DeleteOlderThan`: Deletes entries older than given duration
- `ComputeStats`: Aggregates from `request_logs` where `log_type = 'proxy'`:
  - Global: total_requests, successes, failures, input_tokens, output_tokens, cached_tokens
  - By virtual_model: same fields grouped by virtual_model
  - By provider_name/model_name: same fields grouped by provider + model
  - Returns a struct matching `StatsData` shape from `stats_handler.go`

## Integration Points

### `main.go` changes

1. Create `LogRepository` after database init
2. Pass `LogRepository` to `StatsHandler`
3. Modify `syslogWriter` to also persist entries via `LogRepository`
4. On startup: load last 500 logs + 1000 syslogs from DB
5. Compute stats from logs
6. Pass pre-loaded data to TUI
7. Start background cleanup goroutine (hourly)

### `StatsHandler` changes

- Accept `*repository.LogRepository` in `NewStatsHandler`
- In `RecordLog`: call `logRepo.InsertRequestLog()` alongside existing in-memory update
- Add method to load initial stats from computed DB stats

### `syslogWriter` changes

- Accept `*repository.LogRepository`
- In `Write`: parse level from log line, call `logRepo.InsertSyslogEntry()`

### TUI changes

- `New()` accepts pre-loaded `[]proxy.RequestLog` and `[]SyslogEntry`
- Stats loaded from computed stats on startup
- No changes to runtime behavior (still receives new entries via channels)

## Retention

- 7-day retention, auto-cleanup every hour
- Cleanup runs in background goroutine started from `main.go`
- Calls `logRepo.DeleteOlderThan(7 * 24 * time.Hour)`

## Files Modified

| File | Change |
|------|--------|
| `internal/db/migrations/013_create_request_logs.sql` | New migration |
| `internal/db/migrations/014_create_syslog_entries.sql` | New migration |
| `internal/repository/log_repo.go` | New repository |
| `internal/api/handlers/stats_handler.go` | Accept + use LogRepository |
| `cmd/llm-router/main.go` | Wire up persistence, startup load, cleanup |
| `internal/tui/tui.go` | Accept pre-loaded data |
| `internal/tui/logview.go` | Add method to load initial entries |
| `internal/tui/syslog.go` | Add method to load initial entries |
