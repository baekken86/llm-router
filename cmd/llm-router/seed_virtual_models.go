package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

type virtualModelSeed struct {
	Name       string
	FilterExpr models.FilterExpr
	SortExpr   models.SortExpr
}

func runSeedVirtualModels(args []string) {
	fs := flag.NewFlagSet("seed-virtual-models", flag.ExitOnError)
	dbPath := fs.String("db", "./data/llm-router.db", "SQLite database path")
	fs.Parse(args)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	database, err := db.Open(*dbPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	vmRepo := repository.NewVirtualModelRepository(database)
	ctx := context.Background()

	seeds := []virtualModelSeed{
		{
			Name: "docs",
			FilterExpr: models.FilterExpr{
				And: []models.FilterCondition{
					{Key: "intelligence", Op: "gte", Value: "80"},
					{Key: "hallucination", Op: "lte", Value: "20"},
				},
			},
			SortExpr: models.SortExpr{
				{Key: "intelligence", Direction: "desc"},
			},
		},
		{
			Name: "exploration",
			FilterExpr: models.FilterExpr{
				And: []models.FilterCondition{
					{Key: "intelligence", Op: "gte", Value: "75"},
					{Key: "coding", Op: "gte", Value: "70"},
				},
			},
			SortExpr: models.SortExpr{
				{Key: "intelligence", Direction: "desc"},
			},
		},
		{
			Name: "planning",
			FilterExpr: models.FilterExpr{
				And: []models.FilterCondition{
					{Key: "intelligence", Op: "gte", Value: "85"},
				},
			},
			SortExpr: models.SortExpr{
				{Key: "intelligence", Direction: "desc"},
			},
		},
		{
			Name: "refine",
			FilterExpr: models.FilterExpr{
				And: []models.FilterCondition{
					{Key: "coding", Op: "gte", Value: "75"},
					{Key: "hallucination", Op: "lte", Value: "25"},
				},
			},
			SortExpr: models.SortExpr{
				{Key: "coding", Direction: "desc"},
			},
		},
		{
			Name: "troubleshooting",
			FilterExpr: models.FilterExpr{
				And: []models.FilterCondition{
					{Key: "coding", Op: "gte", Value: "70"},
					{Key: "intelligence", Op: "gte", Value: "70"},
				},
			},
			SortExpr: models.SortExpr{
				{Key: "coding", Direction: "desc"},
				{Key: "intelligence", Direction: "desc"},
			},
		},
		{
			Name: "brainstorming",
			FilterExpr: models.FilterExpr{
				And: []models.FilterCondition{
					{Key: "intelligence", Op: "gte", Value: "80"},
				},
			},
			SortExpr: models.SortExpr{
				{Key: "intelligence", Direction: "desc"},
			},
		},
		{
			Name: "chat",
			FilterExpr: models.FilterExpr{
				And: []models.FilterCondition{
					{Key: "intelligence", Op: "gte", Value: "50"},
					{Key: "cost_type", Op: "eq", Value: "free"},
				},
			},
			SortExpr: models.SortExpr{
				{Key: "intelligence", Direction: "desc"},
			},
		},
	}

	created := 0
	for _, seed := range seeds {
		filterJSON, _ := json.Marshal(seed.FilterExpr)
		sortJSON, _ := json.Marshal(seed.SortExpr)

		err = vmRepo.Create(ctx, &models.VirtualModel{
			Name:       seed.Name,
			FilterExpr: filterJSON,
			SortExpr:   sortJSON,
		})
		if err != nil {
			fmt.Printf("  ⚠ %s: %v\n", seed.Name, err)
			continue
		}
		fmt.Printf("  + %s\n", seed.Name)
		created++
	}

	fmt.Printf("\n✓ Created %d virtual models\n", created)
}
