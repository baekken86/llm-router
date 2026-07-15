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

func runConnect(args []string) {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	dbPath := fs.String("db", "./data/llm-router.db", "SQLite database path")
	providerName := fs.String("provider", "claude-code", "Provider name to connect")
	fs.Parse(args)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	database, err := db.Open(*dbPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	providerRepo := repository.NewProviderRepository(database)
	oauthRepo := repository.NewOAuthRepository(database)
	providerService := service.NewProviderService(providerRepo, []byte("00000000000000000000000000000000"))
	oauthService := service.NewOAuthService(oauthRepo, providerRepo, providerService, logger)

	ctx := context.Background()

	provider, err := providerRepo.GetByName(ctx, *providerName)
	if err != nil || provider == nil {
		logger.Error("provider not found", "name", *providerName)
		fmt.Println("\nRun 'llm-router setup' first to create the provider.")
		os.Exit(1)
	}

	connected, token, err := oauthService.IsConnected(ctx, provider.ID)
	if err != nil {
		logger.Error("failed to check connection", "error", err)
		os.Exit(1)
	}

	if connected {
		logger.Info("already connected",
			"provider", *providerName,
			"email", token.Email,
			"account_id", token.AccountID,
		)
		return
	}

	authURL, state, err := oauthService.StartAuthFlow(ctx, provider.ID)
	if err != nil {
		logger.Error("failed to start auth flow", "error", err)
		os.Exit(1)
	}

	fmt.Println("\n=== llm-router - Connect to Claude Code ===")
	fmt.Println()
	fmt.Println("1. Open this URL in your browser:")
	fmt.Println()
	fmt.Printf("   %s\n", authURL)
	fmt.Println()
	fmt.Println("2. Login with your Anthropic account")
	fmt.Println("3. Authorize the application")
	fmt.Println()
	fmt.Println("Waiting for callback...")

	token, err = oauthService.StartCallbackServer(ctx, state, provider.ID)
	if err != nil {
		logger.Error("auth failed", "error", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("✓ Connected successfully!")
	fmt.Printf("  Account: %s\n", token.AccountID)
	fmt.Printf("  Email:   %s\n", token.Email)
	fmt.Println()
	fmt.Println("You can now use Claude Code models in your virtual models.")
}
