package service

import (
	"context"
	"strings"
	"testing"

	"github.com/chris/llm-router/internal/models"
)

// newProviderKeyFixture wires a providerService against the in-memory mock
// provider repo (GetByKey-backed uniqueness checks).
func newProviderKeyFixture(t *testing.T) (ProviderService, *mockProviderRepo) {
	t.Helper()
	repo := newMockProviderRepo()
	svc := NewProviderService(repo, newMockProviderMetaRepo(), []byte("00000000000000000000000000000000"))
	return svc, repo
}

// TestProviderService_Create_ProviderKeyDefaultsToName verifies that a
// provider created without an explicit provider_key gets its name as key.
func TestProviderService_Create_ProviderKeyDefaultsToName(t *testing.T) {
	svc, repo := newProviderKeyFixture(t)
	ctx := context.Background()

	p, err := svc.Create(ctx, models.CreateProviderRequest{
		Name:    "openai",
		APIType: models.APITypeOpenAI,
		BaseURL: "https://api.openai.com/v1",
		APIKey:  "sk-test",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ProviderKey != "openai" {
		t.Errorf("ProviderKey = %q, want %q (default to name)", p.ProviderKey, "openai")
	}

	byKey, err := repo.GetByKey(ctx, "openai")
	if err != nil || byKey == nil {
		t.Fatalf("GetByKey(openai) = %v, %v; want provider", byKey, err)
	}
	if byKey.ID != p.ID {
		t.Errorf("GetByKey returned ID %d, want %d", byKey.ID, p.ID)
	}
}

// TestProviderService_Create_ProviderKeyExplicit verifies an explicit key is
// stored and addressable via GetByKey.
func TestProviderService_Create_ProviderKeyExplicit(t *testing.T) {
	svc, repo := newProviderKeyFixture(t)
	ctx := context.Background()

	p, err := svc.Create(ctx, models.CreateProviderRequest{
		Name:        "my-openai",
		APIType:     models.APITypeOpenAI,
		BaseURL:     "https://api.openai.com/v1",
		APIKey:      "sk-test",
		ProviderKey: "oai",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ProviderKey != "oai" {
		t.Errorf("ProviderKey = %q, want %q", p.ProviderKey, "oai")
	}

	byKey, err := repo.GetByKey(ctx, "oai")
	if err != nil || byKey == nil || byKey.ID != p.ID {
		t.Fatalf("GetByKey(oai) = %v, %v; want provider %d", byKey, err, p.ID)
	}
}

// TestProviderService_Create_ProviderKeyUnique verifies a duplicate key is
// rejected (both explicit-vs-explicit and default-name collisions).
func TestProviderService_Create_ProviderKeyUnique(t *testing.T) {
	svc, _ := newProviderKeyFixture(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, models.CreateProviderRequest{
		Name:        "first",
		APIType:     models.APITypeOpenAI,
		BaseURL:     "https://x",
		APIKey:      "k",
		ProviderKey: "shared",
	}); err != nil {
		t.Fatalf("first create: %v", err)
	}

	// Same explicit key → rejected.
	_, err := svc.Create(ctx, models.CreateProviderRequest{
		Name:        "second",
		APIType:     models.APITypeOpenAI,
		BaseURL:     "https://x",
		APIKey:      "k",
		ProviderKey: "shared",
	})
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Errorf("expected 'already in use' error, got %v", err)
	}

	// Default (name) colliding with an existing key → rejected.
	if _, err := svc.Create(ctx, models.CreateProviderRequest{
		Name:    "shared",
		APIType: models.APITypeOpenAI,
		BaseURL: "https://x",
		APIKey:  "k",
	}); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Errorf("expected 'already in use' error for name collision, got %v", err)
	}
}

// TestProviderService_Create_ProviderKeyValidation verifies reserved names
// and separators are rejected.
func TestProviderService_Create_ProviderKeyValidation(t *testing.T) {
	svc, _ := newProviderKeyFixture(t)
	ctx := context.Background()

	for _, key := range []string{"virtual", "a/b", ""} {
		req := models.CreateProviderRequest{
			Name:        "p-" + key,
			APIType:     models.APITypeOpenAI,
			BaseURL:     "https://x",
			APIKey:      "k",
			ProviderKey: key,
		}
		// Empty key defaults to the (invalid-looking but actually fine) name,
		// so only explicit invalid keys must fail here.
		if key == "" {
			continue
		}
		if _, err := svc.Create(ctx, req); err == nil {
			t.Errorf("provider_key %q: expected error, got nil", key)
		}
	}
}

// TestProviderService_Update_ProviderKey verifies key changes: rename,
// uniqueness (other provider's key rejected, own key allowed), reset to
// name on empty string, and validation.
func TestProviderService_Update_ProviderKey(t *testing.T) {
	svc, _ := newProviderKeyFixture(t)
	ctx := context.Background()

	p1, err := svc.Create(ctx, models.CreateProviderRequest{
		Name: "one", APIType: models.APITypeOpenAI, BaseURL: "https://x", APIKey: "k",
	})
	if err != nil {
		t.Fatalf("create one: %v", err)
	}
	if _, err := svc.Create(ctx, models.CreateProviderRequest{
		Name: "two", APIType: models.APITypeOpenAI, BaseURL: "https://x", APIKey: "k",
	}); err != nil {
		t.Fatalf("create two: %v", err)
	}

	newKey := "renamed"
	updated, err := svc.Update(ctx, p1.ID, models.UpdateProviderRequest{ProviderKey: &newKey})
	if err != nil {
		t.Fatalf("update to renamed: %v", err)
	}
	if updated.ProviderKey != "renamed" {
		t.Errorf("ProviderKey = %q, want %q", updated.ProviderKey, "renamed")
	}

	// Stealing p2's key (default = name "two") must fail.
	taken := "two"
	if _, err := svc.Update(ctx, p1.ID, models.UpdateProviderRequest{ProviderKey: &taken}); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Errorf("expected 'already in use', got %v", err)
	}

	// Setting p1's key to its own current value is allowed.
	self := "renamed"
	if _, err := svc.Update(ctx, p1.ID, models.UpdateProviderRequest{ProviderKey: &self}); err != nil {
		t.Errorf("self key update should be allowed, got %v", err)
	}

	// Empty string resets to the provider name.
	reset := ""
	updated, err = svc.Update(ctx, p1.ID, models.UpdateProviderRequest{ProviderKey: &reset})
	if err != nil {
		t.Fatalf("reset key: %v", err)
	}
	if updated.ProviderKey != "one" {
		t.Errorf("ProviderKey = %q, want %q (reset to name)", updated.ProviderKey, "one")
	}

	// Reserved / separator values rejected.
	bad := "virtual"
	if _, err := svc.Update(ctx, p1.ID, models.UpdateProviderRequest{ProviderKey: &bad}); err == nil {
		t.Error("expected error for provider_key 'virtual'")
	}
	bad = "x/y"
	if _, err := svc.Update(ctx, p1.ID, models.UpdateProviderRequest{ProviderKey: &bad}); err == nil {
		t.Error("expected error for provider_key containing '/'")
	}
}

// TestProviderService_Create_ProviderKeyLegacyNameCollision documents the
// upgrade edge case: two legacy rows can never exist with duplicate keys
// (backfill uses the unique name), so creating "two" after "one" renamed its
// key to "two" must fail on the name.
func TestProviderService_Create_ProviderKeyLegacyNameCollision(t *testing.T) {
	svc, repo := newProviderKeyFixture(t)
	ctx := context.Background()

	p1, err := svc.Create(ctx, models.CreateProviderRequest{
		Name: "one", APIType: models.APITypeOpenAI, BaseURL: "https://x", APIKey: "k",
	})
	if err != nil {
		t.Fatalf("create one: %v", err)
	}

	newKey := "two"
	if _, err := svc.Update(ctx, p1.ID, models.UpdateProviderRequest{ProviderKey: &newKey}); err != nil {
		t.Fatalf("rename one→two: %v", err)
	}
	_ = repo

	// Creating a provider NAMED "two" now collides with the renamed key.
	_, err = svc.Create(ctx, models.CreateProviderRequest{
		Name: "two", APIType: models.APITypeOpenAI, BaseURL: "https://x", APIKey: "k",
	})
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Errorf("expected 'already in use', got %v", err)
	}
}
