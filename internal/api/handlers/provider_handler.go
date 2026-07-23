package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/service"
)

type ProviderHandler struct {
	providerService service.ProviderService
	modelService    service.ModelService
}

func NewProviderHandler(ps service.ProviderService, ms service.ModelService) *ProviderHandler {
	return &ProviderHandler{providerService: ps, modelService: ms}
}

func (h *ProviderHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/{id}", h.GetByID)
	r.Put("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	r.Post("/{id}/discover", h.Discover)
	r.Post("/discover-all", h.DiscoverAll)
	return r
}

func (h *ProviderHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.APIType == "" || req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "name, api_type, and base_url are required")
		return
	}
	if req.APIKey == "" && req.APIType != models.APITypeOllama && req.APIType != models.APITypeOllamaCloud {
		writeError(w, http.StatusBadRequest, "api_key is required")
		return
	}

	if req.APIType != models.APITypeOpenAI && req.APIType != models.APITypeAnthropic && req.APIType != models.APITypeCloudflare && req.APIType != models.APITypeOllama && req.APIType != models.APITypeOllamaCloud {
		writeError(w, http.StatusBadRequest, "api_type must be 'openai', 'anthropic', 'cloudflare', 'ollama', or 'ollama-cloud'")
		return
	}

	if req.APIType == models.APITypeCloudflare && strings.TrimSpace(req.AccountID) == "" {
		writeError(w, http.StatusBadRequest, "account_id is required when api_type is 'cloudflare'")
		return
	}

	p, err := h.providerService.Create(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, p)
}

func (h *ProviderHandler) List(w http.ResponseWriter, r *http.Request) {
	filters := make(map[string]string)
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			filters[key] = values[0]
		}
	}

	var providers []models.Provider
	var err error
	if len(filters) > 0 {
		providers, err = h.providerService.ListByMetadata(r.Context(), filters)
	} else {
		providers, err = h.providerService.List(r.Context())
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, providers)
}

func (h *ProviderHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	p, err := h.providerService.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

func (h *ProviderHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req models.UpdateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	p, err := h.providerService.Update(r.Context(), id, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, p)
}

func (h *ProviderHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.providerService.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *ProviderHandler) Discover(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	p, err := h.providerService.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	if p.Disabled {
		writeError(w, http.StatusBadRequest, "provider is disabled")
		return
	}

	deactivated, err := h.modelService.Discover(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"provider_id":  id,
		"deactivated": deactivated,
	})
}

func (h *ProviderHandler) DiscoverAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	providers, err := h.providerService.List(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type providerResult struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Deactivated int64  `json:"deactivated"`
		Error       string `json:"error,omitempty"`
	}

	var results []providerResult
	for _, p := range providers {
		if p.Disabled {
			continue
		}
		pr := providerResult{ID: p.ID, Name: p.Name}
		deactivated, err := h.modelService.Discover(ctx, p.ID)
		if err != nil {
			pr.Error = err.Error()
		} else {
			pr.Deactivated = deactivated
		}
		results = append(results, pr)
	}

	writeJSON(w, http.StatusOK, results)
}
