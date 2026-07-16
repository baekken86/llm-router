package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	llmrouter "github.com/chris/llm-router"
	"github.com/chris/llm-router/internal/api"
	"github.com/chris/llm-router/internal/api/handlers"
	"github.com/chris/llm-router/internal/config"
	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/proxy"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
	"github.com/chris/llm-router/internal/tui"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "proxy":
			runProxy(os.Args[2:])
			return
		case "admin":
			runAdmin(os.Args[2:])
			return
		case "init":
			runInit(os.Args[2:])
			return
		case "setup":
			runSetup(os.Args[2:])
			return
		case "add-provider":
			runAddProvider(os.Args[2:])
			return
		case "discover":
			runDiscover(os.Args[2:])
			return
		case "tag":
			runTag(os.Args[2:])
			return
		case "import":
			runImportCmd(os.Args[2:])
			return
		case "create-key":
			runCreateKey(os.Args[2:])
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
  llm-router setup                Setup a provider (OAuth or API key)
  llm-router add-provider         Add a custom provider
  llm-router discover             Discover models from a provider
  llm-router tag                  Set metadata tags on models
  llm-router create-key [flags]   Create a new proxy API key
  llm-router admin [flags]        Connect to running proxy as admin viewer
  llm-router import [flags]       Import CSV metadata
  llm-router help                 Show this help

Examples:
  # Setup Claude Code (OAuth)
  llm-router setup --provider claude-code

  # Setup OpenCode Go (API key)
  llm-router setup --provider opencode-go --key sk-...

  # Setup custom provider
  llm-router setup --provider my-api --url https://api.example.com/v1 --key sk-...

  # Start proxy
  llm-router proxy --port 8080 --db ./data/router.db

Proxy flags:
  --port int                      HTTP port (default 8080, env LLM_ROUTER_PORT)
  --db string                     SQLite path (default ~/.local/share/llm-router/llm-router.db)
  --encryption-key string         32-byte hex key (env LLM_ROUTER_ENCRYPTION_KEY, config encryption_key)
  --no-tui                        Disable terminal UI

Setup flags:
  --provider string               Provider name (required)
  --key string                    API key (required for API key providers)
  --url string                    Custom base URL (optional)

Admin flags:
  --connect string                Proxy URL (default http://localhost:8080)
  --key string                    Proxy API key (required)

Add-provider flags:
  --name string                   Provider name (required)
  --type string                   API type: openai or anthropic (auto-detected)
  --url string                    Base URL (required)
  --key string                    API key (required)

Tag flags:
  --model string                  Model name (required)
  --set string                    Tag in format key=value (repeatable)

Import flags:
  --file string                   CSV file path (required)
  --mode string                   merge or replace (default merge)
  --db string                     SQLite path (default ~/.local/share/llm-router/llm-router.db)

Create-key flags:
  --db string                     SQLite path (default ~/.local/share/llm-router/llm-router.db)
  --description string            Key description (default "admin")`)
}

func runProxy(args []string) {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	port := fs.Int("port", 8080, "HTTP server port")
	dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
	encryptKey := fs.String("encryption-key", "", "32-byte hex encryption key")
	adminPassword := fs.String("admin-password", "", "Admin password for web UI")
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
	if envPass := os.Getenv("LLM_ROUTER_ADMIN_PASSWORD"); envPass != "" {
		*adminPassword = envPass
	}

	cfg := config.New()
	settings := cfg.Get()

	logLevel := slog.LevelInfo
	switch settings.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	logChan := make(chan proxy.RequestLog, 100)
	syslogChan := make(chan string, 200)

	var logger *slog.Logger
	if *noTUI {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	} else {
		logger = slog.New(slog.NewTextHandler(&syslogWriter{ch: syslogChan}, &slog.HandlerOptions{Level: logLevel}))
	}

	database, err := db.Open(*dbPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	key := *encryptKey
	if key == "" {
		key = settings.EncryptionKey
	}
	if key == "" {
		key = generateEncryptionKey()
		logger.Info("generated encryption key (save this!)", "key", key)
	}
	keyBytes, err := hex.DecodeString(key)
	if err != nil || len(keyBytes) != 32 {
		logger.Error("invalid encryption key: must be 32 bytes hex-encoded")
		os.Exit(1)
	}

	pass := *adminPassword
	if pass == "" {
		pass = settings.AdminPassword
	}
	if pass == "" {
		pass = generateAdminPassword()
		logger.Info("generated admin password (save this!)", "password", pass)
	}

	providerRepo := repository.NewProviderRepository(database)
	providerMetadataRepo := repository.NewProviderMetadataRepository(database)
	modelRepo := repository.NewModelRepository(database)
	tagRepo := repository.NewTagRepository(database)
	vmRepo := repository.NewVirtualModelRepository(database)
	keyRepo := repository.NewProxyKeyRepository(database)
	globalMetaRepo := repository.NewGlobalMetadataRepository(database)

	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, keyBytes)
	modelService := service.NewModelService(modelRepo, tagRepo, providerRepo, providerService)
	vmService := service.NewVirtualModelService(vmRepo, modelRepo, tagRepo, providerRepo, providerMetadataRepo, globalMetaRepo)
	keyService := service.NewKeyService(keyRepo)
	adminService := service.NewAdminService(pass)

	statsHandler := handlers.NewStatsHandler()

	go func() {
		for log := range logChan {
			statsHandler.RecordLog(log)
		}
	}()

	engine := proxy.NewEngine(vmService, providerService, logger, logChan)
	engine.GetRTK().SetEnabled(settings.RTKEnabled)
	engine.GetCaveman().SetEnabled(settings.CavemanEnabled)
	engine.ApplySettings(settings.MaxRetries, settings.TimeoutSeconds, settings.MaxTokens)

	providerHandler := handlers.NewProviderHandler(providerService, modelService)
	modelHandler := handlers.NewModelHandler(modelService)
	vmHandler := handlers.NewVirtualModelHandler(vmService)
	keyHandler := handlers.NewKeyHandler(keyService)
	importHandler := handlers.NewImportHandler(service.NewImportService(modelRepo, tagRepo))
	metadataHandler := handlers.NewMetadataHandler(llmrouter.ModelsJSON)
	adminHandler := handlers.NewAdminHandler(adminService)

	oauthHandler := handlers.NewOAuthHandler(func(key string) int64 {
		pk, _ := keyService.ValidateKey(context.Background(), key)
		if pk != nil {
			return pk.ID
		}
		return 0
	})

	var webFS *embed.FS
	if _, err := llmrouter.WebDistFS.Open("web/dist"); err == nil {
		webFS = &llmrouter.WebDistFS
		logger.Info("web UI embedded")
	} else {
		logger.Warn("web UI not embedded", "error", err)
	}

	r := api.NewRouter(logger, providerHandler, modelHandler, vmHandler, keyHandler, importHandler, statsHandler, oauthHandler, metadataHandler, keyService, adminService, adminHandler, webFS)

	r.Route("/v1", func(r chi.Router) {
		r.Use(middlewareAuthOrOAuth(keyService, oauthHandler))
		r.Post("/chat/completions", engine.HandleChatCompletion)
		r.Post("/messages", engine.HandleAnthropicMessages)
		r.Post("/messages/stream", engine.HandleAnthropicMessagesStream)
		r.Get("/models", handleListModels(vmService))
	})

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
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	if !*noTUI {
		tuiQuit := make(chan struct{})
		go tui.Run(logChan, syslogChan, vmRepo, modelRepo, tagRepo, providerRepo, cfg, tuiQuit)
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		select {
		case <-quit:
		case <-tuiQuit:
		}
	} else {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
	}

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

func generateAdminPassword() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func defaultDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "llm-router", "llm-router.db")
}

func middlewareAuthOrOAuth(ks service.KeyService, oauth *handlers.OAuthHandler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if len(auth) < 8 || auth[:7] != "Bearer " {
				http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
				return
			}

			token := auth[7:]

			pk, err := ks.ValidateKey(r.Context(), token)
			if err == nil && pk != nil {
				next.ServeHTTP(w, r)
				return
			}

			if oauth.ValidateToken(token) > 0 {
				next.ServeHTTP(w, r)
				return
			}

			http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
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

type syslogWriter struct {
	ch chan<- string
}

func (w *syslogWriter) Write(p []byte) (int, error) {
	line := string(p)
	select {
	case w.ch <- line:
	default:
	}
	return len(p), nil
}
