package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

func runSetup(args []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	dbPath := fs.String("db", "./data/llm-router.db", "SQLite database path")
	fs.Parse(args)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	database, err := db.Open(*dbPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	providerRepo := repository.NewProviderRepository(database)
	modelRepo := repository.NewModelRepository(database)
	tagRepo := repository.NewTagRepository(database)

	ctx := context.Background()

	setupClaudeCode(ctx, providerRepo, modelRepo, tagRepo, logger)
	setupDefaultModels(ctx, modelRepo, tagRepo, logger)

	logger.Info("setup completed")
}

func setupClaudeCode(ctx context.Context, providerRepo repository.ProviderRepository, modelRepo repository.ModelRepository, tagRepo repository.TagRepository, logger *slog.Logger) {
	existing, err := providerRepo.GetByName(ctx, "claude-code")
	if err != nil {
		logger.Error("failed to check claude-code provider", "error", err)
		return
	}
	if existing != nil {
		logger.Info("claude-code provider already exists", "id", existing.ID)
		return
	}

	provider := &models.Provider{
		Name:            "claude-code",
		APIType:         models.APITypeAnthropic,
		BaseURL:         "http://localhost:8080",
		APIKeyEncrypted: "oauth",
	}

	if err := providerRepo.Create(ctx, provider); err != nil {
		logger.Error("failed to create claude-code provider", "error", err)
		return
	}

	logger.Info("created claude-code provider", "id", provider.ID)

	claudeModels := []struct {
		name string
		tags map[string]string
	}{
		{"claude-opus-4-6", map[string]string{
			"intel":           "95",
			"speed":           "60",
			"cost-type":       "subscription",
			"context_window":  "200000",
			"hallucination":   "8",
			"reasoning":       "95",
			"cost_per_1m_input":  "15.00",
			"cost_per_1m_output": "75.00",
		}},
		{"claude-sonnet-4-6", map[string]string{
			"intel":           "88",
			"speed":           "85",
			"cost-type":       "subscription",
			"context_window":  "200000",
			"hallucination":   "10",
			"reasoning":       "88",
			"cost_per_1m_input":  "3.00",
			"cost_per_1m_output": "15.00",
		}},
		{"claude-haiku-4-5", map[string]string{
			"intel":           "75",
			"speed":           "95",
			"cost-type":       "subscription",
			"context_window":  "200000",
			"hallucination":   "15",
			"reasoning":       "70",
			"cost_per_1m_input":  "0.25",
			"cost_per_1m_output": "1.25",
		}},
	}

	for _, cm := range claudeModels {
		m := &models.Model{
			ProviderID: provider.ID,
			Name:       cm.name,
		}
		if err := modelRepo.Create(ctx, m); err != nil {
			logger.Error("failed to create model", "model", cm.name, "error", err)
			continue
		}

		if err := tagRepo.Set(ctx, m.ID, cm.tags); err != nil {
			logger.Error("failed to set tags", "model", cm.name, "error", err)
		} else {
			logger.Info("created model with tags", "model", cm.name)
		}
	}
}

func setupDefaultModels(ctx context.Context, modelRepo repository.ModelRepository, tagRepo repository.TagRepository, logger *slog.Logger) {
	defaultModels := []struct {
		name string
		tags map[string]string
	}{
		{"gpt-4o", map[string]string{
			"intel":           "85",
			"speed":           "80",
			"cost-type":       "api-creds",
			"context_window":  "128000",
			"hallucination":   "12",
			"cost_per_1m_input":  "2.50",
			"cost_per_1m_output": "10.00",
		}},
		{"gpt-4-turbo", map[string]string{
			"intel":           "82",
			"speed":           "75",
			"cost-type":       "api-creds",
			"context_window":  "128000",
			"hallucination":   "14",
			"cost_per_1m_input":  "10.00",
			"cost_per_1m_output": "30.00",
		}},
		{"gemini-2.0-flash", map[string]string{
			"intel":           "80",
			"speed":           "90",
			"cost-type":       "free",
			"context_window":  "1000000",
			"hallucination":   "15",
			"cost_per_1m_input":  "0.00",
			"cost_per_1m_output": "0.00",
		}},
		{"deepseek-v3", map[string]string{
			"intel":           "82",
			"speed":           "85",
			"cost-type":       "api-creds",
			"context_window":  "128000",
			"hallucination":   "13",
			"cost_per_1m_input":  "0.27",
			"cost_per_1m_output": "1.10",
		}},
	}

	allModels, _ := modelRepo.ListAll(ctx)
	existing := make(map[string]bool)
	for _, m := range allModels {
		existing[m.Name] = true
	}

	for _, dm := range defaultModels {
		if existing[dm.name] {
			continue
		}

		allModels, _ := modelRepo.ListAll(ctx)
		for _, m := range allModels {
			if m.Name == dm.name {
				tagRepo.Set(ctx, m.ID, dm.tags)
				logger.Info("updated tags for existing model", "model", dm.name)
				break
			}
		}
	}
}
