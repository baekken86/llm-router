package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"

	_ "modernc.org/sqlite"
)

type testMappingDB struct {
	db          *sql.DB
	mappingRepo repository.ModelMappingRepository
	modelRepo   repository.ModelRepository
	providerRepo repository.ProviderRepository
}

func setupMappingHandlerTest(t *testing.T) *testMappingDB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`PRAGMA foreign_keys = ON`)

	for _, stmt := range []string{
		`CREATE TABLE providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			api_type TEXT NOT NULL CHECK(api_type IN ('openai', 'anthropic', 'cloudflare', 'ollama')),
			base_url TEXT NOT NULL,
			api_key_encrypted TEXT NOT NULL DEFAULT '',
			account_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE models (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(provider_id, name)
		)`,
		`CREATE TABLE model_mappings (
			source_model_id INTEGER PRIMARY KEY REFERENCES models(id) ON DELETE CASCADE,
			target_model_name TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}

	return &testMappingDB{
		db:           db,
		mappingRepo:  repository.NewModelMappingRepository(db),
		modelRepo:    repository.NewModelRepository(db),
		providerRepo: repository.NewProviderRepository(db),
	}
}

func seedTestData(tdb *testMappingDB) {
	p1 := &models.Provider{Name: "p1", APIType: models.APITypeOpenAI, BaseURL: "http://localhost"}
	tdb.providerRepo.Create(context.Background(), p1)
	p2 := &models.Provider{Name: "p2", APIType: models.APITypeOpenAI, BaseURL: "http://localhost"}
	tdb.providerRepo.Create(context.Background(), p2)
	m1 := &models.Model{ProviderID: p1.ID, Name: "@cf/meta/llama"}
	tdb.modelRepo.Create(context.Background(), m1)
	m2 := &models.Model{ProviderID: p2.ID, Name: "llama"}
	tdb.modelRepo.Create(context.Background(), m2)
}

func TestCreateMapping(t *testing.T) {
	tdb := setupMappingHandlerTest(t)
	defer tdb.db.Close()
	seedTestData(tdb)

	handler := NewModelMappingHandler(tdb.mappingRepo, tdb.modelRepo, slog.Default())

	r := chi.NewRouter()
	r.Post("/models/{id}/mapping", handler.CreateMapping)

	body := `{"target_model_name": "llama"}`
	req := httptest.NewRequest("POST", "/models/1/mapping", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["source_model_id"].(float64) != 1 || resp["target_model_name"] != "llama" {
		t.Errorf("unexpected response: %v", resp)
	}
}

func TestCreateMapping_SourceEqualsTarget(t *testing.T) {
	tdb := setupMappingHandlerTest(t)
	defer tdb.db.Close()
	seedTestData(tdb)

	handler := NewModelMappingHandler(tdb.mappingRepo, tdb.modelRepo, slog.Default())

	r := chi.NewRouter()
	r.Post("/models/{id}/mapping", handler.CreateMapping)

	// Source model id=1 has name "@cf/meta/llama", target is same name
	body := `{"target_model_name": "@cf/meta/llama"}`
	req := httptest.NewRequest("POST", "/models/1/mapping", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateMapping_SourceNotFound(t *testing.T) {
	tdb := setupMappingHandlerTest(t)
	defer tdb.db.Close()
	seedTestData(tdb)

	handler := NewModelMappingHandler(tdb.mappingRepo, tdb.modelRepo, slog.Default())

	r := chi.NewRouter()
	r.Post("/models/{id}/mapping", handler.CreateMapping)

	body := `{"target_model_name": "llama"}`
	req := httptest.NewRequest("POST", "/models/999/mapping", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateMapping_EmptyTarget(t *testing.T) {
	tdb := setupMappingHandlerTest(t)
	defer tdb.db.Close()
	seedTestData(tdb)

	handler := NewModelMappingHandler(tdb.mappingRepo, tdb.modelRepo, slog.Default())

	r := chi.NewRouter()
	r.Post("/models/{id}/mapping", handler.CreateMapping)

	body := `{"target_model_name": ""}`
	req := httptest.NewRequest("POST", "/models/1/mapping", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetMapping(t *testing.T) {
	tdb := setupMappingHandlerTest(t)
	defer tdb.db.Close()
	seedTestData(tdb)
	tdb.mappingRepo.Set(context.Background(), 1, "llama")

	handler := NewModelMappingHandler(tdb.mappingRepo, tdb.modelRepo, slog.Default())

	r := chi.NewRouter()
	r.Get("/models/{id}/mapping", handler.GetMapping)

	req := httptest.NewRequest("GET", "/models/1/mapping", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetMapping_NotFound(t *testing.T) {
	tdb := setupMappingHandlerTest(t)
	defer tdb.db.Close()
	seedTestData(tdb)

	handler := NewModelMappingHandler(tdb.mappingRepo, tdb.modelRepo, slog.Default())

	r := chi.NewRouter()
	r.Get("/models/{id}/mapping", handler.GetMapping)

	req := httptest.NewRequest("GET", "/models/999/mapping", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteMapping(t *testing.T) {
	tdb := setupMappingHandlerTest(t)
	defer tdb.db.Close()
	seedTestData(tdb)
	tdb.mappingRepo.Set(context.Background(), 1, "llama")

	handler := NewModelMappingHandler(tdb.mappingRepo, tdb.modelRepo, slog.Default())

	r := chi.NewRouter()
	r.Delete("/models/{id}/mapping", handler.DeleteMapping)

	req := httptest.NewRequest("DELETE", "/models/1/mapping", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	m, _ := tdb.mappingRepo.Get(context.Background(), 1)
	if m != nil {
		t.Error("expected mapping to be deleted")
	}
}

func TestListMappings(t *testing.T) {
	tdb := setupMappingHandlerTest(t)
	defer tdb.db.Close()
	seedTestData(tdb)
	tdb.mappingRepo.Set(context.Background(), 1, "llama")

	handler := NewModelMappingHandler(tdb.mappingRepo, tdb.modelRepo, slog.Default())

	r := chi.NewRouter()
	r.Get("/mappings", handler.ListMappings)

	req := httptest.NewRequest("GET", "/mappings", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(resp))
	}
	if resp[0]["source_model_name"] != "@cf/meta/llama" {
		t.Errorf("expected source name @cf/meta/llama, got %v", resp[0]["source_model_name"])
	}
}

func TestCascadeDeleteModel(t *testing.T) {
	tdb := setupMappingHandlerTest(t)
	defer tdb.db.Close()
	seedTestData(tdb)
	tdb.mappingRepo.Set(context.Background(), 1, "llama")

	tdb.db.Exec(`DELETE FROM models WHERE id = 1`)

	m, _ := tdb.mappingRepo.Get(context.Background(), 1)
	if m != nil {
		t.Error("expected mapping to be cascade-deleted")
	}
}
