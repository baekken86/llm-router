package repository

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestGetByModel_CFPrefixStripping(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create table
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

	// Insert canonical model names (what CF prefix strips to)
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
			name:      "strips @cf/meta/ prefix",
			input:     "@cf/meta/llama-3.1-8b-instruct",
			expectHit: true,
		},
		{
			name:      "strips @cf/google/ prefix",
			input:     "@cf/google/gemma-3-12b-it",
			expectHit: true,
		},
		{
			name:      "plain name unchanged",
			input:     "llama-3.1-8b",
			expectHit: false,
		},
		{
			name:      "malformed @cf/ no parts",
			input:     "@cf/",
			expectHit: false,
		},
		{
			name:      "only two parts unchanged",
			input:     "@cf/meta",
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

func TestGetByModelEffort_CFPrefixStripping(t *testing.T) {
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

	// Seed data matching what GetByModel test uses
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
			name:      "strips @cf/meta/ prefix with effort",
			model:     "@cf/meta/llama-3.1-8b-instruct",
			effort:    "default",
			expectHit: true,
		},
		{
			name:      "strips @cf/google/ prefix with effort",
			model:     "@cf/google/gemma-3-12b-it",
			effort:    "high",
			expectHit: true,
		},
		{
			name:      "plain name with effort",
			model:     "llama-3.1-8b-instruct",
			effort:    "default",
			expectHit: true,
		},
		{
			name:      "CF prefix wrong effort returns empty",
			model:     "@cf/meta/llama-3.1-8b-instruct",
			effort:    "high",
			expectHit: false,
		},
		{
			name:      "unknown model returns empty",
			model:     "@cf/meta/nonexistent-model",
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
