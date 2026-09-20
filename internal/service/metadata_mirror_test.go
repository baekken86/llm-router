package service

import (
	"context"
	"testing"

	"github.com/chris/llm-router/internal/models"
)

// TestMirrorGlobalMetadata verifies that global metadata (m.*) written for a
// model name is copied into model_tags (mc.*) for EVERY provider row carrying
// that name, merged over existing tags, and that other models are untouched.
func TestMirrorGlobalMetadata(t *testing.T) {
	modelRepo := newMockModelRepo()
	tagRepo := newMockTagRepo()
	globalMeta := newMockGlobalMetaRepo()

	// Two providers carry the same model name; a third model stays untouched.
	modelRepo.models = []models.Model{
		{ID: 1, ProviderID: 15, Name: "stealth/union-alpha"},
		{ID: 2, ProviderID: 6, Name: "stealth/union-alpha"},
		{ID: 3, ProviderID: 15, Name: "other-model"},
	}

	// Existing tags on model 1 must survive (mirror merges, gm wins).
	if err := tagRepo.Set(context.Background(), 1, "", map[string]string{"speed": "90"}); err != nil {
		t.Fatalf("seed tags: %v", err)
	}

	// Global metadata as the UI/import writes it.
	if err := globalMeta.Set(context.Background(), "stealth/union-alpha", "", map[string]string{
		"intelligence": "22", "coding": "50", "has_reasoning_effort": "false",
	}); err != nil {
		t.Fatalf("seed global meta: %v", err)
	}

	ms := NewModelService(modelRepo, tagRepo, newMockProviderRepo(), nil, globalMeta, nil)
	ms.MirrorGlobalMetadata(context.Background(), "stealth/union-alpha")

	for _, id := range []int64{1, 2} {
		tags := tagsFor(t, tagRepo, id)
		if tags["intelligence"] != "22" {
			t.Errorf("model %d intelligence = %q, want 22", id, tags["intelligence"])
		}
		if tags["coding"] != "50" {
			t.Errorf("model %d coding = %q, want 50", id, tags["coding"])
		}
		if tags["has_reasoning_effort"] != "false" {
			t.Errorf("model %d has_reasoning_effort = %q, want false", id, tags["has_reasoning_effort"])
		}
	}
	if tags := tagsFor(t, tagRepo, 1); tags["speed"] != "90" {
		t.Errorf("existing tag lost: speed = %q, want 90", tags["speed"])
	}
	if tags := tagsFor(t, tagRepo, 3); tags["intelligence"] != "" {
		t.Errorf("other model touched: intelligence = %q", tags["intelligence"])
	}
}

// TestMirrorGlobalMetadata_EffortRows mirrors effort-specific metadata rows.
func TestMirrorGlobalMetadata_EffortRows(t *testing.T) {
	modelRepo := newMockModelRepo()
	tagRepo := newMockTagRepo()
	globalMeta := newMockGlobalMetaRepo()

	modelRepo.models = []models.Model{{ID: 7, ProviderID: 13, Name: "gpt-5.5"}}
	_ = globalMeta.Set(context.Background(), "gpt-5.5", "high", map[string]string{"intelligence": "37"})
	_ = globalMeta.Set(context.Background(), "gpt-5.5", "low", map[string]string{"intelligence": "31"})

	ms := NewModelService(modelRepo, tagRepo, newMockProviderRepo(), nil, globalMeta, nil)
	ms.MirrorGlobalMetadata(context.Background(), "gpt-5.5")

	// Effort-specific: check via the mock's internal map.
	if len(tagRepo.tags[7]) == 0 {
		t.Fatalf("no tags mirrored for model 7")
	}
	found := map[string]string{}
	for _, tg := range tagRepo.tags[7] {
		found[tg.ReasoningEffort+"."+tg.Key] = tg.Value
	}
	if found["high.intelligence"] != "37" || found["low.intelligence"] != "31" {
		t.Errorf("effort rows not mirrored: %v", found)
	}
}
