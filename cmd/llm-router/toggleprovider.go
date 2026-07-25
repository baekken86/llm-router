package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
)

func runToggleProvider(args []string) {
	fs := flag.NewFlagSet("toggle-provider", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
	name := fs.String("name", "", "Provider name (required)")
	enable := fs.Bool("enable", false, "Enable the provider (default: toggle)")
	disable := fs.Bool("disable", false, "Disable the provider (default: toggle)")
	duration := fs.String("duration", "", "Disable duration: 10m, 1h, 24h (default: indefinite)")
	fs.Parse(args)

	if *name == "" {
		fmt.Fprintln(os.Stderr, "Usage: llm-router toggle-provider --name <name> [--enable|--disable] [--duration 10m|1h|24h]")
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

	providerRepo := repository.NewProviderRepository(database)
	providerMetadataRepo := repository.NewProviderMetadataRepository(database)
	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, loadEncryptionKey())

	ctx := context.Background()
	provider, err := providerRepo.GetByName(ctx, *name)
	if err != nil || provider == nil {
		fmt.Fprintf(os.Stderr, "Error: provider '%s' not found\n", *name)
		os.Exit(1)
	}

	newDisabled := provider.Disabled
	if *enable {
		newDisabled = false
	} else if *disable {
		newDisabled = true
	} else {
		newDisabled = !provider.Disabled
	}

	if newDisabled == provider.Disabled {
		state := "enabled"
		if provider.Disabled {
			state = "disabled"
		}
		fmt.Printf("Provider '%s' already %s\n", *name, state)
		return
	}

	req := models.UpdateProviderRequest{Disabled: &newDisabled}
	if *duration != "" && newDisabled {
		req.Duration = duration
	}
	_, err = providerService.Update(ctx, provider.ID, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to toggle provider: %v\n", err)
		os.Exit(1)
	}

	state := "enabled"
	if newDisabled {
		state = "disabled"
		if *duration != "" {
			state += " for " + *duration
		}
	}
	fmt.Printf("✓ Provider '%s' %s\n", *name, state)
}
