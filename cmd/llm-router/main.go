package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/api"
	"github.com/chris/llm-router/internal/api/handlers"
	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/proxy"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
	"github.com/chris/llm-router/internal/tui"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "admin":
			runAdmin(os.Args[2:])
			return
		case "proxy":
			runProxy(os.Args[2:])
			return
		case "import":
			runImportCmd(os.Args[2:])
			return
		case "help", "--help", "-h":
			printUsage()
			return
		}
	}

	runProxy(os.Args[1:])
}

func printUsage() {
	fmt.Println(`llm-router - OpenAI-compatible LLM proxy with virtual models

Usage:
  llm-router [flags]              Start proxy (default)
  llm-router proxy [flags]        Start proxy server
  llm-router admin [flags]        Connect to running proxy as admin viewer
  llm-router import [flags]       Import CSV metadata
  llm-router help                 Show this help

Proxy flags:
  --port int                      HTTP port (default 8080, env LLM_ROUTER_PORT)
  --db string                     SQLite path (default ./data/llm-router.db, env LLM_ROUTER_DB)
  --encryption-key string         32-byte hex key (env LLM_ROUTER_ENCRYPTION_KEY)
  --no-tui                        Disable terminal UI

Admin flags:
  --connect string                Proxy URL (default http://localhost:8080)
  --key string                    Proxy API key (required)

Import flags:
  --file string                   CSV file path (required)
  --mode string                   merge or replace (default merge)
  --db string                     SQLite path (default ./data/llm-router.db)`)
}

func runProxy(args []string) {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	port := fs.Int("port", 8080, "HTTP server port")
	dbPath := fs.String("db", "./data/llm-router.db", "SQLite database path")
	encryptKey := fs.String("encryption-key", "", "32-byte hex encryption key")
	noTUI := fs.Bool("no-tui", false, "Disable terminal UI")
	fs.Parse(args)

	if envPort := os.Getenv("LLM_ROUTER_PORT"); envPort != "" {
		fmt.Sscanf(envPort, "%d", port)
	}
	if envDB := os.Getenv("LLM_ROUTER_DB"); envDB != "" {
		*dbPath = envDB
	}
	if envKey := os.Getenv("LLM_ROUTER_ENCRYPTION_KEY"); envKey != "" {
		*encryptKey = envKey
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	database, err := db.Open(*dbPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	key := *encryptKey
	if key == "" {
		key = generateEncryptionKey()
		logger.Info("generated encryption key (save this!)", "key", key)
	}
	keyBytes, err := hex.DecodeString(key)
	if err != nil || len(keyBytes) != 32 {
		logger.Error("invalid encryption key: must be 32 bytes hex-encoded")
		os.Exit(1)
	}

	providerRepo := repository.NewProviderRepository(database)
	modelRepo := repository.NewModelRepository(database)
	tagRepo := repository.NewTagRepository(database)
	vmRepo := repository.NewVirtualModelRepository(database)
	keyRepo := repository.NewProxyKeyRepository(database)

	providerService := service.NewProviderService(providerRepo, keyBytes)
	modelService := service.NewModelService(modelRepo, tagRepo, providerRepo, providerService)
	vmService := service.NewVirtualModelService(vmRepo, modelRepo, tagRepo, providerRepo)
	keyService := service.NewKeyService(keyRepo)

	statsHandler := handlers.NewStatsHandler()
	logChan := make(chan proxy.RequestLog, 100)

	go func() {
		for log := range logChan {
			statsHandler.RecordLog(log)
		}
	}()

	engine := proxy.NewEngine(vmService, providerService, logger, logChan)

	providerHandler := handlers.NewProviderHandler(providerService, modelService)
	modelHandler := handlers.NewModelHandler(modelService)
	vmHandler := handlers.NewVirtualModelHandler(vmService)
	keyHandler := handlers.NewKeyHandler(keyService)
	importHandler := handlers.NewImportHandler(service.NewImportService(modelRepo, tagRepo))

	r := api.NewRouter(logger, providerHandler, modelHandler, vmHandler, keyHandler, importHandler, statsHandler, keyService)

	r.Route("/v1", func(r chi.Router) {
		r.Use(middlewareAuth(keyService))
		r.Post("/chat/completions", engine.HandleChatCompletion)
		r.Get("/models", handleListModels(vmService))
	})

	adminKey := createAdminKeyIfEmpty(keyService, logger, context.Background())

	addr := fmt.Sprintf(":%d", *port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		logger.Info("llm-router started", "addr", addr)
		if adminKey != "" {
			logger.Info("admin API key (save this!)", "key", adminKey)
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	if !*noTUI {
		go tui.Run(logChan)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
	logger.Info("stopped")
}

func generateEncryptionKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func createAdminKeyIfEmpty(ks service.KeyService, logger *slog.Logger, ctx context.Context) string {
	keys, err := ks.List(ctx)
	if err != nil || len(keys) > 0 {
		return ""
	}

	key, err := ks.Create(ctx, models.CreateKeyRequest{Description: "admin"})
	if err != nil {
		logger.Error("failed to create admin key", "error", err)
		return ""
	}

	return key.Key
}

func middlewareAuth(ks service.KeyService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if len(auth) < 8 || auth[:7] != "Bearer " {
				http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
				return
			}

			pk, err := ks.ValidateKey(r.Context(), auth[7:])
			if err != nil || pk == nil {
				http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func handleListModels(vmService service.VirtualModelService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vms, err := vmService.List(r.Context())
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		type modelEntry struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
		}

		var models []modelEntry
		for _, vm := range vms {
			models = append(models, modelEntry{
				ID:      vm.Name,
				Object:  "model",
				Created: vm.CreatedAt.Unix(),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"object":"list","data":`)
		if models == nil {
			w.Write([]byte("[]"))
		} else {
			json.NewEncoder(w).Encode(models)
		}
		fmt.Fprintf(w, `}`)
	}
}
