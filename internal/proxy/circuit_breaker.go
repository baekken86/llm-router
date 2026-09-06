package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chris/llm-router/internal/repository"
)

type CircuitBreakerSettings struct {
	Enabled             bool
	ModelThreshold      int
	ModelWindowSec      int
	ModelCooldownSec    int
	ProviderThreshold   int
	ProviderWindowSec   int
	ProviderCooldownSec int
	ProviderMinModels   int
}

const (
	// escalationMultiplier doubles the cooldown each time a re-enabled model
	// trips the breaker again.
	escalationMultiplier = 2
	// maxModelCooldownSec caps escalating cooldowns at 24h.
	maxModelCooldownSec = 86400
)

type CircuitBreaker struct {
	modelErrors       sync.Map // "providerID:modelName" -> []time.Time
	modelCooldowns    sync.Map // "providerID:modelName" -> time.Time
	providerCooldowns sync.Map // providerID (int64) -> time.Time
	modelStrikesMem   sync.Map // "providerID:modelName" -> struct{}; keys with strikes>0

	settings  CircuitBreakerSettings
	mu        sync.RWMutex
	modelRepo repository.ModelRepository
	provRepo  repository.ProviderRepository
	logger    *slog.Logger
	stopCh    chan struct{}
}

func NewCircuitBreaker(
	modelRepo repository.ModelRepository,
	provRepo repository.ProviderRepository,
	logger *slog.Logger,
) *CircuitBreaker {
	cb := &CircuitBreaker{
		modelRepo: modelRepo,
		provRepo:  provRepo,
		logger:    logger,
		stopCh:    make(chan struct{}),
		settings: CircuitBreakerSettings{
			Enabled:             true,
			ModelThreshold:      5,
			ModelWindowSec:      300,
			ModelCooldownSec:    600,
			ProviderThreshold:   10,
			ProviderWindowSec:   300,
			ProviderCooldownSec: 600,
			ProviderMinModels:   2,
		},
	}
	go cb.reenableLoop()
	return cb
}

func (cb *CircuitBreaker) ApplySettings(s CircuitBreakerSettings) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.settings = s
}

func (cb *CircuitBreaker) GetSettings() CircuitBreakerSettings {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.settings
}

func (cb *CircuitBreaker) Stop() {
	close(cb.stopCh)
}

func (cb *CircuitBreaker) Record5xx(ctx context.Context, providerID int64, modelName string) {
	cb.mu.RLock()
	s := cb.settings
	cb.mu.RUnlock()

	if !s.Enabled {
		return
	}

	count := cb.recordModelError(providerID, modelName, s)

	// Model-level check
	if count >= s.ModelThreshold {
		cb.disableModel(ctx, providerID, modelName, s.ModelCooldownSec)
	}

	// Provider-level check
	cb.checkProvider(ctx, providerID, s)
}

// recordModelError appends now to the model's sliding-window error log and
// returns the count of errors inside the window.
func (cb *CircuitBreaker) recordModelError(providerID int64, modelName string, s CircuitBreakerSettings) int {
	now := time.Now()
	key := modelKey(providerID, modelName)

	// Append to sliding window
	val, _ := cb.modelErrors.LoadOrStore(key, &modelWindow{})
	mw := val.(*modelWindow)
	mw.mu.Lock()
	mw.timestamps = append(mw.timestamps, now)
	window := time.Duration(s.ModelWindowSec) * time.Second
	cutoff := now.Add(-window)
	n := 0
	for _, t := range mw.timestamps {
		if t.After(cutoff) {
			mw.timestamps[n] = t
			n++
		}
	}
	mw.timestamps = mw.timestamps[:n]
	count := len(mw.timestamps)
	mw.mu.Unlock()
	return count
}

// RecordUnavailable counts an upstream "model unavailable" style error against
// the model-level breaker only. Stale catalog entries must not trip the
// provider-level breaker: the provider itself is healthy.
func (cb *CircuitBreaker) RecordUnavailable(ctx context.Context, providerID int64, modelName string) {
	cb.mu.RLock()
	s := cb.settings
	cb.mu.RUnlock()
	if !s.Enabled {
		return
	}
	count := cb.recordModelError(providerID, modelName, s)
	if count >= s.ModelThreshold {
		cb.disableModel(ctx, providerID, modelName, s.ModelCooldownSec)
	}
}

// RecordSuccess clears escalation strikes after a successful response, so a
// recovered model gets a fresh cooldown on its next failure.
func (cb *CircuitBreaker) RecordSuccess(providerID int64, modelName string) {
	key := modelKey(providerID, modelName)
	if _, has := cb.modelStrikesMem.Load(key); !has {
		return
	}
	cb.modelStrikesMem.Delete(key)
	if cb.modelRepo != nil {
		if model, err := cb.modelRepo.GetByProviderAndName(context.Background(), providerID, modelName); err == nil && model != nil {
			if err := cb.modelRepo.SetCBStrikes(context.Background(), model.ID, 0); err != nil {
				cb.logger.Error("circuit breaker: failed to reset strikes", "model", modelName, "provider_id", providerID, "error", err)
			}
		}
	}
}

func (cb *CircuitBreaker) checkProvider(ctx context.Context, providerID int64, s CircuitBreakerSettings) {
	window := time.Duration(s.ProviderWindowSec) * time.Second
	cutoff := time.Now().Add(-window)

	totalCount := 0
	modelsAffected := make(map[string]int)

	cb.modelErrors.Range(func(key, value interface{}) bool {
		k := key.(string)
		pID, mName := parseModelKey(k)
		if pID != providerID {
			return true
		}
		mw := value.(*modelWindow)
		mw.mu.Lock()
		c := 0
		for _, t := range mw.timestamps {
			if t.After(cutoff) {
				c++
			}
		}
		mw.mu.Unlock()
		if c > 0 {
			totalCount += c
			modelsAffected[mName] = c
		}
		return true
	})

	if totalCount >= s.ProviderThreshold && len(modelsAffected) >= s.ProviderMinModels {
		cb.disableProvider(ctx, providerID, s.ProviderCooldownSec)
	}
}

func (cb *CircuitBreaker) disableModel(ctx context.Context, providerID int64, modelName string, baseCooldownSec int) {
	key := modelKey(providerID, modelName)

	// Already in cooldown?
	if _, loaded := cb.modelCooldowns.Load(key); loaded {
		return
	}

	model, err := cb.modelRepo.GetByProviderAndName(ctx, providerID, modelName)
	if err != nil || model == nil || model.Disabled {
		return
	}

	// Load + increment persisted escalation strikes and compute the escalated
	// cooldown: each re-offense doubles the cooldown, capped at maxModelCooldownSec.
	strikes := 1
	if model != nil {
		if prev, err := cb.modelRepo.GetCBStrikes(ctx, model.ID); err == nil && prev > 0 {
			strikes = prev + 1
		}
	}
	cooldownSec := baseCooldownSec
	for i := 1; i < strikes; i++ {
		cooldownSec *= escalationMultiplier
		if cooldownSec >= maxModelCooldownSec {
			cooldownSec = maxModelCooldownSec
			break
		}
	}
	duration := time.Duration(cooldownSec) * time.Second

	if err := cb.modelRepo.ToggleDisabled(ctx, model.ID, true, &duration); err != nil {
		cb.logger.Error("circuit breaker: failed to disable model", "model", modelName, "provider_id", providerID, "error", err)
		return
	}

	if err := cb.modelRepo.SetCBStrikes(ctx, model.ID, strikes); err != nil {
		cb.logger.Error("circuit breaker: failed to record strikes", "model", modelName, "provider_id", providerID, "error", err)
	}
	cb.modelStrikesMem.Store(key, struct{}{})

	cooldown := time.Now().Add(duration)
	cb.modelCooldowns.Store(key, cooldown)
	cb.logger.Warn("circuit breaker: model disabled",
		"model", modelName,
		"provider_id", providerID,
		"cooldown", duration,
		"strikes", strikes,
	)
}

func (cb *CircuitBreaker) disableProvider(ctx context.Context, providerID int64, cooldownSec int) {
	// Already in cooldown?
	if _, loaded := cb.providerCooldowns.Load(providerID); loaded {
		return
	}

	p, err := cb.provRepo.GetByID(ctx, providerID)
	if err != nil || p == nil || p.Disabled {
		return
	}

	p.Disabled = true
	if err := cb.provRepo.Update(ctx, p); err != nil {
		cb.logger.Error("circuit breaker: failed to disable provider", "provider_id", providerID, "error", err)
		return
	}

	cooldown := time.Now().Add(time.Duration(cooldownSec) * time.Second)
	cb.providerCooldowns.Store(providerID, cooldown)
	cb.logger.Warn("circuit breaker: provider disabled",
		"provider_id", providerID,
		"cooldown", time.Duration(cooldownSec)*time.Second,
	)
}

// DisableModelPermanent disables a model with no expiry (e.g. HTTP 402
// subscription required). It bypasses the circuit-breaker Enabled setting and
// registers no cooldown, so the re-enable loop never auto re-enables it.
// Returns true only when the model transitioned disabled=0 → disabled=1.
func (cb *CircuitBreaker) DisableModelPermanent(ctx context.Context, providerID int64, modelName string) bool {
	model, err := cb.modelRepo.GetByProviderAndName(ctx, providerID, modelName)
	if err != nil || model == nil {
		cb.logger.Error("circuit breaker: permanent disable: model not found",
			"model", modelName, "provider_id", providerID, "error", err)
		return false
	}
	if model.Disabled {
		return false
	}

	if err := cb.modelRepo.ToggleDisabled(ctx, model.ID, true, nil); err != nil {
		cb.logger.Error("circuit breaker: failed to permanently disable model",
			"model", modelName, "provider_id", providerID, "error", err)
		return false
	}

	cb.logger.Warn("circuit breaker: model permanently disabled (subscription required)",
		"model", modelName,
		"provider_id", providerID,
	)
	return true
}

func (cb *CircuitBreaker) reenableLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-cb.stopCh:
			return
		case <-ticker.C:
			cb.reenableExpired()
		}
	}
}

func (cb *CircuitBreaker) reenableExpired() {
	ctx := context.Background()
	now := time.Now()

	// Re-enable models
	cb.modelCooldowns.Range(func(key, value interface{}) bool {
		expiry := value.(time.Time)
		if now.Before(expiry) {
			return true
		}
		k := key.(string)
		pID, mName := parseModelKey(k)

		model, err := cb.modelRepo.GetByProviderAndName(ctx, pID, mName)
		if err != nil || model == nil {
			cb.modelCooldowns.Delete(key)
			return true
		}

		if err := cb.modelRepo.ToggleDisabled(ctx, model.ID, false, nil); err != nil {
			cb.logger.Error("circuit breaker: failed to re-enable model", "model", mName, "provider_id", pID, "error", err)
			return true
		}

		cb.modelCooldowns.Delete(key)
		cb.modelErrors.Delete(key)
		cb.logger.Info("circuit breaker: model re-enabled", "model", mName, "provider_id", pID)
		return true
	})

	// Re-enable providers
	cb.providerCooldowns.Range(func(key, value interface{}) bool {
		expiry := value.(time.Time)
		if now.Before(expiry) {
			return true
		}
		providerID := key.(int64)

		p, err := cb.provRepo.GetByID(ctx, providerID)
		if err != nil || p == nil {
			cb.providerCooldowns.Delete(key)
			return true
		}

		p.Disabled = false
		if err := cb.provRepo.Update(ctx, p); err != nil {
			cb.logger.Error("circuit breaker: failed to re-enable provider", "provider_id", providerID, "error", err)
			return true
		}

		cb.providerCooldowns.Delete(key)
		cb.logger.Info("circuit breaker: provider re-enabled", "provider_id", providerID)
		return true
	})
}

type modelWindow struct {
	mu         sync.Mutex
	timestamps []time.Time
}

func modelKey(providerID int64, modelName string) string {
	return fmt.Sprintf("%d:%s", providerID, modelName)
}

func parseModelKey(key string) (int64, string) {
	for i, c := range key {
		if c == ':' {
			var id int64
			for _, ch := range key[:i] {
				id = id*10 + int64(ch-'0')
			}
			return id, key[i+1:]
		}
	}
	return 0, key
}

type CircuitBreakerStatus struct {
	ModelDisabled     bool   `json:"model_disabled"`
	ProviderDisabled  bool   `json:"provider_disabled"`
	CooldownRemaining string `json:"cooldown_remaining,omitempty"`
}

func (cb *CircuitBreaker) GetModelStatus(providerID int64, modelName string) CircuitBreakerStatus {
	key := modelKey(providerID, modelName)
	var status CircuitBreakerStatus

	if val, ok := cb.modelCooldowns.Load(key); ok {
		expiry := val.(time.Time)
		remaining := time.Until(expiry)
		if remaining > 0 {
			status.ModelDisabled = true
			status.CooldownRemaining = remaining.Round(time.Second).String()
		} else {
			cb.modelCooldowns.Delete(key)
		}
	}

	return status
}

func (cb *CircuitBreaker) GetProviderStatus(providerID int64) CircuitBreakerStatus {
	var status CircuitBreakerStatus

	if val, ok := cb.providerCooldowns.Load(providerID); ok {
		expiry := val.(time.Time)
		remaining := time.Until(expiry)
		if remaining > 0 {
			status.ProviderDisabled = true
			status.CooldownRemaining = remaining.Round(time.Second).String()
		} else {
			cb.providerCooldowns.Delete(providerID)
		}
	}

	return status
}
