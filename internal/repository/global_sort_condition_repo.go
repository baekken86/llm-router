package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chris/llm-router/internal/models"
)

type GlobalSortConditionRepository interface {
	List(ctx context.Context) ([]models.GlobalSortCondition, error)
	Get(ctx context.Context, id int64) (*models.GlobalSortCondition, error)
	Create(ctx context.Context, cond *models.GlobalSortCondition) error
	Update(ctx context.Context, cond *models.GlobalSortCondition) error
	Delete(ctx context.Context, id int64) error
	// ListDisabledIDs returns the ids of global conditions disabled for a VM.
	ListDisabledIDs(ctx context.Context, virtualModelID int64) ([]int64, error)
	// SetDisabled replaces the set of conditions disabled for a VM.
	SetDisabled(ctx context.Context, virtualModelID int64, disabledIDs []int64) error
	// ListDisabledIDsByModel loads per-VM disabled ids for many VMs.
	ListDisabledIDsByModel(ctx context.Context, virtualModelIDs []int64) (map[int64][]int64, error)
	// Reorder sets each condition's position to its index in the ids list
	// (single transaction). Ids not present keep their existing position.
	Reorder(ctx context.Context, ids []int64) error
}

type sqliteGlobalSortConditionRepo struct {
	db *sql.DB
}

func NewGlobalSortConditionRepository(db *sql.DB) GlobalSortConditionRepository {
	return &sqliteGlobalSortConditionRepo{db: db}
}

const gscColumns = `id, name, description, sort_expr, enabled, position, created_at, updated_at`

func scanGlobalSortCondition(row interface{ Scan(...interface{}) error }) (*models.GlobalSortCondition, error) {
	cond := &models.GlobalSortCondition{}
	var sortStr string
	var enabled int
	err := row.Scan(&cond.ID, &cond.Name, &cond.Description, &sortStr, &enabled, &cond.Position, &cond.CreatedAt, &cond.UpdatedAt)
	if err != nil {
		return nil, err
	}
	cond.Enabled = enabled != 0
	cond.SortExpr = models.SortExpr{}
	if sortStr != "" {
		var se models.SortExpr
		if err := json.Unmarshal([]byte(sortStr), &se); err == nil {
			cond.SortExpr = se
		}
	}
	return cond, nil
}

func (r *sqliteGlobalSortConditionRepo) List(ctx context.Context) ([]models.GlobalSortCondition, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+gscColumns+` FROM global_sort_conditions ORDER BY position, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list global sort conditions: %w", err)
	}
	defer rows.Close()

	var result []models.GlobalSortCondition
	for rows.Next() {
		cond, err := scanGlobalSortCondition(rows)
		if err != nil {
			return nil, fmt.Errorf("scan global sort condition: %w", err)
		}
		result = append(result, *cond)
	}
	return result, nil
}

func (r *sqliteGlobalSortConditionRepo) Get(ctx context.Context, id int64) (*models.GlobalSortCondition, error) {
	cond, err := scanGlobalSortCondition(r.db.QueryRowContext(ctx,
		`SELECT `+gscColumns+` FROM global_sort_conditions WHERE id = ?`, id,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get global sort condition: %w", err)
	}
	return cond, nil
}

func (r *sqliteGlobalSortConditionRepo) Create(ctx context.Context, cond *models.GlobalSortCondition) error {
	now := time.Now()
	data, err := json.Marshal(cond.SortExpr)
	if err != nil {
		return fmt.Errorf("marshal sort_expr: %w", err)
	}

	result, err := r.db.ExecContext(ctx,
		`INSERT INTO global_sort_conditions (name, description, sort_expr, enabled, position, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		cond.Name, cond.Description, string(data), boolToInt(cond.Enabled), cond.Position, now, now,
	)
	if err != nil {
		return fmt.Errorf("insert global sort condition: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	cond.ID = id
	cond.CreatedAt = now
	cond.UpdatedAt = now
	return nil
}

func (r *sqliteGlobalSortConditionRepo) Update(ctx context.Context, cond *models.GlobalSortCondition) error {
	now := time.Now()
	data, err := json.Marshal(cond.SortExpr)
	if err != nil {
		return fmt.Errorf("marshal sort_expr: %w", err)
	}

	_, err = r.db.ExecContext(ctx,
		`UPDATE global_sort_conditions SET name = ?, description = ?, sort_expr = ?, enabled = ?, position = ?, updated_at = ?
		 WHERE id = ?`,
		cond.Name, cond.Description, string(data), boolToInt(cond.Enabled), cond.Position, now, cond.ID,
	)
	if err != nil {
		return fmt.Errorf("update global sort condition: %w", err)
	}
	cond.UpdatedAt = now
	return nil
}

func (r *sqliteGlobalSortConditionRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM global_sort_conditions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete global sort condition: %w", err)
	}
	return nil
}

func (r *sqliteGlobalSortConditionRepo) ListDisabledIDs(ctx context.Context, virtualModelID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT condition_id FROM virtual_model_sort_disabled WHERE virtual_model_id = ?`,
		virtualModelID,
	)
	if err != nil {
		return nil, fmt.Errorf("list disabled sort conditions: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan disabled sort condition id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *sqliteGlobalSortConditionRepo) ListDisabledIDsByModel(ctx context.Context, virtualModelIDs []int64) (map[int64][]int64, error) {
	result := make(map[int64][]int64)
	if len(virtualModelIDs) == 0 {
		return result, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT virtual_model_id, condition_id FROM virtual_model_sort_disabled`,
	)
	if err != nil {
		return nil, fmt.Errorf("list disabled sort conditions: %w", err)
	}
	defer rows.Close()

	want := make(map[int64]bool, len(virtualModelIDs))
	for _, id := range virtualModelIDs {
		want[id] = true
	}
	for rows.Next() {
		var vmID, condID int64
		if err := rows.Scan(&vmID, &condID); err != nil {
			return nil, fmt.Errorf("scan disabled sort condition: %w", err)
		}
		if want[vmID] {
			result[vmID] = append(result[vmID], condID)
		}
	}
	return result, nil
}

// SetDisabled replaces the per-VM disabled set inside a transaction.
func (r *sqliteGlobalSortConditionRepo) SetDisabled(ctx context.Context, virtualModelID int64, disabledIDs []int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM virtual_model_sort_disabled WHERE virtual_model_id = ?`, virtualModelID); err != nil {
		return fmt.Errorf("clear disabled sort conditions: %w", err)
	}
	for _, condID := range disabledIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO virtual_model_sort_disabled (virtual_model_id, condition_id) VALUES (?, ?)`,
			virtualModelID, condID,
		); err != nil {
			return fmt.Errorf("insert disabled sort condition: %w", err)
		}
	}
	return tx.Commit()
}

// Reorder assigns position = index for each id inside one transaction.
func (r *sqliteGlobalSortConditionRepo) Reorder(ctx context.Context, ids []int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for i, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE global_sort_conditions SET position = ?, updated_at = ? WHERE id = ?`,
			i, time.Now(), id,
		); err != nil {
			return fmt.Errorf("reorder global sort condition: %w", err)
		}
	}
	return tx.Commit()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
