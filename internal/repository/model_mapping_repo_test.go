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
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(provider_id, name)
		)`,
		`CREATE TABLE model_mappings (
			source_model_id INTEGER PRIMARY KEY REFERENCES models(id) ON DELETE CASCADE,
			target_model_id INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
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
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-b')`)

	if err := repo.Set(ctx, 1, 2); err != nil {
		t.Fatal(err)
	}

	m, err := repo.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("expected mapping, got nil")
	}
	if m.SourceModelID != 1 || m.TargetModelID != 2 {
		t.Errorf("expected 1->2, got %d->%d", m.SourceModelID, m.TargetModelID)
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
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-b')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-c')`)

	repo.Set(ctx, 1, 2)
	repo.Set(ctx, 1, 3)

	m, _ := repo.Get(ctx, 1)
	if m.TargetModelID != 3 {
		t.Errorf("expected target 3, got %d", m.TargetModelID)
	}
}

func TestDeleteMapping(t *testing.T) {
	db := setupMappingTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewModelMappingRepository(db)

	db.Exec(`INSERT INTO providers (name, base_url) VALUES ('p1', 'http://localhost')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-a')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-b')`)

	repo.Set(ctx, 1, 2)
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
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-c')`)

	repo.Set(ctx, 1, 2)
	repo.Set(ctx, 2, 3)

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
	db.Exec(`INSERT INTO providers (name, base_url) VALUES ('p2', 'http://localhost')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, '@cf/meta/llama')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (2, 'llama')`)

	repo.Set(ctx, 1, 2)

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
	if j.TargetProviderName != "p2" {
		t.Errorf("expected target provider p2, got %s", j.TargetProviderName)
	}
}

func TestFKCascade(t *testing.T) {
	db := setupMappingTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewModelMappingRepository(db)

	db.Exec(`INSERT INTO providers (name, base_url) VALUES ('p1', 'http://localhost')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-a')`)
	db.Exec(`INSERT INTO models (provider_id, name) VALUES (1, 'model-b')`)

	repo.Set(ctx, 1, 2)

	// Delete source model - mapping should cascade
	db.Exec(`DELETE FROM models WHERE id = 1`)

	m, _ := repo.Get(ctx, 1)
	if m != nil {
		t.Error("expected mapping to be cascade-deleted")
	}
}
