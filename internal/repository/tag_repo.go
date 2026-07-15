package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/chris/llm-router/internal/models"
)

type TagRepository interface {
	Set(ctx context.Context, modelID int64, effort string, tags map[string]string) error
	GetByModel(ctx context.Context, modelID int64) ([]models.Tag, error)
	GetByModelEffort(ctx context.Context, modelID int64, effort string) ([]models.Tag, error)
	GetAvailableEfforts(ctx context.Context, modelID int64) ([]string, error)
	DeleteByModel(ctx context.Context, modelID int64) error
}

type sqliteTagRepo struct {
	db *sql.DB
}

func NewTagRepository(db *sql.DB) TagRepository {
	return &sqliteTagRepo{db: db}
}

func (r *sqliteTagRepo) Set(ctx context.Context, modelID int64, effort string, tags map[string]string) error {
	if effort == "" {
		effort = "default"
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `DELETE FROM model_tags WHERE model_id = ? AND reasoning_effort = ?`, modelID, effort)
	if err != nil {
		return fmt.Errorf("delete old tags: %w", err)
	}

	for key, value := range tags {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO model_tags (model_id, reasoning_effort, key, value) VALUES (?, ?, ?, ?)`,
			modelID, effort, key, value,
		)
		if err != nil {
			return fmt.Errorf("insert tag %s: %w", key, err)
		}
	}

	return tx.Commit()
}

func (r *sqliteTagRepo) GetByModel(ctx context.Context, modelID int64) ([]models.Tag, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, model_id, reasoning_effort, key, value, created_at FROM model_tags WHERE model_id = ? ORDER BY reasoning_effort, key`, modelID,
	)
	if err != nil {
		return nil, fmt.Errorf("get tags: %w", err)
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.ModelID, &t.ReasoningEffort, &t.Key, &t.Value, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, nil
}

func (r *sqliteTagRepo) GetByModelEffort(ctx context.Context, modelID int64, effort string) ([]models.Tag, error) {
	if effort == "" {
		effort = "default"
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, model_id, reasoning_effort, key, value, created_at FROM model_tags WHERE model_id = ? AND reasoning_effort = ? ORDER BY key`, modelID, effort,
	)
	if err != nil {
		return nil, fmt.Errorf("get tags: %w", err)
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.ModelID, &t.ReasoningEffort, &t.Key, &t.Value, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, nil
}

func (r *sqliteTagRepo) GetAvailableEfforts(ctx context.Context, modelID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT reasoning_effort FROM model_tags WHERE model_id = ? ORDER BY reasoning_effort`, modelID,
	)
	if err != nil {
		return nil, fmt.Errorf("get efforts: %w", err)
	}
	defer rows.Close()

	var efforts []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, fmt.Errorf("scan effort: %w", err)
		}
		efforts = append(efforts, e)
	}
	return efforts, nil
}

func (r *sqliteTagRepo) DeleteByModel(ctx context.Context, modelID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM model_tags WHERE model_id = ?`, modelID)
	if err != nil {
		return fmt.Errorf("delete tags: %w", err)
	}
	return nil
}
