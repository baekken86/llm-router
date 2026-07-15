package models

import "time"

type Model struct {
	ID         int64     `json:"id"`
	ProviderID int64     `json:"provider_id"`
	Name       string    `json:"name"`
	Tags       []Tag     `json:"tags,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Tag struct {
	ID        int64     `json:"id"`
	ModelID   int64     `json:"model_id"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at"`
}

type SetTagsRequest struct {
	Tags map[string]string `json:"tags"`
}
