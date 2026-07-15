package repository

import (
	"context"
	"database/sql"
	"fmt"
)

type GlobalMetadataRepository interface {
	Set(ctx context.Context, modelName, effort string, tags map[string]string) error
	GetByModel(ctx context.Context, modelName string) (map[string]map[string]string, error)
	GetByModelEffort(ctx context.Context, modelName, effort string) (map[string]string, error)
	ListModels(ctx context.Context) ([]string, error)
}

type sqliteGlobalMetadataRepo struct {
	db *sql.DB
}

func NewGlobalMetadataRepository(db *sql.DB) GlobalMetadataRepository {
	return &sqliteGlobalMetadataRepo{db: db}
}

func (r *sqliteGlobalMetadataRepo) Set(ctx context.Context, modelName, effort string, tags map[string]string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `DELETE FROM model_metadata_global WHERE model_name = ? AND reasoning_effort = ?`, modelName, effort)
	if err != nil {
		return fmt.Errorf("delete old: %w", err)
	}

	for key, value := range tags {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO model_metadata_global (model_name, reasoning_effort, key, value) VALUES (?, ?, ?, ?)`,
			modelName, effort, key, value,
		)
		if err != nil {
			return fmt.Errorf("insert %s: %w", key, err)
		}
	}

	return tx.Commit()
}

func (r *sqliteGlobalMetadataRepo) GetByModel(ctx context.Context, modelName string) (map[string]map[string]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT reasoning_effort, key, value FROM model_metadata_global WHERE model_name = ? ORDER BY reasoning_effort, key`, modelName,
	)
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	defer rows.Close()

	result := make(map[string]map[string]string)
	for rows.Next() {
		var effort, key, value string
		if err := rows.Scan(&effort, &key, &value); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if result[effort] == nil {
			result[effort] = make(map[string]string)
		}
		result[effort][key] = value
	}
	return result, nil
}

func (r *sqliteGlobalMetadataRepo) GetByModelEffort(ctx context.Context, modelName, effort string) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT key, value FROM model_metadata_global WHERE model_name = ? AND reasoning_effort = ? ORDER BY key`, modelName, effort,
	)
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		result[key] = value
	}
	return result, nil
}

func (r *sqliteGlobalMetadataRepo) ListModels(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT model_name FROM model_metadata_global ORDER BY model_name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	defer rows.Close()

	var models []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		models = append(models, name)
	}
	return models, nil
}
