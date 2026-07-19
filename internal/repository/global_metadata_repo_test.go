package repository

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestGetByModel_DirectMatch(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE model_metadata_global (
		model_name TEXT NOT NULL,
		reasoning_effort TEXT NOT NULL,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		PRIMARY KEY (model_name, reasoning_effort, key)
	)`)
	if err != nil {
		t.Fatal(err)
	}

	// Insert with exact model names (no prefix stripping)
	_, err = db.Exec(`INSERT INTO model_metadata_global (model_name, reasoning_effort, key, value)
		VALUES ('llama-3.1-8b-instruct', 'default', 'intelligence', '5.0')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO model_metadata_global (model_name, reasoning_effort, key, value)
		VALUES ('gemma-3-12b-it', 'default', 'intelligence', '6.0')`)
	if err != nil {
		t.Fatal(err)
	}

	repo := NewGlobalMetadataRepository(db)
	ctx := context.Background()

	tests := []struct {
		name      string
		input     string
		expectHit bool
	}{
		{
			name:      "exact match llama",
			input:     "llama-3.1-8b-instruct",
			expectHit: true,
		},
		{
			name:      "exact match gemma",
			input:     "gemma-3-12b-it",
			expectHit: true,
		},
		{
			name:      "cf prefixed name no longer matches",
			input:     "@cf/meta/llama-3.1-8b-instruct",
			expectHit: false,
		},
		{
			name:      "unknown model returns empty",
			input:     "nonexistent-model",
			expectHit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetByModel(ctx, tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.expectHit {
				if len(result) == 0 {
					t.Errorf("expected non-empty result for %q", tt.input)
				}
			} else {
				if len(result) > 0 {
					t.Errorf("expected empty result for %q, got %v", tt.input, result)
				}
			}
		})
	}
}

func TestGetByModelEffort_DirectMatch(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE model_metadata_global (
		model_name TEXT NOT NULL,
		reasoning_effort TEXT NOT NULL,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		PRIMARY KEY (model_name, reasoning_effort, key)
	)`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`INSERT INTO model_metadata_global (model_name, reasoning_effort, key, value)
		VALUES ('llama-3.1-8b-instruct', 'default', 'intelligence', '5.0')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO model_metadata_global (model_name, reasoning_effort, key, value)
		VALUES ('gemma-3-12b-it', 'high', 'intelligence', '7.0')`)
	if err != nil {
		t.Fatal(err)
	}

	repo := NewGlobalMetadataRepository(db)
	ctx := context.Background()

	tests := []struct {
		name      string
		model     string
		effort    string
		expectHit bool
	}{
		{
			name:      "exact match with effort",
			model:     "llama-3.1-8b-instruct",
			effort:    "default",
			expectHit: true,
		},
		{
			name:      "cf prefixed no longer matches",
			model:     "@cf/meta/llama-3.1-8b-instruct",
			effort:    "default",
			expectHit: false,
		},
		{
			name:      "wrong effort returns empty",
			model:     "llama-3.1-8b-instruct",
			effort:    "high",
			expectHit: false,
		},
		{
			name:      "unknown model returns empty",
			model:     "nonexistent-model",
			effort:    "default",
			expectHit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetByModelEffort(ctx, tt.model, tt.effort)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.expectHit {
				if len(result) == 0 {
					t.Errorf("expected non-empty result for %q effort %q", tt.model, tt.effort)
				}
			} else {
				if len(result) > 0 {
					t.Errorf("expected empty result for %q effort %q, got %v", tt.model, tt.effort, result)
				}
			}
		})
	}
}
