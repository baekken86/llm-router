package handlers

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/config"
	"github.com/chris/llm-router/internal/proxy"
)

type SettingsHandler struct {
	cfg    *config.Config
	engine *proxy.Engine
	mu     sync.Mutex
}

func NewSettingsHandler(cfg *config.Config, engine *proxy.Engine) *SettingsHandler {
	return &SettingsHandler{cfg: cfg, engine: engine}
}

func (h *SettingsHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.GetSettings)
	r.Put("/", h.UpdateSettings)
	return r
}

type settingsResponse struct {
	RTKEnabled     bool   `json:"rtk_enabled"`
	CavemanEnabled bool   `json:"caveman_enabled"`
	LogLevel       string `json:"log_level"`
	MaxRetries     int    `json:"max_retries"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxTokens      int    `json:"max_tokens"`

	CircuitBreakerEnabled             bool `json:"circuit_breaker_enabled"`
	CircuitBreakerModelThreshold      int  `json:"circuit_breaker_model_threshold"`
	CircuitBreakerModelWindowSec      int  `json:"circuit_breaker_model_window_sec"`
	CircuitBreakerModelCooldownSec    int  `json:"circuit_breaker_model_cooldown_sec"`
	CircuitBreakerProviderThreshold   int  `json:"circuit_breaker_provider_threshold"`
	CircuitBreakerProviderWindowSec   int  `json:"circuit_breaker_provider_window_sec"`
	CircuitBreakerProviderCooldownSec int  `json:"circuit_breaker_provider_cooldown_sec"`
	CircuitBreakerProviderMinModels   int  `json:"circuit_breaker_provider_min_models"`
}

type settingsUpdate struct {
	RTKEnabled     *bool   `json:"rtk_enabled,omitempty"`
	CavemanEnabled *bool   `json:"caveman_enabled,omitempty"`
	LogLevel       *string `json:"log_level,omitempty"`
	MaxRetries     *int    `json:"max_retries,omitempty"`
	TimeoutSeconds *int    `json:"timeout_seconds,omitempty"`
	MaxTokens      *int    `json:"max_tokens,omitempty"`

	CircuitBreakerEnabled             *bool `json:"circuit_breaker_enabled,omitempty"`
	CircuitBreakerModelThreshold      *int  `json:"circuit_breaker_model_threshold,omitempty"`
	CircuitBreakerModelWindowSec      *int  `json:"circuit_breaker_model_window_sec,omitempty"`
	CircuitBreakerModelCooldownSec    *int  `json:"circuit_breaker_model_cooldown_sec,omitempty"`
	CircuitBreakerProviderThreshold   *int  `json:"circuit_breaker_provider_threshold,omitempty"`
	CircuitBreakerProviderWindowSec   *int  `json:"circuit_breaker_provider_window_sec,omitempty"`
	CircuitBreakerProviderCooldownSec *int  `json:"circuit_breaker_provider_cooldown_sec,omitempty"`
	CircuitBreakerProviderMinModels   *int  `json:"circuit_breaker_provider_min_models,omitempty"`
}

func (h *SettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	s := h.cfg.Get()
	resp := settingsResponse{
		RTKEnabled:                       s.RTKEnabled,
		CavemanEnabled:                   s.CavemanEnabled,
		LogLevel:                         s.LogLevel,
		MaxRetries:                       s.MaxRetries,
		TimeoutSeconds:                   s.TimeoutSeconds,
		MaxTokens:                        s.MaxTokens,
		CircuitBreakerEnabled:            s.CircuitBreakerEnabled,
		CircuitBreakerModelThreshold:     s.CircuitBreakerModelThreshold,
		CircuitBreakerModelWindowSec:     s.CircuitBreakerModelWindowSec,
		CircuitBreakerModelCooldownSec:   s.CircuitBreakerModelCooldownSec,
		CircuitBreakerProviderThreshold:  s.CircuitBreakerProviderThreshold,
		CircuitBreakerProviderWindowSec:  s.CircuitBreakerProviderWindowSec,
		CircuitBreakerProviderCooldownSec: s.CircuitBreakerProviderCooldownSec,
		CircuitBreakerProviderMinModels:  s.CircuitBreakerProviderMinModels,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (h *SettingsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var update settingsUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	s := h.cfg.Get()

	if update.RTKEnabled != nil {
		s.RTKEnabled = *update.RTKEnabled
		h.engine.GetRTK().SetEnabled(*update.RTKEnabled)
	}
	if update.CavemanEnabled != nil {
		s.CavemanEnabled = *update.CavemanEnabled
		h.engine.GetCaveman().SetEnabled(*update.CavemanEnabled)
	}
	if update.LogLevel != nil {
		s.LogLevel = *update.LogLevel
	}
	if update.MaxRetries != nil {
		s.MaxRetries = clampInt(*update.MaxRetries, 0, 10)
	}
	if update.TimeoutSeconds != nil {
		s.TimeoutSeconds = clampInt(*update.TimeoutSeconds, 10, 600)
	}
	if update.MaxTokens != nil {
		s.MaxTokens = clampInt(*update.MaxTokens, 256, 1000000)
	}

	if update.CircuitBreakerEnabled != nil {
		s.CircuitBreakerEnabled = *update.CircuitBreakerEnabled
	}
	if update.CircuitBreakerModelThreshold != nil {
		s.CircuitBreakerModelThreshold = clampInt(*update.CircuitBreakerModelThreshold, 1, 1000)
	}
	if update.CircuitBreakerModelWindowSec != nil {
		s.CircuitBreakerModelWindowSec = clampInt(*update.CircuitBreakerModelWindowSec, 10, 3600)
	}
	if update.CircuitBreakerModelCooldownSec != nil {
		s.CircuitBreakerModelCooldownSec = clampInt(*update.CircuitBreakerModelCooldownSec, 10, 3600)
	}
	if update.CircuitBreakerProviderThreshold != nil {
		s.CircuitBreakerProviderThreshold = clampInt(*update.CircuitBreakerProviderThreshold, 1, 10000)
	}
	if update.CircuitBreakerProviderWindowSec != nil {
		s.CircuitBreakerProviderWindowSec = clampInt(*update.CircuitBreakerProviderWindowSec, 10, 3600)
	}
	if update.CircuitBreakerProviderCooldownSec != nil {
		s.CircuitBreakerProviderCooldownSec = clampInt(*update.CircuitBreakerProviderCooldownSec, 10, 3600)
	}
	if update.CircuitBreakerProviderMinModels != nil {
		s.CircuitBreakerProviderMinModels = clampInt(*update.CircuitBreakerProviderMinModels, 1, 100)
	}

	h.cfg.Set(s)
	h.engine.ApplySettings(s.MaxRetries, s.TimeoutSeconds, s.MaxTokens)

	if h.engine.GetCircuitBreaker() != nil {
		h.engine.GetCircuitBreaker().ApplySettings(proxy.CircuitBreakerSettings{
			Enabled:             s.CircuitBreakerEnabled,
			ModelThreshold:      s.CircuitBreakerModelThreshold,
			ModelWindowSec:      s.CircuitBreakerModelWindowSec,
			ModelCooldownSec:    s.CircuitBreakerModelCooldownSec,
			ProviderThreshold:   s.CircuitBreakerProviderThreshold,
			ProviderWindowSec:   s.CircuitBreakerProviderWindowSec,
			ProviderCooldownSec: s.CircuitBreakerProviderCooldownSec,
			ProviderMinModels:   s.CircuitBreakerProviderMinModels,
		})
	}

	resp := settingsResponse{
		RTKEnabled:                       s.RTKEnabled,
		CavemanEnabled:                   s.CavemanEnabled,
		LogLevel:                         s.LogLevel,
		MaxRetries:                       s.MaxRetries,
		TimeoutSeconds:                   s.TimeoutSeconds,
		MaxTokens:                        s.MaxTokens,
		CircuitBreakerEnabled:            s.CircuitBreakerEnabled,
		CircuitBreakerModelThreshold:     s.CircuitBreakerModelThreshold,
		CircuitBreakerModelWindowSec:     s.CircuitBreakerModelWindowSec,
		CircuitBreakerModelCooldownSec:   s.CircuitBreakerModelCooldownSec,
		CircuitBreakerProviderThreshold:  s.CircuitBreakerProviderThreshold,
		CircuitBreakerProviderWindowSec:  s.CircuitBreakerProviderWindowSec,
		CircuitBreakerProviderCooldownSec: s.CircuitBreakerProviderCooldownSec,
		CircuitBreakerProviderMinModels:  s.CircuitBreakerProviderMinModels,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
