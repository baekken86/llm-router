package repository

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupMappingTestDB(t *testing.T) *sql.DB {
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
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE models (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			disabled INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(provider_id, name)
		)`,
		`CREATE TABLE model_mappings (
			source_model_id INTEGER PRIMARY KEY REFERENCES models(id) ON DELETE CASCADE,
			target_model_name TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	return db
}

func TestSetAndGetMapping(t *testing.T) {
	db := setupMappingTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewModelMappingRepository(db)

	// Insert test data
	db.Exec(`INSERT INTO providers (name, base_url) VALUES ('p1', 'http://localhost')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-a')`)

	if err := repo.Set(ctx, 1, "llama-3.1"); err != nil {
		t.Fatal(err)
	}

	m, err := repo.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("expected mapping, got nil")
	}
	if m.SourceModelID != 1 || m.TargetModelName != "llama-3.1" {
		t.Errorf("expected 1->llama-3.1, got %d->%s", m.SourceModelID, m.TargetModelName)
	}
}

func TestGetNonexistentMapping(t *testing.T) {
	db := setupMappingTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewModelMappingRepository(db)

	m, err := repo.Get(ctx, 999)
	if err != nil {
		t.Fatal(err)
	}
	if m != nil {
		t.Error("expected nil, got mapping")
	}
}

func TestSetOverwriteMapping(t *testing.T) {
	db := setupMappingTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewModelMappingRepository(db)

	db.Exec(`INSERT INTO providers (name, base_url) VALUES ('p1', 'http://localhost')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-a')`)

	repo.Set(ctx, 1, "llama-3.1")
	repo.Set(ctx, 1, "gpt-4o")

	m, _ := repo.Get(ctx, 1)
	if m.TargetModelName != "gpt-4o" {
		t.Errorf("expected target gpt-4o, got %s", m.TargetModelName)
	}
}

func TestDeleteMapping(t *testing.T) {
	db := setupMappingTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewModelMappingRepository(db)

	db.Exec(`INSERT INTO providers (name, base_url) VALUES ('p1', 'http://localhost')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-a')`)

	repo.Set(ctx, 1, "llama-3.1")
	repo.Delete(ctx, 1)

	m, _ := repo.Get(ctx, 1)
	if m != nil {
		t.Error("expected nil after delete")
	}
}

func TestGetAll(t *testing.T) {
	db := setupMappingTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewModelMappingRepository(db)

	db.Exec(`INSERT INTO providers (name, base_url) VALUES ('p1', 'http://localhost')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-a')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-b')`)

	repo.Set(ctx, 1, "llama-3.1")
	repo.Set(ctx, 2, "gpt-4o")

	all, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 mappings, got %d", len(all))
	}
}

func TestGetAllJoined(t *testing.T) {
	db := setupMappingTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewModelMappingRepository(db)

	db.Exec(`INSERT INTO providers (name, base_url) VALUES ('p1', 'http://localhost')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, '@cf/meta/llama')`)

	repo.Set(ctx, 1, "llama")

	joined, err := repo.GetAllJoined(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(joined) != 1 {
		t.Fatalf("expected 1 joined mapping, got %d", len(joined))
	}
	j := joined[0]
	if j.SourceModelName != "@cf/meta/llama" {
		t.Errorf("expected source name @cf/meta/llama, got %s", j.SourceModelName)
	}
	if j.TargetModelName != "llama" {
		t.Errorf("expected target name llama, got %s", j.TargetModelName)
	}
	if j.SourceProviderName != "p1" {
		t.Errorf("expected source provider p1, got %s", j.SourceProviderName)
	}
}

func TestFKCascade(t *testing.T) {
	db := setupMappingTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewModelMappingRepository(db)

	db.Exec(`INSERT INTO providers (name, base_url) VALUES ('p1', 'http://localhost')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-a')`)

	repo.Set(ctx, 1, "llama-3.1")

	// Delete source model - mapping should cascade
	db.Exec(`DELETE FROM models WHERE id = 1`)

	m, _ := repo.Get(ctx, 1)
	if m != nil {
		t.Error("expected mapping to be cascade-deleted")
	}
}
