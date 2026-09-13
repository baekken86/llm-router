package handlers_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/chris/llm-router/internal/api/handlers"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

// fakeProviderMetaRepo implements repository.ProviderMetadataRepository for testing.
type fakeProviderMetaRepo struct {
	keys map[string][]string
}

func (f *fakeProviderMetaRepo) Set(_ context.Context, _ int64, _ map[string]string) error {
	return nil
}

func (f *fakeProviderMetaRepo) UpsertKey(_ context.Context, _ int64, _, _ string) error {
	return nil
}

func (f *fakeProviderMetaRepo) GetByProvider(_ context.Context, _ int64) ([]models.ProviderMetadata, error) {
	return nil, nil
}

func (f *fakeProviderMetaRepo) GetByProviders(_ context.Context, _ []int64) (map[int64][]models.ProviderMetadata, error) {
	return map[int64][]models.ProviderMetadata{}, nil
}

func (f *fakeProviderMetaRepo) ListAll(_ context.Context) (map[int64]map[string]string, error) {
	return map[int64]map[string]string{}, nil
}

func (f *fakeProviderMetaRepo) ListAllKeys(_ context.Context) (map[string][]string, error) {
	return f.keys, nil
}

func (f *fakeProviderMetaRepo) DeleteByProvider(_ context.Context, _ int64) error {
	return nil
}

var _ repository.ProviderMetadataRepository = (*fakeProviderMetaRepo)(nil)

type fieldsResponse struct {
	Fields map[string]json.RawMessage `json:"fields"`
}

func getFieldsResponse(t *testing.T, h *handlers.MetadataHandler) fieldsResponse {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/metadata/fields", nil)
	rec := httptest.NewRecorder()
	h.GetFields(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp fieldsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

func TestGetFields_ProviderFieldIncludesValues(t *testing.T) {
	repo := &fakeProviderMetaRepo{keys: map[string][]string{
		// deliberately unsorted input; response must be sorted
		"cost_type": {"api-creds", "free", "subscription"},
	}}
	h := handlers.NewMetadataHandler([]byte(`{"fields":{}}`), repo, nil)

	resp := getFieldsResponse(t, h)

	raw, ok := resp.Fields["p.cost_type"]
	if !ok {
		t.Fatalf("expected p.cost_type in response, got keys: %v", resp.Fields)
	}
	var def struct {
		Description string   `json:"description"`
		Type        string   `json:"type"`
		Values      []string `json:"values"`
	}
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("failed to decode p.cost_type: %v", err)
	}
	if def.Type != "string" {
		t.Errorf("expected type string, got %q", def.Type)
	}
	want := []string{"api-creds", "free", "subscription"}
	if !reflect.DeepEqual(def.Values, want) {
		t.Errorf("expected values %v, got %v", want, def.Values)
	}
}

func TestGetFields_ProviderFieldNoValuesOmitted(t *testing.T) {
	repo := &fakeProviderMetaRepo{keys: map[string][]string{
		"foo": nil, // no distinct values
	}}
	h := handlers.NewMetadataHandler([]byte(`{"fields":{}}`), repo, nil)

	resp := getFieldsResponse(t, h)

	raw, ok := resp.Fields["p.foo"]
	if !ok {
		t.Fatalf("expected p.foo in response, got keys: %v", resp.Fields)
	}
	var def map[string]interface{}
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("failed to decode p.foo: %v", err)
	}
	if _, hasValues := def["values"]; hasValues {
		t.Errorf("expected no values key for empty distinct values, got: %v", def)
	}
	if def["type"] != "string" {
		t.Errorf("expected type string, got %v", def["type"])
	}
}

func TestGetFields_ExistingFieldsPreserved(t *testing.T) {
	repo := &fakeProviderMetaRepo{keys: map[string][]string{
		"cost_type": {"subscription", "api-creds", "free"},
	}}
	modelsJSON := []byte(`{"fields":{"cost_type":{"type":"string","values":["subscription","api-creds","free"],"description":"Model cost type"}}}`)
	h := handlers.NewMetadataHandler(modelsJSON, repo, nil)

	resp := getFieldsResponse(t, h)

	// mc.* passthrough must be untouched
	mcRaw, ok := resp.Fields["mc.cost_type"]
	if !ok {
		t.Fatalf("expected mc.cost_type in response")
	}
	var mcDef map[string]interface{}
	if err := json.Unmarshal(mcRaw, &mcDef); err != nil {
		t.Fatalf("failed to decode mc.cost_type: %v", err)
	}
	wantMC := map[string]interface{}{
		"type":        "string",
		"values":      []interface{}{"subscription", "api-creds", "free"},
		"description": "Model cost type",
	}
	if !reflect.DeepEqual(mcDef, wantMC) {
		t.Errorf("mc.cost_type definition changed: got %v, want %v", mcDef, wantMC)
	}

	// p.* from repo must include values
	pRaw, ok := resp.Fields["p.cost_type"]
	if !ok {
		t.Fatalf("expected p.cost_type in response")
	}
	var def struct {
		Values []string `json:"values"`
	}
	if err := json.Unmarshal(pRaw, &def); err != nil {
		t.Fatalf("failed to decode p.cost_type: %v", err)
	}
	want := []string{"api-creds", "free", "subscription"}
	if !reflect.DeepEqual(def.Values, want) {
		t.Errorf("expected values %v, got %v", want, def.Values)
	}
}
