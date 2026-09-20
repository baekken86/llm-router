package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/repository"
)

type ModelOverrideHandler struct {
	repo repository.ModelOverrideRepository
	// mirrorFn is called after a successful override write with the model
	// NAME and the set of keys cleared in this write, so the global-metadata
	// layer (m.*) is updated from the effective override state. Overrides
	// shadow m.* in the resolver; without this sync a stale m.* value
	// resurfaces the moment an override is cleared. Optional.
	mirrorFn  func(modelName string, clearedKeys map[string]bool)
	modelName func(ctx context.Context, modelID int64) (string, bool)
}

func NewModelOverrideHandler(repo repository.ModelOverrideRepository) *ModelOverrideHandler {
	return &ModelOverrideHandler{repo: repo}
}

// SetMirrorFn wires the post-write sync callbacks:
//   - mirrorFn(modelName, clearedKeys): push effective override state into
//     the m.* layer (implemented as MirrorOverridesToGlobal in the service).
//   - modelNameFn(ctx, modelID): resolve a model ID to its name.
func (h *ModelOverrideHandler) SetMirrorFn(mirrorFn func(modelName string, clearedKeys map[string]bool), modelNameFn func(ctx context.Context, modelID int64) (string, bool)) {
	h.mirrorFn = mirrorFn
	h.modelName = modelNameFn
}

// syncGlobalMetadata pushes the override's effective values into the global
// metadata layer (m.*) for the model name. Best-effort: failures are logged,
// the API response is unaffected.
func (h *ModelOverrideHandler) syncGlobalMetadata(ctx context.Context, modelID int64, clearedKeys map[string]bool) {
	if h.mirrorFn == nil || h.modelName == nil {
		return
	}
	name, ok := h.modelName(ctx, modelID)
	if !ok || name == "" {
		return
	}
	h.mirrorFn(name, clearedKeys)
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

	var cleared map[string]bool
	for k, v := range body.Tags {
		if v == "" {
			if cleared == nil {
				cleared = make(map[string]bool)
			}
			cleared[k] = true
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

	// Sync the m.* layer with the new effective override state so values
	// survive override-clears and stay consistent across provider rows.
	h.syncGlobalMetadata(r.Context(), id, cleared)

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

	// All override keys for this (model, effort) are being cleared.
	rowsBefore, _ := h.repo.GetByModelAndEffort(r.Context(), id, effort)
	cleared := make(map[string]bool, len(rowsBefore))
	for _, row := range rowsBefore {
		cleared[row.Key] = true
	}

	if err := h.repo.DeleteAll(r.Context(), id, effort); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Sync the m.* layer: deleted overrides would otherwise resurface as
	// stale m.* values.
	h.syncGlobalMetadata(r.Context(), id, cleared)

	w.WriteHeader(http.StatusNoContent)
}
