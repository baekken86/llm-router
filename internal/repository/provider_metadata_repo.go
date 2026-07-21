package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/chris/llm-router/internal/models"
)

type ProviderMetadataRepository interface {
	Set(ctx context.Context, providerID int64, tags map[string]string) error
	UpsertKey(ctx context.Context, providerID int64, key, value string) error
	GetByProvider(ctx context.Context, providerID int64) ([]models.ProviderMetadata, error)
	GetByProviders(ctx context.Context, providerIDs []int64) (map[int64][]models.ProviderMetadata, error)
	ListAll(ctx context.Context) (map[int64]map[string]string, error)
	ListAllKeys(ctx context.Context) (map[string][]string, error)
	DeleteByProvider(ctx context.Context, providerID int64) error
}

type sqliteProviderMetadataRepo struct {
	db *sql.DB
}

func NewProviderMetadataRepository(db *sql.DB) ProviderMetadataRepository {
	return &sqliteProviderMetadataRepo{db: db}
}

func (r *sqliteProviderMetadataRepo) UpsertKey(ctx context.Context, providerID int64, key, value string) error {
	if value == "" {
		_, err := r.db.ExecContext(ctx, `DELETE FROM provider_metadata WHERE provider_id = ? AND key = ?`, providerID, key)
		return err
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO provider_metadata (provider_id, key, value) VALUES (?, ?, ?)
		 ON CONFLICT(provider_id, key) DO UPDATE SET value = excluded.value`,
		providerID, key, value,
	)
	return err
}

func (r *sqliteProviderMetadataRepo) Set(ctx context.Context, providerID int64, tags map[string]string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `DELETE FROM provider_metadata WHERE provider_id = ?`, providerID)
	if err != nil {
		return fmt.Errorf("delete old: %w", err)
	}

	for key, value := range tags {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO provider_metadata (provider_id, key, value) VALUES (?, ?, ?)`,
			providerID, key, value,
		)
		if err != nil {
			return fmt.Errorf("insert %s: %w", key, err)
		}
	}

	return tx.Commit()
}

func (r *sqliteProviderMetadataRepo) GetByProvider(ctx context.Context, providerID int64) ([]models.ProviderMetadata, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, provider_id, key, value, created_at FROM provider_metadata WHERE provider_id = ? ORDER BY key`, providerID,
	)
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	defer rows.Close()

	var result []models.ProviderMetadata
	for rows.Next() {
		var m models.ProviderMetadata
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.Key, &m.Value, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		result = append(result, m)
	}
	return result, nil
}

func (r *sqliteProviderMetadataRepo) GetByProviders(ctx context.Context, providerIDs []int64) (map[int64][]models.ProviderMetadata, error) {
	if len(providerIDs) == 0 {
		return make(map[int64][]models.ProviderMetadata), nil
	}

	placeholders := ""
	args := make([]interface{}, 0, len(providerIDs))
	for i, id := range providerIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}

	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, provider_id, key, value, created_at FROM provider_metadata WHERE provider_id IN (%s) ORDER BY provider_id, key`, placeholders),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("get by providers: %w", err)
	}
	defer rows.Close()

	result := make(map[int64][]models.ProviderMetadata)
	for rows.Next() {
		var m models.ProviderMetadata
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.Key, &m.Value, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		result[m.ProviderID] = append(result[m.ProviderID], m)
	}
	return result, nil
}

func (r *sqliteProviderMetadataRepo) ListAll(ctx context.Context) (map[int64]map[string]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT provider_id, key, value FROM provider_metadata ORDER BY provider_id, key`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all: %w", err)
	}
	defer rows.Close()

	result := make(map[int64]map[string]string)
	for rows.Next() {
		var providerID int64
		var key, value string
		if err := rows.Scan(&providerID, &key, &value); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if result[providerID] == nil {
			result[providerID] = make(map[string]string)
		}
		result[providerID][key] = value
	}
	return result, nil
}

func (r *sqliteProviderMetadataRepo) DeleteByProvider(ctx context.Context, providerID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM provider_metadata WHERE provider_id = ?`, providerID)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}

func (r *sqliteProviderMetadataRepo) ListAllKeys(ctx context.Context) (map[string][]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT key, value FROM provider_metadata ORDER BY key, value`,
	)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]string)
	seen := make(map[string]map[string]bool)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if seen[key] == nil {
			seen[key] = make(map[string]bool)
		}
		if !seen[key][value] {
			seen[key][value] = true
			result[key] = append(result[key], value)
		}
	}
	return result, nil
}
