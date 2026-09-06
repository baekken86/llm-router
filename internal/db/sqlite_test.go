package db

import (
	"path/filepath"
	"testing"
)

// TestOpenFreshDatabase runs the full migration chain against a brand-new
// database. Regression test for the 003/007 reasoning_effort duplicate-column
// crash (fresh installs died in migration 007 because 003 had been
// retrofitted to already create the column). Also guards future migrations
// that must not assume state created by an older revision of a prior
// migration.
func TestOpenFreshDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fresh.db")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open fresh database: %v", err)
	}
	defer database.Close()

	// model_tags.reasoning_effort must exist exactly once (inlined by 003).
	var count int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('model_tags') WHERE name = 'reasoning_effort'`,
	).Scan(&count); err != nil {
		t.Fatalf("pragma_table_info(model_tags): %v", err)
	}
	if count != 1 {
		t.Fatalf("model_tags.reasoning_effort column count = %d, want 1", count)
	}

	// providers.provider_key (migration 030) must exist.
	var keyCount int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('providers') WHERE name = 'provider_key'`,
	).Scan(&keyCount); err != nil {
		t.Fatalf("pragma_table_info(providers): %v", err)
	}
	if keyCount != 1 {
		t.Fatalf("providers.provider_key column count = %d, want 1", keyCount)
	}

	// The 007 uniqueness index must be present on the fresh schema.
	var idxCount int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_model_tags_model_effort_key'`,
	).Scan(&idxCount); err != nil {
		t.Fatalf("sqlite_master index lookup: %v", err)
	}
	if idxCount != 1 {
		t.Fatalf("idx_model_tags_model_effort_key index count = %d, want 1", idxCount)
	}
}
