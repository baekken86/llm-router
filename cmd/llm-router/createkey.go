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

func runCreateKey(args []string) {
	home, _ := os.UserHomeDir()
	defaultDB := home + "/.local/share/llm-router/llm-router.db"

	fs := flag.NewFlagSet("create-key", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "SQLite database path")
	desc := fs.String("description", "admin", "Key description")
	fs.Parse(args)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	database, err := db.Open(*dbPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	keyRepo := repository.NewProxyKeyRepository(database)
	keyService := service.NewKeyService(keyRepo)

	key, err := keyService.Create(context.Background(), models.CreateKeyRequest{Description: *desc})
	if err != nil {
		logger.Error("failed to create key", "error", err)
		os.Exit(1)
	}

	fmt.Printf("New proxy key: %s\n", key.Key)
}
