package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chris/llm-router/internal/models"
)

type ModelRepository interface {
	Create(ctx context.Context, m *models.Model) error
	GetByID(ctx context.Context, id int64) (*models.Model, error)
	GetByProviderAndName(ctx context.Context, providerID int64, name string) (*models.Model, error)
	ListByProvider(ctx context.Context, providerID int64) ([]models.Model, error)
	ListAll(ctx context.Context) ([]models.Model, error)
	ListEnabled(ctx context.Context) ([]models.Model, error)
	Delete(ctx context.Context, id int64) error
	Upsert(ctx context.Context, providerID int64, name string) (*models.Model, error)
	DisableByProviderExcept(ctx context.Context, providerID int64, names []string) (int64, error)
	ToggleDisabled(ctx context.Context, id int64, disabled bool, duration *time.Duration) error
	ListExpiredDisabled(ctx context.Context, now time.Time) ([]int64, error)
	GetCBStrikes(ctx context.Context, modelID int64) (int, error)
	SetCBStrikes(ctx context.Context, modelID int64, strikes int) error
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
		`SELECT id, provider_id, name, disabled, disabled_until, created_at FROM models WHERE id = ?`, id,
	).Scan(&m.ID, &m.ProviderID, &m.Name, &m.Disabled, &m.DisabledUntil, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get model: %w", err)
	}
	return m, nil
}

func (r *sqliteModelRepo) GetByProviderAndName(ctx context.Context, providerID int64, name string) (*models.Model, error) {
	m := &models.Model{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, provider_id, name, disabled, disabled_until, created_at FROM models WHERE provider_id = ? AND name = ?`, providerID, name,
	).Scan(&m.ID, &m.ProviderID, &m.Name, &m.Disabled, &m.DisabledUntil, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get model by provider and name: %w", err)
	}
	return m, nil
}

func (r *sqliteModelRepo) ListByProvider(ctx context.Context, providerID int64) ([]models.Model, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, provider_id, name, disabled, disabled_until, created_at FROM models WHERE provider_id = ? ORDER BY name`, providerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer rows.Close()

	var result []models.Model
	for rows.Next() {
		var m models.Model
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.Name, &m.Disabled, &m.DisabledUntil, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		result = append(result, m)
	}
	return result, nil
}

func (r *sqliteModelRepo) ListAll(ctx context.Context) ([]models.Model, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, provider_id, name, disabled, disabled_until, created_at FROM models ORDER BY provider_id, name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all models: %w", err)
	}
	defer rows.Close()

	var result []models.Model
	for rows.Next() {
		var m models.Model
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.Name, &m.Disabled, &m.DisabledUntil, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		result = append(result, m)
	}
	return result, nil
}

func (r *sqliteModelRepo) ListEnabled(ctx context.Context) ([]models.Model, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, provider_id, name, disabled, disabled_until, created_at FROM models WHERE disabled = 0 ORDER BY provider_id, name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list enabled models: %w", err)
	}
	defer rows.Close()

	var result []models.Model
	for rows.Next() {
		var m models.Model
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.Name, &m.Disabled, &m.DisabledUntil, &m.CreatedAt); err != nil {
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
		 RETURNING id, provider_id, name, disabled, disabled_until, created_at`,
		providerID, name,
	).Scan(&m.ID, &m.ProviderID, &m.Name, &m.Disabled, &m.DisabledUntil, &m.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert model: %w", err)
	}
	return m, nil
}

func (r *sqliteModelRepo) ToggleDisabled(ctx context.Context, id int64, disabled bool, duration *time.Duration) error {
	var disabledUntil *time.Time
	if disabled && duration != nil {
		t := time.Now().Add(*duration)
		disabledUntil = &t
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE models SET disabled = ?, disabled_until = ? WHERE id = ?`, disabled, disabledUntil, id,
	)
	if err != nil {
		return fmt.Errorf("toggle model disabled: %w", err)
	}
	return nil
}

func (r *sqliteModelRepo) ListExpiredDisabled(ctx context.Context, now time.Time) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id FROM models WHERE disabled = 1 AND disabled_until IS NOT NULL AND disabled_until <= ?`, now,
	)
	if err != nil {
		return nil, fmt.Errorf("list expired disabled models: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan expired model id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *sqliteModelRepo) GetCBStrikes(ctx context.Context, modelID int64) (int, error) {
	var strikes int
	err := r.db.QueryRowContext(ctx, `SELECT cb_strikes FROM models WHERE id = ?`, modelID).Scan(&strikes)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get model cb_strikes: %w", err)
	}
	return strikes, nil
}

func (r *sqliteModelRepo) SetCBStrikes(ctx context.Context, modelID int64, strikes int) error {
	_, err := r.db.ExecContext(ctx, `UPDATE models SET cb_strikes = ? WHERE id = ?`, strikes, modelID)
	if err != nil {
		return fmt.Errorf("set model cb_strikes: %w", err)
	}
	return nil
}

func (r *sqliteModelRepo) DisableByProviderExcept(ctx context.Context, providerID int64, names []string) (int64, error) {
	if len(names) == 0 {
		result, err := r.db.ExecContext(ctx,
			`UPDATE models SET disabled = 1 WHERE provider_id = ? AND disabled = 0`, providerID,
		)
		if err != nil {
			return 0, fmt.Errorf("disable all models: %w", err)
		}
		count, err := result.RowsAffected()
		return count, err
	}

	query := `UPDATE models SET disabled = 1 WHERE provider_id = ? AND disabled = 0 AND name NOT IN (` + placeholders(len(names)) + `)`
	args := []interface{}{providerID}
	for _, name := range names {
		args = append(args, name)
	}

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("disable stale models: %w", err)
	}
	count, err := result.RowsAffected()
	return count, err
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	result := "?"
	for i := 1; i < n; i++ {
		result += ", ?"
	}
	return result
}
