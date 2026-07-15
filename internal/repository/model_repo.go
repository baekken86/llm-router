package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/chris/llm-router/internal/models"
)

type ModelRepository interface {
	Create(ctx context.Context, m *models.Model) error
	GetByID(ctx context.Context, id int64) (*models.Model, error)
	ListByProvider(ctx context.Context, providerID int64) ([]models.Model, error)
	ListAll(ctx context.Context) ([]models.Model, error)
	Delete(ctx context.Context, id int64) error
	Upsert(ctx context.Context, providerID int64, name string) (*models.Model, error)
}

type sqliteModelRepo struct {
	db *sql.DB
}

func NewModelRepository(db *sql.DB) ModelRepository {
	return &sqliteModelRepo{db: db}
}

func (r *sqliteModelRepo) Create(ctx context.Context, m *models.Model) error {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO models (provider_id, name) VALUES (?, ?)`,
		m.ProviderID, m.Name,
	)
	if err != nil {
		return fmt.Errorf("insert model: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	m.ID = id
	return nil
}

func (r *sqliteModelRepo) GetByID(ctx context.Context, id int64) (*models.Model, error) {
	m := &models.Model{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, provider_id, name, created_at FROM models WHERE id = ?`, id,
	).Scan(&m.ID, &m.ProviderID, &m.Name, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get model: %w", err)
	}
	return m, nil
}

func (r *sqliteModelRepo) ListByProvider(ctx context.Context, providerID int64) ([]models.Model, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, provider_id, name, created_at FROM models WHERE provider_id = ? ORDER BY name`, providerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer rows.Close()

	var result []models.Model
	for rows.Next() {
		var m models.Model
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.Name, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		result = append(result, m)
	}
	return result, nil
}

func (r *sqliteModelRepo) ListAll(ctx context.Context) ([]models.Model, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, provider_id, name, created_at FROM models ORDER BY provider_id, name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all models: %w", err)
	}
	defer rows.Close()

	var result []models.Model
	for rows.Next() {
		var m models.Model
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.Name, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		result = append(result, m)
	}
	return result, nil
}

func (r *sqliteModelRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM models WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete model: %w", err)
	}
	return nil
}

func (r *sqliteModelRepo) Upsert(ctx context.Context, providerID int64, name string) (*models.Model, error) {
	m := &models.Model{}
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO models (provider_id, name) VALUES (?, ?)
		 ON CONFLICT(provider_id, name) DO UPDATE SET name = excluded.name
		 RETURNING id, provider_id, name, created_at`,
		providerID, name,
	).Scan(&m.ID, &m.ProviderID, &m.Name, &m.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert model: %w", err)
	}
	return m, nil
}
