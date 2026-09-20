package service

import (
	"context"
	"testing"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

// mirrorOverridesRepo is a minimal in-memory override repo for the mirror tests.
type mirrorOverridesRepo struct {
	rows map[int64]map[string]map[string]string // modelID → effort → key → value
}

func newMirrorOverridesRepo() *mirrorOverridesRepo {
	return &mirrorOverridesRepo{rows: make(map[int64]map[string]map[string]string)}
}

func (r *mirrorOverridesRepo) GetByModelAndEffort(_ context.Context, modelID int64, effort string) ([]repository.OverrideRow, error) {
	var out []repository.OverrideRow
	for k, v := range r.rows[modelID][effort] {
		out = append(out, repository.OverrideRow{ModelID: modelID, ReasoningEffort: effort, Key: k, Value: v})
	}
	return out, nil
}

func (r *mirrorOverridesRepo) GetEffortsByModel(_ context.Context, modelID int64) ([]string, error) {
	var out []string
	for effort := range r.rows[modelID] {
		out = append(out, effort)
	}
	return out, nil
}

func (r *mirrorOverridesRepo) Set(_ context.Context, modelID int64, effort, key, value string) error {
	if r.rows[modelID] == nil {
		r.rows[modelID] = make(map[string]map[string]string)
	}
	if r.rows[modelID][effort] == nil {
		r.rows[modelID][effort] = make(map[string]string)
	}
	r.rows[modelID][effort][key] = value
	return nil
}

func (r *mirrorOverridesRepo) Delete(_ context.Context, modelID int64, effort, key string) error {
	delete(r.rows[modelID][effort], key)
	return nil
}

func (r *mirrorOverridesRepo) DeleteAll(_ context.Context, modelID int64, effort string) error {
	delete(r.rows[modelID], effort)
	return nil
}

func gmMap(t *testing.T, gm *mockGlobalMetaRepo, model, effort string) map[string]string {
	t.Helper()
	data, err := gm.GetByModelEffort(context.Background(), model, effort)
	if err != nil {
		t.Fatalf("GetByModelEffort: %v", err)
	}
	return data
}

// TestMirrorOverridesToGlobal verifies override writes land in the m.* layer.
func TestMirrorOverridesToGlobal(t *testing.T) {
	modelRepo := newMockModelRepo()
	tagRepo := newMockTagRepo()
	gm := newMockGlobalMetaRepo()
	overrides := newMirrorOverridesRepo()

	modelRepo.models = []models.Model{
		{ID: 1, ProviderID: 15, Name: "stealth/union-alpha"},
		{ID: 2, ProviderID: 6, Name: "stealth/union-alpha"},
		{ID: 3, ProviderID: 15, Name: "other"},
	}

	// Existing m.* values that must be replaced/cleared by overrides.
	_ = gm.Set(context.Background(), "stealth/union-alpha", "", map[string]string{
		"intelligence": "22", "coding": "50", "has_reasoning_effort": "false",
	})
	// Override: intelligence 22→99, coding deleted (passed as cleared key,
	// mirroring the handler which reports deleted keys to the mirror).
	_ = overrides.Set(context.Background(), 1, "", "intelligence", "99")
	ms := NewModelService(modelRepo, tagRepo, newMockProviderRepo(), nil, gm, nil)
	ms.SetOverrideRepo(overrides)
	ms.MirrorOverridesToGlobal(context.Background(), "stealth/union-alpha", map[string]bool{"coding": true})

	// m.* layer: intelligence updated, coding gone, has_reasoning_effort kept.
	got := gmMap(t, gm, "stealth/union-alpha", "")
	if got["intelligence"] != "99" {
		t.Errorf("m.intelligence = %q, want 99", got["intelligence"])
	}
	if _, ok := got["coding"]; ok {
		t.Errorf("m.coding should be deleted, got %q", got["coding"])
	}
	if got["has_reasoning_effort"] != "false" {
		t.Errorf("m.has_reasoning_effort = %q, want false (untouched key preserved)", got["has_reasoning_effort"])
	}

	// mc.* tags on both provider rows follow the new m.* values.
	for _, id := range []int64{1, 2} {
		tags := tagsFor(t, tagRepo, id)
		if tags["intelligence"] != "99" {
			t.Errorf("model %d mc.intelligence = %q, want 99", id, tags["intelligence"])
		}
		if _, ok := tags["coding"]; ok {
			t.Errorf("model %d mc.coding should be gone", id)
		}
	}

	// Unrelated model untouched.
	if got := gmMap(t, gm, "other", ""); len(got) != 0 {
		t.Errorf("other model metadata touched: %v", got)
	}
}

// TestMirrorOverridesToGlobal_ClearAll verifies that clearing all overrides
// purges their keys from the m.* layer instead of resurrecting stale values.
func TestMirrorOverridesToGlobal_ClearAll(t *testing.T) {
	modelRepo := newMockModelRepo()
	tagRepo := newMockTagRepo()
	gm := newMockGlobalMetaRepo()
	overrides := newMirrorOverridesRepo()

	modelRepo.models = []models.Model{{ID: 5, ProviderID: 13, Name: "gpt-9"}}

	// m.* layer was previously synced with the override (intelligence=77).
	_ = gm.Set(context.Background(), "gpt-9", "", map[string]string{
		"intelligence": "77", "context_window": "128000",
	})
	// Override row that is about to be cleared.
	_ = overrides.Set(context.Background(), 5, "", "intelligence", "77")

	ms := NewModelService(modelRepo, tagRepo, newMockProviderRepo(), nil, gm, nil)
	ms.SetOverrideRepo(overrides)

	// User clears the override (handler reports the deleted key).
	_ = overrides.DeleteAll(context.Background(), 5, "")
	ms.MirrorOverridesToGlobal(context.Background(), "gpt-9", map[string]bool{"intelligence": true})

	got := gmMap(t, gm, "gpt-9", "")
	if _, ok := got["intelligence"]; ok {
		t.Errorf("m.intelligence should be purged after override clear, got %q", got["intelligence"])
	}
	if got["context_window"] != "128000" {
		t.Errorf("untouched key lost: context_window = %q", got["context_window"])
	}
}

// TestMirrorOverridesToGlobal_MultiProvider ensures both provider rows of the
// same model name converge on the same effective values.
func TestMirrorOverridesToGlobal_MultiProvider(t *testing.T) {
	modelRepo := newMockModelRepo()
	tagRepo := newMockTagRepo()
	gm := newMockGlobalMetaRepo()
	overrides := newMirrorOverridesRepo()

	modelRepo.models = []models.Model{
		{ID: 1, ProviderID: 15, Name: "same/model"},
		{ID: 2, ProviderID: 7, Name: "same/model"},
	}

	_ = gm.Set(context.Background(), "same/model", "", map[string]string{"speed": "40"})
	// Only provider 15's row has the override — the effective m.* value must
	// still land on BOTH rows' tags (resolver uses m.*/mc.* per row).
	_ = overrides.Set(context.Background(), 1, "", "speed", "80")

	ms := NewModelService(modelRepo, tagRepo, newMockProviderRepo(), nil, gm, nil)
	ms.SetOverrideRepo(overrides)
	ms.MirrorOverridesToGlobal(context.Background(), "same/model", nil)

	for _, id := range []int64{1, 2} {
		if tags := tagsFor(t, tagRepo, id); tags["speed"] != "80" {
			t.Errorf("model %d mc.speed = %q, want 80", id, tags["speed"])
		}
	}
	if got := gmMap(t, gm, "same/model", ""); got["speed"] != "80" {
		t.Errorf("m.speed = %q, want 80", got["speed"])
	}
}
