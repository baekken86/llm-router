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
	"github.com/chris/llm-router/internal/proxy"
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
	"opencode-zen": {
		name:    "opencode-zen",
		apiType: "openai",
		baseURL: "https://opencode.ai/zen",
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
	"zai": {
		name:    "zai",
		apiType: "anthropic",
		baseURL: "https://api.z.ai/api/anthropic/v1",
		auth:    "apikey",
	},
	"openrouter": {
		name:    "openrouter",
		apiType: "openai",
		baseURL: "https://openrouter.ai/api/v1",
		auth:    "apikey",
	},
	"cloudflare": {
		name:    "cloudflare",
		apiType: "cloudflare",
		baseURL: "https://api.cloudflare.com/client/v4/accounts/{account_id}/ai",
		auth:    "apikey",
	},
	"ollama": {
		name:    "ollama",
		apiType: "ollama",
		baseURL: "http://{host}/v1",
		auth:    "none",
	},
	"chatgpt": {
		name:    "chatgpt",
		apiType: "codex",
		baseURL: "https://chatgpt.com/backend-api/codex",
		auth:    "oauth",
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
	dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
	providerName := fs.String("provider", "", "Provider name (required). Supported: claude-code, chatgpt (alias: codex), opencode-go, opencode-zen, openai, anthropic, zai, openrouter, cloudflare")
	manual := fs.Bool("manual", false, "OAuth only: paste the callback URL manually instead of the local callback server")
	apiKey := fs.String("key", "", "API key (required for API key providers)")
	baseURL := fs.String("url", "", "Custom base URL (optional, overrides default)")
	accountID := fs.String("account-id", "", "Account ID (required for cloudflare)")
	host := fs.String("host", "localhost:11434", "Ollama host (default localhost:11434)")
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

	// "codex" is an accepted alias of the canonical "chatgpt" setup entry.
	providerKey := normalizeChatgptProviderName(*providerName)
	providerCfg, isKnown := supportedProviders[providerKey]
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

	// Validate cloudflare account_id
	if providerCfg.apiType == "cloudflare" {
		if *accountID == "" {
			logger.Error("--account-id is required for cloudflare provider")
			os.Exit(1)
		}
		// Substitute {account_id} in baseURL
		providerCfg.baseURL = strings.Replace(providerCfg.baseURL, "{account_id}", *accountID, 1)
	}

	// Substitute {host} in ollama baseURL
	if providerCfg.apiType == "ollama" {
		isLocal := strings.HasPrefix(*host, "localhost:") || *host == "localhost"
		if isLocal {
			providerCfg.baseURL = "http://" + *host + "/v1"
		} else {
			providerCfg.baseURL = "https://" + *host + "/v1"
			providerCfg.auth = "apikey"
		}
	}

	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, loadEncryptionKey())
	modelService := service.NewModelService(modelRepo, tagRepo, providerRepo, providerService, globalRepo, nil)
	oauthService := service.NewOAuthService(oauthRepo, providerRepo, providerService, logger)

	// Codex model discovery (design §4.7): wire the live-catalog collaborators
	// so `setup --provider chatgpt` (and the discovery step below) can fetch
	// the real codex catalog instead of only the static seed list. Same
	// wiring as main.go's proxy path.
	service.SetCodexDiscovery(modelService, oauthService, oauthRepo, codexProxyModelLister{client: proxy.NewCodexClient()})

	existing, _ := providerRepo.GetByName(ctx, providerCfg.name)
	if existing == nil && providerCfg.apiType == "codex" {
		// Codex alias: reuse an existing ChatGPT provider row stored under
		// the other name (chatgpt/codex) instead of creating a duplicate
		// (same convergence rule as connect's ensureChatgptProvider).
		aliasName := "codex"
		if providerCfg.name == "codex" {
			aliasName = "chatgpt"
		}
		existing, _ = providerRepo.GetByName(ctx, aliasName)
	}
	if existing != nil {
		logger.Info("provider already exists", "name", providerCfg.name)

		if providerCfg.apiType == "cloudflare" {
			if *apiKey != "" {
				updateProviderKey(ctx, providerService, existing, *apiKey, logger)
			}
			createPredefinedModels(ctx, modelRepo, tagRepo, globalRepo, existing, "cloudflare", logger)
		} else if providerCfg.auth == "oauth" {
			if providerCfg.apiType == "codex" {
				// ChatGPT (codex): same shared flow as connect --provider chatgpt.
				// A failed OAuth exchange must not skip predefined-model creation
				// (mirrors handleOAuthSetup's log-and-continue behavior).
				if err := runChatgptConnect(ctx, existing, oauthService, logger, *manual); err != nil {
					logger.Warn("ChatGPT OAuth failed (models still created)", "error", err)
				}
			} else {
				handleOAuthSetup(ctx, existing, oauthRepo, providerRepo, providerService, logger)
			}
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
		Name:      providerCfg.name,
		APIType:   models.APIType(providerCfg.apiType),
		BaseURL:   providerCfg.baseURL,
		APIKey:    *apiKey,
		AccountID: *accountID,
	})
	if err != nil {
		logger.Error("failed to create provider", "error", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Provider '%s' created\n", providerCfg.name)
	fmt.Printf("  Type: %s\n", providerCfg.apiType)
	fmt.Printf("  URL:  %s\n", providerCfg.baseURL)

	if providerCfg.apiType == "cloudflare" {
		createPredefinedModels(ctx, modelRepo, tagRepo, globalRepo, provider, "cloudflare", logger)
	} else if providerCfg.auth == "oauth" {
		if providerCfg.apiType == "codex" {
			// ChatGPT (codex): same shared flow as connect --provider chatgpt.
			// A failed OAuth exchange must not skip predefined-model creation
			// (mirrors handleOAuthSetup's log-and-continue behavior).
			if err := runChatgptConnect(ctx, provider, oauthService, logger, *manual); err != nil {
				logger.Warn("ChatGPT OAuth failed (models still created)", "error", err)
			}
		} else {
			handleOAuthSetup(ctx, provider, oauthRepo, providerRepo, providerService, logger)
		}
		createPredefinedModels(ctx, modelRepo, tagRepo, globalRepo, provider, providerCfg.name, logger)
	} else {
		discoverModels(ctx, modelService, provider, logger)
	}
}

func handleOAuthSetup(ctx context.Context, provider *models.Provider, oauthRepo repository.OAuthRepository, providerRepo repository.ProviderRepository, providerService service.ProviderService, logger *slog.Logger) {
	oauthService := service.NewOAuthService(oauthRepo, providerRepo, providerService, logger)

	oauthRepo.Delete(ctx, provider.ID)

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

	result, err := modelService.Discover(ctx, provider.ID)
	if err != nil {
		logger.Warn("discovery failed (you can add models manually)", "error", err)
		return
	}

	if len(result.Added) > 0 {
		fmt.Printf("+ %d new models discovered\n", len(result.Added))
	}
	if len(result.Removed) > 0 {
		fmt.Printf("- %d models removed from provider\n", len(result.Removed))
	}
}

func createPredefinedModels(ctx context.Context, modelRepo repository.ModelRepository, tagRepo repository.TagRepository, globalRepo repository.GlobalMetadataRepository, provider *models.Provider, providerName string, logger *slog.Logger) {
	predefined := map[string][]string{
		"claude-code": {
			"claude-opus-4-6",
			"claude-sonnet-4-6",
			"claude-sonnet-5",
			"claude-opus-4-7",
			"claude-opus-4-8",
		},
		"cloudflare": {
			"@cf/deepseek-ai/deepseek-r1-distill-qwen-32b",
			"@cf/google/gemma-4-26b-a4b-it",
			"@cf/moonshotai/kimi-k2.6",
			"@cf/moonshotai/kimi-k2.7-code",
			"@cf/nvidia/nemotron-3-120b-a12b",
			"@cf/openai/gpt-oss-120b",
			"@cf/openai/gpt-oss-20b",
			"@cf/qwen/qwen3-30b-a3b-fp8",
			"@cf/qwen/qwq-32b",
			"@cf/zai-org/glm-4.7-flash",
			"@cf/zai-org/glm-5.2",
		},
		// chatgpt shares the static codex seed (design §3.3) kept in the
		// service layer; discovery (`llm-router discover --provider chatgpt`)
		// replaces it with the live catalog when reachable.
		"chatgpt": service.CodexFallbackModels,
		// zai GLM Coding Plan: the Anthropic-compatible endpoint does not
		// reliably expose /v1/models for discovery, so seed the current plan
		// lineup ("[1m]" suffix selects the 1M-context variant).
		"zai": {
			"glm-5.3",
			"glm-5.3-flash",
			"glm-5.3[1m]",
			"glm-5.3-flash[1m]",
		},
	}

	modelNames, exists := predefined[providerName]
	if !exists {
		return
	}

	fmt.Printf("\nCreating predefined models for %s...\n", providerName)

	created := 0
	for _, name := range modelNames {
		m, err := modelRepo.Upsert(ctx, provider.ID, name)
		if err != nil {
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

	deactivated, _ := modelRepo.DisableByProviderExcept(ctx, provider.ID, modelNames)
	if deactivated > 0 {
		fmt.Printf("⚠ %d stale predefined models deactivated\n", deactivated)
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
	fmt.Println("    chatgpt         ChatGPT Plus/Pro subscription (OAuth; alias: codex)")
	fmt.Println()
	fmt.Println("  API Key (--key required):")
	fmt.Println("    opencode-go     OpenCode Go ($10/mo)")
	fmt.Println("    opencode-zen    OpenCode Zen (pay-as-you-go, free models available)")
	fmt.Println("    openai          OpenAI (GPT-4o, o3, etc.)")
	fmt.Println("    anthropic       Anthropic API (Claude)")
	fmt.Println("    zai             Z.AI GLM Coding Plan subscription (Anthropic-compatible)")
	fmt.Println("    openrouter      OpenRouter (multi-provider)")
	fmt.Println("    cloudflare      Cloudflare Workers AI (--account-id required)")
	fmt.Println("    ollama          Ollama local models (--host, default localhost:11434)")
	fmt.Println()
	fmt.Println("  Custom (requires --url and --key):")
	fmt.Println("    llm-router setup --provider my-provider --url https://api.example.com/v1 --key sk-...")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  llm-router setup --provider claude-code")
	fmt.Println("  llm-router setup --provider chatgpt")
	fmt.Println("  llm-router setup --provider opencode-go --key sk-...")
	fmt.Println("  llm-router setup --provider opencode-zen --key sk-...")
	fmt.Println("  llm-router setup --provider openai --key sk-...")
}
