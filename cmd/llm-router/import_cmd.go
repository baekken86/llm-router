package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
)

func runImportCmd(args []string) {
	home, _ := os.UserHomeDir()
	defaultDB := filepath.Join(home, ".local", "share", "llm-router", "llm-router.db")

	fs := flag.NewFlagSet("import", flag.ExitOnError)
	filePath := fs.String("file", "", "JSON file path (required)")
	mode := fs.String("mode", "merge", "Import mode: merge or replace")
	dbPath := fs.String("db", defaultDB, "SQLite database path")
	fs.Parse(args)

	if *filePath == "" {
		fmt.Fprintln(os.Stderr, "Error: --file is required")
		fmt.Fprintln(os.Stderr, "Usage: llm-router import --file models.json --mode merge")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	database, err := db.Open(*dbPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	modelRepo := repository.NewModelRepository(database)
	tagRepo := repository.NewTagRepository(database)
	importService := service.NewImportService(modelRepo, tagRepo)

	f, err := os.Open(*filePath)
	if err != nil {
		logger.Error("failed to open file", "error", err, "file", *filePath)
		os.Exit(1)
	}
	defer f.Close()

	logger.Info("importing metadata", "file", *filePath, "mode", *mode)

	result, err := importService.ImportJSON(context.Background(), f, service.ImportMode(*mode))
	if err != nil {
		logger.Error("import failed", "error", err)
		os.Exit(1)
	}

	logger.Info("import completed",
		"total_models", result.TotalRows,
		"imported", result.Imported,
		"skipped", result.Skipped,
		"matched_models", len(result.MatchedModels),
	)

	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			logger.Warn("import warning", "detail", e)
		}
	}
}
