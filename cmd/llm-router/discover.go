package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
)

func runDiscover(args []string) {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
	providerName := fs.String("provider", "", "Provider name (required)")
	fs.Parse(args)

	if *providerName == "" {
		fmt.Fprintln(os.Stderr, "Usage: llm-router discover --provider opencode-go")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	database, err := db.Open(*dbPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	providerRepo := repository.NewProviderRepository(database)
	providerMetadataRepo := repository.NewProviderMetadataRepository(database)
	modelRepo := repository.NewModelRepository(database)
	tagRepo := repository.NewTagRepository(database)
	globalRepo := repository.NewGlobalMetadataRepository(database)

	ctx := context.Background()

	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, make([]byte, 32))
	modelService := service.NewModelService(modelRepo, tagRepo, providerRepo, providerService)

	provider, err := providerRepo.GetByName(ctx, *providerName)
	if err != nil || provider == nil {
		logger.Error("provider not found", "name", *providerName)
		os.Exit(1)
	}

	fmt.Printf("Discovering models from %s...\n", *providerName)

	models, err := modelService.Discover(ctx, provider.ID)
	if err != nil {
		logger.Error("discovery failed", "error", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ Found %d models\n", len(models))

	autoTagged := 0
	for _, m := range models {
		metadata, err := globalRepo.GetByModel(ctx, m.Name)
		if err != nil || len(metadata) == 0 {
			continue
		}

		for effort, tags := range metadata {
			if err := tagRepo.Set(ctx, m.ID, effort, tags); err != nil {
				logger.Warn("failed to auto-tag", "model", m.Name, "error", err)
				continue
			}
			autoTagged++
		}
	}

	if autoTagged > 0 {
		fmt.Printf("✓ Auto-tagged %d models from global metadata\n", autoTagged)
	} else {
		fmt.Println("\nNo global metadata found. Run 'llm-router init' first to import benchmark data.")
	}
}
