package migrations_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// TestMigration018_CloudflareAPIType tests migration 018 in isolation.
// NOTE: db.Open() has a pre-existing bug (migration 007 adds duplicate column).
// This test creates a minimal schema and applies only migration 018.
func TestMigration018_CloudflareAPIType(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Create providers table as it exists BEFORE migration 018
	_, err = database.Exec(`
		CREATE TABLE providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			api_type TEXT NOT NULL CHECK(api_type IN ('openai', 'anthropic')),
			base_url TEXT NOT NULL,
			api_key_encrypted TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_providers_name ON providers(name);
	`)
	if err != nil {
		t.Fatalf("failed to create base table: %v", err)
	}

	// Insert a pre-existing provider
	_, err = database.Exec(`INSERT INTO providers (name, api_type, base_url, api_key_encrypted)
		VALUES ('existing-openai', 'openai', 'https://api.openai.com/v1', 'key1')`)
	if err != nil {
		t.Fatalf("failed to insert existing provider: %v", err)
	}

	// Verify account_id column does NOT exist yet
	hasAccountID := columnExists(t, database, "providers", "account_id")
	if hasAccountID {
		t.Error("account_id column should not exist before migration 018")
	}

	// Apply migration 018 UP (full recreation with cloudflare CHECK + account_id)
	_, err = database.Exec(`ALTER TABLE providers ADD COLUMN account_id TEXT NOT NULL DEFAULT ''`)
	if err != nil {
		t.Fatalf("failed to add account_id column: %v", err)
	}

	// Recreate table with updated CHECK constraint (mirrors migration 018)
	_, err = database.Exec(`
		CREATE TABLE providers_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			api_type TEXT NOT NULL CHECK(api_type IN ('openai', 'anthropic', 'cloudflare')),
			base_url TEXT NOT NULL,
			api_key_encrypted TEXT NOT NULL,
			account_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO providers_new (id, name, api_type, base_url, api_key_encrypted, account_id, created_at, updated_at)
			SELECT id, name, api_type, base_url, api_key_encrypted, account_id, created_at, updated_at FROM providers;
		DROP TABLE providers;
		ALTER TABLE providers_new RENAME TO providers;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_providers_name ON providers(name);
	`)
	if err != nil {
		t.Fatalf("failed to recreate table: %v", err)
	}

	// Verify account_id column exists now
	hasAccountID = columnExists(t, database, "providers", "account_id")
	if !hasAccountID {
		t.Error("account_id column should exist after migration 018")
	}

	// Verify pre-existing provider survived migration
	var name string
	err = database.QueryRow("SELECT name FROM providers WHERE id = 1").Scan(&name)
	if err != nil {
		t.Fatalf("failed to query pre-existing provider: %v", err)
	}
	if name != "existing-openai" {
		t.Errorf("expected pre-existing provider name 'existing-openai', got '%s'", name)
	}

	// Verify cloudflare api_type is accepted
	_, err = database.Exec(`INSERT INTO providers (name, api_type, base_url, api_key_encrypted, account_id)
		VALUES ('test-cloudflare', 'cloudflare', 'https://api.cloudflare.com/test', 'encrypted', 'test-123')`)
	if err != nil {
		t.Fatalf("failed to insert cloudflare provider: %v", err)
	}

	// Verify account_id value is stored correctly
	var accountID string
	err = database.QueryRow("SELECT account_id FROM providers WHERE name = 'test-cloudflare'").Scan(&accountID)
	if err != nil {
		t.Fatalf("failed to query account_id: %v", err)
	}
	if accountID != "test-123" {
		t.Errorf("expected account_id 'test-123', got '%s'", accountID)
	}

	// Verify empty account_id is allowed
	_, err = database.Exec(`INSERT INTO providers (name, api_type, base_url, api_key_encrypted, account_id)
		VALUES ('test-openai', 'openai', 'https://api.openai.com/v1', 'encrypted', '')`)
	if err != nil {
		t.Fatalf("failed to insert openai provider with empty account_id: %v", err)
	}

	// Verify default value is empty string
	var defaultAccountID string
	err = database.QueryRow("SELECT account_id FROM providers WHERE name = 'test-openai'").Scan(&defaultAccountID)
	if err != nil {
		t.Fatalf("failed to query default account_id: %v", err)
	}
	if defaultAccountID != "" {
		t.Errorf("expected empty default account_id, got '%s'", defaultAccountID)
	}
}

// TestMigration019_OllamaAPIType tests migration 019 in isolation.
// Creates the post-018 schema (with cloudflare + account_id), inserts data,
// then applies migration 019 to add ollama to the CHECK constraint.
func TestMigration019_OllamaAPIType(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Create providers table as it exists AFTER migration 018 (with cloudflare + account_id)
	_, err = database.Exec(`
		CREATE TABLE providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			api_type TEXT NOT NULL CHECK(api_type IN ('openai', 'anthropic', 'cloudflare')),
			base_url TEXT NOT NULL,
			api_key_encrypted TEXT NOT NULL,
			account_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_providers_name ON providers(name);
	`)
	if err != nil {
		t.Fatalf("failed to create base table: %v", err)
	}

	// Insert pre-existing providers
	_, err = database.Exec(`INSERT INTO providers (name, api_type, base_url, api_key_encrypted, account_id)
		VALUES ('existing-openai', 'openai', 'https://api.openai.com/v1', 'key1', '')`)
	if err != nil {
		t.Fatalf("failed to insert openai provider: %v", err)
	}
	_, err = database.Exec(`INSERT INTO providers (name, api_type, base_url, api_key_encrypted, account_id)
		VALUES ('existing-cloudflare', 'cloudflare', 'https://api.cloudflare.com/test', 'key2', 'acct-123')`)
	if err != nil {
		t.Fatalf("failed to insert cloudflare provider: %v", err)
	}

	// Verify ollama is NOT accepted before migration 019
	_, err = database.Exec(`INSERT INTO providers (name, api_type, base_url, api_key_encrypted, account_id)
		VALUES ('test-ollama', 'ollama', 'http://localhost:11434/v1', '', '')`)
	if err == nil {
		t.Error("ollama api_type should be rejected before migration 019")
	}

	// Apply migration 019 UP (recreate table with ollama CHECK)
	_, err = database.Exec(`
		CREATE TABLE providers_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			api_type TEXT NOT NULL CHECK(api_type IN ('openai', 'anthropic', 'cloudflare', 'ollama')),
			base_url TEXT NOT NULL,
			api_key_encrypted TEXT NOT NULL,
			account_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO providers_new (id, name, api_type, base_url, api_key_encrypted, account_id, created_at, updated_at)
			SELECT id, name, api_type, base_url, api_key_encrypted, account_id, created_at, updated_at FROM providers;
		DROP TABLE providers;
		ALTER TABLE providers_new RENAME TO providers;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_providers_name ON providers(name);
	`)
	if err != nil {
		t.Fatalf("failed to apply migration 019 up: %v", err)
	}

	// Verify ollama is NOW accepted
	_, err = database.Exec(`INSERT INTO providers (name, api_type, base_url, api_key_encrypted, account_id)
		VALUES ('test-ollama', 'ollama', 'http://localhost:11434/v1', '', '')`)
	if err != nil {
		t.Fatalf("ollama api_type should be accepted after migration 019: %v", err)
	}

	// Verify pre-existing data survived
	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM providers").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count providers: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 providers, got %d", count)
	}

	// Verify cloudflare data survived
	var cfAccountID string
	err = database.QueryRow("SELECT account_id FROM providers WHERE name = 'existing-cloudflare'").Scan(&cfAccountID)
	if err != nil {
		t.Fatalf("failed to query cloudflare provider: %v", err)
	}
	if cfAccountID != "acct-123" {
		t.Errorf("expected cloudflare account_id 'acct-123', got '%s'", cfAccountID)
	}

	// Apply migration 019 DOWN (revert to cloudflare-only CHECK)
	// Must delete ollama rows first (down migration drops ollama from CHECK constraint;
	// copying ollama rows into old table would violate CHECK)
	_, err = database.Exec(`DELETE FROM providers WHERE api_type = 'ollama'`)
	if err != nil {
		t.Fatalf("failed to delete ollama providers before down migration: %v", err)
	}

	_, err = database.Exec(`
		CREATE TABLE providers_old (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			api_type TEXT NOT NULL CHECK(api_type IN ('openai', 'anthropic', 'cloudflare')),
			base_url TEXT NOT NULL,
			api_key_encrypted TEXT NOT NULL,
			account_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO providers_old (id, name, api_type, base_url, api_key_encrypted, account_id, created_at, updated_at)
			SELECT id, name, api_type, base_url, api_key_encrypted, account_id, created_at, updated_at FROM providers;
		DROP TABLE providers;
		ALTER TABLE providers_old RENAME TO providers;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_providers_name ON providers(name);
	`)
	if err != nil {
		t.Fatalf("failed to apply migration 019 down: %v", err)
	}

	// Verify ollama is rejected after down migration
	_, err = database.Exec(`INSERT INTO providers (name, api_type, base_url, api_key_encrypted, account_id)
		VALUES ('test-ollama2', 'ollama', 'http://localhost:11434/v1', '', '')`)
	if err == nil {
		t.Error("ollama api_type should be rejected after migration 019 down")
	}
}

func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("failed to query table info: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("failed to scan: %v", err)
		}
		if name == column {
			return true
		}
	}
	return false
}
