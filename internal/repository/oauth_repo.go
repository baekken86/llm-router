package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chris/llm-router/internal/models"
)

type OAuthRepository interface {
	Upsert(ctx context.Context, token *models.OAuthToken) error
	GetByProviderID(ctx context.Context, providerID int64) (*models.OAuthToken, error)
	Delete(ctx context.Context, providerID int64) error
}

type sqliteOAuthRepo struct {
	db *sql.DB
}

func NewOAuthRepository(db *sql.DB) OAuthRepository {
	return &sqliteOAuthRepo{db: db}
}

func (r *sqliteOAuthRepo) Upsert(ctx context.Context, token *models.OAuthToken) error {
	now := time.Now()

	result, err := r.db.ExecContext(ctx,
		`INSERT INTO oauth_tokens (provider_id, access_token, refresh_token, expires_at, account_id, email, last_refresh_at, id_token, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(provider_id) DO UPDATE SET
		   access_token = excluded.access_token,
		   refresh_token = excluded.refresh_token,
		   expires_at = excluded.expires_at,
		   account_id = excluded.account_id,
		   email = excluded.email,
		   last_refresh_at = excluded.last_refresh_at,
		   id_token = excluded.id_token,
		   updated_at = excluded.updated_at`,
		token.ProviderID, token.AccessToken, token.RefreshToken, token.ExpiresAt,
		token.AccountID, token.Email, token.LastRefreshAt, token.IDToken, now, now,
	)
	if err != nil {
		return fmt.Errorf("upsert oauth token: %w", err)
	}

	id, err := result.LastInsertId()
	if err == nil && token.ID == 0 {
		token.ID = id
	}
	token.UpdatedAt = now
	return nil
}

func (r *sqliteOAuthRepo) GetByProviderID(ctx context.Context, providerID int64) (*models.OAuthToken, error) {
	token := &models.OAuthToken{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, provider_id, access_token, refresh_token, expires_at, account_id, email, last_refresh_at, id_token, created_at, updated_at
		 FROM oauth_tokens WHERE provider_id = ?`, providerID,
	).Scan(&token.ID, &token.ProviderID, &token.AccessToken, &token.RefreshToken,
		&token.ExpiresAt, &token.AccountID, &token.Email, &token.LastRefreshAt, &token.IDToken,
		&token.CreatedAt, &token.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get oauth token: %w", err)
	}
	return token, nil
}

func (r *sqliteOAuthRepo) Delete(ctx context.Context, providerID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE provider_id = ?`, providerID)
	if err != nil {
		return fmt.Errorf("delete oauth token: %w", err)
	}
	return nil
}
