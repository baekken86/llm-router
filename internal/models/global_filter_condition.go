package models

import (
	"time"
)

// GlobalFilterCondition is a named filter expression that applies by default
// to all virtual models. Enabled conditions (ordered by Priority, then
// Position) are combined with each VM's own filter_expr as an AND tree by
// priority, minus the conditions disabled for that VM.
type GlobalFilterCondition struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	FilterExpr  FilterNode `json:"filter_expr"`
	Enabled     bool       `json:"enabled"`
	Position    int        `json:"position"`
	Priority    int        `json:"priority"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CreateGlobalFilterConditionRequest struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	FilterExpr  FilterNode `json:"filter_expr"`
	Enabled     *bool      `json:"enabled,omitempty"`
	Position    *int       `json:"position,omitempty"`
	Priority    *int       `json:"priority,omitempty"`
}

type UpdateGlobalFilterConditionRequest struct {
	Name        *string     `json:"name,omitempty"`
	Description *string     `json:"description,omitempty"`
	FilterExpr  *FilterNode `json:"filter_expr,omitempty"`
	Enabled     *bool       `json:"enabled,omitempty"`
	Position    *int        `json:"position,omitempty"`
	Priority    *int        `json:"priority,omitempty"`
}
