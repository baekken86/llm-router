package models

import "time"

type APIType string

const (
	APITypeOpenAI      APIType = "openai"
	APITypeAnthropic   APIType = "anthropic"
	APITypeCloudflare  APIType = "cloudflare"
	APITypeOllama      APIType = "ollama"
	APITypeOllamaCloud APIType = "ollama-cloud"
	APITypeCodex       APIType = "codex"
)

type Provider struct {
	ID              int64              `json:"id"`
	Name            string             `json:"name"`
	APIType         APIType            `json:"api_type"`
	BaseURL         string             `json:"base_url"`
	APIKeyEncrypted string             `json:"-"`
	AccountID       string             `json:"account_id,omitempty"`
	Disabled        bool               `json:"disabled"`
	DisabledUntil   *time.Time         `json:"disabled_until,omitempty"`
	Metadata        []ProviderMetadata `json:"metadata,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type ProviderMetadata struct {
	ID         int64     `json:"id"`
	ProviderID int64     `json:"provider_id"`
	Key        string    `json:"key"`
	Value      string    `json:"value"`
	CreatedAt  time.Time `json:"created_at"`
}

type CreateProviderRequest struct {
	Name      string            `json:"name"`
	APIType   APIType           `json:"api_type"`
	BaseURL   string            `json:"base_url"`
	APIKey    string            `json:"api_key"`
	AccountID string            `json:"account_id,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type UpdateProviderRequest struct {
	Name     *string            `json:"name,omitempty"`
	APIType  *APIType           `json:"api_type,omitempty"`
	BaseURL  *string            `json:"base_url,omitempty"`
	APIKey   *string            `json:"api_key,omitempty"`
	Metadata *map[string]string `json:"metadata,omitempty"`
	Disabled *bool              `json:"disabled,omitempty"`
	Duration *string            `json:"duration,omitempty"`
}

type OAuthToken struct {
	ID            int64      `json:"id"`
	ProviderID    int64      `json:"provider_id"`
	AccessToken   string     `json:"-"`
	RefreshToken  string     `json:"-"`
	ExpiresAt     time.Time  `json:"expires_at"`
	AccountID     string     `json:"account_id,omitempty"`
	Email         string     `json:"email,omitempty"`
	LastRefreshAt *time.Time `json:"last_refresh_at,omitempty"`
	IDToken       string     `json:"-"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
