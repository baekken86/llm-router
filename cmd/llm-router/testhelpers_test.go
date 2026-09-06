package main

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

// testEncryptionKey is a fixed 32-byte key so provider service calls in tests
// are deterministic (production uses loadEncryptionKey).
var testEncryptionKey = []byte("00000000000000000000000000000000")

// testLogger discards output.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// discardWriter is a slog sink that throws everything away.
type discardWriter struct{}

func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// --- DB fixture -------------------------------------------------------------

// newSetupTestDB opens an in-memory sqlite DB with the schemas the setup and
// connect flows touch: providers (+ provider_metadata), oauth_tokens (with
// migration 029 columns), models, model_tags and model_metadata_global.
// Mirrors newOAuthTestDB in internal/service/oauth_service_test.go.
func newSetupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`
		CREATE TABLE providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			api_type TEXT NOT NULL CHECK(api_type IN ('openai', 'anthropic', 'cloudflare', 'ollama', 'ollama-cloud', 'codex')),
			base_url TEXT NOT NULL,
			api_key_encrypted TEXT NOT NULL,
			account_id TEXT NOT NULL DEFAULT '',
			provider_key TEXT NOT NULL DEFAULT '',
			disabled INTEGER NOT NULL DEFAULT 0,
			disabled_until TIMESTAMP NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_providers_name ON providers(name);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_providers_provider_key ON providers(provider_key);

		CREATE TABLE provider_metadata (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE oauth_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
			access_token TEXT NOT NULL,
			refresh_token TEXT NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			account_id TEXT,
			email TEXT,
			last_refresh_at DATETIME,
			id_token TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_tokens_provider ON oauth_tokens(provider_id);

		CREATE TABLE models (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			disabled INTEGER NOT NULL DEFAULT 0,
			disabled_until TIMESTAMP NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(provider_id, name)
		);

		CREATE TABLE model_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			model_id INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
			reasoning_effort TEXT NOT NULL DEFAULT '',
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE model_metadata_global (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			model_name TEXT NOT NULL,
			reasoning_effort TEXT NOT NULL DEFAULT '',
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}
	// One connection only: each pooled connection would otherwise get its own
	// empty :memory: database (same constraint as production db.Open).
	database.SetMaxOpenConns(1)
	return database
}

// setupModelFixture bundles the repositories createPredefinedModels needs.
type setupModelFixture struct {
	db        *sql.DB
	providers repository.ProviderRepository
	metadata  repository.ProviderMetadataRepository
	models    repository.ModelRepository
	tags      repository.TagRepository
	globals   repository.GlobalMetadataRepository
}

func newSetupModelFixture(t *testing.T) *setupModelFixture {
	t.Helper()
	database := newSetupTestDB(t)
	return &setupModelFixture{
		db:        database,
		providers: repository.NewProviderRepository(database),
		metadata:  repository.NewProviderMetadataRepository(database),
		models:    repository.NewModelRepository(database),
		tags:      repository.NewTagRepository(database),
		globals:   repository.NewGlobalMetadataRepository(database),
	}
}

func (f *setupModelFixture) Close() { f.db.Close() }

// seedProvider inserts a provider row directly and returns it. ProviderKey
// mirrors the service-layer default (name) so direct seeds satisfy the
// provider_key unique index.
func (f *setupModelFixture) seedProvider(t *testing.T, name string, apiType models.APIType) *models.Provider {
	t.Helper()
	p := &models.Provider{Name: name, APIType: apiType, BaseURL: "https://example.invalid", ProviderKey: name}
	if err := f.providers.Create(context.Background(), p); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	return p
}

// listModels returns the model names for a provider.
func (f *setupModelFixture) listModels(t *testing.T, providerID int64) []string {
	t.Helper()
	ms, err := f.models.ListByProvider(context.Background(), providerID)
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	names := make([]string, 0, len(ms))
	for _, m := range ms {
		names = append(names, m.Name)
	}
	return names
}

// captureStdout runs fn while capturing everything printed to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	old := os.Stdout
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()

	fn()

	os.Stdout = old
	w.Close()
	return <-done
}
