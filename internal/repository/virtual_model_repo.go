package repository

import (
	"context"
	"database/sql"
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

func (r *sqliteVirtualModelRepo) Create(ctx context.Context, vm *models.VirtualModel) error {
	now := time.Now()
	if vm.MaxRetries == 0 {
		vm.MaxRetries = 1
	}
	if len(vm.RetryOnStatus) == 0 {
		vm.RetryOnStatus = []byte(`[429,500,502,503,504]`)
	}

	result, err := r.db.ExecContext(ctx,
		`INSERT INTO virtual_models (name, filter_expr, sort_expr, max_retries, retry_on_status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		vm.Name, string(vm.FilterExpr), string(vm.SortExpr), vm.MaxRetries, string(vm.RetryOnStatus), now, now,
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
	vm := &models.VirtualModel{}
	var filterStr, sortStr, retryStr string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, filter_expr, sort_expr, max_retries, retry_on_status, created_at, updated_at
		 FROM virtual_models WHERE id = ?`, id,
	).Scan(&vm.ID, &vm.Name, &filterStr, &sortStr, &vm.MaxRetries, &retryStr, &vm.CreatedAt, &vm.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get virtual model: %w", err)
	}
	vm.FilterExpr = []byte(filterStr)
	vm.SortExpr = []byte(sortStr)
	vm.RetryOnStatus = []byte(retryStr)
	return vm, nil
}

func (r *sqliteVirtualModelRepo) GetByName(ctx context.Context, name string) (*models.VirtualModel, error) {
	vm := &models.VirtualModel{}
	var filterStr, sortStr, retryStr string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, filter_expr, sort_expr, max_retries, retry_on_status, created_at, updated_at
		 FROM virtual_models WHERE name = ?`, name,
	).Scan(&vm.ID, &vm.Name, &filterStr, &sortStr, &vm.MaxRetries, &retryStr, &vm.CreatedAt, &vm.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get virtual model by name: %w", err)
	}
	vm.FilterExpr = []byte(filterStr)
	vm.SortExpr = []byte(sortStr)
	vm.RetryOnStatus = []byte(retryStr)
	return vm, nil
}

func (r *sqliteVirtualModelRepo) List(ctx context.Context) ([]models.VirtualModel, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, filter_expr, sort_expr, max_retries, retry_on_status, created_at, updated_at
		 FROM virtual_models ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list virtual models: %w", err)
	}
	defer rows.Close()

	var vms []models.VirtualModel
	for rows.Next() {
		var vm models.VirtualModel
		var filterStr, sortStr, retryStr string
		if err := rows.Scan(&vm.ID, &vm.Name, &filterStr, &sortStr, &vm.MaxRetries, &retryStr, &vm.CreatedAt, &vm.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan virtual model: %w", err)
		}
		vm.FilterExpr = []byte(filterStr)
		vm.SortExpr = []byte(sortStr)
		vm.RetryOnStatus = []byte(retryStr)
		vms = append(vms, vm)
	}
	return vms, nil
}

func (r *sqliteVirtualModelRepo) Update(ctx context.Context, vm *models.VirtualModel) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE virtual_models SET name = ?, filter_expr = ?, sort_expr = ?, max_retries = ?, retry_on_status = ?, updated_at = ?
		 WHERE id = ?`,
		vm.Name, string(vm.FilterExpr), string(vm.SortExpr), vm.MaxRetries, string(vm.RetryOnStatus), now, vm.ID,
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
