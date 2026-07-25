package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
)

func runToggleModel(args []string) {
	fs := flag.NewFlagSet("toggle-model", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
	name := fs.String("name", "", "Model name (required)")
	enable := fs.Bool("enable", false, "Enable the model (default: toggle)")
	disable := fs.Bool("disable", false, "Disable the model (default: toggle)")
	duration := fs.String("duration", "", "Disable duration: 10m, 1h, 24h (default: indefinite)")
	fs.Parse(args)

	if *name == "" {
		fmt.Fprintln(os.Stderr, "Usage: llm-router toggle-model --name <name> [--enable|--disable] [--duration 10m|1h|24h]")
		os.Exit(1)
	}
	if *enable && *disable {
		fmt.Fprintln(os.Stderr, "Error: --enable and --disable are mutually exclusive")
		os.Exit(1)
	}
	if *duration != "" {
		if _, err := models.ParseDuration(*duration); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	database, err := db.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	modelRepo := repository.NewModelRepository(database)
	providerRepo := repository.NewProviderRepository(database)
	providerMetadataRepo := repository.NewProviderMetadataRepository(database)
	tagRepo := repository.NewTagRepository(database)
	globalMetaRepo := repository.NewGlobalMetadataRepository(database)
	modelMappingRepo := repository.NewModelMappingRepository(database)
	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, loadEncryptionKey())
	modelService := service.NewModelService(modelRepo, tagRepo, providerRepo, providerService, globalMetaRepo, modelMappingRepo)

	ctx := context.Background()
	allModels, err := modelRepo.ListAll(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to list models: %v\n", err)
		os.Exit(1)
	}

	var target *models.Model
	for i := range allModels {
		if allModels[i].Name == *name {
			target = &allModels[i]
			break
		}
	}
	if target == nil {
		fmt.Fprintf(os.Stderr, "Error: model '%s' not found\n", *name)
		os.Exit(1)
	}

	newDisabled := target.Disabled
	if *enable {
		newDisabled = false
	} else if *disable {
		newDisabled = true
	} else {
		newDisabled = !target.Disabled
	}

	if newDisabled == target.Disabled {
		state := "enabled"
		if target.Disabled {
			state = "disabled"
		}
		fmt.Printf("Model '%s' already %s\n", *name, state)
		return
	}

	var dur *time.Duration
	if *duration != "" && newDisabled {
		d, err := models.ParseDuration(*duration)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		dur = &d
	}

	if err := modelService.ToggleDisabled(ctx, target.ID, newDisabled, dur); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to toggle model: %v\n", err)
		os.Exit(1)
	}

	state := "enabled"
	if newDisabled {
		state = "disabled"
		if *duration != "" {
			state += " for " + *duration
		}
	}
	fmt.Printf("✓ Model '%s' %s\n", *name, state)
}
