package service

import (
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
