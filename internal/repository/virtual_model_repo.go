package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chris/llm-router/internal/models"
)

type VirtualModelRepository interface {
	Create(ctx context.Context, vm *models.VirtualModel) error
	GetByID(ctx context.Context, id int64) (*models.VirtualModel, error)
	GetByName(ctx context.Context, name string) (*models.VirtualModel, error)
	List(ctx context.Context) ([]models.VirtualModel, error)
	Update(ctx context.Context, vm *models.VirtualModel) error
	Delete(ctx context.Context, id int64) error
}

type sqliteVirtualModelRepo struct {
	db *sql.DB
}

func NewVirtualModelRepository(db *sql.DB) VirtualModelRepository {
	return &sqliteVirtualModelRepo{db: db}
}

const vmColumns = `id, name, description, filter_expr, sort_expr, include_models, composition, max_retries, retry_on_status, created_at, updated_at`

func (r *sqliteVirtualModelRepo) scanVM(row interface{ Scan(...interface{}) error }) (*models.VirtualModel, error) {
	vm := &models.VirtualModel{}
	var filterStr, sortStr, includeStr, retryStr string
	var compositionNull sql.NullString
	err := row.Scan(&vm.ID, &vm.Name, &vm.Description, &filterStr, &sortStr, &includeStr, &compositionNull, &vm.MaxRetries, &retryStr, &vm.CreatedAt, &vm.UpdatedAt)
	if err != nil {
		return nil, err
	}
	vm.FilterExpr = jsonRawMessageOrDefault(filterStr, "{}")
	vm.SortExpr = jsonRawMessageOrDefault(sortStr, "[]")
	vm.IncludeModels = jsonRawMessageOrDefault(includeStr, "[]")
	vm.RetryOnStatus = jsonRawMessageOrDefault(retryStr, "[]")
	if compositionNull.Valid && compositionNull.String != "" && compositionNull.String != "null" {
		node := &models.CompositionNode{}
		if err := models.ParseCompositionJSON([]byte(compositionNull.String), node); err == nil && (node.Vm != "" || node.Operation != "") {
			vm.Composition = node
		}
	}
	return vm, nil
}

func jsonRawMessageOrDefault(val, def string) json.RawMessage {
	if val == "" {
		return json.RawMessage(def)
	}
	return json.RawMessage(val)
}

func (r *sqliteVirtualModelRepo) Create(ctx context.Context, vm *models.VirtualModel) error {
	now := time.Now()
	if vm.MaxRetries == 0 {
		vm.MaxRetries = 1
	}
	if len(vm.RetryOnStatus) == 0 {
		vm.RetryOnStatus = []byte(`[429,500,502,503,504]`)
	}
	if len(vm.IncludeModels) == 0 {
		vm.IncludeModels = []byte(`[]`)
	}

	var compositionParam interface{}
	if vm.Composition != nil {
		data, err := models.MarshalComposition(vm.Composition)
		if err != nil {
			return fmt.Errorf("marshal composition: %w", err)
		}
		compositionParam = string(data)
	}

	result, err := r.db.ExecContext(ctx,
		`INSERT INTO virtual_models (name, description, filter_expr, sort_expr, include_models, composition, max_retries, retry_on_status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		vm.Name, vm.Description, string(vm.FilterExpr), string(vm.SortExpr), string(vm.IncludeModels), compositionParam, vm.MaxRetries, string(vm.RetryOnStatus), now, now,
	)
	if err != nil {
		return fmt.Errorf("insert virtual model: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	vm.ID = id
	vm.CreatedAt = now
	vm.UpdatedAt = now
	return nil
}

func (r *sqliteVirtualModelRepo) GetByID(ctx context.Context, id int64) (*models.VirtualModel, error) {
	vm, err := r.scanVM(r.db.QueryRowContext(ctx,
		`SELECT `+vmColumns+` FROM virtual_models WHERE id = ?`, id,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get virtual model: %w", err)
	}
	return vm, nil
}

func (r *sqliteVirtualModelRepo) GetByName(ctx context.Context, name string) (*models.VirtualModel, error) {
	vm, err := r.scanVM(r.db.QueryRowContext(ctx,
		`SELECT `+vmColumns+` FROM virtual_models WHERE name = ?`, name,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get virtual model by name: %w", err)
	}
	return vm, nil
}

func (r *sqliteVirtualModelRepo) List(ctx context.Context) ([]models.VirtualModel, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+vmColumns+` FROM virtual_models ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list virtual models: %w", err)
	}
	defer rows.Close()

	var vms []models.VirtualModel
	for rows.Next() {
		vm, err := r.scanVM(rows)
		if err != nil {
			return nil, fmt.Errorf("scan virtual model: %w", err)
		}
		vms = append(vms, *vm)
	}
	return vms, nil
}

func (r *sqliteVirtualModelRepo) Update(ctx context.Context, vm *models.VirtualModel) error {
	now := time.Now()

	var compositionParam interface{}
	if vm.Composition != nil {
		data, err := models.MarshalComposition(vm.Composition)
		if err != nil {
			return fmt.Errorf("marshal composition: %w", err)
		}
		compositionParam = string(data)
	}

	_, err := r.db.ExecContext(ctx,
		`UPDATE virtual_models SET name = ?, description = ?, filter_expr = ?, sort_expr = ?, include_models = ?, composition = ?, max_retries = ?, retry_on_status = ?, updated_at = ?
		 WHERE id = ?`,
		vm.Name, vm.Description, string(vm.FilterExpr), string(vm.SortExpr), string(vm.IncludeModels), compositionParam, vm.MaxRetries, string(vm.RetryOnStatus), now, vm.ID,
	)
	if err != nil {
		return fmt.Errorf("update virtual model: %w", err)
	}
	vm.UpdatedAt = now
	return nil
}

func (r *sqliteVirtualModelRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM virtual_models WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete virtual model: %w", err)
	}
	return nil
}
