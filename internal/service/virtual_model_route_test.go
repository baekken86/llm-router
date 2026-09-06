package service

import (
	"context"
	"testing"

	"github.com/chris/llm-router/internal/models"
)

// setupRouteModelService builds a virtualModelService with:
//   - provider "OpenAI Prod" with provider_key "openai", models "gpt-4o"
//   - provider "Local Ollama" with provider_key "ollama", model "llama3"
//   - virtual model named "foo/bar" (slash in a VM name must still resolve)
func setupRouteModelService(t *testing.T) VirtualModelService {
	t.Helper()

	modelRepo := newMockModelRepo()
	modelRepo.models = []models.Model{
		{ID: 1, ProviderID: 1, Name: "gpt-4o"},
		{ID: 2, ProviderID: 2, Name: "llama3"},
	}

	providerRepo := newMockProviderRepo()
	providerRepo.add(&models.Provider{ID: 1, Name: "OpenAI Prod", ProviderKey: "openai", APIType: models.APITypeOpenAI})
	providerRepo.add(&models.Provider{ID: 2, Name: "Local Ollama", ProviderKey: "ollama", APIType: models.APITypeOllama})

	// A VM literally named "foo/bar" — reachable via the unknown-key fallback.
	vmRepo := newMockVMRepo()
	vmRepo.vms["foo/bar"] = &models.VirtualModel{ID: 10, Name: "foo/bar", FilterExpr: []byte(`{}`)}

	return NewVirtualModelService(
		vmRepo,
		modelRepo,
		newMockTagRepo(),
		providerRepo,
		newMockProviderMetaRepo(),
		newMockGlobalMetaRepo(),
		newMockMappingRepo(),
		nil,
	)
}

func TestRouteModel_VirtualPrefix(t *testing.T) {
	svc := setupRouteModelService(t)
	route, err := svc.RouteModel(context.Background(), "virtual/foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route.Kind != RouteKindVirtual {
		t.Errorf("Kind = %q, want %q", route.Kind, RouteKindVirtual)
	}
	if route.VirtualName != "foo" {
		t.Errorf("VirtualName = %q, want %q", route.VirtualName, "foo")
	}
	if route.Resolved != nil {
		t.Errorf("Resolved should be nil for virtual routes, got %+v", route.Resolved)
	}
}

func TestRouteModel_VirtualPrefixEmpty(t *testing.T) {
	svc := setupRouteModelService(t)
	if _, err := svc.RouteModel(context.Background(), "virtual/"); err == nil {
		t.Error("expected error for 'virtual/' with empty name")
	}
}

func TestRouteModel_BareName(t *testing.T) {
	svc := setupRouteModelService(t)
	route, err := svc.RouteModel(context.Background(), "foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route.Kind != RouteKindVirtual || route.VirtualName != "foo" {
		t.Errorf("got Kind=%q VirtualName=%q, want virtual/foo", route.Kind, route.VirtualName)
	}
}

func TestRouteModel_ProviderModel(t *testing.T) {
	svc := setupRouteModelService(t)
	route, err := svc.RouteModel(context.Background(), "openai/gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route.Kind != RouteKindProvider {
		t.Fatalf("Kind = %q, want %q", route.Kind, RouteKindProvider)
	}
	if route.NotFound {
		t.Error("NotFound = true, want false")
	}
	if route.Provider == nil || route.Provider.ProviderKey != "openai" {
		t.Errorf("Provider = %+v, want provider_key openai", route.Provider)
	}
	if route.Model == nil || route.Model.Name != "gpt-4o" {
		t.Errorf("Model = %+v, want gpt-4o", route.Model)
	}
	if len(route.Resolved) != 1 {
		t.Fatalf("Resolved has %d entries, want 1", len(route.Resolved))
	}
	rm := route.Resolved[0]
	if rm.Model.Name != "gpt-4o" || rm.Provider.ProviderKey != "openai" {
		t.Errorf("Resolved[0] = model %q provider %q, want gpt-4o/openai", rm.Model.Name, rm.Provider.ProviderKey)
	}
	if rm.ProviderMetadata["p.name"] != "OpenAI Prod" {
		t.Errorf("ProviderMetadata[p.name] = %q, want %q", rm.ProviderMetadata["p.name"], "OpenAI Prod")
	}
}

func TestRouteModel_ProviderModelNotFound(t *testing.T) {
	svc := setupRouteModelService(t)
	route, err := svc.RouteModel(context.Background(), "openai/nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route.Kind != RouteKindProvider {
		t.Fatalf("Kind = %q, want %q", route.Kind, RouteKindProvider)
	}
	if !route.NotFound {
		t.Error("NotFound = false, want true")
	}
}

func TestRouteModel_UnknownKeyFallsBackToVirtual(t *testing.T) {
	svc := setupRouteModelService(t)
	route, err := svc.RouteModel(context.Background(), "unknownkey/model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route.Kind != RouteKindVirtual {
		t.Fatalf("Kind = %q, want %q", route.Kind, RouteKindVirtual)
	}
	if route.VirtualName != "unknownkey/model" {
		t.Errorf("VirtualName = %q, want full original name", route.VirtualName)
	}
}

func TestRouteModel_UnknownKeyWithExistingVMNamedWithSlash(t *testing.T) {
	svc := setupRouteModelService(t)
	// "foo/bar" is not a known provider key, so the full name is treated as a
	// VM name — and a VM with that literal name exists.
	route, err := svc.RouteModel(context.Background(), "foo/bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route.Kind != RouteKindVirtual || route.VirtualName != "foo/bar" {
		t.Errorf("got Kind=%q VirtualName=%q, want virtual/foo/bar", route.Kind, route.VirtualName)
	}

	// The engine path then resolves it via GetByName.
	vm, err := svc.GetByName(context.Background(), route.VirtualName)
	if err != nil || vm == nil {
		t.Fatalf("GetByName(foo/bar) = %v, %v; want VM", vm, err)
	}
}

func TestRouteModel_VirtualPrefixNeverRoutesToProvider(t *testing.T) {
	svc := setupRouteModelService(t)
	// Even though provider key "virtual" is rejected at create time, verify
	// the canonical prefix always wins.
	route, err := svc.RouteModel(context.Background(), "virtual/openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route.Kind != RouteKindVirtual || route.VirtualName != "openai" {
		t.Errorf("got Kind=%q VirtualName=%q, want virtual/openai", route.Kind, route.VirtualName)
	}
}

func TestRouteModel_RemainderAfterFirstSlashKept(t *testing.T) {
	svc := setupRouteModelService(t)
	// "ollama/a/b" — model name keeps the remainder after the first slash.
	// No model "a/b" exists on ollama → provider route with NotFound.
	route, err := svc.RouteModel(context.Background(), "ollama/a/b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route.Kind != RouteKindProvider || !route.NotFound {
		t.Errorf("got Kind=%q NotFound=%v, want provider/NotFound=true", route.Kind, route.NotFound)
	}
}

func TestRouteModel_EmptyKeySegment(t *testing.T) {
	svc := setupRouteModelService(t)
	// "/model" has an empty key segment — can never match a provider key, so
	// it falls back to a virtual lookup of the full name.
	route, err := svc.RouteModel(context.Background(), "/model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route.Kind != RouteKindVirtual || route.VirtualName != "/model" {
		t.Errorf("got Kind=%q VirtualName=%q, want virtual//model", route.Kind, route.VirtualName)
	}
}
