package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/chris/llm-router/internal/models"
	_ "modernc.org/sqlite"
)

func setupGlobalSortTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`PRAGMA foreign_keys = ON`)

	for _, stmt := range []string{
		`CREATE TABLE virtual_models (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			description TEXT DEFAULT '',
			filter_expr TEXT DEFAULT '{}',
			sort_expr TEXT DEFAULT '[]',
			include_models TEXT DEFAULT '[]',
			composition TEXT,
			max_retries INTEGER DEFAULT 1,
			retry_on_status TEXT DEFAULT '[429,500,502,503,504]',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE global_sort_conditions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			description TEXT,
			sort_expr TEXT NOT NULL DEFAULT '[]',
			enabled INTEGER NOT NULL DEFAULT 1,
			position INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE virtual_model_sort_disabled (
			virtual_model_id INTEGER NOT NULL REFERENCES virtual_models(id) ON DELETE CASCADE,
			condition_id INTEGER NOT NULL REFERENCES global_sort_conditions(id) ON DELETE CASCADE,
			PRIMARY KEY (virtual_model_id, condition_id)
		)`,
		`CREATE TABLE global_filter_conditions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			description TEXT,
			filter_expr TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1,
			position INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE virtual_model_filter_disabled (
			virtual_model_id INTEGER NOT NULL REFERENCES virtual_models(id) ON DELETE CASCADE,
			condition_id INTEGER NOT NULL REFERENCES global_filter_conditions(id) ON DELETE CASCADE,
			PRIMARY KEY (virtual_model_id, condition_id)
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	return db
}

func TestGlobalSortConditionCRUD(t *testing.T) {
	db := setupGlobalSortTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewGlobalSortConditionRepository(db)

	enabled := true
	pos := 5
	cond := &models.GlobalSortCondition{
		Name:     "prefer-cheap",
		SortExpr: models.SortExpr{{Key: "cost", Direction: "asc"}},
		Enabled:  enabled,
		Position: pos,
	}

	if err := repo.Create(ctx, cond); err != nil {
		t.Fatal(err)
	}
	if cond.ID == 0 {
		t.Fatal("expected ID assigned after create")
	}

	got, err := repo.Get(ctx, cond.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected condition, got nil")
	}
	if got.Name != "prefer-cheap" || got.Position != 5 || !got.Enabled {
		t.Errorf("unexpected condition: %+v", got)
	}
	if len(got.SortExpr) != 1 || got.SortExpr[0].Key != "cost" {
		t.Errorf("unexpected sort expr: %+v", got.SortExpr)
	}

	// Update
	newName := "prefer-fast"
	got.Name = newName
	got.Enabled = false
	if err := repo.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	updated, _ := repo.Get(ctx, cond.ID)
	if updated.Name != newName || updated.Enabled {
		t.Errorf("update not persisted: %+v", updated)
	}

	// List ordering by position, id
	other := &models.GlobalSortCondition{Name: "aaa", SortExpr: models.SortExpr{{Key: "x"}}, Enabled: true, Position: 0}
	if err := repo.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	all, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Name != "aaa" || all[1].Name != newName {
		t.Errorf("list ordering wrong: %+v", all)
	}

	// Delete
	if err := repo.Delete(ctx, cond.ID); err != nil {
		t.Fatal(err)
	}
	deleted, _ := repo.Get(ctx, cond.ID)
	if deleted != nil {
		t.Error("expected nil after delete")
	}
}

func TestGlobalSortConditionDisabled(t *testing.T) {
	db := setupGlobalSortTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewGlobalSortConditionRepository(db)
	repoVM := NewVirtualModelRepository(db)

	vm := &models.VirtualModel{Name: "vm1", FilterExpr: []byte(`{}`)}
	if err := repoVM.Create(ctx, vm); err != nil {
		t.Fatal(err)
	}

	c1 := &models.GlobalSortCondition{Name: "c1", SortExpr: models.SortExpr{{Key: "a"}}, Enabled: true}
	c2 := &models.GlobalSortCondition{Name: "c2", SortExpr: models.SortExpr{{Key: "b"}}, Enabled: true}
	if err := repo.Create(ctx, c1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, c2); err != nil {
		t.Fatal(err)
	}

	if err := repo.SetDisabled(ctx, vm.ID, []int64{c1.ID, c2.ID}); err != nil {
		t.Fatal(err)
	}
	ids, err := repo.ListDisabledIDs(ctx, vm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 disabled ids, got %v", ids)
	}

	// Replace with a single id
	if err := repo.SetDisabled(ctx, vm.ID, []int64{c2.ID}); err != nil {
		t.Fatal(err)
	}
	ids, _ = repo.ListDisabledIDs(ctx, vm.ID)
	if len(ids) != 1 || ids[0] != c2.ID {
		t.Errorf("expected [{c2}], got %v", ids)
	}

	// Clear
	if err := repo.SetDisabled(ctx, vm.ID, nil); err != nil {
		t.Fatal(err)
	}
	ids, _ = repo.ListDisabledIDs(ctx, vm.ID)
	if len(ids) != 0 {
		t.Errorf("expected no disabled ids, got %v", ids)
	}
}

func TestVirtualModelDisabledConditionsRoundTrip(t *testing.T) {
	db := setupGlobalSortTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewGlobalSortConditionRepository(db)
	repoVM := NewVirtualModelRepository(db)

	vm := &models.VirtualModel{Name: "vm1", FilterExpr: []byte(`{}`)}
	if err := repoVM.Create(ctx, vm); err != nil {
		t.Fatal(err)
	}

	c1 := &models.GlobalSortCondition{Name: "c1", SortExpr: models.SortExpr{{Key: "a"}}, Enabled: true}
	if err := repo.Create(ctx, c1); err != nil {
		t.Fatal(err)
	}

	vm.DisabledSortConditions = []int64{c1.ID}
	if err := repoVM.Update(ctx, vm); err != nil {
		t.Fatal(err)
	}

	reloaded, err := repoVM.GetByID(ctx, vm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.DisabledSortConditions) != 1 || reloaded.DisabledSortConditions[0] != c1.ID {
		t.Errorf("expected disabled ids [%d], got %v", c1.ID, reloaded.DisabledSortConditions)
	}
}

func TestGlobalFilterConditionCRUD(t *testing.T) {
	db := setupGlobalSortTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewGlobalFilterConditionRepository(db)

	enabled := true
	pos := 3
	cond := &models.GlobalFilterCondition{
		Name:       "no-codex",
		FilterExpr: models.FilterNode{Key: "m.name", Op: "neq", Value: "codex-model"},
		Enabled:    enabled,
		Position:   pos,
	}

	if err := repo.Create(ctx, cond); err != nil {
		t.Fatal(err)
	}
	if cond.ID == 0 {
		t.Fatal("expected ID assigned after create")
	}

	got, err := repo.Get(ctx, cond.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected condition, got nil")
	}
	if got.Name != "no-codex" || got.Position != 3 || !got.Enabled {
		t.Errorf("unexpected condition: %+v", got)
	}
	if got.FilterExpr.Key != "m.name" || got.FilterExpr.Op != "neq" {
		t.Errorf("unexpected filter expr: %+v", got.FilterExpr)
	}

	// Update
	got.Enabled = false
	if err := repo.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	updated, _ := repo.Get(ctx, cond.ID)
	if updated.Enabled {
		t.Error("enabled should be false after update")
	}

	// List ordering by position, id
	other := &models.GlobalFilterCondition{Name: "aaa", FilterExpr: models.FilterNode{Key: "k", Op: "eq", Value: "v"}, Enabled: true}
	if err := repo.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	all, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Name != "aaa" || all[1].Name != "no-codex" {
		t.Errorf("list ordering wrong: %+v", all)
	}

	// Delete
	if err := repo.Delete(ctx, cond.ID); err != nil {
		t.Fatal(err)
	}
	deleted, _ := repo.Get(ctx, cond.ID)
	if deleted != nil {
		t.Error("expected nil after delete")
	}
}

func TestGlobalFilterConditionDisabled(t *testing.T) {
	db := setupGlobalSortTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewGlobalFilterConditionRepository(db)
	repoVM := NewVirtualModelRepository(db)

	vm := &models.VirtualModel{Name: "vm1", FilterExpr: []byte(`{}`)}
	if err := repoVM.Create(ctx, vm); err != nil {
		t.Fatal(err)
	}

	c1 := &models.GlobalFilterCondition{Name: "c1", FilterExpr: models.FilterNode{Key: "a", Op: "eq", Value: "1"}, Enabled: true}
	c2 := &models.GlobalFilterCondition{Name: "c2", FilterExpr: models.FilterNode{Key: "b", Op: "eq", Value: "2"}, Enabled: true}
	if err := repo.Create(ctx, c1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, c2); err != nil {
		t.Fatal(err)
	}

	if err := repo.SetDisabled(ctx, vm.ID, []int64{c1.ID, c2.ID}); err != nil {
		t.Fatal(err)
	}
	ids, err := repo.ListDisabledIDs(ctx, vm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 disabled ids, got %v", ids)
	}

	// Replace with a single id
	if err := repo.SetDisabled(ctx, vm.ID, []int64{c2.ID}); err != nil {
		t.Fatal(err)
	}
	ids, _ = repo.ListDisabledIDs(ctx, vm.ID)
	if len(ids) != 1 || ids[0] != c2.ID {
		t.Errorf("expected [c2], got %v", ids)
	}

	// Clear
	if err := repo.SetDisabled(ctx, vm.ID, nil); err != nil {
		t.Fatal(err)
	}
	ids, _ = repo.ListDisabledIDs(ctx, vm.ID)
	if len(ids) != 0 {
		t.Errorf("expected no disabled ids, got %v", ids)
	}
}

func TestReorderConditions(t *testing.T) {
	db := setupGlobalSortTestDB(t)
	defer db.Close()
	ctx := context.Background()
	sortRepo := NewGlobalSortConditionRepository(db)
	filterRepo := NewGlobalFilterConditionRepository(db)

	// Insert sort conditions 1..3
	var sortIDs []int64
	for i := 0; i < 3; i++ {
		c := &models.GlobalSortCondition{Name: fmt.Sprintf("s%d", i), Enabled: true, Position: i}
		if err := sortRepo.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
		sortIDs = append(sortIDs, c.ID)
	}
	// Reorder reversed
	if err := sortRepo.Reorder(ctx, []int64{sortIDs[2], sortIDs[1], sortIDs[0]}); err != nil {
		t.Fatal(err)
	}
	all, _ := sortRepo.List(ctx)
	if all[0].ID != sortIDs[2] || all[1].ID != sortIDs[1] || all[2].ID != sortIDs[0] {
		t.Errorf("sort reorder not applied: %+v", all)
	}
	for i, c := range all {
		if c.Position != i {
			t.Errorf("expected position %d for index %d, got %d", i, i, c.Position)
		}
	}

	// Filter conditions
	var filterIDs []int64
	for i := 0; i < 3; i++ {
		c := &models.GlobalFilterCondition{Name: fmt.Sprintf("f%d", i), FilterExpr: models.FilterNode{Key: "k", Op: "eq", Value: i}, Enabled: true, Position: i}
		if err := filterRepo.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
		filterIDs = append(filterIDs, c.ID)
	}
	if err := filterRepo.Reorder(ctx, []int64{filterIDs[2], filterIDs[0], filterIDs[1]}); err != nil {
		t.Fatal(err)
	}
	fall, _ := filterRepo.List(ctx)
	if fall[0].ID != filterIDs[2] || fall[1].ID != filterIDs[0] || fall[2].ID != filterIDs[1] {
		t.Errorf("filter reorder not applied: %+v", fall)
	}
}
