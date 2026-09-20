package models

import "time"

type Model struct {
	ID            int64      `json:"id"`
	ProviderID    int64      `json:"provider_id"`
	Name          string     `json:"name"`
	Disabled       bool       `json:"disabled"`
	DisabledUntil  *time.Time `json:"disabled_until,omitempty"`
	DisabledReason string     `json:"disabled_reason,omitempty"`
	Tags          []Tag      `json:"tags,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`

	// RateLimitIsolated: when true, a 429 from this model only cools down
	// this model — not the whole provider — and provider-level cooldowns
	// never block requests for it.
	RateLimitIsolated bool `json:"rate_limit_isolated"`
}

type Tag struct {
	ID               int64     `json:"id"`
	ModelID          int64     `json:"model_id"`
	ReasoningEffort  string    `json:"reasoning_effort"`
	Key              string    `json:"key"`
	Value            string    `json:"value"`
	CreatedAt        time.Time `json:"created_at"`
}

type SetTagsRequest struct {
	Tags map[string]string `json:"tags"`
}
