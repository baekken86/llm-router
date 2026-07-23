package proxy

import (
	"strings"
	"sync"
	"time"
)

const defaultCooldown = 60 * time.Second

const (
	quotaCooldownDaily  = 12 * time.Hour
	quotaCooldownWeekly = 72 * time.Hour
)

// classifyRateLimit inspects the raw 429 response body for quota exhaustion
// keywords and returns an appropriate extended cooldown duration.
// Returns 0 if no quota keywords found (caller uses Retry-After or default).
func classifyRateLimit(body []byte) time.Duration {
	if len(body) == 0 {
		return 0
	}
	lower := strings.ToLower(string(body))
	// Daily quota keywords (checked first — longer cooldown wins)
	for _, kw := range []string{"daily", "allocation", "neurons"} {
		if strings.Contains(lower, kw) {
			return quotaCooldownDaily
		}
	}
	// Weekly quota keywords
	if strings.Contains(lower, "weekly") {
		return quotaCooldownWeekly
	}
	return 0
}

type RateLimitTracker struct {
	entries sync.Map // providerID (int64) -> cooldownUntil (time.Time)
}

type ProviderRateLimitStatus struct {
	ProviderID int64         `json:"provider_id"`
	Limited    bool          `json:"limited"`
	Remaining  time.Duration `json:"remaining"`
	Until      time.Time     `json:"until"`
}

func (t *RateLimitTracker) MarkLimited(providerID int64, retryAfter time.Duration) {
	cooldown := retryAfter
	if cooldown <= 0 {
		cooldown = defaultCooldown
	}
	t.entries.Store(providerID, time.Now().Add(cooldown))
}

func (t *RateLimitTracker) IsLimited(providerID int64) (bool, time.Duration) {
	val, ok := t.entries.Load(providerID)
	if !ok {
		return false, 0
	}
	until := val.(time.Time)
	remaining := time.Until(until)
	if remaining <= 0 {
		t.entries.Delete(providerID)
		return false, 0
	}
	return true, remaining
}

func (t *RateLimitTracker) GetStatus() []ProviderRateLimitStatus {
	var statuses []ProviderRateLimitStatus
	t.entries.Range(func(key, value interface{}) bool {
		providerID := key.(int64)
		until := value.(time.Time)
		remaining := time.Until(until)
		if remaining <= 0 {
			t.entries.Delete(key)
			return true
		}
		statuses = append(statuses, ProviderRateLimitStatus{
			ProviderID: providerID,
			Limited:    true,
			Remaining:  remaining,
			Until:      until,
		})
		return true
	})
	return statuses
}

func (t *RateLimitTracker) Clear(providerID int64) {
	t.entries.Delete(providerID)
}
