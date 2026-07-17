package proxy

import (
	"testing"
	"time"
)

func TestRateLimitTracker_NotLimited(t *testing.T) {
	tracker := &RateLimitTracker{}
	limited, remaining := tracker.IsLimited(1)
	if limited {
		t.Error("expected not limited for unknown provider")
	}
	if remaining != 0 {
		t.Errorf("expected 0 remaining, got %v", remaining)
	}
}

func TestRateLimitTracker_MarkAndCheck(t *testing.T) {
	tracker := &RateLimitTracker{}
	tracker.MarkLimited(1, 10*time.Second)

	limited, remaining := tracker.IsLimited(1)
	if !limited {
		t.Error("expected limited after MarkLimited")
	}
	if remaining <= 0 || remaining > 10*time.Second {
		t.Errorf("expected remaining between 0-10s, got %v", remaining)
	}
}

func TestRateLimitTracker_DefaultCooldown(t *testing.T) {
	tracker := &RateLimitTracker{}
	tracker.MarkLimited(1, 0) // zero means default 60s

	limited, remaining := tracker.IsLimited(1)
	if !limited {
		t.Error("expected limited with default cooldown")
	}
	if remaining < 50*time.Second || remaining > 60*time.Second {
		t.Errorf("expected ~60s remaining, got %v", remaining)
	}
}

func TestRateLimitTracker_IndependentProviders(t *testing.T) {
	tracker := &RateLimitTracker{}
	tracker.MarkLimited(1, 10*time.Second)

	limited1, _ := tracker.IsLimited(1)
	limited2, _ := tracker.IsLimited(2)
	if !limited1 {
		t.Error("expected provider 1 limited")
	}
	if limited2 {
		t.Error("expected provider 2 not limited")
	}
}

func TestRateLimitTracker_Expiry(t *testing.T) {
	tracker := &RateLimitTracker{}
	tracker.MarkLimited(1, 50*time.Millisecond)

	time.Sleep(100 * time.Millisecond)

	limited, _ := tracker.IsLimited(1)
	if limited {
		t.Error("expected not limited after cooldown expired")
	}
}

func TestRateLimitTracker_UpdateCooldown(t *testing.T) {
	tracker := &RateLimitTracker{}
	tracker.MarkLimited(1, 10*time.Second)
	tracker.MarkLimited(1, 120*time.Second) // extend

	_, remaining := tracker.IsLimited(1)
	if remaining < 100*time.Second {
		t.Errorf("expected extended cooldown, got %v remaining", remaining)
	}
}
