package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/service"
)

type ModelHandler struct {
	modelService service.ModelService
}

func NewModelHandler(ms service.ModelService) *ModelHandler {
	return &ModelHandler{modelService: ms}
}

func (h *ModelHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.ListAll)
	r.Get("/{id}", h.GetByID)
	r.Put("/{id}/tags", h.SetTags)
	r.Get("/{id}/tags", h.GetTags)
	r.Put("/{id}/disabled", h.ToggleDisabled)
	r.Delete("/{id}", h.Delete)
	return r
}

func (h *ModelHandler) ListAll(w http.ResponseWriter, r *http.Request) {
	models, err := h.modelService.ListAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func (h *ModelHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	m, err := h.modelService.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if m == nil {
		writeError(w, http.StatusNotFound, "model not found")
		return
	}

	writeJSON(w, http.StatusOK, m)
}

func (h *ModelHandler) SetTags(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req models.SetTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.modelService.SetTags(r.Context(), id, req.Tags); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	tags, err := h.modelService.GetTags(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, tags)
}

func (h *ModelHandler) GetTags(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	tags, err := h.modelService.GetTags(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, tags)
}

func (h *ModelHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.modelService.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *ModelHandler) ToggleDisabled(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	m, err := h.modelService.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if m == nil {
		writeError(w, http.StatusNotFound, "model not found")
		return
	}

	var req struct {
		Disabled bool    `json:"disabled"`
		Duration *string `json:"duration,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var duration *time.Duration
	if req.Duration != nil {
		d, err := models.ParseDuration(*req.Duration)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		duration = &d
	}

	if err := h.modelService.ToggleDisabled(r.Context(), id, req.Disabled, duration); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, m)
}
