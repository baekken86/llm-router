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

func TestRateLimitTracker_GetStatus(t *testing.T) {
	tracker := &RateLimitTracker{}

	statuses := tracker.GetStatus()
	if len(statuses) != 0 {
		t.Errorf("expected empty status, got %d entries", len(statuses))
	}

	tracker.MarkLimited(1, 10*time.Second)
	tracker.MarkLimited(2, 30*time.Second)

	statuses = tracker.GetStatus()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(statuses))
	}

	byID := make(map[int64]ProviderRateLimitStatus)
	for _, s := range statuses {
		byID[s.ProviderID] = s
	}

	s1, ok := byID[1]
	if !ok {
		t.Fatal("expected provider 1 in status")
	}
	if !s1.Limited {
		t.Error("expected provider 1 to be limited")
	}
	if s1.Remaining <= 0 || s1.Remaining > 10*time.Second {
		t.Errorf("expected provider 1 remaining 0-10s, got %v", s1.Remaining)
	}

	s2, ok := byID[2]
	if !ok {
		t.Fatal("expected provider 2 in status")
	}
	if !s2.Limited {
		t.Error("expected provider 2 to be limited")
	}
	if s2.Remaining <= 0 || s2.Remaining > 30*time.Second {
		t.Errorf("expected provider 2 remaining 0-30s, got %v", s2.Remaining)
	}
}

func TestRateLimitTracker_Clear(t *testing.T) {
	tracker := &RateLimitTracker{}
	tracker.MarkLimited(1, 10*time.Second)

	limited, _ := tracker.IsLimited(1)
	if !limited {
		t.Error("expected limited before clear")
	}

	tracker.Clear(1)

	limited, _ = tracker.IsLimited(1)
	if limited {
		t.Error("expected not limited after clear")
	}

	statuses := tracker.GetStatus()
	if len(statuses) != 0 {
		t.Errorf("expected empty status after clear, got %d entries", len(statuses))
	}
}

func TestRateLimitTracker_GetStatus_CleansExpired(t *testing.T) {
	tracker := &RateLimitTracker{}
	tracker.MarkLimited(1, 50*time.Millisecond)
	tracker.MarkLimited(2, 10*time.Second)

	time.Sleep(100 * time.Millisecond)

	statuses := tracker.GetStatus()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 entry after expiry, got %d", len(statuses))
	}
	if statuses[0].ProviderID != 2 {
		t.Errorf("expected provider 2, got provider %d", statuses[0].ProviderID)
	}
}

func TestRateLimitTracker_ModelNotLimited(t *testing.T) {
	tracker := &RateLimitTracker{}
	limited, remaining := tracker.IsModelLimited(1, "gpt-x")
	if limited {
		t.Error("expected not limited for unknown model")
	}
	if remaining != 0 {
		t.Errorf("expected 0 remaining, got %v", remaining)
	}
}

func TestRateLimitTracker_ModelMarkAndCheck(t *testing.T) {
	tracker := &RateLimitTracker{}
	tracker.MarkModelLimited(1, "gpt-x", 10*time.Second)

	limited, remaining := tracker.IsModelLimited(1, "gpt-x")
	if !limited {
		t.Error("expected limited after MarkModelLimited")
	}
	if remaining <= 0 || remaining > 10*time.Second {
		t.Errorf("expected remaining between 0-10s, got %v", remaining)
	}
}

func TestRateLimitTracker_ModelDefaultCooldown(t *testing.T) {
	tracker := &RateLimitTracker{}
	tracker.MarkModelLimited(1, "gpt-x", 0) // zero means default 60s

	limited, remaining := tracker.IsModelLimited(1, "gpt-x")
	if !limited {
		t.Error("expected limited with default cooldown")
	}
	if remaining < 50*time.Second || remaining > 60*time.Second {
		t.Errorf("expected ~60s remaining, got %v", remaining)
	}
}

func TestRateLimitTracker_ModelIndependentModels(t *testing.T) {
	tracker := &RateLimitTracker{}
	tracker.MarkModelLimited(1, "gpt-x", 10*time.Second)

	limitedX, _ := tracker.IsModelLimited(1, "gpt-x")
	limitedY, _ := tracker.IsModelLimited(1, "gpt-y")
	if !limitedX {
		t.Error("expected model gpt-x limited")
	}
	if limitedY {
		t.Error("expected model gpt-y not limited")
	}
}

func TestRateLimitTracker_ModelExpiry(t *testing.T) {
	tracker := &RateLimitTracker{}
	tracker.MarkModelLimited(1, "gpt-x", 50*time.Millisecond)

	time.Sleep(100 * time.Millisecond)

	limited, _ := tracker.IsModelLimited(1, "gpt-x")
	if limited {
		t.Error("expected not limited after cooldown expired")
	}
}

func TestRateLimitTracker_ModelClear(t *testing.T) {
	tracker := &RateLimitTracker{}
	tracker.MarkModelLimited(1, "gpt-x", 10*time.Second)

	limited, _ := tracker.IsModelLimited(1, "gpt-x")
	if !limited {
		t.Error("expected limited before clear")
	}

	tracker.ClearModel(1, "gpt-x")

	limited, _ = tracker.IsModelLimited(1, "gpt-x")
	if limited {
		t.Error("expected not limited after clear")
	}

	statuses := tracker.GetModelStatus()
	if len(statuses) != 0 {
		t.Errorf("expected empty model status after clear, got %d entries", len(statuses))
	}
}

func TestRateLimitTracker_ModelAndProviderIsolation(t *testing.T) {
	tracker := &RateLimitTracker{}

	// Marking a model limited must NOT mark its provider limited.
	tracker.MarkModelLimited(1, "gpt-x", 10*time.Second)
	if limited, _ := tracker.IsModelLimited(1, "gpt-x"); !limited {
		t.Error("expected model gpt-x limited")
	}
	if limited, _ := tracker.IsLimited(1); limited {
		t.Error("expected provider 1 not limited when only model marked")
	}

	// Marking a provider limited must NOT mark its models limited.
	tracker2 := &RateLimitTracker{}
	tracker2.MarkLimited(2, 10*time.Second)
	if limited, _ := tracker2.IsLimited(2); !limited {
		t.Error("expected provider 2 limited")
	}
	if limited, _ := tracker2.IsModelLimited(2, "gpt-x"); limited {
		t.Error("expected model gpt-x not limited when only provider marked")
	}
}

func TestRateLimitTracker_GetModelStatus(t *testing.T) {
	tracker := &RateLimitTracker{}

	statuses := tracker.GetModelStatus()
	if len(statuses) != 0 {
		t.Errorf("expected empty model status, got %d entries", len(statuses))
	}

	tracker.MarkModelLimited(1, "gpt-x", 10*time.Second)
	tracker.MarkModelLimited(2, "claude-y", 30*time.Second)

	statuses = tracker.GetModelStatus()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 model entries, got %d", len(statuses))
	}

	byKey := make(map[string]ModelRateLimitStatus)
	for _, s := range statuses {
		byKey[modelKey(s.ProviderID, s.ModelName)] = s
	}

	s1, ok := byKey[modelKey(1, "gpt-x")]
	if !ok {
		t.Fatal("expected model gpt-x in status")
	}
	if !s1.Limited {
		t.Error("expected model gpt-x to be limited")
	}
	if s1.Remaining <= 0 || s1.Remaining > 10*time.Second {
		t.Errorf("expected gpt-x remaining 0-10s, got %v", s1.Remaining)
	}
	if s1.Until.IsZero() {
		t.Error("expected non-zero until timestamp")
	}

	s2, ok := byKey[modelKey(2, "claude-y")]
	if !ok {
		t.Fatal("expected model claude-y in status")
	}
	if !s2.Limited {
		t.Error("expected model claude-y to be limited")
	}
	if s2.Remaining <= 0 || s2.Remaining > 30*time.Second {
		t.Errorf("expected claude-y remaining 0-30s, got %v", s2.Remaining)
	}
}

func TestRateLimitTracker_GetModelStatus_CleansExpired(t *testing.T) {
	tracker := &RateLimitTracker{}
	tracker.MarkModelLimited(1, "expired-model", 50*time.Millisecond)
	tracker.MarkModelLimited(2, "live-model", 10*time.Second)

	time.Sleep(100 * time.Millisecond)

	statuses := tracker.GetModelStatus()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 model entry after expiry, got %d", len(statuses))
	}
	if statuses[0].ModelName != "live-model" {
		t.Errorf("expected live-model, got %s", statuses[0].ModelName)
	}
}

func TestClassifyRateLimit(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		expected time.Duration
	}{
		{"daily allocation", []byte("you have used up your daily free allocation"), quotaCooldownDaily},
		{"neurons exceeded", []byte("limit: neurons exceeded"), quotaCooldownDaily},
		{"DAILY case-insensitive", []byte("DAILY limit hit"), quotaCooldownDaily},
		{"weekly usage limit", []byte("you have reached your weekly usage limit"), quotaCooldownWeekly},
		{"weekly quota", []byte("weekly quota"), quotaCooldownWeekly},
		{"generic rate limit no keyword", []byte("rate limit exceeded, try later"), 0},
		{"empty body", []byte{}, 0},
		{"nil body", nil, 0},
		{"mixed case daily allocation", []byte("Daily FREE Allocation exceeded"), quotaCooldownDaily},
		{"daily wins over weekly", []byte("daily and weekly both present"), quotaCooldownDaily},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyRateLimit(tt.body)
			if got != tt.expected {
				t.Errorf("classifyRateLimit(%q) = %v, want %v", tt.body, got, tt.expected)
			}
		})
	}
}
