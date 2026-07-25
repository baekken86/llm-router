package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/repository"
)

type ModelOverrideHandler struct {
	repo repository.ModelOverrideRepository
}

func NewModelOverrideHandler(repo repository.ModelOverrideRepository) *ModelOverrideHandler {
	return &ModelOverrideHandler{repo: repo}
}

func (h *ModelOverrideHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.GetOverrides)
	r.Put("/", h.SetOverrides)
	r.Delete("/", h.DeleteAllOverrides)
	return r
}

func (h *ModelOverrideHandler) GetOverrides(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	effort := r.URL.Query().Get("effort")

	rows, err := h.repo.GetByModelAndEffort(r.Context(), id, effort)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	tags := make(map[string]string, len(rows))
	for _, row := range rows {
		tags[row.Key] = row.Value
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"model_id":         id,
		"reasoning_effort": effort,
		"overrides":        tags,
	})
}

func (h *ModelOverrideHandler) SetOverrides(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	effort := r.URL.Query().Get("effort")

	var body struct {
		Tags map[string]string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for k, v := range body.Tags {
		if v == "" {
			if err := h.repo.Delete(r.Context(), id, effort, k); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		} else {
			if err := h.repo.Set(r.Context(), id, effort, k, v); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}

	// Return updated overrides
	rows, err := h.repo.GetByModelAndEffort(r.Context(), id, effort)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	tags := make(map[string]string, len(rows))
	for _, row := range rows {
		tags[row.Key] = row.Value
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"model_id":         id,
		"reasoning_effort": effort,
		"overrides":        tags,
	})
}

func (h *ModelOverrideHandler) DeleteAllOverrides(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	effort := r.URL.Query().Get("effort")

	if err := h.repo.DeleteAll(r.Context(), id, effort); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
