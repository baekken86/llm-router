package service

import (
	"testing"

	"github.com/chris/llm-router/internal/models"
)

func makeResolved(name, provider string) ResolvedModel {
	return ResolvedModel{
		Model:            models.Model{Name: name},
		Provider:         models.Provider{Name: provider},
		ReasoningEffort:  "",
		GlobalMetadata:   map[string]string{},
		ProviderMetadata: map[string]string{"p.name": provider},
	}
}

func makeResolvedWithEffort(name, provider, effort string) ResolvedModel {
	return ResolvedModel{
		Model:            models.Model{Name: name},
		Provider:         models.Provider{Name: provider},
		ReasoningEffort:  effort,
		GlobalMetadata:   map[string]string{},
		ProviderMetadata: map[string]string{"p.name": provider},
	}
}

func TestUnionResults(t *testing.T) {
	a := []ResolvedModel{makeResolved("m1", "p1"), makeResolved("m2", "p1")}
	b := []ResolvedModel{makeResolved("m2", "p1"), makeResolved("m3", "p2")}

	result := unionResults([][]ResolvedModel{a, b})

	if len(result) != 3 {
		t.Fatalf("union returned %d models, want 3", len(result))
	}
	// Order: a's models first, then b's new models
	if result[0].Model.Name != "m1" {
		t.Errorf("result[0] = %q, want m1", result[0].Model.Name)
	}
	if result[1].Model.Name != "m2" {
		t.Errorf("result[1] = %q, want m2", result[1].Model.Name)
	}
	if result[2].Model.Name != "m3" {
		t.Errorf("result[2] = %q, want m3", result[2].Model.Name)
	}
}

func TestUnionResults_NoDuplicates(t *testing.T) {
	a := []ResolvedModel{makeResolved("m1", "p1")}
	b := []ResolvedModel{makeResolved("m1", "p1")}

	result := unionResults([][]ResolvedModel{a, b})

	if len(result) != 1 {
		t.Fatalf("union returned %d models, want 1", len(result))
	}
}

func TestUnionResults_Empty(t *testing.T) {
	result := unionResults([][]ResolvedModel{})
	if len(result) != 0 {
		t.Fatalf("union returned %d models, want 0", len(result))
	}
}

func TestIntersectionResults(t *testing.T) {
	a := []ResolvedModel{makeResolved("m1", "p1"), makeResolved("m2", "p1"), makeResolved("m3", "p1")}
	b := []ResolvedModel{makeResolved("m2", "p1"), makeResolved("m3", "p1"), makeResolved("m4", "p2")}

	result := intersectionResults([][]ResolvedModel{a, b})

	if len(result) != 2 {
		t.Fatalf("intersection returned %d models, want 2", len(result))
	}
	// Preserves order from first source
	if result[0].Model.Name != "m2" {
		t.Errorf("result[0] = %q, want m2", result[0].Model.Name)
	}
	if result[1].Model.Name != "m3" {
		t.Errorf("result[1] = %q, want m3", result[1].Model.Name)
	}
}

func TestIntersectionResults_NoOverlap(t *testing.T) {
	a := []ResolvedModel{makeResolved("m1", "p1")}
	b := []ResolvedModel{makeResolved("m2", "p2")}

	result := intersectionResults([][]ResolvedModel{a, b})

	if len(result) != 0 {
		t.Fatalf("intersection returned %d models, want 0", len(result))
	}
}

func TestIntersectionResults_ThreeSources(t *testing.T) {
	a := []ResolvedModel{makeResolved("m1", "p1"), makeResolved("m2", "p1")}
	b := []ResolvedModel{makeResolved("m2", "p1"), makeResolved("m3", "p1")}
	c := []ResolvedModel{makeResolved("m2", "p1")}

	result := intersectionResults([][]ResolvedModel{a, b, c})

	if len(result) != 1 {
		t.Fatalf("intersection returned %d models, want 1", len(result))
	}
	if result[0].Model.Name != "m2" {
		t.Errorf("result[0] = %q, want m2", result[0].Model.Name)
	}
}

func TestDifferenceResults(t *testing.T) {
	a := []ResolvedModel{makeResolved("m1", "p1"), makeResolved("m2", "p1"), makeResolved("m3", "p1")}
	b := []ResolvedModel{makeResolved("m2", "p1")}

	result := differenceResults([][]ResolvedModel{a, b})

	if len(result) != 2 {
		t.Fatalf("difference returned %d models, want 2", len(result))
	}
	if result[0].Model.Name != "m1" {
		t.Errorf("result[0] = %q, want m1", result[0].Model.Name)
	}
	if result[1].Model.Name != "m3" {
		t.Errorf("result[1] = %q, want m3", result[1].Model.Name)
	}
}

func TestDifferenceResults_NoOverlap(t *testing.T) {
	a := []ResolvedModel{makeResolved("m1", "p1")}
	b := []ResolvedModel{makeResolved("m2", "p2")}

	result := differenceResults([][]ResolvedModel{a, b})

	if len(result) != 1 {
		t.Fatalf("difference returned %d models, want 1", len(result))
	}
	if result[0].Model.Name != "m1" {
		t.Errorf("result[0] = %q, want m1", result[0].Model.Name)
	}
}

func TestDifferenceResults_AllExcluded(t *testing.T) {
	a := []ResolvedModel{makeResolved("m1", "p1")}
	b := []ResolvedModel{makeResolved("m1", "p1")}

	result := differenceResults([][]ResolvedModel{a, b})

	if len(result) != 0 {
		t.Fatalf("difference returned %d models, want 0", len(result))
	}
}

func TestDifferenceResults_SingleSource(t *testing.T) {
	a := []ResolvedModel{makeResolved("m1", "p1")}

	result := differenceResults([][]ResolvedModel{a})

	if len(result) != 1 {
		t.Fatalf("difference returned %d models, want 1", len(result))
	}
}

func TestDifferenceResults_Empty(t *testing.T) {
	result := differenceResults([][]ResolvedModel{})
	if len(result) != 0 {
		t.Fatalf("difference returned %d models, want 0", len(result))
	}
}

func TestModelKey(t *testing.T) {
	rm := makeResolvedWithEffort("gpt-4", "openai", "high")
	key := modelKey(rm)
	expected := "openai/gpt-4/high"
	if key != expected {
		t.Errorf("modelKey() = %q, want %q", key, expected)
	}
}

func TestModelKey_EmptyEffort(t *testing.T) {
	rm := makeResolved("gpt-4", "openai")
	key := modelKey(rm)
	expected := "openai/gpt-4/"
	if key != expected {
		t.Errorf("modelKey() = %q, want %q", key, expected)
	}
}

func TestApplySetOperation(t *testing.T) {
	a := []ResolvedModel{makeResolved("m1", "p1"), makeResolved("m2", "p1")}
	b := []ResolvedModel{makeResolved("m2", "p1"), makeResolved("m3", "p2")}

	result := applySetOperation("union", [][]ResolvedModel{a, b})
	if len(result) != 3 {
		t.Errorf("union: got %d, want 3", len(result))
	}

	result = applySetOperation("intersection", [][]ResolvedModel{a, b})
	if len(result) != 1 {
		t.Errorf("intersection: got %d, want 1", len(result))
	}

	result = applySetOperation("difference", [][]ResolvedModel{a, b})
	if len(result) != 1 {
		t.Errorf("difference: got %d, want 1", len(result))
	}

	result = applySetOperation("unknown", [][]ResolvedModel{a, b})
	if result != nil {
		t.Errorf("unknown: got %v, want nil", result)
	}
}

func TestFilterResolvedModels(t *testing.T) {
	s := &virtualModelService{}

	resolved := []ResolvedModel{
		{
			Model:            models.Model{Name: "gpt-4"},
			Provider:         models.Provider{Name: "openai"},
			ReasoningEffort:  "",
			ProviderMetadata: map[string]string{"p.name": "openai"},
		},
		{
			Model:            models.Model{Name: "claude-3"},
			Provider:         models.Provider{Name: "anthropic"},
			ReasoningEffort:  "",
			ProviderMetadata: map[string]string{"p.name": "anthropic"},
		},
	}

	// Filter: p.name == "openai"
	filter := models.FilterNode{Key: "p.name", Op: "eq", Value: "openai"}
	result := s.filterResolvedModels(resolved, filter)

	if len(result) != 1 {
		t.Fatalf("filter returned %d models, want 1", len(result))
	}
	if result[0].Model.Name != "gpt-4" {
		t.Errorf("result[0] = %q, want gpt-4", result[0].Model.Name)
	}
}

func TestFilterResolvedModels_NoMatch(t *testing.T) {
	s := &virtualModelService{}

	resolved := []ResolvedModel{
		{
			Model:            models.Model{Name: "gpt-4"},
			ProviderMetadata: map[string]string{"p.name": "openai"},
		},
	}

	filter := models.FilterNode{Key: "p.name", Op: "eq", Value: "anthropic"}
	result := s.filterResolvedModels(resolved, filter)

	if len(result) != 0 {
		t.Fatalf("filter returned %d models, want 0", len(result))
	}
}
