package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/proxy"
	"github.com/chris/llm-router/internal/service"
)

type StatusHandler struct {
	engine          *proxy.Engine
	providerService service.ProviderService
}

func NewStatusHandler(engine *proxy.Engine, providerService service.ProviderService) *StatusHandler {
	return &StatusHandler{
		engine:          engine,
		providerService: providerService,
	}
}

func (h *StatusHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.GetStatus)
	r.Post("/clear/{providerID}", h.ClearRateLimit)
	return r
}

type ProviderStatus struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	RateLimited  bool   `json:"rate_limited"`
	RetryIn      string `json:"retry_in,omitempty"`
	OAuthConfigured bool `json:"oauth_configured"`
	APIKeyConfigured bool `json:"api_key_configured"`
}

type StatusResponse struct {
	Providers []ProviderStatus `json:"providers"`
}

func (h *StatusHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerService.List(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list providers"}`, http.StatusInternalServerError)
		return
	}

	rateLimits := h.engine.GetRateLimitStatus()
	rlMap := make(map[int64]proxy.ProviderRateLimitStatus)
	for _, rl := range rateLimits {
		rlMap[rl.ProviderID] = rl
	}

	var result []ProviderStatus
	for _, p := range providers {
		ps := ProviderStatus{
			ID:   p.ID,
			Name: p.Name,
		}

		if rl, ok := rlMap[p.ID]; ok {
			ps.RateLimited = rl.Limited
			if rl.Limited {
				ps.RetryIn = rl.Remaining.Round(1e6).String()
			}
		}

		result = append(result, ps)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StatusResponse{Providers: result})
}

func (h *StatusHandler) ClearRateLimit(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "providerID")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid provider ID"}`, http.StatusBadRequest)
		return
	}

	h.engine.ClearRateLimit(id)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}
