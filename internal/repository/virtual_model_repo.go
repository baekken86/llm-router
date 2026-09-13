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
		if err := models.ParseCompositionJSON([]byte(compositionNull.String), node); err == nil && (node.Vm != "" || node.Operation != "" || node.FilterExpr != nil || len(node.SortExpr) > 0) {
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
	r.hydrateDisabledConditions(ctx, vm)
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
	r.hydrateDisabledConditions(ctx, vm)
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
	r.hydrateDisabledConditionsForList(ctx, vms)
	return vms, nil
}

// hydrateDisabledConditions loads the per-VM disabled global condition ids from
// the virtual_model_sort_disabled join table into the VM object.
func (r *sqliteVirtualModelRepo) hydrateDisabledConditions(ctx context.Context, vm *models.VirtualModel) {
	ids, err := listDisabledSortIDs(ctx, r.db, vm.ID)
	if err != nil {
		return
	}
	vm.DisabledSortConditions = ids

	cids, err := listDisabledFilterIDs(ctx, r.db, vm.ID)
	if err != nil {
		return
	}
	vm.DisabledFilterConditions = cids
}

// hydrateDisabledConditionsForList batch-loads disabled ids for a list of VMs
// using a single query over the join table.
func (r *sqliteVirtualModelRepo) hydrateDisabledConditionsForList(ctx context.Context, vms []models.VirtualModel) {
	if len(vms) == 0 {
		return
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT virtual_model_id, condition_id FROM virtual_model_sort_disabled`,
	)
	if err != nil {
		return
	}
	defer rows.Close()

	byVM := make(map[int64][]int64)
	for rows.Next() {
		var vmID, condID int64
		if err := rows.Scan(&vmID, &condID); err != nil {
			return
		}
		byVM[vmID] = append(byVM[vmID], condID)
	}
	for i := range vms {
		vms[i].DisabledSortConditions = byVM[vms[i].ID]
	}

	// Filter join table
	frows, err := r.db.QueryContext(ctx,
		`SELECT virtual_model_id, condition_id FROM virtual_model_filter_disabled`,
	)
	if err != nil {
		return
	}
	defer frows.Close()

	fbyVM := make(map[int64][]int64)
	for frows.Next() {
		var vmID, condID int64
		if err := frows.Scan(&vmID, &condID); err != nil {
			return
		}
		fbyVM[vmID] = append(fbyVM[vmID], condID)
	}
	for i := range vms {
		vms[i].DisabledFilterConditions = fbyVM[vms[i].ID]
	}
}

func listDisabledSortIDs(ctx context.Context, db *sql.DB, virtualModelID int64) ([]int64, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT condition_id FROM virtual_model_sort_disabled WHERE virtual_model_id = ?`,
		virtualModelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func listDisabledFilterIDs(ctx context.Context, db *sql.DB, virtualModelID int64) ([]int64, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT condition_id FROM virtual_model_filter_disabled WHERE virtual_model_id = ?`,
		virtualModelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
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
	return r.saveDisabledConditions(ctx, vm)
}

// saveDisabledConditions replaces the VM's disabled global condition rows with
// the set currently on the struct (nil/empty clears them).
func (r *sqliteVirtualModelRepo) saveDisabledConditions(ctx context.Context, vm *models.VirtualModel) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM virtual_model_sort_disabled WHERE virtual_model_id = ?`, vm.ID); err != nil {
		return fmt.Errorf("clear disabled sort conditions: %w", err)
	}
	for _, condID := range vm.DisabledSortConditions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO virtual_model_sort_disabled (virtual_model_id, condition_id) VALUES (?, ?)`,
			vm.ID, condID,
		); err != nil {
			return fmt.Errorf("insert disabled sort condition: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM virtual_model_filter_disabled WHERE virtual_model_id = ?`, vm.ID); err != nil {
		return fmt.Errorf("clear disabled filter conditions: %w", err)
	}
	for _, condID := range vm.DisabledFilterConditions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO virtual_model_filter_disabled (virtual_model_id, condition_id) VALUES (?, ?)`,
			vm.ID, condID,
		); err != nil {
			return fmt.Errorf("insert disabled filter condition: %w", err)
		}
	}
	return tx.Commit()
}

func (r *sqliteVirtualModelRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM virtual_models WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete virtual model: %w", err)
	}
	return nil
}
