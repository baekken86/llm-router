package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
	"github.com/go-chi/chi/v5"
)

type GlobalSortConditionHandler struct {
	repo repository.GlobalSortConditionRepository
}

func NewGlobalSortConditionHandler(repo repository.GlobalSortConditionRepository) *GlobalSortConditionHandler {
	return &GlobalSortConditionHandler{repo: repo}
}

func (h *GlobalSortConditionHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Get("/{id}", h.Get)
	r.Put("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	r.Put("/reorder", h.Reorder)
	return r
}

// reorderRequest is the body for PUT /reorder: the full list of condition ids
// in their desired order; each gets position = index.
type reorderRequest struct {
	IDs []int64 `json:"ids"`
}

func (h *GlobalSortConditionHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	var req reorderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.repo.Reorder(r.Context(), req.IDs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	conds, err := h.repo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if conds == nil {
		conds = []models.GlobalSortCondition{}
	}
	writeJSON(w, http.StatusOK, conds)
}

func (h *GlobalSortConditionHandler) List(w http.ResponseWriter, r *http.Request) {
	conds, err := h.repo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if conds == nil {
		conds = []models.GlobalSortCondition{}
	}
	writeJSON(w, http.StatusOK, conds)
}

func (h *GlobalSortConditionHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	cond, err := h.repo.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cond == nil {
		writeError(w, http.StatusNotFound, "global sort condition not found")
		return
	}
	writeJSON(w, http.StatusOK, cond)
}

func (h *GlobalSortConditionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		SortExpr    json.RawMessage `json:"sort_expr"`
		Enabled     *bool           `json:"enabled"`
		Position    *int            `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	var sortExpr models.SortExpr
	if len(req.SortExpr) > 0 {
		if err := json.Unmarshal(req.SortExpr, &sortExpr); err != nil {
			writeError(w, http.StatusBadRequest, "invalid sort_expr: must be a valid sort expression")
			return
		}
	}

	cond := &models.GlobalSortCondition{
		Name:        req.Name,
		Description: req.Description,
		SortExpr:    sortExpr,
		Enabled:     true,
	}
	if req.Enabled != nil {
		cond.Enabled = *req.Enabled
	}
	if req.Position != nil {
		cond.Position = *req.Position
	}

	if err := h.repo.Create(r.Context(), cond); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, cond)
}

func (h *GlobalSortConditionHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req struct {
		Name        *string          `json:"name"`
		Description *string          `json:"description"`
		SortExpr    *json.RawMessage `json:"sort_expr"`
		Enabled     *bool            `json:"enabled"`
		Position    *int             `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cond, err := h.repo.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cond == nil {
		writeError(w, http.StatusNotFound, "global sort condition not found")
		return
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		cond.Name = name
	}
	if req.Description != nil {
		cond.Description = *req.Description
	}
	if req.SortExpr != nil {
		var sortExpr models.SortExpr
		if err := json.Unmarshal(*req.SortExpr, &sortExpr); err != nil {
			writeError(w, http.StatusBadRequest, "invalid sort_expr: must be a valid sort expression")
			return
		}
		cond.SortExpr = sortExpr
	}
	if req.Enabled != nil {
		cond.Enabled = *req.Enabled
	}
	if req.Position != nil {
		cond.Position = *req.Position
	}

	if err := h.repo.Update(r.Context(), cond); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cond)
}

func (h *GlobalSortConditionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.repo.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
