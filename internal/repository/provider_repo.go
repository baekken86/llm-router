package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chris/llm-router/internal/models"
)

type ProviderRepository interface {
	Create(ctx context.Context, p *models.Provider) error
	GetByID(ctx context.Context, id int64) (*models.Provider, error)
	GetByName(ctx context.Context, name string) (*models.Provider, error)
	List(ctx context.Context) ([]models.Provider, error)
	ListByMetadata(ctx context.Context, filters map[string]string) ([]models.Provider, error)
	Update(ctx context.Context, p *models.Provider) error
	Delete(ctx context.Context, id int64) error
}

type sqliteProviderRepo struct {
	db *sql.DB
}

func NewProviderRepository(db *sql.DB) ProviderRepository {
	return &sqliteProviderRepo{db: db}
}

func (r *sqliteProviderRepo) Create(ctx context.Context, p *models.Provider) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO providers (name, api_type, base_url, api_key_encrypted, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		p.Name, p.APIType, p.BaseURL, p.APIKeyEncrypted, now, now,
	)
	if err != nil {
		return fmt.Errorf("insert provider: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	p.ID = id
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

func (r *sqliteProviderRepo) GetByID(ctx context.Context, id int64) (*models.Provider, error) {
	p := &models.Provider{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, api_type, base_url, api_key_encrypted, created_at, updated_at
		 FROM providers WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.APIType, &p.BaseURL, &p.APIKeyEncrypted, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	return p, nil
}

func (r *sqliteProviderRepo) GetByName(ctx context.Context, name string) (*models.Provider, error) {
	p := &models.Provider{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, api_type, base_url, api_key_encrypted, created_at, updated_at
		 FROM providers WHERE name = ?`, name,
	).Scan(&p.ID, &p.Name, &p.APIType, &p.BaseURL, &p.APIKeyEncrypted, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider by name: %w", err)
	}
	return p, nil
}

func (r *sqliteProviderRepo) List(ctx context.Context) ([]models.Provider, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, api_type, base_url, api_key_encrypted, created_at, updated_at FROM providers ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()

	var providers []models.Provider
	for rows.Next() {
		var p models.Provider
		if err := rows.Scan(&p.ID, &p.Name, &p.APIType, &p.BaseURL, &p.APIKeyEncrypted, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		providers = append(providers, p)
	}
	return providers, nil
}

func (r *sqliteProviderRepo) Update(ctx context.Context, p *models.Provider) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE providers SET name = ?, api_type = ?, base_url = ?, api_key_encrypted = ?, updated_at = ?
		 WHERE id = ?`,
		p.Name, p.APIType, p.BaseURL, p.APIKeyEncrypted, now, p.ID,
	)
	if err != nil {
		return fmt.Errorf("update provider: %w", err)
	}
	p.UpdatedAt = now
	return nil
}

func (r *sqliteProviderRepo) ListByMetadata(ctx context.Context, filters map[string]string) ([]models.Provider, error) {
	if len(filters) == 0 {
		return r.List(ctx)
	}

	query := `SELECT DISTINCT p.id, p.name, p.api_type, p.base_url, p.api_key_encrypted, p.created_at, p.updated_at
		 FROM providers p`
	args := []interface{}{}

	i := 0
	for key, value := range filters {
		alias := fmt.Sprintf("pm%d", i)
		query += fmt.Sprintf(" INNER JOIN provider_metadata %s ON %s.provider_id = p.id AND %s.key = ? AND %s.value = ?",
			alias, alias, alias, alias)
		args = append(args, key, value)
		i++
	}

	query += " ORDER BY p.name"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list providers by metadata: %w", err)
	}
	defer rows.Close()

	var providers []models.Provider
	for rows.Next() {
		var p models.Provider
		if err := rows.Scan(&p.ID, &p.Name, &p.APIType, &p.BaseURL, &p.APIKeyEncrypted, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		providers = append(providers, p)
	}
	return providers, nil
}

func (r *sqliteProviderRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM providers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	return nil
}
