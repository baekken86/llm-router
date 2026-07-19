package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/repository"
)

type ModelMappingHandler struct {
	mappingRepo repository.ModelMappingRepository
	modelRepo   repository.ModelRepository
	logger      *slog.Logger
}

func NewModelMappingHandler(mappingRepo repository.ModelMappingRepository, modelRepo repository.ModelRepository, logger *slog.Logger) *ModelMappingHandler {
	return &ModelMappingHandler{
		mappingRepo: mappingRepo,
		modelRepo:   modelRepo,
		logger:      logger,
	}
}

func (h *ModelMappingHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.ListMappings)
	return r
}

type createMappingRequest struct {
	TargetModelName string `json:"target_model_name"`
}

func (h *ModelMappingHandler) CreateMapping(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req createMappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate target name not empty
	if req.TargetModelName == "" {
		writeError(w, http.StatusBadRequest, "target_model_name is required")
		return
	}

	// Validate source exists
	source, err := h.modelRepo.GetByID(r.Context(), id)
	if err != nil || source == nil {
		writeError(w, http.StatusNotFound, "source model not found")
		return
	}

	// Validate source name != target name
	if source.Name == req.TargetModelName {
		writeError(w, http.StatusBadRequest, "source and target must be different")
		return
	}

	if err := h.mappingRepo.Set(r.Context(), id, req.TargetModelName); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	mapping, _ := h.mappingRepo.Get(r.Context(), id)
	writeJSON(w, http.StatusCreated, mapping)
}

func (h *ModelMappingHandler) GetMapping(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	mapping, err := h.mappingRepo.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if mapping == nil {
		writeError(w, http.StatusNotFound, "mapping not found")
		return
	}

	writeJSON(w, http.StatusOK, mapping)
}

func (h *ModelMappingHandler) DeleteMapping(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.mappingRepo.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *ModelMappingHandler) ListMappings(w http.ResponseWriter, r *http.Request) {
	mappings, err := h.mappingRepo.GetAllJoined(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, mappings)
}
