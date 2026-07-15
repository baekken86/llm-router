package models

import "time"

type ProxyKey struct {
	ID          int64     `json:"id"`
	KeyHash     string    `json:"-"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type CreateKeyRequest struct {
	Description string `json:"description"`
}

type CreateKeyResponse struct {
	ID          int64     `json:"id"`
	Key         string    `json:"key"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}
