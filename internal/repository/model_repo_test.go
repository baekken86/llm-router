package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupModelTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`PRAGMA foreign_keys = ON`)

	// Create required tables
	for _, stmt := range []string{
		`CREATE TABLE providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			base_url TEXT NOT NULL,
			api_key_encrypted TEXT NOT NULL DEFAULT '',
			api_type TEXT NOT NULL DEFAULT 'openai',
			provider_key TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE models (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			disabled INTEGER NOT NULL DEFAULT 0,
			disabled_until TIMESTAMP NULL,
			disabled_reason TEXT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(provider_id, name)
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	return db
}

func TestDisabledReasonRoundTrip(t *testing.T) {
	db := setupModelTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewModelRepository(db)

	db.Exec(`INSERT INTO providers (name, base_url) VALUES ('p1', 'http://localhost')`)

	stale, err := repo.Upsert(ctx, 1, "model-stale")
	if err != nil {
		t.Fatal(err)
	}
	manual, err := repo.Upsert(ctx, 1, "model-manual")
	if err != nil {
		t.Fatal(err)
	}

	// Stale disable: no duration, reason "stale"
	if err := repo.ToggleDisabled(ctx, stale.ID, true, nil, "stale"); err != nil {
		t.Fatal(err)
	}
	// Manual disable: reason "manual"
	if err := repo.ToggleDisabled(ctx, manual.ID, true, nil, "manual"); err != nil {
		t.Fatal(err)
	}

	// GetByID must surface the reasons
	gotStale, err := repo.GetByID(ctx, stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotStale == nil || !gotStale.Disabled || gotStale.DisabledReason != "stale" {
		t.Errorf("GetByID: expected disabled model with reason 'stale', got %+v", gotStale)
	}

	gotManual, err := repo.GetByID(ctx, manual.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotManual == nil || !gotManual.Disabled || gotManual.DisabledReason != "manual" {
		t.Errorf("GetByID: expected disabled model with reason 'manual', got %+v", gotManual)
	}

	// ListAll must surface the reasons too
	all, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[int64]string{}
	for _, m := range all {
		reasons[m.ID] = m.DisabledReason
	}
	if reasons[stale.ID] != "stale" {
		t.Errorf("ListAll: model %d expected reason 'stale', got %q", stale.ID, reasons[stale.ID])
	}
	if reasons[manual.ID] != "manual" {
		t.Errorf("ListAll: model %d expected reason 'manual', got %q", manual.ID, reasons[manual.ID])
	}

	// Upsert on existing row must preserve disabled state and round-trip the reason
	upserted, err := repo.Upsert(ctx, 1, "model-stale")
	if err != nil {
		t.Fatal(err)
	}
	if !upserted.Disabled || upserted.DisabledReason != "stale" {
		t.Errorf("Upsert: expected disabled reason 'stale' preserved, got disabled=%v reason=%q", upserted.Disabled, upserted.DisabledReason)
	}

	// Re-enable clears the reason via ToggleDisabled
	if err := repo.ToggleDisabled(ctx, stale.ID, false, nil, ""); err != nil {
		t.Fatal(err)
	}
	enabled, err := repo.GetByID(ctx, stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if enabled.Disabled || enabled.DisabledReason != "" {
		t.Errorf("expected re-enabled model with empty reason, got disabled=%v reason=%q", enabled.Disabled, enabled.DisabledReason)
	}
}

func TestDisabledReasonWithDuration(t *testing.T) {
	db := setupModelTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewModelRepository(db)

	db.Exec(`INSERT INTO providers (name, base_url) VALUES ('p1', 'http://localhost')`)
	m, err := repo.Upsert(ctx, 1, "model-temp")
	if err != nil {
		t.Fatal(err)
	}

	d := 5 * time.Minute
	if err := repo.ToggleDisabled(ctx, m.ID, true, &d, "manual"); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByID(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.Disabled || got.DisabledReason != "manual" {
		t.Errorf("GetByID: expected disabled with reason 'manual', got %+v", got)
	}
	if got.DisabledUntil == nil {
		t.Error("GetByID: expected DisabledUntil set, got nil")
	}

	// ListEnabled must not include disabled models
	enabled, err := repo.ListEnabled(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(enabled) != 0 {
		t.Errorf("ListEnabled: expected 0 enabled models, got %d", len(enabled))
	}
}
