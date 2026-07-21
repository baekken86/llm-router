package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type OverrideRow struct {
	ID              int64     `json:"id"`
	ModelID         int64     `json:"model_id"`
	ReasoningEffort string    `json:"reasoning_effort"`
	Key             string    `json:"key"`
	Value           string    `json:"value"`
	CreatedAt       time.Time `json:"created_at"`
}

type ModelOverrideRepository interface {
	GetByModelAndEffort(ctx context.Context, modelID int64, reasoningEffort string) ([]OverrideRow, error)
	GetEffortsByModel(ctx context.Context, modelID int64) ([]string, error)
	Set(ctx context.Context, modelID int64, reasoningEffort string, key string, value string) error
	Delete(ctx context.Context, modelID int64, reasoningEffort string, key string) error
	DeleteAll(ctx context.Context, modelID int64, reasoningEffort string) error
}

type sqliteModelOverrideRepo struct {
	db *sql.DB
}

func NewModelOverrideRepository(db *sql.DB) ModelOverrideRepository {
	return &sqliteModelOverrideRepo{db: db}
}

func (r *sqliteModelOverrideRepo) GetByModelAndEffort(ctx context.Context, modelID int64, reasoningEffort string) ([]OverrideRow, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, model_id, reasoning_effort, key, value, created_at
		 FROM model_overrides
		 WHERE model_id = ? AND reasoning_effort = ?
		 ORDER BY key`, modelID, reasoningEffort,
	)
	if err != nil {
		return nil, fmt.Errorf("get overrides: %w", err)
	}
	defer rows.Close()

	var result []OverrideRow
	for rows.Next() {
		var o OverrideRow
		if err := rows.Scan(&o.ID, &o.ModelID, &o.ReasoningEffort, &o.Key, &o.Value, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan override: %w", err)
		}
		result = append(result, o)
	}
	return result, nil
}

func (r *sqliteModelOverrideRepo) GetEffortsByModel(ctx context.Context, modelID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT reasoning_effort FROM model_overrides WHERE model_id = ? ORDER BY reasoning_effort`, modelID,
	)
	if err != nil {
		return nil, fmt.Errorf("get override efforts: %w", err)
	}
	defer rows.Close()
	var efforts []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, fmt.Errorf("scan override effort: %w", err)
		}
		efforts = append(efforts, e)
	}
	return efforts, nil
}

func (r *sqliteModelOverrideRepo) Set(ctx context.Context, modelID int64, reasoningEffort string, key string, value string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO model_overrides (model_id, reasoning_effort, key, value)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (model_id, reasoning_effort, key) DO UPDATE SET value = excluded.value`,
		modelID, reasoningEffort, key, value,
	)
	if err != nil {
		return fmt.Errorf("set override: %w", err)
	}
	return nil
}

func (r *sqliteModelOverrideRepo) Delete(ctx context.Context, modelID int64, reasoningEffort string, key string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM model_overrides WHERE model_id = ? AND reasoning_effort = ? AND key = ?`,
		modelID, reasoningEffort, key,
	)
	if err != nil {
		return fmt.Errorf("delete override: %w", err)
	}
	return nil
}

func (r *sqliteModelOverrideRepo) DeleteAll(ctx context.Context, modelID int64, reasoningEffort string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM model_overrides WHERE model_id = ? AND reasoning_effort = ?`,
		modelID, reasoningEffort,
	)
	if err != nil {
		return fmt.Errorf("delete all overrides: %w", err)
	}
	return nil
}
