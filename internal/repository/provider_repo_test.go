package repository_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

// setupTestDB creates a minimal in-memory DB with the providers table schema.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Create providers table matching migration 030 schema
	_, err = database.Exec(`
		CREATE TABLE providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			api_type TEXT NOT NULL CHECK(api_type IN ('openai', 'anthropic', 'cloudflare')),
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
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}
	return database
}

func TestProviderRepo_AccountID_RoundTrip(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := repository.NewProviderRepository(database)
	ctx := t

	// Create provider with AccountID
	p := &models.Provider{
		Name:            "test-cloudflare",
		APIType:         models.APITypeCloudflare,
		BaseURL:         "https://api.cloudflare.com/client/v4/accounts/test-123/ai",
		APIKeyEncrypted: "encrypted",
		AccountID:       "test-123",
	}

	if err := repo.Create(t.Context(), p); err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if p.ID == 0 {
		t.Error("expected provider ID to be set")
	}

	// GetByID
	fetched, err := repo.GetByID(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("failed to get provider by ID: %v", err)
	}
	if fetched == nil {
		t.Fatal("expected provider to exist")
	}
	if fetched.AccountID != "test-123" {
		t.Errorf("expected AccountID 'test-123', got '%s'", fetched.AccountID)
	}

	// GetByName
	fetchedByName, err := repo.GetByName(t.Context(), "test-cloudflare")
	if err != nil {
		t.Fatalf("failed to get provider by name: %v", err)
	}
	if fetchedByName == nil {
		t.Fatal("expected provider to exist")
	}
	if fetchedByName.AccountID != "test-123" {
		t.Errorf("expected AccountID 'test-123', got '%s'", fetchedByName.AccountID)
	}

	// List
	providers, err := repo.List(t.Context())
	if err != nil {
		t.Fatalf("failed to list providers: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}
	if providers[0].AccountID != "test-123" {
		t.Errorf("expected AccountID 'test-123', got '%s'", providers[0].AccountID)
	}

	// ListByMetadata (empty filters → delegates to List)
	providersByMeta, err := repo.ListByMetadata(t.Context(), map[string]string{})
	if err != nil {
		t.Fatalf("failed to list providers by metadata: %v", err)
	}
	if len(providersByMeta) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providersByMeta))
	}

	// Update: AccountID should NOT change (immutable)
	fetched.Name = "test-cloudflare-updated"
	if err := repo.Update(t.Context(), fetched); err != nil {
		t.Fatalf("failed to update provider: %v", err)
	}
	updated, err := repo.GetByID(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("failed to get updated provider: %v", err)
	}
	if updated.AccountID != "test-123" {
		t.Errorf("expected AccountID to remain 'test-123' after update, got '%s'", updated.AccountID)
	}

	// Empty AccountID round-trip
	emptyProvider := &models.Provider{
		Name:            "test-openai",
		APIType:         models.APITypeOpenAI,
		BaseURL:         "https://api.openai.com/v1",
		APIKeyEncrypted: "encrypted",
		AccountID:       "",
	}
	if err := repo.Create(t.Context(), emptyProvider); err != nil {
		t.Fatalf("failed to create provider with empty AccountID: %v", err)
	}
	fetchedEmpty, err := repo.GetByID(t.Context(), emptyProvider.ID)
	if err != nil {
		t.Fatalf("failed to get provider with empty AccountID: %v", err)
	}
	if fetchedEmpty.AccountID != "" {
		t.Errorf("expected empty AccountID, got '%s'", fetchedEmpty.AccountID)
	}

	_ = ctx
	_ = repo
}

// TestProviderRepo_ProviderKey_RoundTrip verifies provider_key persistence,
// GetByKey lookup, and the empty-key→name normalization for legacy rows.
func TestProviderRepo_ProviderKey_RoundTrip(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := repository.NewProviderRepository(database)

	// Create with explicit key
	p := &models.Provider{
		Name:        "OpenAI Prod",
		APIType:     models.APITypeOpenAI,
		BaseURL:     "https://api.openai.com/v1",
		ProviderKey: "openai",
	}
	if err := repo.Create(t.Context(), p); err != nil {
		t.Fatalf("create: %v", err)
	}

	byKey, err := repo.GetByKey(t.Context(), "openai")
	if err != nil || byKey == nil {
		t.Fatalf("GetByKey(openai) = %v, %v; want provider", byKey, err)
	}
	if byKey.ID != p.ID || byKey.ProviderKey != "openai" {
		t.Errorf("GetByKey returned %+v, want ID %d key openai", byKey, p.ID)
	}

	// Missing key → nil, no error
	missing, err := repo.GetByKey(t.Context(), "nope")
	if err != nil || missing != nil {
		t.Errorf("GetByKey(nope) = %v, %v; want nil, nil", missing, err)
	}

	// Update the key
	p.ProviderKey = "oai"
	if err := repo.Update(t.Context(), p); err != nil {
		t.Fatalf("update: %v", err)
	}
	byKey, err = repo.GetByKey(t.Context(), "oai")
	if err != nil || byKey == nil {
		t.Fatalf("GetByKey(oai) after update = %v, %v; want provider", byKey, err)
	}
	old, err := repo.GetByKey(t.Context(), "openai")
	if err != nil || old != nil {
		t.Errorf("GetByKey(openai) after update = %v, %v; want nil, nil", old, err)
	}

	// Legacy row: empty provider_key in DB normalizes to name on read.
	_, err = database.Exec(`INSERT INTO providers (name, api_type, base_url, api_key_encrypted, account_id, provider_key)
		VALUES ('legacy', 'openai', 'https://x', 'k', '', '')`)
	if err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	legacy, err := repo.GetByName(t.Context(), "legacy")
	if err != nil || legacy == nil {
		t.Fatalf("GetByName(legacy) = %v, %v; want provider", legacy, err)
	}
	if legacy.ProviderKey != "legacy" {
		t.Errorf("legacy ProviderKey = %q, want %q (normalized to name)", legacy.ProviderKey, "legacy")
	}

	// All read paths normalize
	listed, err := repo.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, lp := range listed {
		if lp.ProviderKey == "" {
			t.Errorf("List returned empty ProviderKey for %q", lp.Name)
		}
	}
}
