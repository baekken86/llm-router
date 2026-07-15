package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/service"
)

type ImportHandler struct {
	importService service.ImportService
}

func NewImportHandler(is service.ImportService) *ImportHandler {
	return &ImportHandler{importService: is}
}

func (h *ImportHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/csv", h.ImportCSV)
	return r
}

func (h *ImportHandler) ImportCSV(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "merge"
	}

	if mode != "merge" && mode != "replace" {
		writeError(w, http.StatusBadRequest, "mode must be 'merge' or 'replace'")
		return
	}

	result, err := h.importService.ImportCSV(r.Context(), r.Body, service.ImportMode(mode))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}
