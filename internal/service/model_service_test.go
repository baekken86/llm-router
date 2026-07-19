package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chris/llm-router/internal/models"
)

func TestFetchModels_Ollama(t *testing.T) {
	// Mock Ollama /api/tags endpoint
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]string{
				{"name": "llama3.2:latest"},
				{"name": "mistral:7b"},
			},
		})
	}))
	defer ts.Close()

	// baseURL is the OpenAI-compatible URL; fetchModels strips /v1 to get ollama host
	baseURL := ts.URL + "/v1"
	names, err := fetchModels(baseURL, "", models.APITypeOllama)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 models, got %d", len(names))
	}
	if names[0] != "llama3.2:latest" {
		t.Errorf("expected first model 'llama3.2:latest', got '%s'", names[0])
	}
	if names[1] != "mistral:7b" {
		t.Errorf("expected second model 'mistral:7b', got '%s'", names[1])
	}
}

func TestFetchModels_Ollama_Unreachable(t *testing.T) {
	// Point to unreachable address
	baseURL := "http://127.0.0.1:1/v1"
	_, err := fetchModels(baseURL, "", models.APITypeOllama)
	if err == nil {
		t.Error("expected error for unreachable Ollama host, got nil")
	}
}

func TestFetchModels_Ollama_EmptyModels(t *testing.T) {
	// Mock Ollama /api/tags returning empty list
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]string{},
		})
	}))
	defer ts.Close()

	baseURL := ts.URL + "/v1"
	names, err := fetchModels(baseURL, "", models.APITypeOllama)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected 0 models, got %d", len(names))
	}
}

func TestListAll_NotMapped_BaseEffort(t *testing.T) {
	ctx := context.Background()
	modelRepo := newMockModelRepo()
	tagRepo := newMockTagRepo()
	providerRepo := newMockProviderRepo()
	globalMetaRepo := newMockGlobalMetaRepo()

	providerRepo.add(&models.Provider{ID: 1, Name: "openai"})
	modelRepo.Create(ctx, &models.Model{ID: 1, ProviderID: 1, Name: "gpt-4o"})

	svc := NewModelService(modelRepo, tagRepo, providerRepo, nil, globalMetaRepo, nil)
	entries, err := svc.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.ReasoningEffort != "" {
		t.Errorf("expected empty effort, got %q", e.ReasoningEffort)
	}
	if e.Tags == nil {
		t.Error("Tags should not be nil")
	}
	if e.GlobalMetadata == nil {
		t.Error("GlobalMetadata should not be nil")
	}
	// Verify JSON marshals to {} not null
	data, _ := json.Marshal(e)
	var parsed map[string]json.RawMessage
	json.Unmarshal(data, &parsed)
	if string(parsed["tags"]) == "null" {
		t.Error("tags serialized as null, expected {}")
	}
	if string(parsed["global_metadata"]) == "null" {
		t.Error("global_metadata serialized as null, expected {}")
	}
}

func TestListAll_NotMapped_MultipleEfforts(t *testing.T) {
	ctx := context.Background()
	modelRepo := newMockModelRepo()
	tagRepo := newMockTagRepo()
	providerRepo := newMockProviderRepo()
	globalMetaRepo := newMockGlobalMetaRepo()

	providerRepo.add(&models.Provider{ID: 1, Name: "openai"})
	modelRepo.Create(ctx, &models.Model{ID: 1, ProviderID: 1, Name: "gpt-4o"})

	tagRepo.efforts[1] = []string{"", "high"}
	// Directly populate tags map (Set overwrites per model)
	tagRepo.tags[1] = []models.Tag{
		{ModelID: 1, ReasoningEffort: "", Key: "speed", Value: "5"},
		{ModelID: 1, ReasoningEffort: "high", Key: "speed", Value: "9"},
	}

	svc := NewModelService(modelRepo, tagRepo, providerRepo, nil, globalMetaRepo, nil)
	entries, err := svc.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].ReasoningEffort != "" {
		t.Errorf("expected empty effort for first, got %q", entries[0].ReasoningEffort)
	}
	if entries[0].Tags["speed"] != "5" {
		t.Errorf("expected speed=5 for base effort, got %q", entries[0].Tags["speed"])
	}
	if entries[1].ReasoningEffort != "high" {
		t.Errorf("expected high effort, got %q", entries[1].ReasoningEffort)
	}
	if entries[1].Tags["speed"] != "9" {
		t.Errorf("expected speed=9 for high effort, got %q", entries[1].Tags["speed"])
	}
}

func TestListAll_Mapped_GlobalMetadata(t *testing.T) {
	ctx := context.Background()
	modelRepo := newMockModelRepo()
	tagRepo := newMockTagRepo()
	providerRepo := newMockProviderRepo()
	globalMetaRepo := newMockGlobalMetaRepo()
	mappingRepo := newMockMappingRepo()

	providerRepo.add(&models.Provider{ID: 1, Name: "openai"})
	modelRepo.Create(ctx, &models.Model{ID: 1, ProviderID: 1, Name: "gpt-4o"})
	mappingRepo.Set(ctx, 1, "gpt-4o-real")

	// Target has per-effort metadata
	globalMetaRepo.Set(ctx, "gpt-4o-real", "", map[string]string{"context_window": "128k"})
	globalMetaRepo.Set(ctx, "gpt-4o-real", "high", map[string]string{"context_window": "256k"})

	svc := NewModelService(modelRepo, tagRepo, providerRepo, nil, globalMetaRepo, mappingRepo)
	entries, err := svc.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Base effort
	if entries[0].ReasoningEffort != "" {
		t.Errorf("expected empty effort, got %q", entries[0].ReasoningEffort)
	}
	if entries[0].GlobalMetadata["context_window"] != "128k" {
		t.Errorf("expected context_window=128k, got %q", entries[0].GlobalMetadata["context_window"])
	}
	if len(entries[0].Tags) != 0 {
		t.Errorf("expected empty tags for mapped model, got %v", entries[0].Tags)
	}

	// High effort
	if entries[1].ReasoningEffort != "high" {
		t.Errorf("expected high effort, got %q", entries[1].ReasoningEffort)
	}
	if entries[1].GlobalMetadata["context_window"] != "256k" {
		t.Errorf("expected context_window=256k, got %q", entries[1].GlobalMetadata["context_window"])
	}
}

func TestListAll_Mapped_EmptyGlobalMetadata(t *testing.T) {
	ctx := context.Background()
	modelRepo := newMockModelRepo()
	tagRepo := newMockTagRepo()
	providerRepo := newMockProviderRepo()
	globalMetaRepo := newMockGlobalMetaRepo()
	mappingRepo := newMockMappingRepo()

	providerRepo.add(&models.Provider{ID: 1, Name: "openai"})
	modelRepo.Create(ctx, &models.Model{ID: 1, ProviderID: 1, Name: "gpt-4o"})
	mappingRepo.Set(ctx, 1, "gpt-4o-real")
	// No metadata set for target

	svc := NewModelService(modelRepo, tagRepo, providerRepo, nil, globalMetaRepo, mappingRepo)
	entries, err := svc.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.ReasoningEffort != "" {
		t.Errorf("expected empty effort, got %q", e.ReasoningEffort)
	}
	if len(e.GlobalMetadata) != 0 {
		t.Errorf("expected empty global metadata, got %v", e.GlobalMetadata)
	}
	if len(e.Tags) != 0 {
		t.Errorf("expected empty tags, got %v", e.Tags)
	}
}
