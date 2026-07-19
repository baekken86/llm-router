package service

import (
	"context"
	"encoding/json"
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

// --- Filter source resolution tests ---

func setupFilterSourceService() (*virtualModelService, *mockVMRepo, *mockProviderRepo) {
	vmRepo := newMockVMRepo()
	modelRepo := newMockModelRepo()
	providerRepo := newMockProviderRepo()
	p1 := &models.Provider{ID: 1, Name: "openai"}
	p2 := &models.Provider{ID: 2, Name: "anthropic"}
	providerRepo.add(p1)
	providerRepo.add(p2)

	modelRepo.models = []models.Model{
		{ID: 1, ProviderID: 1, Name: "gpt-4"},
		{ID: 2, ProviderID: 1, Name: "gpt-4o"},
		{ID: 3, ProviderID: 2, Name: "claude-3"},
	}

	svc := &virtualModelService{
		vmRepo:           vmRepo,
		modelRepo:        modelRepo,
		tagRepo:          newMockTagRepo(),
		providerRepo:     providerRepo,
		providerMetaRepo: newMockProviderMetaRepo(),
		globalMetaRepo:   newMockGlobalMetaRepo(),
	}
	return svc, vmRepo, providerRepo
}

func TestResolveSource_BasicFilter(t *testing.T) {
	svc, _, _ := setupFilterSourceService()

	// Filter: p.name == "openai"
	node := &models.CompositionNode{
		FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "openai"},
	}

	result, err := svc.resolveSource(context.Background(), node, make(map[string]bool))
	if err != nil {
		t.Fatalf("resolveSource() error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d models, want 2", len(result))
	}
	for _, r := range result {
		if r.Provider.Name != "openai" {
			t.Errorf("provider = %q, want openai", r.Provider.Name)
		}
	}
}

func TestResolveSource_EmptyResult(t *testing.T) {
	svc, _, _ := setupFilterSourceService()

	// Filter matches nothing
	node := &models.CompositionNode{
		FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "nonexistent"},
	}

	result, err := svc.resolveSource(context.Background(), node, make(map[string]bool))
	if err != nil {
		t.Fatalf("resolveSource() error = %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("got %d models, want 0", len(result))
	}
}

func TestResolveSource_WithSort(t *testing.T) {
	svc, _, _ := setupFilterSourceService()

	// Filter: all models, sort by m.name desc
	node := &models.CompositionNode{
		FilterExpr: &models.FilterNode{Key: "m.name", Op: "neq", Value: "zzz"}, // match all
		SortExpr:   models.SortExpr{{Key: "m.name", Direction: "desc"}},
	}

	result, err := svc.resolveSource(context.Background(), node, make(map[string]bool))
	if err != nil {
		t.Fatalf("resolveSource() error = %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("got %d models, want 3", len(result))
	}
	// Descending by name: gpt-4o, gpt-4, claude-3
	if result[0].Model.Name != "gpt-4o" {
		t.Errorf("result[0] = %q, want gpt-4o", result[0].Model.Name)
	}
}

func TestResolveFilterSource_InUnion(t *testing.T) {
	svc, vmRepo, _ := setupFilterSourceService()

	// Create VM-A (leaf)
	vmA := &models.VirtualModel{Name: "vm-a", FilterExpr: []byte(`{"key":"p.name","op":"eq","value":"openai"}`)}
	vmRepo.Create(context.Background(), vmA)

	// Composition: union(VM-A, filter_source{anthropic})
	comp := &models.CompositionNode{
		Operation: "union",
		Sources: []models.CompositionNode{
			{Vm: "vm-a"},
			{FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "anthropic"}},
		},
	}

	vm := &models.VirtualModel{Name: "test", Composition: comp}
	result, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("got %d models, want 3 (2 openai + 1 anthropic)", len(result))
	}
}

func TestResolveFilterSource_InIntersection(t *testing.T) {
	svc, vmRepo, _ := setupFilterSourceService()

	// VM-A: all openai
	vmA := &models.VirtualModel{Name: "vm-a", FilterExpr: []byte(`{"key":"p.name","op":"eq","value":"openai"}`)}
	vmRepo.Create(context.Background(), vmA)

	// Intersection: VM-A ∩ filter{name starts with "gpt"}
	comp := &models.CompositionNode{
		Operation: "intersection",
		Sources: []models.CompositionNode{
			{Vm: "vm-a"},
			{FilterExpr: &models.FilterNode{Key: "m.name", Op: "contains", Value: "gpt"}},
		},
	}

	vm := &models.VirtualModel{Name: "test", Composition: comp}
	result, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	// openai models containing "gpt": gpt-4, gpt-4o
	if len(result) != 2 {
		t.Fatalf("got %d models, want 2", len(result))
	}
}

func TestResolveFilterSource_InDifference(t *testing.T) {
	svc, vmRepo, _ := setupFilterSourceService()

	// VM-A: all openai
	vmA := &models.VirtualModel{Name: "vm-a", FilterExpr: []byte(`{"key":"p.name","op":"eq","value":"openai"}`)}
	vmRepo.Create(context.Background(), vmA)

	// Difference: VM-A \ filter{name contains "gpt-4o"} → gpt-4 only
	comp := &models.CompositionNode{
		Operation: "difference",
		Sources: []models.CompositionNode{
			{Vm: "vm-a"},
			{FilterExpr: &models.FilterNode{Key: "m.name", Op: "eq", Value: "gpt-4o"}},
		},
	}

	vm := &models.VirtualModel{Name: "test", Composition: comp}
	result, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d models, want 1", len(result))
	}
	if result[0].Model.Name != "gpt-4" {
		t.Errorf("result[0] = %q, want gpt-4", result[0].Model.Name)
	}
}

func TestResolveFilterSource_NestedInOperation(t *testing.T) {
	svc, vmRepo, _ := setupFilterSourceService()

	// VM-A: openai
	vmA := &models.VirtualModel{Name: "vm-a", FilterExpr: []byte(`{"key":"p.name","op":"eq","value":"openai"}`)}
	vmRepo.Create(context.Background(), vmA)

	// union( intersection(VM-A, filter{gpt-4}), filter{anthropic} )
	comp := &models.CompositionNode{
		Operation: "union",
		Sources: []models.CompositionNode{
			{
				Operation: "intersection",
				Sources: []models.CompositionNode{
					{Vm: "vm-a"},
					{FilterExpr: &models.FilterNode{Key: "m.name", Op: "eq", Value: "gpt-4"}},
				},
			},
			{FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "anthropic"}},
		},
	}

	vm := &models.VirtualModel{Name: "test", Composition: comp}
	result, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	// gpt-4 (from intersection) + claude-3 (from filter)
	if len(result) != 2 {
		t.Fatalf("got %d models, want 2", len(result))
	}
}

func TestResolveSource_CircularRefNotPossible(t *testing.T) {
	svc, _, _ := setupFilterSourceService()

	// Filter source has no vm field — stack check irrelevant
	node := &models.CompositionNode{
		FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "openai"},
	}

	result, err := svc.resolveSource(context.Background(), node, make(map[string]bool))
	if err != nil {
		t.Fatalf("resolveSource() error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d models, want 2", len(result))
	}
}

// --- Service-layer validation tests (Step 4) ---

func TestCreate_InvalidFilterSourceFilterExpr(t *testing.T) {
	svc, _, _ := setupFilterSourceService()

	// Filter source with invalid filter_expr (missing operator)
	comp := &models.CompositionNode{
		Operation: "union",
		Sources: []models.CompositionNode{
			{Vm: "vm-a"},
			{FilterExpr: &models.FilterNode{Key: "mc.coding"}}, // missing Op
		},
	}
	_, err := svc.Create(context.Background(), models.CreateVirtualModelRequest{
		Name:        "test",
		Composition: comp,
	})
	if err == nil {
		t.Fatal("expected error for invalid filter_expr in filter source, got nil")
	}
}

func TestCreate_ValidFilterSourceFilterExpr(t *testing.T) {
	svc, _, _ := setupFilterSourceService()

	comp := &models.CompositionNode{
		Operation: "union",
		Sources: []models.CompositionNode{
			{Vm: "vm-a"},
			{FilterExpr: &models.FilterNode{Key: "mc.coding", Op: "gte", Value: float64(8)}},
		},
	}
	vm, err := svc.Create(context.Background(), models.CreateVirtualModelRequest{
		Name:        "test",
		Composition: comp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vm.Name != "test" {
		t.Errorf("Name = %q, want test", vm.Name)
	}
}

// --- Integration tests (Step 9) ---

func TestIntegration_CreateWithFilterSource(t *testing.T) {
	svc, vmRepo, _ := setupFilterSourceService()

	// Create VM-A (leaf)
	vmA := &models.VirtualModel{Name: "vm-a", FilterExpr: []byte(`{"key":"p.name","op":"eq","value":"openai"}`)}
	vmRepo.Create(context.Background(), vmA)

	// Create composite VM: union(VM-A, filter_source{anthropic})
	comp := &models.CompositionNode{
		Operation: "union",
		Sources: []models.CompositionNode{
			{Vm: "vm-a"},
			{FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "anthropic"}},
		},
	}
	_, err := svc.Create(context.Background(), models.CreateVirtualModelRequest{
		Name:        "composite-test",
		Composition: comp,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Get back
	got, err := svc.GetByName(context.Background(), "composite-test")
	if err != nil {
		t.Fatalf("GetByName() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetByName() returned nil")
	}
	if got.Composition == nil {
		t.Fatal("Composition is nil after create")
	}

	// Resolve
	result, err := svc.ResolveModels(context.Background(), got)
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	// openai (gpt-4, gpt-4o) + anthropic (claude-3) = 3
	if len(result) != 3 {
		t.Fatalf("got %d models, want 3", len(result))
	}
}

func TestIntegration_LeafToCompositionConvert(t *testing.T) {
	svc, _, _ := setupFilterSourceService()

	// Create leaf VM
	vm, err := svc.Create(context.Background(), models.CreateVirtualModelRequest{
		Name:       "leaf-vm",
		FilterExpr: json.RawMessage(`{"key":"p.name","op":"eq","value":"openai"}`),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Update to composition with filter source
	comp := &models.CompositionNode{
		Operation: "union",
		Sources: []models.CompositionNode{
			{Vm: "leaf-vm"},
			{FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "anthropic"}},
		},
	}
	updated, err := svc.Update(context.Background(), vm.ID, models.UpdateVirtualModelRequest{
		Composition: comp,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Composition == nil {
		t.Fatal("Composition is nil after update")
	}
}

func TestIntegration_MixedTreeResolve(t *testing.T) {
	svc, vmRepo, _ := setupFilterSourceService()

	// VM-A: openai
	vmA := &models.VirtualModel{Name: "vm-a", FilterExpr: []byte(`{"key":"p.name","op":"eq","value":"openai"}`)}
	vmRepo.Create(context.Background(), vmA)

	// Complex tree:
	// union(
	//   intersection(VM-A, filter{gpt-4}),
	//   filter{anthropic}
	// )
	comp := &models.CompositionNode{
		Operation: "union",
		Sources: []models.CompositionNode{
			{
				Operation: "intersection",
				Sources: []models.CompositionNode{
					{Vm: "vm-a"},
					{FilterExpr: &models.FilterNode{Key: "m.name", Op: "eq", Value: "gpt-4"}},
				},
			},
			{FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "anthropic"}},
		},
	}

	vm := &models.VirtualModel{Name: "mixed", Composition: comp}
	result, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	// gpt-4 (from intersection) + claude-3 (from filter) = 2
	if len(result) != 2 {
		t.Fatalf("got %d models, want 2", len(result))
	}
}

// --- Edge case tests (Step 10, spec section 8) ---

func TestEdgeCase_EmptyFilterInUnion(t *testing.T) {
	svc, vmRepo, _ := setupFilterSourceService()

	vmA := &models.VirtualModel{Name: "vm-a", FilterExpr: []byte(`{"key":"p.name","op":"eq","value":"openai"}`)}
	vmRepo.Create(context.Background(), vmA)

	// union(VM-A, empty-filter) → VM-A's models
	comp := &models.CompositionNode{
		Operation: "union",
		Sources: []models.CompositionNode{
			{Vm: "vm-a"},
			{FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "nonexistent"}},
		},
	}

	vm := &models.VirtualModel{Name: "test", Composition: comp}
	result, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d models, want 2 (openai only)", len(result))
	}
}

func TestEdgeCase_EmptyFilterInIntersection(t *testing.T) {
	svc, vmRepo, _ := setupFilterSourceService()

	vmA := &models.VirtualModel{Name: "vm-a", FilterExpr: []byte(`{"key":"p.name","op":"eq","value":"openai"}`)}
	vmRepo.Create(context.Background(), vmA)

	// intersection(VM-A, empty-filter) → empty
	comp := &models.CompositionNode{
		Operation: "intersection",
		Sources: []models.CompositionNode{
			{Vm: "vm-a"},
			{FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "nonexistent"}},
		},
	}

	vm := &models.VirtualModel{Name: "test", Composition: comp}
	result, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("got %d models, want 0", len(result))
	}
}

func TestEdgeCase_EmptyFilterInDifference(t *testing.T) {
	svc, vmRepo, _ := setupFilterSourceService()

	vmA := &models.VirtualModel{Name: "vm-a", FilterExpr: []byte(`{"key":"p.name","op":"eq","value":"openai"}`)}
	vmRepo.Create(context.Background(), vmA)

	// difference(VM-A, empty-filter) → VM-A's models
	comp := &models.CompositionNode{
		Operation: "difference",
		Sources: []models.CompositionNode{
			{Vm: "vm-a"},
			{FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "nonexistent"}},
		},
	}

	vm := &models.VirtualModel{Name: "test", Composition: comp}
	result, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d models, want 2", len(result))
	}
}

func TestEdgeCase_CompositeOfCompositeWithInlineFilter(t *testing.T) {
	svc, vmRepo, _ := setupFilterSourceService()

	vmA := &models.VirtualModel{Name: "vm-a", FilterExpr: []byte(`{"key":"p.name","op":"eq","value":"openai"}`)}
	vmRepo.Create(context.Background(), vmA)

	// union( intersection(VM-A, filter{gpt-4}), filter{anthropic} )
	comp := &models.CompositionNode{
		Operation: "union",
		Sources: []models.CompositionNode{
			{
				Operation: "intersection",
				Sources: []models.CompositionNode{
					{Vm: "vm-a"},
					{FilterExpr: &models.FilterNode{Key: "m.name", Op: "eq", Value: "gpt-4"}},
				},
			},
			{FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "anthropic"}},
		},
	}

	vm := &models.VirtualModel{Name: "test", Composition: comp}
	result, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	// gpt-4 + claude-3 = 2
	if len(result) != 2 {
		t.Fatalf("got %d models, want 2", len(result))
	}
}

func TestEdgeCase_ParentSortOverridesInlineSort(t *testing.T) {
	svc, vmRepo, _ := setupFilterSourceService()

	vmA := &models.VirtualModel{Name: "vm-a", FilterExpr: []byte(`{"key":"p.name","op":"eq","value":"openai"}`)}
	vmRepo.Create(context.Background(), vmA)

	// union with parent sort: union(VM-A, filter{name desc}) + parent sort by m.name asc
	comp := &models.CompositionNode{
		Operation: "union",
		Sources: []models.CompositionNode{
			{Vm: "vm-a"},
			{FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "anthropic"}},
		},
		SortExpr: models.SortExpr{{Key: "m.name", Direction: "asc"}},
	}

	vm := &models.VirtualModel{Name: "test", Composition: comp}
	result, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("got %d models, want 3", len(result))
	}
	// Ascending by name: claude-3, gpt-4, gpt-4o
	if result[0].Model.Name != "claude-3" {
		t.Errorf("result[0] = %q, want claude-3", result[0].Model.Name)
	}
	if result[1].Model.Name != "gpt-4" {
		t.Errorf("result[1] = %q, want gpt-4", result[1].Model.Name)
	}
	if result[2].Model.Name != "gpt-4o" {
		t.Errorf("result[2] = %q, want gpt-4o", result[2].Model.Name)
	}
}

func TestEdgeCase_FilterSourceAtDepthLimit(t *testing.T) {
	// Build a tree of depth 10 with filter sources at leaves
	leaf := &models.CompositionNode{FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "openai"}}
	node := leaf
	for i := 0; i < 10; i++ {
		node = &models.CompositionNode{
			Operation: "union",
			Sources:   []models.CompositionNode{*node, {FilterExpr: &models.FilterNode{Key: "p.name", Op: "eq", Value: "openai"}}},
		}
	}

	// Depth 10 should be valid
	err := models.ValidateCompositionNode(node, 0)
	if err != nil {
		t.Fatalf("ValidateCompositionNode() error = %v, want nil", err)
	}
}

func TestEdgeCase_ParentFilterOnInlineSource(t *testing.T) {
	svc, vmRepo, _ := setupFilterSourceService()

	vmA := &models.VirtualModel{Name: "vm-a", FilterExpr: []byte(`{"key":"p.name","op":"eq","value":"openai"}`)}
	vmRepo.Create(context.Background(), vmA)

	// union(VM-A, filter{name contains "gpt"}) + parent filter{name contains "4"}
	// Result: gpt-4, gpt-4o (from union) then parent filters to only those containing "4"
	comp := &models.CompositionNode{
		Operation: "union",
		Sources: []models.CompositionNode{
			{Vm: "vm-a"},
			{FilterExpr: &models.FilterNode{Key: "m.name", Op: "contains", Value: "gpt"}},
		},
		FilterExpr: &models.FilterNode{Key: "m.name", Op: "contains", Value: "4"},
	}

	vm := &models.VirtualModel{Name: "test", Composition: comp}
	result, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	// gpt-4 and gpt-4o (both openai, both contain "4")
	if len(result) != 2 {
		t.Fatalf("got %d models, want 2", len(result))
	}
}
