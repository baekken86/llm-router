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

func runAddProvider(args []string) {
	fs := flag.NewFlagSet("add-provider", flag.ExitOnError)
	dbPath := fs.String("db", "./data/llm-router.db", "SQLite database path")
	name := fs.String("name", "", "Provider name (required)")
	apiType := fs.String("type", "", "API type: openai or anthropic (auto-detected if not set)")
	baseURL := fs.String("url", "", "Base URL (required)")
	apiKey := fs.String("key", "", "API key (required)")
	fs.Parse(args)

	if *name == "" || *baseURL == "" || *apiKey == "" {
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  llm-router add-provider --name opencode-go --url https://opencode.ai/zen/go --key sk-...")
		fmt.Fprintln(os.Stderr, "  llm-router add-provider --name anthropic --type anthropic --url https://api.anthropic.com/v1 --key sk-ant-...")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "API types: openai (default), anthropic")
		os.Exit(1)
	}

	if *apiType == "" {
		if contains(*baseURL, "anthropic") || contains(*baseURL, "/messages") {
			*apiType = "anthropic"
		} else {
			*apiType = "openai"
		}
	}

	if *apiType != "openai" && *apiType != "anthropic" {
		fmt.Fprintln(os.Stderr, "Error: --type must be 'openai' or 'anthropic'")
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

	existing, _ := providerRepo.GetByName(ctx, *name)
	if existing != nil {
		logger.Error("provider already exists", "name", *name)
		os.Exit(1)
	}

	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, make([]byte, 32))
	_, err = providerService.Create(ctx, models.CreateProviderRequest{
		Name:    *name,
		APIType: models.APIType(*apiType),
		BaseURL: *baseURL,
		APIKey:  *apiKey,
	})
	if err != nil {
		logger.Error("failed to create provider", "error", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Provider '%s' created\n", *name)
	fmt.Printf("  Type: %s\n", *apiType)
	fmt.Printf("  URL:  %s\n", *baseURL)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Discover models: llm-router discover --provider %s\n", *name)
	fmt.Printf("  2. Tag models:      llm-router tag --model <name> --set intel=85\n")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr)))
}
