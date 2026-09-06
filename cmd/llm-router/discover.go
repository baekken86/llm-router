package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/proxy"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
)

// cliSilentLogger is the OAuth service logger for the discover command (the
// CLI prints its own progress; OAuth refresh chatter would be noise here).
var cliSilentLogger = slog.New(slog.NewTextHandler(cliSilentWriter{}, nil))

type cliSilentWriter struct{}

func (cliSilentWriter) Write(p []byte) (int, error) { return len(p), nil }

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
	oauthRepo := repository.NewOAuthRepository(database)

	ctx := context.Background()

	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, loadEncryptionKey())
	modelService := buildDiscoverModelService(providerRepo, oauthRepo, modelRepo, tagRepo, providerService, globalRepo)

	provider, err := providerRepo.GetByName(ctx, *providerName)
	if err != nil || provider == nil {
		logger.Error("provider not found", "name", *providerName)
		os.Exit(1)
	}

	fmt.Printf("Discovering models from %s...\n", *providerName)

	result, err := modelService.Discover(ctx, provider.ID)
	if err != nil {
		logger.Error("discovery failed", "error", err)
		os.Exit(1)
	}

	if len(result.Added) > 0 {
		fmt.Printf("\n+ %d new models discovered\n", len(result.Added))
	}
	if len(result.Removed) > 0 {
		fmt.Printf("\n- %d models removed from provider\n", len(result.Removed))
	}
}

// buildDiscoverModelService constructs the discover command's model service
// with the codex live-catalog discovery wired (§4.7): the proxy CodexClient
// lister plus the OAuth token/account sources — the same wiring cmd main
// uses for the proxy's model service. Without it, codex discovery would
// always fall back to the static seed list.
func buildDiscoverModelService(
	providerRepo repository.ProviderRepository,
	oauthRepo repository.OAuthRepository,
	modelRepo repository.ModelRepository,
	tagRepo repository.TagRepository,
	providerService service.ProviderService,
	globalRepo repository.GlobalMetadataRepository,
) service.ModelService {
	modelService := service.NewModelService(modelRepo, tagRepo, providerRepo, providerService, globalRepo, nil)
	oauthService := service.NewOAuthService(oauthRepo, providerRepo, providerService, cliSilentLogger)
	service.SetCodexDiscovery(modelService, oauthService, oauthRepo, codexProxyModelLister{client: proxy.NewCodexClient()})
	return modelService
}
