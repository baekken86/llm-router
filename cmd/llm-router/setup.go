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

var supportedProviders = map[string]providerConfig{
	"claude-code": {
		name:    "claude-code",
		apiType: "anthropic",
		baseURL: "https://api.anthropic.com/v1",
		auth:    "oauth",
	},
	"opencode-go": {
		name:    "opencode-go",
		apiType: "openai",
		baseURL: "https://opencode.ai/zen/go",
		auth:    "apikey",
	},
	"openai": {
		name:    "openai",
		apiType: "openai",
		baseURL: "https://api.openai.com/v1",
		auth:    "apikey",
	},
	"anthropic": {
		name:    "anthropic",
		apiType: "anthropic",
		baseURL: "https://api.anthropic.com/v1",
		auth:    "apikey",
	},
	"openrouter": {
		name:    "openrouter",
		apiType: "openai",
		baseURL: "https://openrouter.ai/api/v1",
		auth:    "apikey",
	},
}

type providerConfig struct {
	name    string
	apiType string
	baseURL string
	auth    string
}

func runSetup(args []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	dbPath := fs.String("db", "./data/llm-router.db", "SQLite database path")
	providerName := fs.String("provider", "", "Provider name (required). Supported: claude-code, opencode-go, openai, anthropic, openrouter")
	apiKey := fs.String("key", "", "API key (required for API key providers)")
	baseURL := fs.String("url", "", "Custom base URL (optional, overrides default)")
	fs.Parse(args)

	if *providerName == "" {
		printSupportedProviders()
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
	oauthRepo := repository.NewOAuthRepository(database)
	globalRepo := repository.NewGlobalMetadataRepository(database)

	ctx := context.Background()

	providerCfg, isKnown := supportedProviders[*providerName]
	if !isKnown {
		if *apiKey == "" || *baseURL == "" {
			logger.Error("unknown provider, use --url and --key for custom providers")
			os.Exit(1)
		}
		providerCfg = providerConfig{
			name:    *providerName,
			apiType: "openai",
			baseURL: *baseURL,
			auth:    "apikey",
		}
	}

	if *baseURL != "" {
		providerCfg.baseURL = *baseURL
	}

	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, make([]byte, 32))
	modelService := service.NewModelService(modelRepo, tagRepo, providerRepo, providerService)

	existing, _ := providerRepo.GetByName(ctx, *providerName)
	if existing != nil {
		logger.Info("provider already exists", "name", *providerName)

		if providerCfg.auth == "oauth" {
			handleOAuthSetup(ctx, existing, oauthRepo, providerRepo, providerService, logger)
			createPredefinedModels(ctx, modelRepo, tagRepo, globalRepo, existing, providerCfg.name, logger)
		} else {
			if *apiKey != "" {
				updateProviderKey(ctx, providerService, existing, *apiKey, logger)
			}
			discoverModels(ctx, modelService, existing, logger)
		}
		return
	}

	if providerCfg.auth == "apikey" && *apiKey == "" {
		logger.Error("--key is required for this provider")
		os.Exit(1)
	}

	provider, err := providerService.Create(ctx, models.CreateProviderRequest{
		Name:    providerCfg.name,
		APIType: models.APIType(providerCfg.apiType),
		BaseURL: providerCfg.baseURL,
		APIKey:  *apiKey,
	})
	if err != nil {
		logger.Error("failed to create provider", "error", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Provider '%s' created\n", providerCfg.name)
	fmt.Printf("  Type: %s\n", providerCfg.apiType)
	fmt.Printf("  URL:  %s\n", providerCfg.baseURL)

	if providerCfg.auth == "oauth" {
		handleOAuthSetup(ctx, provider, oauthRepo, providerRepo, providerService, logger)
		createPredefinedModels(ctx, modelRepo, tagRepo, globalRepo, provider, providerCfg.name, logger)
	} else {
		discoverModels(ctx, modelService, provider, logger)
	}
}

func handleOAuthSetup(ctx context.Context, provider *models.Provider, oauthRepo repository.OAuthRepository, providerRepo repository.ProviderRepository, providerService service.ProviderService, logger *slog.Logger) {
	oauthService := service.NewOAuthService(oauthRepo, providerRepo, providerService, logger)

	connected, _, err := oauthService.IsConnected(ctx, provider.ID)
	if err == nil && connected {
		fmt.Println("✓ Already connected via OAuth")
		return
	}

	authURL, state, err := oauthService.StartAuthFlow(ctx, provider.ID)
	if err != nil {
		logger.Error("failed to start OAuth flow", "error", err)
		return
	}

	fmt.Println()
	fmt.Println("=== OAuth Authorization ===")
	fmt.Println()
	fmt.Printf("1. Open this URL in your browser:\n\n   %s\n\n", authURL)
	fmt.Println("2. Login and authorize")
	fmt.Println("3. Copy the callback URL from browser and paste it here:")
	fmt.Println()

	fmt.Print("Paste callback URL: ")
	var callbackURL string
	fmt.Scanln(&callbackURL)

	code, returnedState := extractCodeAndState(callbackURL)
	if code == "" {
		logger.Error("could not extract code from URL")
		return
	}

	if returnedState != "" {
		state = returnedState
	}

	token, err := oauthService.HandleCallback(ctx, code, state)
	if err != nil {
		logger.Error("OAuth failed", "error", err)
		return
	}

	fmt.Printf("\n✓ Connected! Expires: %s\n", token.ExpiresAt.Format("2006-01-02 15:04"))
}

func updateProviderKey(ctx context.Context, providerService service.ProviderService, provider *models.Provider, apiKey string, logger *slog.Logger) {
	_, err := providerService.Update(ctx, provider.ID, models.UpdateProviderRequest{
		APIKey: &apiKey,
	})
	if err != nil {
		logger.Error("failed to update API key", "error", err)
	} else {
		fmt.Println("✓ API key updated")
	}
}

func discoverModels(ctx context.Context, modelService service.ModelService, provider *models.Provider, logger *slog.Logger) {
	fmt.Printf("\nDiscovering models from %s...\n", provider.Name)

	models, err := modelService.Discover(ctx, provider.ID)
	if err != nil {
		logger.Warn("discovery failed (you can add models manually)", "error", err)
		return
	}

	fmt.Printf("✓ Found %d models\n", len(models))
	for _, m := range models {
		fmt.Printf("  - %s\n", m.Name)
	}
}

func createPredefinedModels(ctx context.Context, modelRepo repository.ModelRepository, tagRepo repository.TagRepository, globalRepo repository.GlobalMetadataRepository, provider *models.Provider, providerName string, logger *slog.Logger) {
	predefined := map[string][]string{
		"claude-code": {
			"claude-opus-4-6",
			"claude-sonnet-4-6",
			"claude-haiku-4-5",
			"claude-sonnet-5",
			"claude-opus-4-7",
			"claude-opus-4-8",
			"claude-fable-5",
		},
	}

	modelNames, exists := predefined[providerName]
	if !exists {
		return
	}

	fmt.Printf("\nCreating predefined models for %s...\n", providerName)

	created := 0
	for _, name := range modelNames {
		m := &models.Model{
			ProviderID: provider.ID,
			Name:       name,
		}
		if err := modelRepo.Create(ctx, m); err != nil {
			continue
		}

		metadata, err := globalRepo.GetByModel(ctx, name)
		if err == nil && len(metadata) > 0 {
			for effort, tags := range metadata {
				tagRepo.Set(ctx, m.ID, effort, tags)
			}
		}

		created++
		fmt.Printf("  + %s\n", name)
	}

	fmt.Printf("✓ Created %d predefined models\n", created)
}

func importDefaultCSV(ctx context.Context, modelService service.ModelService, logger *slog.Logger) {
	jsonPath := "data/models.json"
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		jsonPath = "../data/models.json"
		if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
			return
		}
	}

	fmt.Printf("\nImporting default metadata from %s...\n", jsonPath)

	f, err := os.Open(jsonPath)
	if err != nil {
		logger.Warn("could not open default JSON", "error", err)
		return
	}
	defer f.Close()

	importService := service.NewImportService(nil, nil)
	result, err := importService.ImportJSON(ctx, f, service.ImportModeMerge)
	if err != nil {
		logger.Warn("could not import default JSON", "error", err)
		return
	}

	fmt.Printf("✓ Imported metadata for %d models\n", result.Imported)
}

func printSupportedProviders() {
	fmt.Println("Usage: llm-router setup --provider <name> [--key <api-key>]")
	fmt.Println()
	fmt.Println("Supported providers:")
	fmt.Println()
	fmt.Println("  OAuth (no API key needed):")
	fmt.Println("    claude-code     Anthropic Claude (subscription)")
	fmt.Println()
	fmt.Println("  API Key (--key required):")
	fmt.Println("    opencode-go     OpenCode Go ($10/mo)")
	fmt.Println("    openai          OpenAI (GPT-4o, o3, etc.)")
	fmt.Println("    anthropic       Anthropic API (Claude)")
	fmt.Println("    openrouter      OpenRouter (multi-provider)")
	fmt.Println()
	fmt.Println("  Custom (requires --url and --key):")
	fmt.Println("    llm-router setup --provider my-provider --url https://api.example.com/v1 --key sk-...")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  llm-router setup --provider claude-code")
	fmt.Println("  llm-router setup --provider opencode-go --key sk-...")
	fmt.Println("  llm-router setup --provider openai --key sk-...")
}
