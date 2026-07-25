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
	Enabled                bool
	ModelThreshold         int
	ModelWindowSec         int
	ModelCooldownSec       int
	ProviderThreshold      int
	ProviderWindowSec      int
	ProviderCooldownSec    int
	ProviderMinModels      int
}

type CircuitBreaker struct {
	modelErrors      sync.Map // "providerID:modelName" -> []time.Time
	modelCooldowns   sync.Map // "providerID:modelName" -> time.Time
	providerCooldowns sync.Map // providerID (int64) -> time.Time

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
		modelRepo:  modelRepo,
		provRepo:   provRepo,
		logger:     logger,
		stopCh:     make(chan struct{}),
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

	// Model-level check
	if count >= s.ModelThreshold {
		cb.disableModel(ctx, providerID, modelName, s.ModelCooldownSec)
	}

	// Provider-level check
	cb.checkProvider(ctx, providerID, s)
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

func (cb *CircuitBreaker) disableModel(ctx context.Context, providerID int64, modelName string, cooldownSec int) {
	key := modelKey(providerID, modelName)

	// Already in cooldown?
	if _, loaded := cb.modelCooldowns.Load(key); loaded {
		return
	}

	model, err := cb.modelRepo.GetByProviderAndName(ctx, providerID, modelName)
	if err != nil || model == nil || model.Disabled {
		return
	}

	if err := cb.modelRepo.ToggleDisabled(ctx, model.ID, true, nil); err != nil {
		cb.logger.Error("circuit breaker: failed to disable model", "model", modelName, "provider_id", providerID, "error", err)
		return
	}

	cooldown := time.Now().Add(time.Duration(cooldownSec) * time.Second)
	cb.modelCooldowns.Store(key, cooldown)
	cb.logger.Warn("circuit breaker: model disabled",
		"model", modelName,
		"provider_id", providerID,
		"cooldown", time.Duration(cooldownSec)*time.Second,
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
	ModelDisabled   bool   `json:"model_disabled"`
	ProviderDisabled bool  `json:"provider_disabled"`
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
