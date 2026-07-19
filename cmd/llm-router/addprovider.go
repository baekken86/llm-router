package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
)

func runAddProvider(args []string) {
	fs := flag.NewFlagSet("add-provider", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
	name := fs.String("name", "", "Provider name (required)")
	apiType := fs.String("type", "", "API type: openai or anthropic (auto-detected if not set)")
	baseURL := fs.String("url", "", "Base URL (required)")
	apiKey := fs.String("key", "", "API key (required)")
	accountID := fs.String("account-id", "", "Account ID (required for cloudflare providers)")
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

	if *apiType != "openai" && *apiType != "anthropic" && *apiType != "cloudflare" {
		fmt.Fprintln(os.Stderr, "Error: --type must be 'openai', 'anthropic', or 'cloudflare'")
		os.Exit(1)
	}

	if *apiType == "cloudflare" && strings.TrimSpace(*accountID) == "" {
		fmt.Fprintln(os.Stderr, "Error: --account-id is required for cloudflare providers")
		os.Exit(1)
	}

	if *accountID != "" && *apiType != "cloudflare" {
		fmt.Fprintln(os.Stderr, "Warning: --account-id ignored (only used for cloudflare)")
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

	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, loadEncryptionKey())

	var createReq models.CreateProviderRequest
	if *apiType == "cloudflare" {
		createReq = models.CreateProviderRequest{
			Name:      *name,
			APIType:   models.APIType(*apiType),
			BaseURL:   fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai", *accountID),
			APIKey:    *apiKey,
			AccountID: *accountID,
		}
	} else {
		createReq = models.CreateProviderRequest{
			Name:    *name,
			APIType: models.APIType(*apiType),
			BaseURL: *baseURL,
			APIKey:  *apiKey,
		}
	}

	provider, err := providerService.Create(ctx, createReq)
	if err != nil {
		logger.Error("failed to create provider", "error", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Provider '%s' created\n", *name)
	fmt.Printf("  Type: %s\n", *apiType)
	if *apiType == "cloudflare" {
		fmt.Printf("  URL:  %s\n", fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai", *accountID))
	} else {
		fmt.Printf("  URL:  %s\n", *baseURL)
	}
	fmt.Println()

	if *apiType == "cloudflare" {
		// For cloudflare, add 9 predefined models
		modelRepo := repository.NewModelRepository(database)
		tagRepo := repository.NewTagRepository(database)
		globalRepo := repository.NewGlobalMetadataRepository(database)
		createPredefinedModels(ctx, modelRepo, tagRepo, globalRepo, provider, "cloudflare", logger)
	} else {
		fmt.Println("Next steps:")
		fmt.Printf("  1. Discover models: llm-router discover --provider %s\n", *name)
		fmt.Printf("  2. Tag models:      llm-router tag --model <name> --set intel=85\n")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr)))
}
