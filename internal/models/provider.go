package models

import "time"

type APIType string

const (
	APITypeOpenAI    APIType = "openai"
	APITypeAnthropic APIType = "anthropic"
)

type Provider struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	APIType         APIType   `json:"api_type"`
	BaseURL         string    `json:"base_url"`
	APIKeyEncrypted string    `json:"-"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreateProviderRequest struct {
	Name    string  `json:"name"`
	APIType APIType `json:"api_type"`
	BaseURL string  `json:"base_url"`
	APIKey  string  `json:"api_key"`
}

type UpdateProviderRequest struct {
	Name    *string  `json:"name,omitempty"`
	APIType *APIType `json:"api_type,omitempty"`
	BaseURL *string  `json:"base_url,omitempty"`
	APIKey  *string  `json:"api_key,omitempty"`
}

type OAuthToken struct {
	ID           int64     `json:"id"`
	ProviderID   int64     `json:"provider_id"`
	AccessToken  string    `json:"-"`
	RefreshToken string    `json:"-"`
	ExpiresAt    time.Time `json:"expires_at"`
	AccountID    string    `json:"account_id,omitempty"`
	Email        string    `json:"email,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
