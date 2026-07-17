package proxy

import (
	"sync"
	"time"
)

const defaultCooldown = 60 * time.Second

type RateLimitTracker struct {
	entries sync.Map // providerID (int64) -> cooldownUntil (time.Time)
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
