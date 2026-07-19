package repository

import (
	"context"
	"database/sql"
	"fmt"
)

type ModelMapping struct {
	SourceModelID int64  `json:"source_model_id"`
	TargetModelID int64  `json:"target_model_id"`
	CreatedAt     string `json:"created_at"`
}

type ModelMappingWithNames struct {
	SourceModelID   int64  `json:"source_model_id"`
	SourceModelName string `json:"source_model_name"`
	SourceProviderName string `json:"source_provider_name"`
	TargetModelID   int64  `json:"target_model_id"`
	TargetModelName string `json:"target_model_name"`
	TargetProviderName string `json:"target_provider_name"`
	CreatedAt       string `json:"created_at"`
}

type ModelMappingRepository interface {
	Set(ctx context.Context, sourceModelID, targetModelID int64) error
	Get(ctx context.Context, sourceModelID int64) (*ModelMapping, error)
	GetAll(ctx context.Context) ([]ModelMapping, error)
	GetAllJoined(ctx context.Context) ([]ModelMappingWithNames, error)
	Delete(ctx context.Context, sourceModelID int64) error
}

type sqliteModelMappingRepo struct {
	db *sql.DB
}

func NewModelMappingRepository(db *sql.DB) ModelMappingRepository {
	return &sqliteModelMappingRepo{db: db}
}

func (r *sqliteModelMappingRepo) Set(ctx context.Context, sourceModelID, targetModelID int64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO model_mappings (source_model_id, target_model_id) VALUES (?, ?)
		 ON CONFLICT(source_model_id) DO UPDATE SET target_model_id = excluded.target_model_id`,
		sourceModelID, targetModelID,
	)
	if err != nil {
		return fmt.Errorf("set mapping: %w", err)
	}
	return nil
}

func (r *sqliteModelMappingRepo) Get(ctx context.Context, sourceModelID int64) (*ModelMapping, error) {
	m := &ModelMapping{}
	err := r.db.QueryRowContext(ctx,
		`SELECT source_model_id, target_model_id, created_at FROM model_mappings WHERE source_model_id = ?`,
		sourceModelID,
	).Scan(&m.SourceModelID, &m.TargetModelID, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get mapping: %w", err)
	}
	return m, nil
}

func (r *sqliteModelMappingRepo) GetAll(ctx context.Context) ([]ModelMapping, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT source_model_id, target_model_id, created_at FROM model_mappings ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("get all mappings: %w", err)
	}
	defer rows.Close()

	var result []ModelMapping
	for rows.Next() {
		var m ModelMapping
		if err := rows.Scan(&m.SourceModelID, &m.TargetModelID, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan mapping: %w", err)
		}
		result = append(result, m)
	}
	return result, nil
}

func (r *sqliteModelMappingRepo) GetAllJoined(ctx context.Context) ([]ModelMappingWithNames, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT mm.source_model_id, sm.name, sp.name AS source_provider_name,
		        mm.target_model_id, tm.name, tp.name AS target_provider_name,
		        mm.created_at
		 FROM model_mappings mm
		 JOIN models sm ON mm.source_model_id = sm.id
		 JOIN providers sp ON sm.provider_id = sp.id
		 JOIN models tm ON mm.target_model_id = tm.id
		 JOIN providers tp ON tm.provider_id = tp.id
		 ORDER BY mm.created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("get all joined mappings: %w", err)
	}
	defer rows.Close()

	var result []ModelMappingWithNames
	for rows.Next() {
		var m ModelMappingWithNames
		if err := rows.Scan(&m.SourceModelID, &m.SourceModelName, &m.SourceProviderName,
			&m.TargetModelID, &m.TargetModelName, &m.TargetProviderName, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan joined mapping: %w", err)
		}
		result = append(result, m)
	}
	return result, nil
}

func (r *sqliteModelMappingRepo) Delete(ctx context.Context, sourceModelID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM model_mappings WHERE source_model_id = ?`, sourceModelID)
	if err != nil {
		return fmt.Errorf("delete mapping: %w", err)
	}
	return nil
}
