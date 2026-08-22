package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
)

func runConnect(args []string) {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
	providerName := fs.String("provider", "claude-code", "Provider name to connect")
	fs.Parse(args)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	database, err := db.Open(*dbPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	providerRepo := repository.NewProviderRepository(database)
	providerMetadataRepo := repository.NewProviderMetadataRepository(database)
	oauthRepo := repository.NewOAuthRepository(database)
	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, []byte("00000000000000000000000000000000"))
	oauthService := service.NewOAuthService(oauthRepo, providerRepo, providerService, logger)

	ctx := context.Background()

	provider, err := providerRepo.GetByName(ctx, *providerName)
	if err != nil || provider == nil {
		logger.Error("provider not found", "name", *providerName)
		fmt.Println("\nRun 'llm-router setup' first to create the provider.")
		os.Exit(1)
	}

	connected, token, err := oauthService.IsConnected(ctx, provider.ID)
	if err != nil {
		logger.Error("failed to check connection", "error", err)
		os.Exit(1)
	}

	if connected {
		logger.Info("already connected",
			"provider", *providerName,
			"expires", token.ExpiresAt.Format("2006-01-02 15:04"),
		)
		return
	}

	authURL, state, err := oauthService.StartAuthFlow(ctx, provider.ID)
	if err != nil {
		logger.Error("failed to start auth flow", "error", err)
		os.Exit(1)
	}

	runManual(ctx, oauthService, authURL, state, logger)
}

func runManual(ctx context.Context, oauthService service.OAuthService, authURL, state string, logger *slog.Logger) {
	fmt.Println("\n=== llm-router - Connect to Claude Code ===")
	fmt.Println()
	fmt.Println("1. Open this URL in your browser:")
	fmt.Println()
	fmt.Printf("   %s\n", authURL)
	fmt.Println()
	fmt.Println("2. Login with your Anthropic account")
	fmt.Println("3. Authorize the application")
	fmt.Println("4. After authorization, the browser redirects to a URL like:")
	fmt.Println("   http://localhost:8080/callback?code=...")
	fmt.Println("5. Copy the FULL URL from the address bar and paste it here:")
	fmt.Println()

	fmt.Print("Paste callback URL: ")
	var callbackURL string
	fmt.Scanln(&callbackURL)

	if callbackURL == "" {
		logger.Error("no URL provided")
		os.Exit(1)
	}

	code, returnedState := extractCodeAndState(callbackURL)
	if code == "" {
		logger.Error("could not extract code from URL")
		os.Exit(1)
	}

	if returnedState != "" {
		state = returnedState
	}

	token, err := oauthService.HandleCallback(ctx, code, state)
	if err != nil {
		logger.Error("auth failed", "error", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("✓ Connected!")
	fmt.Printf("  Token expires: %s\n", token.ExpiresAt.Format("2006-01-02 15:04"))
	fmt.Println()
	fmt.Println("Use models: cc/claude-opus-4-6, cc/claude-sonnet-4-6, cc/claude-haiku-4-5")
}

func extractCodeAndState(rawURL string) (code, state string) {
	raw := strings.TrimSpace(rawURL)
	u, err := url.Parse(raw)
	if err != nil {
		return "", ""
	}
	code = u.Query().Get("code")
	state = u.Query().Get("state")
	// 9router-style: handle fragment after # in code or state (claude.ai sometimes appends state as fragment)
	if frag := u.Fragment; frag != "" && state == "" {
		// fragment may be state or code#state
		if strings.Contains(frag, "=") {
			if vals, err := url.ParseQuery(frag); err == nil {
				if s := vals.Get("state"); s != "" {
					state = s
				}
				if c := vals.Get("code"); c != "" && code == "" {
					code = c
				}
			}
		} else if state == "" {
			state = frag
		}
	}
	// code itself may contain #state (e.g. "abc#xyz")
	if idx := strings.Index(code, "#"); idx != -1 {
		if state == "" {
			state = code[idx+1:]
		}
		code = code[:idx]
	}
	return code, state
}
