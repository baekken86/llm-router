package service

import (
	"context"
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
