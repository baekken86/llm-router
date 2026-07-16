package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/repository"
)

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
	jsonPath := fs.String("file", "data/models.json", "Path to models.json")
	fs.Parse(args)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	database, err := db.Open(*dbPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	globalRepo := repository.NewGlobalMetadataRepository(database)

	f, err := os.Open(*jsonPath)
	if err != nil {
		logger.Error("failed to open file", "error", err, "path", *jsonPath)
		os.Exit(1)
	}
	defer f.Close()

	var file struct {
		Models []struct {
			Name               string                         `json:"name"`
			HasReasoningEffort bool                           `json:"has_reasoning_effort"`
			ContextWindow      *int                           `json:"context_window,omitempty"`
			CostType           *string                        `json:"cost_type,omitempty"`
			CostPer1mInput     *float64                       `json:"cost_per_1m_input,omitempty"`
			CostPer1mOutput    *float64                       `json:"cost_per_1m_output,omitempty"`
			CostPer1mCache     *float64                       `json:"cost_per_1m_cache,omitempty"`
			Efforts            map[string]map[string]interface{} `json:"efforts,omitempty"`
			Intel              *float64                       `json:"intelligence,omitempty"`
			Speed              *float64                       `json:"speed,omitempty"`
			Reasoning          *float64                       `json:"reasoning,omitempty"`
			Hallucination      *float64                       `json:"hallucination,omitempty"`
			Coding             *float64                       `json:"coding,omitempty"`
			Latency            *float64                       `json:"latency,omitempty"`
		} `json:"models"`
	}

	if err := json.NewDecoder(f).Decode(&file); err != nil {
		logger.Error("failed to parse JSON", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	imported := 0

	for _, model := range file.Models {
		modelName := strings.ToLower(model.Name)

		baseTags := make(map[string]string)
		baseTags["has_reasoning_effort"] = fmt.Sprintf("%v", model.HasReasoningEffort)
		if model.ContextWindow != nil {
			baseTags["context_window"] = fmt.Sprintf("%d", *model.ContextWindow)
		}
		if model.CostType != nil {
			baseTags["cost_type"] = *model.CostType
		}
		if model.CostPer1mInput != nil {
			baseTags["cost_per_1m_input"] = fmt.Sprintf("%g", *model.CostPer1mInput)
		}
		if model.CostPer1mOutput != nil {
			baseTags["cost_per_1m_output"] = fmt.Sprintf("%g", *model.CostPer1mOutput)
		}
		if model.CostPer1mCache != nil {
			baseTags["cost_per_1m_cache"] = fmt.Sprintf("%g", *model.CostPer1mCache)
		}

		if model.HasReasoningEffort && len(model.Efforts) > 0 {
			for effort, tags := range model.Efforts {
				effortTags := copyMap(baseTags)
				for k, v := range tags {
					effortTags[k] = fmt.Sprintf("%v", v)
				}

				if err := globalRepo.Set(ctx, modelName, effort, effortTags); err != nil {
					logger.Warn("failed to set metadata", "model", modelName, "effort", effort, "error", err)
					continue
				}
				imported++
			}
		} else {
			if model.Intel != nil {
				baseTags["intelligence"] = fmt.Sprintf("%g", *model.Intel)
			}
			if model.Speed != nil {
				baseTags["speed"] = fmt.Sprintf("%g", *model.Speed)
			}
			if model.Reasoning != nil {
				baseTags["reasoning"] = fmt.Sprintf("%g", *model.Reasoning)
			}
			if model.Hallucination != nil {
				baseTags["hallucination"] = fmt.Sprintf("%g", *model.Hallucination)
			}
			if model.Coding != nil {
				baseTags["coding"] = fmt.Sprintf("%g", *model.Coding)
			}
			if model.Latency != nil {
				baseTags["latency"] = fmt.Sprintf("%g", *model.Latency)
			}

			if err := globalRepo.Set(ctx, modelName, "", baseTags); err != nil {
				logger.Warn("failed to set metadata", "model", modelName, "error", err)
				continue
			}
			imported++
		}
	}

	models, _ := globalRepo.ListModels(ctx)
	logger.Info("init completed", "models", len(models), "entries", imported)
}

func copyMap(m map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range m {
		result[k] = v
	}
	return result
}
