package service

import (
	"context"
	"testing"

	"github.com/chris/llm-router/internal/models"
)

// TestDeleteStaleDisabled_OnlyDeletesStale seeds disabled models with three
// different reasons (stale / manual / circuit_breaker) plus an enabled model,
// then asserts DeleteStaleDisabled removes only the stale one and reports the
// correct count.
func TestDeleteStaleDisabled_OnlyDeletesStale(t *testing.T) {
	repo := newMockModelRepo()
	modelRepo := repo

	seed := []models.Model{
		{ID: 1, ProviderID: 7, Name: "stale-model", Disabled: true, DisabledReason: "stale"},
		{ID: 2, ProviderID: 7, Name: "manual-model", Disabled: true, DisabledReason: "manual"},
		{ID: 3, ProviderID: 7, Name: "cb-model", Disabled: true, DisabledReason: "circuit_breaker"},
		{ID: 4, ProviderID: 7, Name: "enabled-model"},
	}
	for i := range seed {
		m := seed[i]
		if err := modelRepo.Create(context.Background(), &m); err != nil {
			t.Fatalf("seed model %s: %v", m.Name, err)
		}
		// Create doesn't carry the disabled state through — re-apply it.
		modelRepo.models[len(modelRepo.models)-1].Disabled = seed[i].Disabled
		modelRepo.models[len(modelRepo.models)-1].DisabledReason = seed[i].DisabledReason
	}

	providerRepo := newMockProviderRepo()
	providerRepo.add(&models.Provider{ID: 7, Name: "chatgpt"})
	ms := NewModelService(modelRepo, newMockTagRepo(), providerRepo, nil, newMockGlobalMetaRepo(), nil)

	deleted, err := ms.DeleteStaleDisabled(context.Background())
	if err != nil {
		t.Fatalf("DeleteStaleDisabled: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	remaining := map[string]bool{}
	for _, m := range modelRepo.models {
		remaining[m.Name] = true
	}
	if remaining["stale-model"] {
		t.Error("stale-model should have been deleted")
	}
	for _, name := range []string{"manual-model", "cb-model", "enabled-model"} {
		if !remaining[name] {
			t.Errorf("%s should have been kept", name)
		}
	}

	// Idempotent: a second run deletes nothing.
	deleted2, err := ms.DeleteStaleDisabled(context.Background())
	if err != nil {
		t.Fatalf("second DeleteStaleDisabled: %v", err)
	}
	if deleted2 != 0 {
		t.Errorf("second run deleted = %d, want 0", deleted2)
	}
}
