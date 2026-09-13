package models

import (
	"time"
)

// GlobalSortCondition is a named sort expression that applies by default to
// all virtual models. Enabled conditions (ordered by Position) are prepended
// to each VM's own sort_expr, minus the conditions disabled for that VM.
type GlobalSortCondition struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	SortExpr    SortExpr  `json:"sort_expr"`
	Enabled     bool      `json:"enabled"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateGlobalSortConditionRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	SortExpr    SortExpr `json:"sort_expr"`
	Enabled     *bool    `json:"enabled,omitempty"`
	Position    *int     `json:"position,omitempty"`
}

type UpdateGlobalSortConditionRequest struct {
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	SortExpr    *SortExpr `json:"sort_expr,omitempty"`
	Enabled     *bool     `json:"enabled,omitempty"`
	Position    *int      `json:"position,omitempty"`
}
