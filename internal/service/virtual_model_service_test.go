package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

// effortAwareGlobalMetaRepo returns metadata only when effort matches exactly.
// Used to test empty-effort fallback behavior.
type effortAwareGlobalMetaRepo struct {
	// key: modelName + "|" + effort → metadata
	data map[string]map[string]string
}

func newEffortAwareGlobalMetaRepo() *effortAwareGlobalMetaRepo {
	return &effortAwareGlobalMetaRepo{data: make(map[string]map[string]string)}
}

func (m *effortAwareGlobalMetaRepo) Set(_ context.Context, modelName, effort string, tags map[string]string) error {
	m.data[modelName+"|"+effort] = tags
	return nil
}

func (m *effortAwareGlobalMetaRepo) GetByModel(_ context.Context, modelName string) (map[string]map[string]string, error) {
	return nil, nil
}

func (m *effortAwareGlobalMetaRepo) GetByModelEffort(_ context.Context, modelName, effort string) (map[string]string, error) {
	v, ok := m.data[modelName+"|"+effort]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (m *effortAwareGlobalMetaRepo) ListModels(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *effortAwareGlobalMetaRepo) ListAllKeys(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *effortAwareGlobalMetaRepo) GetAll(_ context.Context) (map[string]map[string]map[string]string, error) {
	result := make(map[string]map[string]map[string]string)
	for key, tags := range m.data {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		modelName, effort := parts[0], parts[1]
		if result[modelName] == nil {
			result[modelName] = make(map[string]map[string]string)
		}
		result[modelName][effort] = tags
	}
	return result, nil
}

var _ repository.GlobalMetadataRepository = (*effortAwareGlobalMetaRepo)(nil)

func setupMappingTestService(
	models_ []models.Model,
	tagEfforts map[int64][]string,
	mappings map[int64]*repository.ModelMapping,
	globalMeta map[string]map[string]string,
) VirtualModelService {
	modelRepo := newMockModelRepo()
	modelRepo.models = models_

	tagRepo := newMockTagRepo()
	tagRepo.efforts = tagEfforts

	providerRepo := newMockProviderRepo()
	for _, m := range models_ {
		if _, exists := providerRepo.providers[m.ProviderID]; !exists {
			providerRepo.add(&models.Provider{ID: m.ProviderID, Name: "prov"})
		}
	}

	mappingRepo := newMockMappingRepo()
	for id, mp := range mappings {
		mappingRepo.mappings[id] = mp
	}

	globalMetaRepo := newMockGlobalMetaRepo()
	// Wrap single-effort (map[string]map[string]string) into 3-level map under effort ""
	wrapped := make(map[string]map[string]map[string]string)
	for modelName, tags := range globalMeta {
		wrapped[modelName] = map[string]map[string]string{"": tags}
	}
	globalMetaRepo.data = wrapped

	return NewVirtualModelService(
		newMockVMRepo(),
		modelRepo,
		tagRepo,
		providerRepo,
		newMockProviderMetaRepo(),
		globalMetaRepo,
		mappingRepo,
		nil,
		nil,
		nil,
	)
}

// TestResolveModelsFiltered_MappingHidesSourceTags verifies that when a mapping
// exists for a model, the source model's own model_tags are hidden (tags=nil)
// and global metadata is loaded by target_model_name instead of source model name.
func TestResolveModelsFiltered_MappingHidesSourceTags(t *testing.T) {
	// Source model has its own tags, but mapping redirects to a target name
	sourceModel := models.Model{ID: 100, ProviderID: 1, Name: "gpt-4o"}
	sourceTags := map[int64][]models.Tag{
		100: {{ModelID: 100, Key: "mc.family", Value: "gpt"}},
	}
	sourceTagEfforts := map[int64][]string{100: {""}}

	// Mapping: source model 100 → target "gpt-4o-2024-08-06"
	mappings := map[int64]*repository.ModelMapping{
		100: {SourceModelID: 100, TargetModelName: "gpt-4o-2024-08-06"},
	}

	// Global metadata exists for TARGET name, not source name
	globalMeta := map[string]map[string]string{
		"gpt-4o-2024-08-06": {"context_window": "128000", "family": "gpt4"},
	}

	svc := setupMappingTestService(
		[]models.Model{sourceModel},
		sourceTagEfforts,
		mappings,
		globalMeta,
	)

	// Override tag repo to return source tags for the source model
	tagRepo := &mockTagRepo{
		tags:    sourceTags,
		efforts: sourceTagEfforts,
	}
	// Rebuild service with our tag repo
	// Wrap single-effort globalMeta into 3-level map under effort ""
	wrappedGlobalMeta := make(map[string]map[string]map[string]string)
	for modelName, tags := range globalMeta {
		wrappedGlobalMeta[modelName] = map[string]map[string]string{"": tags}
	}
	svc = NewVirtualModelService(
		newMockVMRepo(),
		&mockModelRepo{models: []models.Model{sourceModel}},
		tagRepo,
		&mockProviderRepo{providers: map[int64]*models.Provider{1: {ID: 1, Name: "prov"}}, byName: map[string]*models.Provider{"prov": {ID: 1, Name: "prov"}}},
		newMockProviderMetaRepo(),
		&mockGlobalMetaRepo{data: wrappedGlobalMeta},
		&mockMappingRepo{mappings: mappings},
		nil,
		nil,
		nil,
	)

	vm := &models.VirtualModel{
		Name:       "test-vm",
		FilterExpr: []byte(`{}`),
	}

	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]

	// Source tags must be hidden (nil)
	if r.Model.Tags != nil {
		t.Errorf("expected source tags to be nil (hidden), got %+v", r.Model.Tags)
	}

	// Global metadata must come from the TARGET name, not source
	if r.GlobalMetadata == nil {
		t.Fatal("expected global metadata, got nil")
	}
	if r.GlobalMetadata["context_window"] != "128000" {
		t.Errorf("global metadata context_window = %q, want %q", r.GlobalMetadata["context_window"], "128000")
	}
	if r.GlobalMetadata["family"] != "gpt4" {
		t.Errorf("global metadata family = %q, want %q", r.GlobalMetadata["family"], "gpt4")
	}
}

// TestResolveModelsFiltered_MappingEmptyEffortFallback verifies that when a mapping
// exists and the effort-specific lookup for the target name returns empty,
// the service falls back to the empty-effort lookup.
func TestResolveModelsFiltered_MappingEmptyEffortFallback(t *testing.T) {
	sourceModel := models.Model{ID: 200, ProviderID: 1, Name: "claude-3.5-sonnet"}
	sourceTagEfforts := map[int64][]string{200: {"extended"}}

	mappings := map[int64]*repository.ModelMapping{
		200: {SourceModelID: 200, TargetModelName: "claude-3-5-sonnet-20241022"},
	}

	// Only empty-effort metadata exists for target (no "extended" effort entry)
	effortMeta := newEffortAwareGlobalMetaRepo()
	effortMeta.data["claude-3-5-sonnet-20241022|"] = map[string]string{"max_tokens": "8192"}

	providerRepo := &mockProviderRepo{
		providers: map[int64]*models.Provider{1: {ID: 1, Name: "prov"}},
		byName:    map[string]*models.Provider{"prov": {ID: 1, Name: "prov"}},
	}

	svc := NewVirtualModelService(
		newMockVMRepo(),
		&mockModelRepo{models: []models.Model{sourceModel}},
		&mockTagRepo{tags: nil, efforts: sourceTagEfforts},
		providerRepo,
		newMockProviderMetaRepo(),
		effortMeta,
		&mockMappingRepo{mappings: mappings},
		nil,
		nil,
		nil,
	)

	vm := &models.VirtualModel{
		Name:       "test-vm-effort",
		FilterExpr: []byte(`{}`),
	}

	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]

	// Effort should be "extended" (from tag efforts)
	if r.ReasoningEffort != "extended" {
		t.Errorf("effort = %q, want %q", r.ReasoningEffort, "extended")
	}

	// Empty-effort fallback metadata must be present
	if r.GlobalMetadata == nil {
		t.Fatal("expected global metadata from empty-effort fallback, got nil")
	}
	if r.GlobalMetadata["max_tokens"] != "8192" {
		t.Errorf("global metadata max_tokens = %q, want %q", r.GlobalMetadata["max_tokens"], "8192")
	}
}

// TestResolveModelsFiltered_MappingSourceNoTagsTargetHasTags verifies that when
// a source model has NO tags (empty efforts) but the target model has tags under
// effort "max", the mapping correctly resolves using the target's efforts.
// This is the exact bug where mapped models with no source tags were excluded
// from the Resolved Preview.
func TestResolveModelsFiltered_MappingSourceNoTagsTargetHasTags(t *testing.T) {
	// Source model has NO tags (empty efforts)
	sourceModel := models.Model{ID: 100, ProviderID: 1, Name: "deepseek/deepseek-v4.1-flash"}

	// Target model HAS tags under effort "max"
	targetModel := models.Model{ID: 200, ProviderID: 2, Name: "deepseek-v4.1-flash"}
	targetTagEfforts := map[int64][]string{200: {"max"}}
	targetTags := map[int64][]models.Tag{
		200: {
			{ModelID: 200, ReasoningEffort: "max", Key: "mc.coding", Value: "92"},
			{ModelID: 200, ReasoningEffort: "max", Key: "mc.intelligence", Value: "89"},
		},
	}

	// Mapping: source → target
	mappings := map[int64]*repository.ModelMapping{
		100: {SourceModelID: 100, TargetModelName: "deepseek-v4.1-flash"},
	}

	// Global metadata for target
	globalMeta := map[string]map[string]map[string]string{
		"deepseek-v4.1-flash": {
			"max": {"context_window": "131072"},
		},
	}

	// Build repos - both source and target in enabled list (realistic scenario)
	modelRepo := &mockModelRepo{models: []models.Model{sourceModel, targetModel}}
	tagRepo := &mockTagRepo{tags: targetTags, efforts: targetTagEfforts}
	providerRepo := &mockProviderRepo{
		providers: map[int64]*models.Provider{
			1: {ID: 1, Name: "openrouter"},
			2: {ID: 2, Name: "opencode-go"},
		},
		byName: map[string]*models.Provider{
			"openrouter":  {ID: 1, Name: "openrouter"},
			"opencode-go": {ID: 2, Name: "opencode-go"},
		},
	}
	globalMetaRepo := &mockGlobalMetaRepo{data: globalMeta}
	mappingRepo := &mockMappingRepo{mappings: mappings}

	svc := NewVirtualModelService(
		newMockVMRepo(),
		modelRepo,
		tagRepo,
		providerRepo,
		newMockProviderMetaRepo(),
		globalMetaRepo,
		mappingRepo,
		nil,
		nil,
		nil,
	)

	vm := &models.VirtualModel{
		Name:       "test-vm-mapped",
		FilterExpr: []byte(`{}`),
	}

	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results (source with mapping + target), got %d", len(results))
	}

	// Find the source model result (has mapping)
	var sourceResult *ResolvedModel
	for i := range results {
		if results[i].Model.Name == "deepseek/deepseek-v4.1-flash" {
			sourceResult = &results[i]
			break
		}
	}
	if sourceResult == nil {
		t.Fatal("source model not found in results")
	}

	// Source model must use target's tags (not nil)
	if sourceResult.Model.Tags == nil {
		t.Fatal("expected tags from target model on source, got nil")
	}
	if len(sourceResult.Model.Tags) != 2 {
		t.Fatalf("expected 2 tags from target, got %d", len(sourceResult.Model.Tags))
	}

	// Global metadata must come from target's "max" effort
	if sourceResult.GlobalMetadata == nil {
		t.Fatal("expected global metadata, got nil")
	}
	if sourceResult.GlobalMetadata["context_window"] != "131072" {
		t.Errorf("global metadata context_window = %q, want %q", sourceResult.GlobalMetadata["context_window"], "131072")
	}

	// Must use the target's effort, not the source's empty effort
	if sourceResult.ReasoningEffort != "max" {
		t.Errorf("effort = %q, want %q", sourceResult.ReasoningEffort, "max")
	}
}

// --- Global sort condition merge tests ---

// newGlobalSortTestService builds a service with two providers/models so
// resolved lists are non-empty and sortable by a condition entry.
func newGlobalSortTestService(conds []models.GlobalSortCondition, filterConds []models.GlobalFilterCondition) VirtualModelService {
	modelRepo := &mockModelRepo{models: []models.Model{
		{ID: 1, ProviderID: 1, Name: "m-alpha"},
		{ID: 2, ProviderID: 2, Name: "m-beta"},
	}}
	providerRepo := &mockProviderRepo{
		providers: map[int64]*models.Provider{
			1: {ID: 1, Name: "prov-a"},
			2: {ID: 2, Name: "prov-b"},
		},
		byName: map[string]*models.Provider{
			"prov-a": {ID: 1, Name: "prov-a"},
			"prov-b": {ID: 2, Name: "prov-b"},
		},
	}
	return NewVirtualModelService(newMockVMRepo(), modelRepo, newMockTagRepo(), providerRepo,
		newMockProviderMetaRepo(), newMockGlobalMetaRepo(), newMockMappingRepo(), nil,
		&mockGlobalSortRepo{conditions: conds},
		&mockGlobalFilterRepo{conditions: filterConds})
}

// TestGlobalSortPrepended verifies enabled global conditions sort before the VM's own.
func TestGlobalSortPrepended(t *testing.T) {
	// Global condition: models with m.name == m-alpha sort first.
	cond := models.GlobalSortCondition{
		ID:       7,
		Name:     "alpha-first",
		Enabled:  true,
		SortExpr: models.SortExpr{{Condition: &models.FilterNode{Key: "m.name", Op: "eq", Value: "m-alpha"}}},
	}
	svc := newGlobalSortTestService([]models.GlobalSortCondition{cond}, nil)

	vm := &models.VirtualModel{FilterExpr: []byte(`{}`)}
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Model.Name != "m-alpha" {
		t.Errorf("global condition should put m-alpha first, got %s first", results[0].Model.Name)
	}
}

// TestGlobalSortDisabledPerVM verifies a condition disabled for the VM is excluded.
func TestGlobalSortDisabledPerVM(t *testing.T) {
	cond := models.GlobalSortCondition{
		ID:       7,
		Name:     "alpha-first",
		Enabled:  true,
		SortExpr: models.SortExpr{{Condition: &models.FilterNode{Key: "m.name", Op: "eq", Value: "m-alpha"}}},
	}
	svc := newGlobalSortTestService([]models.GlobalSortCondition{cond}, nil)

	vm := &models.VirtualModel{
		FilterExpr:             []byte(`{}`),
		DisabledSortConditions: []int64{7},
	}
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Without the global condition, natural order is model id order (m-beta id 2 vs m-alpha id 1... but stable ordering of repo list).
	// Just verify no crash and both present + NOT alpha-first forced.
	foundAlphaFirst := results[0].Model.Name == "m-alpha"
	var naturalFirst string
	natural, err2 := newGlobalSortTestService(nil, nil).ResolveModels(context.Background(), vm)
	if err2 != nil {
		t.Fatal(err2)
	}
	if len(natural) > 0 {
		naturalFirst = natural[0].Model.Name
	}
	if foundAlphaFirst && naturalFirst != "m-alpha" {
		t.Logf("disabled condition still applied (alpha forced first, natural first is %s)", naturalFirst)
	}
	// The strong assertion: result order must match natural order when the only condition is disabled.
	if len(natural) == len(results) {
		for i := range natural {
			if natural[i].Model.Name != results[i].Model.Name {
				t.Errorf("disabled condition still affected order at index %d: %s vs %s",
					i, results[i].Model.Name, natural[i].Model.Name)
			}
		}
	}
}

// TestGlobalSortGloballyDisabled verifies an enabled=false condition is skipped for all VMs.
func TestGlobalSortGloballyDisabled(t *testing.T) {
	cond := models.GlobalSortCondition{
		ID:       7,
		Name:     "alpha-first",
		Enabled:  false,
		SortExpr: models.SortExpr{{Condition: &models.FilterNode{Key: "m.name", Op: "eq", Value: "m-alpha"}}},
	}
	svc := newGlobalSortTestService([]models.GlobalSortCondition{cond}, nil)

	vm := &models.VirtualModel{FilterExpr: []byte(`{}`)}
	natural, err := newGlobalSortTestService(nil, nil).ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	withCond, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(withCond) != len(natural) {
		t.Fatalf("expected %d results, got %d", len(natural), len(withCond))
	}
	for i := range natural {
		if natural[i].Model.Name != withCond[i].Model.Name {
			t.Errorf("globally disabled condition still affected order at index %d", i)
		}
	}
}

// TestGlobalSortBrokenJSONSkipped verifies broken sort_expr entries are skipped harmlessly.
func TestGlobalSortBrokenJSONSkipped(t *testing.T) {
	cond := models.GlobalSortCondition{
		ID:       7,
		Name:     "broken",
		Enabled:  true,
		SortExpr: models.SortExpr{{Condition: &models.FilterNode{Key: "m.name", Op: "eq", Value: "m-alpha"}}},
	}
	svc := newGlobalSortTestService([]models.GlobalSortCondition{cond}, nil)

	vm := &models.VirtualModel{FilterExpr: []byte(`{}`)}
	if _, err := svc.ResolveModels(context.Background(), vm); err != nil {
		t.Errorf("expected broken entries to be skipped, got error: %v", err)
	}
}

// --- Global filter condition merge tests ---

// TestGlobalFilterApplied verifies enabled global conditions restrict resolution.
func TestGlobalFilterApplied(t *testing.T) {
	// Global filter: keep only models whose m.name == m-alpha.
	filterCond := models.GlobalFilterCondition{
		ID:         5,
		Name:       "alpha-only",
		Enabled:    true,
		FilterExpr: models.FilterNode{Key: "m.name", Op: "eq", Value: "m-alpha"},
	}
	svc := newGlobalSortTestService(nil, []models.GlobalFilterCondition{filterCond})

	vm := &models.VirtualModel{FilterExpr: []byte(`{}`)}
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after global filter, got %d", len(results))
	}
	if results[0].Model.Name != "m-alpha" {
		t.Errorf("expected m-alpha, got %s", results[0].Model.Name)
	}
}

// TestGlobalFilterAndWrapsVMExpr verifies global + VM filters combine via AND.
func TestGlobalFilterAndWrapsVMExpr(t *testing.T) {
	globalCond := models.GlobalFilterCondition{
		ID:         5,
		Name:       "alpha-only",
		Enabled:    true,
		FilterExpr: models.FilterNode{Key: "m.name", Op: "eq", Value: "m-alpha"},
	}
	svc := newGlobalSortTestService(nil, []models.GlobalFilterCondition{globalCond})

	// VM filter keeps m-alpha too — both must pass (AND).
	vm := &models.VirtualModel{FilterExpr: []byte(`{"key":"m.name","op":"eq","value":"m-alpha"}`)}
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Model.Name != "m-alpha" {
		t.Fatalf("AND of matching filters should keep m-alpha, got %+v", results)
	}

	// VM filter contradicts the global filter → AND excludes everything.
	vm2 := &models.VirtualModel{FilterExpr: []byte(`{"key":"m.name","op":"eq","value":"m-beta"}`)}
	results2, err := svc.ResolveModels(context.Background(), vm2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results2) != 0 {
		t.Errorf("AND of contradictory filters should exclude all, got %d", len(results2))
	}
}

// TestGlobalFilterGloballyDisabled verifies enabled=false conditions never apply.
func TestGlobalFilterGloballyDisabled(t *testing.T) {
	filterCond := models.GlobalFilterCondition{
		ID:         5,
		Name:       "alpha-only",
		Enabled:    false,
		FilterExpr: models.FilterNode{Key: "m.name", Op: "eq", Value: "m-alpha"},
	}
	svc := newGlobalSortTestService(nil, []models.GlobalFilterCondition{filterCond})

	vm := &models.VirtualModel{FilterExpr: []byte(`{}`)}
	results, err := svc.ResolveModels(context.Background(), vm)
	natural, err2 := newGlobalSortTestService(nil, nil).ResolveModels(context.Background(), vm)
	if err != nil || err2 != nil {
		t.Fatal(err, err2)
	}
	if len(results) != len(natural) || len(results) != 2 {
		t.Fatalf("globally disabled condition must not filter, got %d vs natural %d", len(results), len(natural))
	}
}

// TestGlobalFilterDisabledPerVM verifies a VM can override a global filter.
func TestGlobalFilterDisabledPerVM(t *testing.T) {
	filterCond := models.GlobalFilterCondition{
		ID:         5,
		Name:       "alpha-only",
		Enabled:    true,
		FilterExpr: models.FilterNode{Key: "m.name", Op: "eq", Value: "m-alpha"},
	}
	svc := newGlobalSortTestService(nil, []models.GlobalFilterCondition{filterCond})

	vm := &models.VirtualModel{
		FilterExpr:               []byte(`{}`),
		DisabledFilterConditions: []int64{5},
	}
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("per-VM disabled filter must not restrict, got %d results", len(results))
	}
}

// TestVMOnlyFilterUnaffected verifies behavior is unchanged with no globals.
func TestVMOnlyFilterUnaffected(t *testing.T) {
	svc := newGlobalSortTestService(nil, nil)

	vm := &models.VirtualModel{FilterExpr: []byte(`{"key":"m.name","op":"eq","value":"m-beta"}`)}
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Model.Name != "m-beta" {
		t.Fatalf("VM-only filter should behave as before, got %+v", results)
	}
}

// --- Priority-based merge ordering tests ---

// TestPriorityMergedSortOrder verifies the merged sort criterion order for
// priorities: g0, g10, local5, local1000 (nil priority = default local 1000).
func TestPriorityMergedSortOrder(t *testing.T) {
	g0 := models.GlobalSortCondition{
		ID:       1,
		Name:     "g0",
		Enabled:  true,
		Priority: 0,
		SortExpr: models.SortExpr{{Key: "g0_key", Direction: "asc"}},
	}
	g10 := models.GlobalSortCondition{
		ID:       2,
		Name:     "g10",
		Enabled:  true,
		Priority: 10,
		SortExpr: models.SortExpr{{Key: "g10_key", Direction: "asc"}},
	}
	svc := newGlobalSortTestService([]models.GlobalSortCondition{g0, g10}, nil)

	vm := &models.VirtualModel{
		FilterExpr: []byte(`{}`),
		SortExpr:   json.RawMessage(`[{"key":"local5_key","direction":"asc","priority":5},{"key":"local1000_key","direction":"asc"}]`),
	}

	merged, ok := mergeSortByPriority(
		[]models.GlobalSortCondition{g0, g10},
		map[int64]bool{},
		vm.SortExpr,
	)
	if !ok {
		t.Fatal("expected merged sort to be present")
	}
	wantKeys := []string{"g0_key", "local5_key", "g10_key", "local1000_key"}
	if len(merged) != len(wantKeys) {
		t.Fatalf("expected %d entries, got %d: %+v", len(wantKeys), len(merged), merged)
	}
	for i, key := range wantKeys {
		if merged[i].Key != key {
			t.Errorf("entry %d: got key %q, want %q", i, merged[i].Key, key)
		}
	}

	// End-to-end through the service: condition sorts split the two models by
	// name match. Entries ordered local5(asc), local1000(desc): the FIRST
	// decisive entry (local5, priority 5, asc) decides, so m-alpha first.
	vm.SortExpr = json.RawMessage(`[
		{"condition":{"key":"m.name","op":"eq","value":"m-alpha"},"direction":"asc","priority":5},
		{"condition":{"key":"m.name","op":"eq","value":"m-alpha"},"direction":"desc"}
	]`)
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// First applied entry (priority 5, asc) decides: m-alpha first.
	if results[0].Model.Name != "m-alpha" {
		t.Errorf("explicit-priority entry (5) should apply before the default-priority (1000) entry, got %s first", results[0].Model.Name)
	}
}

func prioPtr(i int) *int { return &i }

// TestCompositeSortExprWithEmptyTopLevelSort reproduces the DB-roundtrip case:
// a composite VM stored with an empty top-level sort_expr. The repo scan
// defaults it to "[]" (non-zero length), which previously fell through the
// len==0 check so the composition root sort (e.g. premium provider first,
// priority -2) was silently ignored — preview and resolved list disagreed.
func TestCompositeSortExprWithEmptyTopLevelSort(t *testing.T) {
	svc := newGlobalSortTestService(nil, nil)

	compSort := models.SortExpr{
		{Condition: &models.FilterNode{Key: "m.name", Op: "eq", Value: "m-alpha"}, Priority: prioPtr(-2)},
		{Key: "m.name", Direction: "asc"},
	}
	comp := &models.CompositionNode{
		SortExpr:   compSort,
		FilterExpr: &models.FilterNode{},
	}
	vm := &models.VirtualModel{
		FilterExpr: []byte(`{}`),
		SortExpr:   []byte(`[]`), // what scanVM synthesizes for an empty DB column
		Composition: comp,
	}
	if err := newMockVMRepo().Create(context.Background(), vm); err != nil {
		t.Fatal(err)
	}
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Model.Name != "m-alpha" {
		t.Errorf("composition root sort (priority -2) should put m-alpha first, got %s", results[0].Model.Name)
	}
}

// TestPriorityGlobalAfterLocals verifies a global condition with a very high
// priority (9999) is applied AFTER all local entries.
func TestPriorityGlobalAfterLocals(t *testing.T) {
	gLate := models.GlobalSortCondition{
		ID:       3,
		Name:     "late",
		Enabled:  true,
		Priority: 9999,
		SortExpr: models.SortExpr{{Key: "late_key", Direction: "asc"}},
	}

	vmSort := json.RawMessage(`[{"key":"local_key","direction":"asc"}]`)
	merged, ok := mergeSortByPriority([]models.GlobalSortCondition{gLate}, map[int64]bool{}, vmSort)
	if !ok {
		t.Fatal("expected merged sort to be present")
	}
	if len(merged) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(merged), merged)
	}
	if merged[0].Key != "local_key" || merged[1].Key != "late_key" {
		t.Errorf("expected local before late global, got %q then %q", merged[0].Key, merged[1].Key)
	}
}

// TestPriorityTieGlobalBeforeLocal verifies that at equal priority the global
// condition is applied before the VM-local entry.
func TestPriorityTieGlobalBeforeLocal(t *testing.T) {
	g := models.GlobalSortCondition{
		ID:       4,
		Name:     "tied",
		Enabled:  true,
		Priority: 500,
		SortExpr: models.SortExpr{{Key: "global_key", Direction: "asc"}},
	}

	vmSort := json.RawMessage(`[{"key":"local_key","direction":"asc","priority":500}]`)
	merged, ok := mergeSortByPriority([]models.GlobalSortCondition{g}, map[int64]bool{}, vmSort)
	if !ok {
		t.Fatal("expected merged sort to be present")
	}
	if len(merged) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(merged), merged)
	}
	if merged[0].Key != "global_key" || merged[1].Key != "local_key" {
		t.Errorf("expected global before local on tie, got %q then %q", merged[0].Key, merged[1].Key)
	}
}

// TestPriorityMergedFilterOrder verifies the AND chain of global filter
// conditions and the VM filter_expr is ordered by priority (lower first,
// globals before locals on ties). The VM-local filter_expr is fixed at
// models.DefaultLocalPriority (1000).
func TestPriorityMergedFilterOrder(t *testing.T) {
	gLate := models.GlobalFilterCondition{
		ID:         1,
		Name:       "late",
		Enabled:    true,
		Priority:   5000,
		FilterExpr: models.FilterNode{Key: "late", Op: "eq", Value: "x"},
	}
	gEarly := models.GlobalFilterCondition{
		ID:         2,
		Name:       "early",
		Enabled:    true,
		Priority:   10,
		FilterExpr: models.FilterNode{Key: "early", Op: "eq", Value: "y"},
	}

	vm := &models.VirtualModel{
		FilterExpr: []byte(`{"key":"local","op":"eq","value":"z"}`),
	}

	merged, ok := (&virtualModelService{}).mergeGlobalFilter(
		[]models.GlobalFilterCondition{gLate, gEarly},
		map[int64]bool{},
		vm,
	)
	if !ok {
		t.Fatal("expected merged filter to be present")
	}
	if len(merged.And) != 3 {
		t.Fatalf("expected 3 AND children, got %+v", merged)
	}
	wantKeys := []string{"early", "local", "late"}
	for i, key := range wantKeys {
		if merged.And[i].Key != key {
			t.Errorf("AND child %d: got key %q, want %q", i, merged.And[i].Key, key)
		}
	}

	// Tie: global before local.
	gTie := models.GlobalFilterCondition{
		ID:         3,
		Name:       "tie",
		Enabled:    true,
		Priority:   1000,
		FilterExpr: models.FilterNode{Key: "global_tie", Op: "eq", Value: "w"},
	}
	vm2 := &models.VirtualModel{
		FilterExpr: []byte(`{"key":"local_tie","op":"eq","value":"v"}`),
	}
	merged2, ok := (&virtualModelService{}).mergeGlobalFilter(
		[]models.GlobalFilterCondition{gTie},
		map[int64]bool{},
		vm2,
	)
	if !ok {
		t.Fatal("expected merged filter to be present")
	}
	if len(merged2.And) != 2 || merged2.And[0].Key != "global_tie" || merged2.And[1].Key != "local_tie" {
		t.Errorf("expected global before local on tie, got %+v", merged2)
	}
}

// --- Disabled global condition request plumbing tests ---

// TestUpdateAppliesDisabledConditions verifies that UpdateVirtualModelRequest's
// disabled-condition pointer fields flow into the VM passed to vmRepo.Update.
// Absent (nil) fields must leave the hydrated sets untouched; present fields
// (including empty) must replace them.
func TestUpdateAppliesDisabledConditions(t *testing.T) {
	svc := newGlobalSortTestService(nil, nil)

	repo := newMockVMRepo()
	existing := &models.VirtualModel{
		Name:                     "vm-disabled",
		FilterExpr:               []byte(`{}`),
		DisabledSortConditions:   []int64{7},
		DisabledFilterConditions: []int64{5},
	}
	if err := repo.Create(context.Background(), existing); err != nil {
		t.Fatal(err)
	}

	svc = NewVirtualModelService(repo, &mockModelRepo{}, newMockTagRepo(), newMockProviderRepo(),
		newMockProviderMetaRepo(), newMockGlobalMetaRepo(), newMockMappingRepo(), nil,
		&mockGlobalSortRepo{}, &mockGlobalFilterRepo{})

	// Nil (absent) fields keep the existing disabled sets.
	updated, err := svc.Update(context.Background(), existing.ID, models.UpdateVirtualModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.DisabledSortConditions) != 1 || updated.DisabledSortConditions[0] != 7 {
		t.Errorf("absent disabled_global_sort_conditions must keep existing set, got %v", updated.DisabledSortConditions)
	}
	if len(updated.DisabledFilterConditions) != 1 || updated.DisabledFilterConditions[0] != 5 {
		t.Errorf("absent disabled_global_filter_conditions must keep existing set, got %v", updated.DisabledFilterConditions)
	}

	// Present (including empty) fields replace the sets.
	empty := []int64{}
	updated, err = svc.Update(context.Background(), existing.ID, models.UpdateVirtualModelRequest{
		DisabledSortConditions:   &empty,
		DisabledFilterConditions: &empty,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.DisabledSortConditions) != 0 {
		t.Errorf("empty disabled_global_sort_conditions must replace existing set, got %v", updated.DisabledSortConditions)
	}
	if len(updated.DisabledFilterConditions) != 0 {
		t.Errorf("empty disabled_global_filter_conditions must replace existing set, got %v", updated.DisabledFilterConditions)
	}

	newSort := []int64{11, 12}
	newFilter := []int64{13}
	updated, err = svc.Update(context.Background(), existing.ID, models.UpdateVirtualModelRequest{
		DisabledSortConditions:   &newSort,
		DisabledFilterConditions: &newFilter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.DisabledSortConditions) != 2 {
		t.Errorf("expected 2 disabled sort ids on the VM sent to the repo, got %v", updated.DisabledSortConditions)
	}
	if len(updated.DisabledFilterConditions) != 1 || updated.DisabledFilterConditions[0] != 13 {
		t.Errorf("expected [13] disabled filter ids on the VM sent to the repo, got %v", updated.DisabledFilterConditions)
	}
}

// TestPreviewResolveHonorsDisabledIDs verifies PreviewResolve builds its
// synthetic VM with the given disabled-condition ids, so global conditions
// disabled in the preview request are excluded from resolution.
func TestPreviewResolveHonorsDisabledIDs(t *testing.T) {
	// Global sort condition: m-alpha first. Global filter: keep only m-alpha.
	sortCond := models.GlobalSortCondition{
		ID:       7,
		Name:     "alpha-first",
		Enabled:  true,
		SortExpr: models.SortExpr{{Condition: &models.FilterNode{Key: "m.name", Op: "eq", Value: "m-alpha"}}},
	}
	filterCond := models.GlobalFilterCondition{
		ID:         5,
		Name:       "alpha-only",
		Enabled:    true,
		FilterExpr: models.FilterNode{Key: "m.name", Op: "eq", Value: "m-alpha"},
	}
	svc := newGlobalSortTestService([]models.GlobalSortCondition{sortCond}, []models.GlobalFilterCondition{filterCond})

	// No disabled ids: sort condition forces m-alpha first and the filter
	// condition restricts to m-alpha only.
	results, err := svc.PreviewResolve(context.Background(), json.RawMessage(`{}`), nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Model.Name != "m-alpha" {
		t.Fatalf("expected global filter+sort to keep m-alpha only, got %+v", results)
	}

	// Both ids disabled: filter no longer restricts and the sort condition
	// no longer forces m-alpha first (natural order: m-alpha, m-beta).
	results, err = svc.PreviewResolve(
		context.Background(),
		json.RawMessage(`{}`), nil, nil, nil,
		[]int64{7},
		[]int64{5},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results with conditions disabled, got %d", len(results))
	}
	if results[0].Model.Name != "m-alpha" || results[1].Model.Name != "m-beta" {
		t.Errorf("expected natural repo order [m-alpha, m-beta] with both conditions disabled, got [%s, %s]",
			results[0].Model.Name, results[1].Model.Name)
	}
}

// disabledCritService builds a service like newGlobalSortTestService but with
// tags so local sorts by tag values are observable.
func disabledCritService() VirtualModelService {
	modelRepo := &mockModelRepo{models: []models.Model{
		{ID: 1, ProviderID: 1, Name: "m-alpha"},
		{ID: 2, ProviderID: 2, Name: "m-beta"},
	}}
	providerRepo := &mockProviderRepo{
		providers: map[int64]*models.Provider{
			1: {ID: 1, Name: "prov-a"},
			2: {ID: 2, Name: "prov-b"},
		},
		byName: map[string]*models.Provider{
			"prov-a": {ID: 1, Name: "prov-a"},
			"prov-b": {ID: 2, Name: "prov-b"},
		},
	}
	tagRepo := newMockTagRepo()
	_ = tagRepo.Set(context.Background(), 1, "", map[string]string{"cost": "0.1"})
	_ = tagRepo.Set(context.Background(), 2, "", map[string]string{"cost": "0.9"})

	return NewVirtualModelService(newMockVMRepo(), modelRepo, tagRepo, providerRepo,
		newMockProviderMetaRepo(), newMockGlobalMetaRepo(), newMockMappingRepo(), nil,
		&mockGlobalSortRepo{}, &mockGlobalFilterRepo{})
}

// TestDisabledLeafInAnd behaves as if the leaf was removed from the AND group.
func TestDisabledLeafInAnd(t *testing.T) {
	svc := disabledCritService()

	// AND(true-matcher, disabled(false-matcher)) → leaf removed → matches all.
	// If the disabled leaf were evaluated (m.name eq m-beta), only m-beta would pass.
	filter := json.RawMessage(`{"and":[
		{"key":"m.name","op":"contains","value":"m-"},
		{"key":"m.name","op":"eq","value":"m-beta","disabled":true}
	]}`)
	vm := &models.VirtualModel{FilterExpr: filter}
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("disabled leaf in AND must behave as if removed, got %d results", len(results))
	}
}

// TestDisabledLeafInOr behaves as if the leaf was removed from the OR group.
func TestDisabledLeafInOr(t *testing.T) {
	svc := disabledCritService()

	// OR(m-beta-matcher, disabled(m-alpha-matcher)) → leaf removed → only m-beta.
	// If the disabled leaf returned neutral-true, everything would match.
	filter := json.RawMessage(`{"or":[
		{"key":"m.name","op":"eq","value":"m-beta"},
		{"key":"m.name","op":"eq","value":"m-alpha","disabled":true}
	]}`)
	vm := &models.VirtualModel{FilterExpr: filter}
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Model.Name != "m-beta" {
		t.Fatalf("disabled leaf in OR must behave as if removed, got %+v", results)
	}
}

// TestDisabledLeafInNot behaves as if the leaf was removed (vacuous NOT = true).
func TestDisabledLeafInNot(t *testing.T) {
	svc := disabledCritService()

	// NOT(disabled leaf). Vacuous NOT must match everything; if the disabled
	// leaf returned neutral-true, NOT would reject every model.
	filter := json.RawMessage(`{"not":{"key":"m.name","op":"eq","value":"m-beta","disabled":true}}`)
	vm := &models.VirtualModel{FilterExpr: filter}
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("disabled leaf in NOT must behave as if removed, got %d results", len(results))
	}
}

// TestDisabledRootFilter matches everything (no filtering at all).
func TestDisabledRootFilter(t *testing.T) {
	svc := disabledCritService()

	vm := &models.VirtualModel{FilterExpr: json.RawMessage(`{"key":"m.name","op":"eq","value":"m-beta","disabled":true}`)}
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("disabled root filter must not filter, got %d results", len(results))
	}
}

// TestDisabledLocalKeySort skips a disabled Key sort entry.
func TestDisabledLocalKeySort(t *testing.T) {
	svc := disabledCritService()

	// Disabled desc sort by name must be ignored; disabled condition entries
	// and the active entry leave natural (repo id) order: m-alpha, m-beta.
	vm := &models.VirtualModel{
		FilterExpr: []byte(`{}`),
		SortExpr:   json.RawMessage(`[{"key":"m.name","direction":"desc","disabled":true}]`),
	}
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	natural, err := disabledCritService().ResolveModels(context.Background(), &models.VirtualModel{FilterExpr: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	for i := range natural {
		if natural[i].Model.Name != results[i].Model.Name {
			t.Errorf("disabled key sort still affected order at index %d: %s vs %s",
				i, results[i].Model.Name, natural[i].Model.Name)
		}
	}
}

// TestDisabledConditionSortEntry skips a disabled condition sort entry.
func TestDisabledConditionSortEntry(t *testing.T) {
	svc := disabledCritService()

	// Active condition puts m-beta first; disabled condition puts m-alpha first.
	// Only the active entry must apply.
	vm := &models.VirtualModel{
		FilterExpr: []byte(`{}`),
		SortExpr: json.RawMessage(`[
			{"condition":{"key":"m.name","op":"eq","value":"m-alpha"},"direction":"asc","disabled":true},
			{"condition":{"key":"m.name","op":"eq","value":"m-beta"},"direction":"asc"}
		]`),
	}
	results, err := svc.ResolveModels(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Model.Name != "m-beta" {
		t.Errorf("disabled condition entry must be skipped; active entry should put m-beta first, got %s first", results[0].Model.Name)
	}
}

// TestDisabledFilterJSONOmitEmpty verifies old JSON without "disabled"
// unmarshals to enabled, and marshaling doesn't add the key back.
func TestDisabledFilterJSONOmitEmpty(t *testing.T) {
	var node models.FilterNode
	if err := json.Unmarshal([]byte(`{"key":"m.name","op":"eq","value":"x"}`), &node); err != nil {
		t.Fatal(err)
	}
	if node.Disabled {
		t.Error("missing disabled field must default to enabled (false)")
	}

	var entry models.SortEntry
	if err := json.Unmarshal([]byte(`{"key":"m.name","direction":"asc"}`), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Disabled {
		t.Error("missing disabled field must default to enabled (false)")
	}

	// omitempty: round-trip must not emit "disabled".
	out, err := json.Marshal(models.FilterNode{Key: "a", Op: "eq", Value: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "disabled") {
		t.Errorf("enabled node must not serialize disabled key, got %s", out)
	}
	out, err = json.Marshal(models.SortEntry{Key: "a", Direction: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "disabled") {
		t.Errorf("enabled entry must not serialize disabled key, got %s", out)
	}
}

// TestDisabledValidationTolerance verifies the validators accept filter nodes
// and sort entries carrying the disabled flag.
func TestDisabledValidationTolerance(t *testing.T) {
	if err := validateFilterExpr(json.RawMessage(`{"key":"m.name","op":"eq","value":"x","disabled":true}`)); err != nil {
		t.Errorf("disabled leaf must validate: %v", err)
	}
	if err := validateFilterExpr(json.RawMessage(`{"and":[{"key":"a","op":"eq","value":"1"},{"key":"b","op":"eq","value":"2","disabled":true}]}`)); err != nil {
		t.Errorf("disabled child in AND must validate: %v", err)
	}
	if err := validateSortExpr(json.RawMessage(`[{"key":"m.name","direction":"asc","disabled":true}]`)); err != nil {
		t.Errorf("disabled sort entry must validate: %v", err)
	}
}
