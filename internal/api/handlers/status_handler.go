package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/proxy"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
)

type StatusHandler struct {
	engine          *proxy.Engine
	providerService service.ProviderService
	oauthService    service.OAuthService
	providerMetaRepo repository.ProviderMetadataRepository
}

func NewStatusHandler(engine *proxy.Engine, providerService service.ProviderService, oauthService service.OAuthService, providerMetaRepo repository.ProviderMetadataRepository) *StatusHandler {
	return &StatusHandler{
		engine:          engine,
		providerService: providerService,
		oauthService:    oauthService,
		providerMetaRepo: providerMetaRepo,
	}
}

func (h *StatusHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.GetStatus)
	r.Post("/clear/{providerID}", h.ClearRateLimit)
	r.Put("/{providerID}/metadata", h.UpdateMetadata)
	return r
}

type ProviderStatus struct {
	ID               int64             `json:"id"`
	Name             string            `json:"name"`
	RateLimited      bool              `json:"rate_limited"`
	RetryIn          string            `json:"retry_in,omitempty"`
	OAuthConfigured  bool              `json:"oauth_configured"`
	OAuthExpired     bool              `json:"oauth_expired,omitempty"`
	OAuthExpiresAt   string            `json:"oauth_expires_at,omitempty"`
	OAuthEmail       string            `json:"oauth_email,omitempty"`
	APIKeyConfigured bool              `json:"api_key_configured"`
	BaseURL          string            `json:"base_url"`
	AccountID        string            `json:"account_id,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	Disabled         bool              `json:"disabled"`
	CircuitBroken    bool              `json:"circuit_broken"`
	CBCooldownRemaining string         `json:"cb_cooldown_remaining,omitempty"`
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

	// Fetch provider metadata
	providerMetaMap := make(map[int64]map[string]string)
	if h.providerMetaRepo != nil {
		providerMetaMap, _ = h.providerMetaRepo.ListAll(r.Context())
	}

	var result []ProviderStatus
	for _, p := range providers {
		ps := ProviderStatus{
			ID:        p.ID,
			Name:      p.Name,
			BaseURL:   p.BaseURL,
			AccountID: p.AccountID,
			Disabled:  p.Disabled,
		}

		if meta, ok := providerMetaMap[p.ID]; ok {
			ps.Metadata = meta
		}

		if rl, ok := rlMap[p.ID]; ok {
			ps.RateLimited = rl.Limited
			if rl.Limited {
				ps.RetryIn = rl.Remaining.Round(time.Second).String()
			}
		}

		if cb := h.engine.GetCircuitBreaker(); cb != nil {
			cbStatus := cb.GetProviderStatus(p.ID)
			ps.CircuitBroken = cbStatus.ProviderDisabled
			if cbStatus.ProviderDisabled {
				ps.CBCooldownRemaining = cbStatus.CooldownRemaining
			}
		}

		// Check OAuth token
		connected, oauthToken, _ := h.oauthService.IsConnected(r.Context(), p.ID)
		if connected && oauthToken != nil {
			ps.OAuthConfigured = true
			ps.OAuthEmail = oauthToken.Email
			if !oauthToken.ExpiresAt.IsZero() {
				ps.OAuthExpiresAt = oauthToken.ExpiresAt.Format(time.RFC3339)
				if time.Now().After(oauthToken.ExpiresAt) {
					ps.OAuthExpired = true
				}
			}
		}

		// Check API key
		if p.APIKeyEncrypted != "" {
			ps.APIKeyConfigured = true
		}

		result = append(result, ps)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StatusResponse{Providers: result})
}

func (h *StatusHandler) UpdateMetadata(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "providerID")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid provider ID"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if h.providerMetaRepo == nil {
		http.Error(w, `{"error":"metadata not supported"}`, http.StatusInternalServerError)
		return
	}

	if err := h.providerMetaRepo.UpsertKey(r.Context(), id, req.Key, req.Value); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
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
