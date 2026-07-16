package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
)

func runSetKey(args []string) {
	fs := flag.NewFlagSet("set-key", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
	providerName := fs.String("provider", "", "Provider name (required)")
	apiKey := fs.String("key", "", "API key (required)")
	fs.Parse(args)

	if *providerName == "" || *apiKey == "" {
		fmt.Fprintln(os.Stderr, "Usage: llm-router set-key --provider opencode-go --key sk-...")
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

	ctx := context.Background()
	provider, err := providerRepo.GetByName(ctx, *providerName)
	if err != nil || provider == nil {
		logger.Error("provider not found", "name", *providerName)
		os.Exit(1)
	}

	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, make([]byte, 32))
	_, err = providerService.Update(ctx, provider.ID, models.UpdateProviderRequest{
		APIKey: apiKey,
	})
	if err != nil {
		logger.Error("failed to update key", "error", err)
		os.Exit(1)
	}

	fmt.Printf("✓ API key set for %s\n", *providerName)
}
