package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
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
// waits for the browser redirect before the paste flow is the only way out.
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

// codexConnectIO bundles the CLI's injectable stdio for the codex connect
// flow: tests pipe stdin (pasted callback URLs) and capture stdout while
// production passes the real streams. A nil codexConnectIO means os.Stdin /
// os.Stdout.
type codexConnectIO struct {
	stdin  io.Reader
	stdout io.Writer
}

// codexServerWait blocks until the OAuth callback server completes (token,
// error) or the context ends. It is the StartCallbackServerOnAddr seam: tests
// inject a fake "callback arrived" signal; production runs the real server
// wait. The wait must honor ctx cancellation (the race cancels it when the
// paste path wins so the server wait — and with the real service, the HTTP
// listener — shuts down cleanly).
type codexServerWait func(ctx context.Context) (*models.OAuthToken, error)

// runChatgptConnect is the shared ChatGPT (codex) OAuth connect flow used by
// both `connect --provider chatgpt` and `setup --provider chatgpt` (design
// §4.4). It prints the authorize URL plus paste instructions immediately and
// then races the automatic loopback callback against a paste prompt, so the
// flow works unchanged when llm-router runs on a REMOTE machine and the
// browser lives on the user's laptop (the redirect to localhost:1455 then
// cannot reach the callback server — the paste path is how the code arrives).
func runChatgptConnect(ctx context.Context, provider *models.Provider, oauthService service.OAuthService, logger *slog.Logger, manual bool) error {
	return runChatgptConnectOnAddr(ctx, provider, oauthService, logger, manual, codexOAuthCallbackAddr, codexOAuthCallbackTimeout, nil, nil)
}

// runChatgptConnectOnAddr is runChatgptConnect with the callback address and
// wait timeout as parameters (tests inject an ephemeral port / short
// timeout); production always passes the fixed §4.4 values. cli and serverWait
// are optional injection points (tests); pass nil for production defaults.
func runChatgptConnectOnAddr(ctx context.Context, provider *models.Provider, oauthService service.OAuthService, logger *slog.Logger, manual bool, callbackAddr string, callbackTimeout time.Duration, cli *codexConnectIO, serverWait codexServerWait) error {
	var stdout io.Writer = os.Stdout
	stdin := io.Reader(os.Stdin)
	if cli != nil {
		if cli.stdout != nil {
			stdout = cli.stdout
		}
		if cli.stdin != nil {
			stdin = cli.stdin
		}
	}
	printf := func(format string, a ...any) { fmt.Fprintf(stdout, format, a...) }

	printf("\n=== llm-router — Connect to ChatGPT (Codex) ===\n\n")

	if connected, token, err := oauthService.IsConnected(ctx, provider.ID); err == nil && connected {
		printChatgptConnectedTo(token, provider.Name, stdout)
		return nil
	}

	authURL, state, err := oauthService.StartAuthFlowWithCallback(ctx, provider.ID, callbackAddr)
	if err != nil {
		return fmt.Errorf("start auth flow: %w", err)
	}

	if manual {
		// Manual mode keeps its meaning: no callback server at all, paste-only.
		printf("Manual mode: automatic callback disabled.\n\n")
		printf("1. Open this URL in your browser (on any machine with a browser):\n\n   %s\n\n", authURL)
		printf("2. Login with your ChatGPT account and authorize the application.\n")
		printf("3. After authorization the browser redirects to a URL like:\n   http://%s/auth/callback?code=...&state=...\n", callbackAddr)
		printf("4. Copy the FULL URL from the browser address bar and paste it here.\n\n")
		token, perr := runCodexPasteLoop(ctx, oauthService, state, printf, stdin, false)
		if perr != nil {
			return perr
		}
		printChatgptConnectedTo(token, provider.Name, stdout)
		return nil
	}

	if !canBindLoopback(callbackAddr) {
		// Port busy (another instance, or the user runs the browser on this
		// box with the port taken): paste-only, as before.
		printf("⚠ could not bind http://%s (is another process using port 1455?) — paste the callback URL instead.\n\n", callbackAddr)
		printf("1. Open this URL in your browser (on any machine with a browser):\n\n   %s\n\n", authURL)
		printf("2. Login with your ChatGPT account and authorize the application.\n")
		printf("3. After authorization the browser redirects to a URL like:\n   http://%s/auth/callback?code=...&state=...\n", callbackAddr)
		printf("4. Copy the FULL URL from the browser address bar and paste it here.\n\n")
		token, perr := runCodexPasteLoop(ctx, oauthService, state, printf, stdin, false)
		if perr != nil {
			return perr
		}
		printChatgptConnectedTo(token, provider.Name, stdout)
		return nil
	}

	// Default flow: the callback server binds this machine's loopback — which
	// only catches the browser redirect when the browser runs on the SAME
	// machine. llm-router is frequently deployed remotely, so the paste
	// instructions print immediately and the paste prompt races the server:
	// whichever delivers the code first wins. A user pasting on a remote box
	// is covered even though the automatic callback can never reach it.
	printf("1. Open this URL in your browser (on any machine with a browser):\n\n   %s\n\n", authURL)
	printf("2. Login with your ChatGPT account and authorize the application.\n")
	printf("3. After authorization the browser redirects to http://%s/auth/callback.\n", callbackAddr)
	printf("   If llm-router runs on a different machine than your browser, that page\n")
	printf("   will fail to load — that is expected. Copy the FULL URL from the browser\n")
	printf("   address bar and paste it here, or just press Enter to keep waiting for\n")
	printf("   the automatic callback (timeout %s).\n\n", callbackTimeout)
	printf("Paste callback URL (Enter to keep waiting): ")

	if serverWait == nil {
		waitCtx, cancelWait := context.WithTimeout(ctx, callbackTimeout)
		defer cancelWait()
		serverWait = func(waitCtx context.Context) (*models.OAuthToken, error) {
			return oauthService.StartCallbackServerOnAddr(waitCtx, state, provider.ID, callbackAddr)
		}
		ctx = waitCtx
	}

	// Race the two completion sources; first wins. The flow context is
	// cancelled on a win so the loser unwinds: the server wait returns via
	// its ctx (the real StartCallbackServerOnAddr then shuts its HTTP
	// listener down — no "already started" surprise later), and the stdin
	// reader goroutine is abandoned (it ends at the next Scan/EOF; nothing
	// needs restoring since lines are read, not consumed destructively).
	flowCtx, cancelFlow := context.WithCancel(ctx)
	defer cancelFlow()

	tokenCh := make(chan *models.OAuthToken, 1)
	errCh := make(chan error, 1)
	go func() {
		token, werr := serverWait(flowCtx)
		if werr != nil {
			errCh <- werr
			return
		}
		tokenCh <- token
	}()

	// Stream stdin lines: every line the user types (paste attempts, empty
	// "keep waiting" lines) flows here until EOF. The loop below abandons
	// this goroutine after a win — it exits at the next Scan or EOF on its
	// own (tests close the pipe; the production process simply exits).
	pasteCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdin)
		scanner.Buffer(make([]byte, 4096), 4096)
		for scanner.Scan() {
			pasteCh <- scanner.Text()
		}
		close(pasteCh)
	}()

	var token *models.OAuthToken
exchange:
	for {
		select {
		case tok := <-tokenCh:
			cancelFlow()
			printChatgptConnectedTo(tok, provider.Name, stdout)
			return nil
		case werr := <-errCh:
			cancelFlow()
			return werr
		case line, ok := <-pasteCh:
			if !ok {
				// stdin closed (EOF): keep waiting for the automatic
				// callback only.
				continue exchange
			}
			if strings.TrimSpace(line) == "" {
				// "Press Enter to keep waiting": stay in the race silently.
				printf("(still waiting for the automatic callback...)\n\nPaste callback URL (Enter to keep waiting): ")
				continue exchange
			}
			code, returnedState := extractCodeAndState(strings.TrimSpace(line))
			if code == "" {
				printf("\n⚠ could not extract a code from that URL — paste the FULL callback URL (or press Enter to keep waiting for the automatic callback).\n\nPaste callback URL (Enter to keep waiting): ")
				continue exchange
			}
			if returnedState != "" {
				state = returnedState
			}
			// Paste wins: stop the automatic callback (clean server shutdown
			// via ctx) BEFORE exchanging the code, so a late browser redirect
			// cannot race the token row.
			cancelFlow()
			tok, herr := oauthService.HandleCallback(ctx, code, state)
			if herr != nil {
				printf("\n⚠ auth failed: %v — try pasting the callback URL again.\n\nPaste callback URL (Enter to keep waiting): ", herr)
				continue exchange
			}
			token = tok
			break exchange
		}
	}
	printChatgptConnectedTo(token, provider.Name, stdout)
	return nil
}

// runCodexPasteLoop is the paste-only path (manual mode / port busy): prompt
// until a URL with a code arrives; empty lines re-prompt, invalid input
// re-prompts with a hint, exchange errors re-prompt. Ctrl+C is the abort.
// noWaitHint suppresses the "press Enter to keep waiting" wording (manual
// mode has no automatic callback to wait for).
func runCodexPasteLoop(ctx context.Context, oauthService service.OAuthService, state string, printf func(string, ...any), stdin io.Reader, noWaitHint bool) (*models.OAuthToken, error) {
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 4096), 4096)

	prompt := "Paste callback URL: "
	if !noWaitHint {
		prompt = "Paste callback URL (Enter to keep waiting): "
	}

	for {
		printf("%s", prompt)
		if !scanner.Scan() {
			return nil, fmt.Errorf("no callback URL provided")
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			// In the paste-only path there is no automatic callback to wait
			// for, so an empty line just re-prompts.
			continue
		}

		code, returnedState := extractCodeAndState(line)
		if code == "" {
			printf("⚠ could not extract a code from that URL — paste the FULL callback URL.\n")
			continue
		}
		if returnedState != "" {
			state = returnedState
		}

		token, err := oauthService.HandleCallback(ctx, code, state)
		if err != nil {
			printf("⚠ auth failed: %v — try pasting the callback URL again.\n", err)
			continue
		}
		return token, nil
	}
}

// runCodexPasteFallback is kept for compatibility: the manual fallback of the
// codex flow, reading os.Stdin. It reports errors (no os.Exit) so callers like
// setup can continue with their remaining steps.
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
	printChatgptConnectedTo(token, providerName, os.Stdout)
}

func printChatgptConnectedTo(token *models.OAuthToken, providerName string, stdout io.Writer) {
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "✓ Connected!")
	fmt.Fprintf(stdout, "  Token expires: %s\n", token.ExpiresAt.Format("2006-01-02 15:04"))
	if token.Email != "" {
		fmt.Fprintf(stdout, "  Account: %s\n", token.Email)
	} else if token.AccountID != "" {
		fmt.Fprintf(stdout, "  Account ID: %s\n", token.AccountID)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Next: discover the available models with:\n")
	fmt.Fprintf(stdout, "  llm-router discover --provider %s\n", providerName)
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
