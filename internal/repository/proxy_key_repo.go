package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chris/llm-router/internal/models"
)

type ProxyKeyRepository interface {
	Create(ctx context.Context, pk *models.ProxyKey) error
	GetByID(ctx context.Context, id int64) (*models.ProxyKey, error)
	GetByHash(ctx context.Context, hash string) (*models.ProxyKey, error)
	List(ctx context.Context) ([]models.ProxyKey, error)
	Delete(ctx context.Context, id int64) error
}

type sqliteProxyKeyRepo struct {
	db *sql.DB
}

func NewProxyKeyRepository(db *sql.DB) ProxyKeyRepository {
	return &sqliteProxyKeyRepo{db: db}
}

func (r *sqliteProxyKeyRepo) Create(ctx context.Context, pk *models.ProxyKey) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO proxy_keys (key_hash, description, created_at) VALUES (?, ?, ?)`,
		pk.KeyHash, pk.Description, now,
	)
	if err != nil {
		return fmt.Errorf("insert proxy key: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	pk.ID = id
	pk.CreatedAt = now
	return nil
}

func (r *sqliteProxyKeyRepo) GetByID(ctx context.Context, id int64) (*models.ProxyKey, error) {
	pk := &models.ProxyKey{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, key_hash, description, created_at FROM proxy_keys WHERE id = ?`, id,
	).Scan(&pk.ID, &pk.KeyHash, &pk.Description, &pk.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get proxy key: %w", err)
	}
	return pk, nil
}

func (r *sqliteProxyKeyRepo) GetByHash(ctx context.Context, hash string) (*models.ProxyKey, error) {
	pk := &models.ProxyKey{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, key_hash, description, created_at FROM proxy_keys WHERE key_hash = ?`, hash,
	).Scan(&pk.ID, &pk.KeyHash, &pk.Description, &pk.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get proxy key by hash: %w", err)
	}
	return pk, nil
}

func (r *sqliteProxyKeyRepo) List(ctx context.Context) ([]models.ProxyKey, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, key_hash, description, created_at FROM proxy_keys ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list proxy keys: %w", err)
	}
	defer rows.Close()

	var keys []models.ProxyKey
	for rows.Next() {
		var pk models.ProxyKey
		if err := rows.Scan(&pk.ID, &pk.KeyHash, &pk.Description, &pk.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan proxy key: %w", err)
		}
		keys = append(keys, pk)
	}
	return keys, nil
}

func (r *sqliteProxyKeyRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM proxy_keys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete proxy key: %w", err)
	}
	return nil
}
