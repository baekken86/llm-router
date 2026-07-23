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
	"strings"
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
		case "toggle-provider":
			runToggleProvider(os.Args[2:])
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
  llm-router toggle-provider      Enable/disable a provider
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
  --type string                   API type: openai, anthropic, cloudflare, or ollama (default openai)
  --url string                    Base URL (required for non-ollama)
  --key string                    API key (required; not needed for local ollama)
  --host string                   Ollama host (default localhost:11434)

Tag flags:
  --model string                  Model name (required)
  --set string                    Tag in format key=value (repeatable)

Toggle-provider flags:
  --name string                   Provider name (required)
  --enable                        Enable the provider
  --disable                       Disable the provider

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

	logChan := make(chan proxy.RequestLog, 500)
	syslogChan := make(chan string, 200)

	database, err := db.Open(*dbPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	logRepo := repository.NewLogRepository(database)

	var logger *slog.Logger
	if *noTUI {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel, AddSource: true}))
	} else {
		logger = slog.New(slog.NewTextHandler(&syslogWriter{ch: syslogChan, logRepo: logRepo}, &slog.HandlerOptions{Level: logLevel, AddSource: true}))
	}

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
	overrideRepo := repository.NewModelOverrideRepository(database)
	vmRepo := repository.NewVirtualModelRepository(database)
	keyRepo := repository.NewProxyKeyRepository(database)
	globalMetaRepo := repository.NewGlobalMetadataRepository(database)
	oauthRepo := repository.NewOAuthRepository(database)
	modelMappingRepo := repository.NewModelMappingRepository(database)

	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, keyBytes)
	modelService := service.NewModelService(modelRepo, tagRepo, providerRepo, providerService, globalMetaRepo, modelMappingRepo)
	vmService := service.NewVirtualModelService(vmRepo, modelRepo, tagRepo, providerRepo, providerMetadataRepo, globalMetaRepo, modelMappingRepo, overrideRepo)
	keyService := service.NewKeyService(keyRepo)
	adminService := service.NewAdminService(pass)

	statsHandler := handlers.NewStatsHandler(logRepo, logger)

	tuiLogChan := make(chan proxy.RequestLog, 500)

	go func() {
		for log := range logChan {
			statsHandler.RecordLog(log)
			select {
			case tuiLogChan <- log:
			default:
			}
		}
		close(tuiLogChan)
	}()

	// Load initial data from DB
	initialLogs, _ := logRepo.ListRequestLogs(context.Background(), 500)
	initialSyslogs, _ := logRepo.ListSyslogEntries(context.Background(), 1000)
	initialStats, _ := logRepo.ComputeStats(context.Background())

	// Convert to tui types
	tuiLogs := make([]proxy.RequestLog, 0, len(initialLogs))
	for _, dl := range initialLogs {
		tuiLogs = append(tuiLogs, proxy.RequestLog{
			Type:               dl.Type,
			Timestamp:          dl.Timestamp,
			RequestID:          dl.RequestID,
			VirtualModel:       dl.VirtualModel,
			ClientKeyID:        dl.ClientKeyID,
			ProviderName:       dl.ProviderName,
			ModelName:          dl.ModelName,
			StatusCode:         dl.StatusCode,
			Latency:            dl.Latency,
			InputTokens:        dl.InputTokens,
			OutputTokens:       dl.OutputTokens,
			CachedTokens:       dl.CachedTokens,
			ReasoningTokens:    dl.ReasoningTokens,
			ErrorMessage:       dl.ErrorMessage,
			RetryCount:         dl.RetryCount,
			FallbackCount:      dl.FallbackCount,
			RTKIntercepted:     dl.RTKIntercepted,
			RTKSavedTokens:     dl.RTKSavedTokens,
			CavemanIntercepted: dl.CavemanIntercepted,
			CavemanSavedTokens: dl.CavemanSavedTokens,
		})
	}

	tuiSyslogs := make([]tui.SysLogEntry, len(initialSyslogs))
	for i, e := range initialSyslogs {
		tuiSyslogs[i] = tui.SysLogEntry{
			Timestamp: e.Timestamp,
			Level:     e.Level,
			Message:   e.Message,
		}
	}

	var tuiStats *tui.StatsResponse
	if initialStats != nil {
		tuiStats = &tui.StatsResponse{
			TotalRequests:      initialStats.TotalRequests,
			Successes:          initialStats.Successes,
			Failures:           initialStats.Failures,
			InputTokens:        initialStats.InputTokens,
			OutputTokens:       initialStats.OutputTokens,
			CachedTokens:       initialStats.CachedTokens,
			ReasoningTokens:    initialStats.ReasoningTokens,
			RTKIntercepts:      initialStats.RTKIntercepts,
			RTKSavedTokens:     initialStats.RTKSavedTokens,
			CavemanIntercepts:  initialStats.CavemanIntercepts,
			CavemanSavedTokens: initialStats.CavemanSavedTokens,
			ByVirtualModel:     make(map[string]tui.ModelStat),
			ByProvider:         make(map[string]tui.ModelStat),
		}
		for name, ms := range initialStats.ByVirtualModel {
			tuiStats.ByVirtualModel[name] = tui.ModelStat{
				Requests:     ms.Requests,
				Successes:    ms.Successes,
				Failures:     ms.Failures,
				InputTokens:  ms.InputTokens,
				OutputTokens: ms.OutputTokens,
				CachedTokens: ms.CachedTokens,
			}
		}
		for name, ms := range initialStats.ByProvider {
			tuiStats.ByProvider[name] = tui.ModelStat{
				Requests:     ms.Requests,
				Successes:    ms.Successes,
				Failures:     ms.Failures,
				InputTokens:  ms.InputTokens,
				OutputTokens: ms.OutputTokens,
				CachedTokens: ms.CachedTokens,
			}
		}
	}

	// Start cleanup goroutine
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := logRepo.DeleteOlderThan(context.Background(), 7*24*time.Hour); err != nil {
				logger.Warn("failed to cleanup old logs", "error", err)
			}
		}
	}()

	oauthService := service.NewOAuthService(oauthRepo, providerRepo, providerService, logger)

	engine := proxy.NewEngine(vmService, providerService, oauthService, logger, logChan)
	engine.GetRTK().SetEnabled(settings.RTKEnabled)
	engine.GetCaveman().SetEnabled(settings.CavemanEnabled)
	engine.ApplySettings(settings.MaxRetries, settings.TimeoutSeconds, settings.MaxTokens)

	cb := proxy.NewCircuitBreaker(modelRepo, providerRepo, logger)
	cb.ApplySettings(proxy.CircuitBreakerSettings{
		Enabled:             settings.CircuitBreakerEnabled,
		ModelThreshold:      settings.CircuitBreakerModelThreshold,
		ModelWindowSec:      settings.CircuitBreakerModelWindowSec,
		ModelCooldownSec:    settings.CircuitBreakerModelCooldownSec,
		ProviderThreshold:   settings.CircuitBreakerProviderThreshold,
		ProviderWindowSec:   settings.CircuitBreakerProviderWindowSec,
		ProviderCooldownSec: settings.CircuitBreakerProviderCooldownSec,
		ProviderMinModels:   settings.CircuitBreakerProviderMinModels,
	})
	engine.SetCircuitBreaker(cb)

	providerHandler := handlers.NewProviderHandler(providerService, modelService)
	modelHandler := handlers.NewModelHandler(modelService)
	vmHandler := handlers.NewVirtualModelHandler(vmService)
	keyHandler := handlers.NewKeyHandler(keyService)
	importHandler := handlers.NewImportHandler(service.NewImportService(modelRepo, tagRepo))
	metadataHandler := handlers.NewMetadataHandler(llmrouter.ModelsJSON, providerMetadataRepo, globalMetaRepo)
	adminHandler := handlers.NewAdminHandler(adminService)
	statusHandler := handlers.NewStatusHandler(engine, providerService, oauthService, providerMetadataRepo)
	syslogHandler := handlers.NewSyslogHandler(logRepo)
	settingsHandler := handlers.NewSettingsHandler(cfg, engine)
	mappingHandler := handlers.NewModelMappingHandler(modelMappingRepo, modelRepo, logger)
	modelOverrideHandler := handlers.NewModelOverrideHandler(overrideRepo)

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

	r := api.NewRouter(logger, providerHandler, modelHandler, vmHandler, keyHandler, importHandler, statsHandler, oauthHandler, metadataHandler, statusHandler, syslogHandler, settingsHandler, mappingHandler, modelOverrideHandler, keyService, adminService, adminHandler, webFS)

	r.Route("/v1", func(r chi.Router) {
		r.Use(middlewareAuthOrOAuth(keyService, oauthHandler))
		r.Post("/chat/completions", engine.HandleChatCompletionRoute)
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
		go tui.Run(tuiLogChan, syslogChan, vmRepo, modelRepo, tagRepo, providerRepo, oauthRepo, modelMappingRepo, globalMetaRepo, cfg, tuiLogs, tuiSyslogs, tuiStats, tuiQuit)
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
	cb.Stop()
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

func loadEncryptionKey() []byte {
	cfg := config.New()
	settings := cfg.Get()

	key := os.Getenv("LLM_ROUTER_ENCRYPTION_KEY")
	if key == "" {
		key = settings.EncryptionKey
	}
	if key == "" {
		key = generateEncryptionKey()
		fmt.Fprintf(os.Stderr, "⚠ generated encryption key (save this!): %s\n", key)
	}

	keyBytes, err := hex.DecodeString(key)
	if err != nil || len(keyBytes) != 32 {
		fmt.Fprintf(os.Stderr, "invalid encryption key: must be 32 bytes hex-encoded\n")
		os.Exit(1)
	}
	return keyBytes
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
	ch      chan<- string
	logRepo *repository.LogRepository
}

func (w *syslogWriter) Write(p []byte) (int, error) {
	line := string(p)
	select {
	case w.ch <- line:
	default:
	}

	if w.logRepo != nil {
		msg := strings.TrimSpace(string(p))
		if msg != "" {
			level := "INFO"
			if strings.Contains(msg, "level=DEBUG") || strings.Contains(msg, "level=debug") {
				level = "DEBUG"
			} else if strings.Contains(msg, "level=WARN") || strings.Contains(msg, "level=warn") {
				level = "WARN"
			} else if strings.Contains(msg, "level=ERROR") || strings.Contains(msg, "level=error") {
				level = "ERROR"
			}
			if err := w.logRepo.InsertSyslogEntry(context.Background(), time.Now(), level, msg); err != nil {
				// silent fail for syslog persistence
			}
		}
	}

	return len(p), nil
}
