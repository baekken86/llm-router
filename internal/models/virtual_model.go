package models

import (
	"encoding/json"
	"time"
)

type VirtualModel struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	FilterExpr    json.RawMessage `json:"filter_expr"`
	SortExpr      json.RawMessage `json:"sort_expr"`
	MaxRetries    int             `json:"max_retries"`
	RetryOnStatus json.RawMessage `json:"retry_on_status"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type CreateVirtualModelRequest struct {
	Name          string          `json:"name"`
	FilterExpr    json.RawMessage `json:"filter_expr"`
	SortExpr      json.RawMessage `json:"sort_expr"`
	MaxRetries    *int            `json:"max_retries,omitempty"`
	RetryOnStatus json.RawMessage `json:"retry_on_status,omitempty"`
}

type UpdateVirtualModelRequest struct {
	Name          *string          `json:"name,omitempty"`
	FilterExpr    *json.RawMessage `json:"filter_expr,omitempty"`
	SortExpr      *json.RawMessage `json:"sort_expr,omitempty"`
	MaxRetries    *int             `json:"max_retries,omitempty"`
	RetryOnStatus *json.RawMessage `json:"retry_on_status,omitempty"`
}

type FilterExpr struct {
	And []FilterCondition `json:"and"`
}

type FilterCondition struct {
	Key    string      `json:"key"`
	Op     string      `json:"op"`
	Value  interface{} `json:"value"`
}

type SortExpr []SortCriterion

type SortCriterion struct {
	Key       string   `json:"key"`
	Direction string   `json:"direction,omitempty"`
	Order     []string `json:"order,omitempty"`
}
