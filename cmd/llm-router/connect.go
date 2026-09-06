package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/chris/llm-router/internal/db"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
)

// codexOAuthCallbackAddr is the fixed loopback callback of the Codex CLI flow
// (design §4.4): the browser is redirected to port 1455 on localhost and the
// OAuth service picks the /auth/callback path for codex-kind providers.
const codexOAuthCallbackAddr = "localhost:1455"

// codexOAuthCallbackTimeout bounds how long the auto-catch callback server
// waits for the browser redirect before the user falls back to pasting the
// callback URL manually.
const codexOAuthCallbackTimeout = 5 * time.Minute

func runConnect(args []string) {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
	providerName := fs.String("provider", "claude-code", "Provider name to connect (claude-code, chatgpt)")
	manual := fs.Bool("manual", false, "Skip the local callback server and paste the callback URL manually")
	fs.Parse(args)

	name := normalizeChatgptProviderName(*providerName)

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
	providerService := service.NewProviderService(providerRepo, providerMetadataRepo, loadEncryptionKey())
	oauthService := service.NewOAuthService(oauthRepo, providerRepo, providerService, logger)

	ctx := context.Background()

	if name == "chatgpt" {
		provider, err := ensureChatgptProvider(ctx, providerRepo, providerService)
		if err != nil {
			logger.Error("failed to create chatgpt provider", "error", err)
			os.Exit(1)
		}
		if err := runChatgptConnect(ctx, provider, oauthService, logger, *manual); err != nil {
			logger.Error("connect failed", "error", err)
			os.Exit(1)
		}
		return
	}

	provider, err := providerRepo.GetByName(ctx, name)
	if err != nil || provider == nil {
		logger.Error("provider not found", "name", name)
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
			"provider", name,
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

// normalizeChatgptProviderName maps the "codex" alias onto the canonical
// "chatgpt" provider name (both select the ChatGPT/codex OAuth flow).
func normalizeChatgptProviderName(name string) string {
	if strings.EqualFold(strings.TrimSpace(name), "codex") {
		return "chatgpt"
	}
	return name
}

// ensureChatgptProvider makes sure a "chatgpt" provider row exists, creating
// the canonical row (api_type codex, ChatGPT backend URL, empty API key) when
// missing. A pre-existing "codex"-named row (the alias) is reused as-is so
// users who created their provider via add-provider --name codex keep it.
// The API key stays empty on purpose: the provider authenticates via ChatGPT
// OAuth only.
func ensureChatgptProvider(ctx context.Context, providerRepo repository.ProviderRepository, providerService service.ProviderService) (*models.Provider, error) {
	existing, err := providerRepo.GetByName(ctx, "chatgpt")
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	alias, err := providerRepo.GetByName(ctx, "codex")
	if err != nil {
		return nil, err
	}
	if alias != nil {
		return alias, nil
	}

	return providerService.Create(ctx, models.CreateProviderRequest{
		Name:    "chatgpt",
		APIType: models.APITypeCodex,
		BaseURL: "https://chatgpt.com/backend-api/codex",
		APIKey:  "",
	})
}

// runChatgptConnect is the shared ChatGPT (codex) OAuth connect flow used by
// both `connect --provider chatgpt` and `setup --provider chatgpt` (design
// §4.4): start the flow with the loopback callback server (port 1455,
// /auth/callback path, 5-minute wait) and fall back to pasting the callback
// URL when the server cannot bind or manual mode was requested.
func runChatgptConnect(ctx context.Context, provider *models.Provider, oauthService service.OAuthService, logger *slog.Logger, manual bool) error {
	return runChatgptConnectOnAddr(ctx, provider, oauthService, logger, manual, codexOAuthCallbackAddr, codexOAuthCallbackTimeout)
}

// runChatgptConnectOnAddr is runChatgptConnect with the callback address and
// wait timeout as parameters (tests inject an ephemeral port / short
// timeout); production always passes the fixed §4.4 values.
func runChatgptConnectOnAddr(ctx context.Context, provider *models.Provider, oauthService service.OAuthService, logger *slog.Logger, manual bool, callbackAddr string, callbackTimeout time.Duration) error {
	fmt.Println("\n=== llm-router — Connect to ChatGPT (Codex) ===")
	fmt.Println()

	if connected, token, err := oauthService.IsConnected(ctx, provider.ID); err == nil && connected {
		printChatgptConnected(token, provider.Name)
		return nil
	}

	authURL, state, err := oauthService.StartAuthFlowWithCallback(ctx, provider.ID, callbackAddr)
	if err != nil {
		return fmt.Errorf("start auth flow: %w", err)
	}

	var token *models.OAuthToken

	if !manual && canBindLoopback(callbackAddr) {
		fmt.Printf("1. Open this URL in your browser (login with your ChatGPT account):\n\n   %s\n\n", authURL)
		fmt.Printf("2. Waiting for the authorization callback on http://%s/auth/callback (timeout %s)...\n", callbackAddr, callbackTimeout)

		callbackCtx, cancel := context.WithTimeout(ctx, callbackTimeout)
		defer cancel()
		token, err = oauthService.StartCallbackServerOnAddr(callbackCtx, state, provider.ID, callbackAddr)
		if err == nil {
			printChatgptConnected(token, provider.Name)
			return nil
		}
		fmt.Printf("\n⚠ automatic callback failed (%v) — paste the callback URL instead.\n", err)
	} else {
		if manual {
			fmt.Println("Manual mode: automatic callback disabled.")
		} else {
			fmt.Printf("⚠ could not bind http://%s (is another process using port 1455?) — paste the callback URL instead.\n", callbackAddr)
		}
	}

	token, perr := runCodexPasteFallback(ctx, oauthService, authURL, state)
	if perr != nil {
		// The paste flow failed; the token row is untouched. surface it and
		// let callers decide (connect exits, setup continues with models).
		return perr
	}
	printChatgptConnected(token, provider.Name)
	return nil
}

// runCodexPasteFallback is the manual fallback of the codex flow: print the
// authorize URL and read the pasted callback URL (extractCodeAndState).
// Mirrors the claude-code runManual flow. It reports errors (no os.Exit) so
// callers like setup can continue with their remaining steps.
func runCodexPasteFallback(ctx context.Context, oauthService service.OAuthService, authURL, state string) (*models.OAuthToken, error) {
	fmt.Println()
	fmt.Println("1. Open this URL in your browser:")
	fmt.Println()
	fmt.Printf("   %s\n", authURL)
	fmt.Println()
	fmt.Println("2. Login with your ChatGPT account and authorize the application")
	fmt.Println("3. After authorization, the browser redirects to a URL like:")
	fmt.Println("   http://localhost:1455/auth/callback?code=...&state=...")
	fmt.Println("4. Copy the FULL URL from the address bar and paste it here:")
	fmt.Println()

	callbackURL := promptLine("Paste callback URL: ")
	if callbackURL == "" {
		return nil, fmt.Errorf("no callback URL provided")
	}

	code, returnedState := extractCodeAndState(callbackURL)
	if code == "" {
		return nil, fmt.Errorf("could not extract code from URL")
	}

	if returnedState != "" {
		state = returnedState
	}

	token, err := oauthService.HandleCallback(ctx, code, state)
	if err != nil {
		return nil, fmt.Errorf("auth failed: %w", err)
	}

	return token, nil
}

// printChatgptConnected prints the post-connect summary. The account/email
// come from the codex id_token claims (parsed by the OAuth service on
// exchange); they are only shown when known. providerName is the actual
// provider row name so the discover hint works for the "codex" alias too.
func printChatgptConnected(token *models.OAuthToken, providerName string) {
	fmt.Println()
	fmt.Println("✓ Connected!")
	fmt.Printf("  Token expires: %s\n", token.ExpiresAt.Format("2006-01-02 15:04"))
	if token.Email != "" {
		fmt.Printf("  Account: %s\n", token.Email)
	} else if token.AccountID != "" {
		fmt.Printf("  Account ID: %s\n", token.AccountID)
	}
	fmt.Println()
	fmt.Printf("Next: discover the available models with:\n")
	fmt.Printf("  llm-router discover --provider %s\n", providerName)
}

// canBindLoopback reports whether a TCP server could bind addr right now
// (i.e. nothing is listening on it). Probing instead of binding keeps the
// port immediately free for the real callback server.
func canBindLoopback(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
	if err != nil {
		// Connection refused / unreachable → nothing listening → free.
		return true
	}
	conn.Close()
	// Something answered → the port is taken; the callback server could not
	// bind and the paste fallback should be used.
	return false
}

// promptLine reads one line of user input, tolerating pasted URLs that contain
// spaces (fmt.Scanln stops at the first space).
func promptLine(prompt string) string {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 4096)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
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

	callbackURL := promptLine("Paste callback URL: ")

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
