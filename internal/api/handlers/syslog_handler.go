package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/repository"
)

type SyslogHandler struct {
	logRepo *repository.LogRepository
}

func NewSyslogHandler(logRepo *repository.LogRepository) *SyslogHandler {
	return &SyslogHandler{logRepo: logRepo}
}

func (h *SyslogHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.ListSyslog)
	return r
}

func (h *SyslogHandler) ListSyslog(w http.ResponseWriter, r *http.Request) {
	limit := 500
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 5000 {
			limit = n
		}
	}

	entries, err := h.logRepo.ListSyslogEntries(r.Context(), limit)
	if err != nil {
		http.Error(w, `{"error":"failed to list syslog entries"}`, http.StatusInternalServerError)
		return
	}

	if entries == nil {
		entries = []repository.SyslogEntry{}
	}

	type syslogEntry struct {
		Timestamp string `json:"timestamp"`
		Level     string `json:"level"`
		Message   string `json:"message"`
	}

	result := make([]syslogEntry, len(entries))
	for i, e := range entries {
		result[i] = syslogEntry{
			Timestamp: e.Timestamp.Format("2006-01-02T15:04:05Z"),
			Level:     e.Level,
			Message:   e.Message,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
