package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/chris/llm-router/internal/models"
)

type TagRepository interface {
	Set(ctx context.Context, modelID int64, tags map[string]string) error
	GetByModel(ctx context.Context, modelID int64) ([]models.Tag, error)
	DeleteByModel(ctx context.Context, modelID int64) error
}

type sqliteTagRepo struct {
	db *sql.DB
}

func NewTagRepository(db *sql.DB) TagRepository {
	return &sqliteTagRepo{db: db}
}

func (r *sqliteTagRepo) Set(ctx context.Context, modelID int64, tags map[string]string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `DELETE FROM model_tags WHERE model_id = ?`, modelID)
	if err != nil {
		return fmt.Errorf("delete old tags: %w", err)
	}

	for key, value := range tags {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO model_tags (model_id, key, value) VALUES (?, ?, ?)`,
			modelID, key, value,
		)
		if err != nil {
			return fmt.Errorf("insert tag %s: %w", key, err)
		}
	}

	return tx.Commit()
}

func (r *sqliteTagRepo) GetByModel(ctx context.Context, modelID int64) ([]models.Tag, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, model_id, key, value, created_at FROM model_tags WHERE model_id = ? ORDER BY key`, modelID,
	)
	if err != nil {
		return nil, fmt.Errorf("get tags: %w", err)
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.ModelID, &t.Key, &t.Value, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, nil
}

func (r *sqliteTagRepo) DeleteByModel(ctx context.Context, modelID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM model_tags WHERE model_id = ?`, modelID)
	if err != nil {
		return fmt.Errorf("delete tags: %w", err)
	}
	return nil
}
