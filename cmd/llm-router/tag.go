package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/repository"
)

func runTag(args []string) {
	fs := flag.NewFlagSet("tag", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
	modelName := fs.String("model", "", "Model name (required)")
	tags := TagList{}
	fs.Var(&tags, "set", "Tag in format key=value (repeatable)")
	fs.Parse(args)

	if *modelName == "" {
		fmt.Fprintln(os.Stderr, "Usage: llm-router tag --model gpt-4o --set intel=85 --set speed=80")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	database, err := db.Open(*dbPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	modelRepo := repository.NewModelRepository(database)
	tagRepo := repository.NewTagRepository(database)

	ctx := context.Background()

	allModels, err := modelRepo.ListAll(ctx)
	if err != nil {
		logger.Error("failed to list models", "error", err)
		os.Exit(1)
	}

	var targetModel *struct{ ID int64; Name string }
	for _, m := range allModels {
		if strings.EqualFold(m.Name, *modelName) {
			targetModel = &struct{ ID int64; Name string }{m.ID, m.Name}
			break
		}
	}

	if targetModel == nil {
		logger.Error("model not found", "name", *modelName)
		os.Exit(1)
	}

	tagMap := make(map[string]string)
	for _, t := range tags {
		parts := strings.SplitN(t, "=", 2)
		if len(parts) == 2 {
			tagMap[parts[0]] = parts[1]
		}
	}

	if len(tagMap) == 0 {
		tags, err := tagRepo.GetByModel(ctx, targetModel.ID)
		if err != nil {
			logger.Error("failed to get tags", "error", err)
			os.Exit(1)
		}
		fmt.Printf("Tags for %s:\n", targetModel.Name)
		for _, t := range tags {
			fmt.Printf("  %s = %s\n", t.Key, t.Value)
		}
		return
	}

	if err := tagRepo.Set(ctx, targetModel.ID, "default", tagMap); err != nil {
		logger.Error("failed to set tags", "error", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Tags set for %s:\n", targetModel.Name)
	for k, v := range tagMap {
		fmt.Printf("  %s = %s\n", k, v)
	}
}

type TagList []string

func (t *TagList) String() string {
	return strings.Join(*t, ", ")
}

func (t *TagList) Set(value string) error {
	*t = append(*t, value)
	return nil
}
