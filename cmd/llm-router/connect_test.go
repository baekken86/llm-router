package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
	"github.com/chris/llm-router/internal/service"
)

// --- Test harness -------------------------------------------------------------

// fakeOAuthService records the calls the connect helpers make and replays a
// scripted token; it implements service.OAuthService.
type fakeOAuthService struct {
	service.OAuthService // embed for the methods the flow never touches

	connected      bool
	connectedToken *models.OAuthToken
	connectedErr   error

	startFlowCalled bool
	startFlowAddr   string
	startFlowURL    string
	startFlowState  string
	startFlowErr    error

	startServerCalled bool
	startServerToken  *models.OAuthToken
	startServerErr    error
	startServerAddr   string // last addr passed to StartCallbackServerOnAddr

	handleCalled bool
	handleToken  *models.OAuthToken
	handleErr    error
	// handleCalls records each HandleCallback invocation (code, state) so the
	// race tests can assert "exactly once with the pasted code".
	handleCalls [][2]string
}

func (f *fakeOAuthService) IsConnected(ctx context.Context, providerID int64) (bool, *models.OAuthToken, error) {
	return f.connected, f.connectedToken, f.connectedErr
}

func (f *fakeOAuthService) StartAuthFlowWithCallback(ctx context.Context, providerID int64, callbackAddr string) (string, string, error) {
	f.startFlowCalled = true
	f.startFlowAddr = callbackAddr
	if f.startFlowErr != nil {
		return "", "", f.startFlowErr
	}
	if f.startFlowURL != "" {
		return f.startFlowURL, f.startFlowState, nil
	}
	return "https://auth.openai.com/oauth/authorize?state=s&redirect_uri=http://" + callbackAddr + "/auth/callback", "state-1", nil
}

func (f *fakeOAuthService) StartCallbackServerOnAddr(ctx context.Context, state string, providerID int64, addr string) (*models.OAuthToken, error) {
	f.startServerCalled = true
	f.startServerAddr = addr
	return f.startServerToken, f.startServerErr
}

func (f *fakeOAuthService) HandleCallback(ctx context.Context, code, state string) (*models.OAuthToken, error) {
	f.handleCalled = true
	f.handleCalls = append(f.handleCalls, [2]string{code, state})
	return f.handleToken, f.handleErr
}

// handleCalledCount is the number of HandleCallback invocations.
func (f *fakeOAuthService) handleCalledCount() int { return len(f.handleCalls) }

// handleCode returns the code of the first HandleCallback invocation.
func (f *fakeOAuthService) handleCode() string {
	if len(f.handleCalls) == 0 {
		return ""
	}
	return f.handleCalls[0][0]
}

// handleState returns the state of the first HandleCallback invocation.
func (f *fakeOAuthService) handleState() string {
	if len(f.handleCalls) == 0 {
		return ""
	}
	return f.handleCalls[0][1]
}

// suppressStdout redirects os.Stdout to /dev/null for the duration of fn and
// restores it afterwards. The connect helpers print to stdout.
func suppressStdout(t *testing.T, fn func()) {
	t.Helper()
	old := os.Stdout
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	os.Stdout = devNull
	defer func() {
		os.Stdout = old
		devNull.Close()
	}()
	fn()
}

// cliIOFor builds the injectable stdio for runChatgptConnectOnAddr from a
// stdin string (pasted lines) and captures stdout for assertions.
func cliIOFor(t *testing.T, stdinContent string) (*codexConnectIO, func() string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString(stdinContent); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	w.Close() // EOF after the pasted lines

	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = oldStdin; r.Close() })

	var out bytes.Buffer
	return &codexConnectIO{stdin: r, stdout: &out}, out.String
}

// mustRunCodexConnect invokes runChatgptConnectOnAddr with the production
// defaults (os.Stdin/os.Stdout, real server wait) — the pre-refactor shape.
func mustRunCodexConnect(t *testing.T, ctx context.Context, provider *models.Provider, fake *fakeOAuthService, logger *slog.Logger, manual bool, addr string, timeout time.Duration) error {
	t.Helper()
	return runChatgptConnectOnAddr(ctx, provider, fake, logger, manual, addr, timeout, nil, nil)
}

// --- normalizeChatgptProviderName ----------------------------------------------

func TestNormalizeChatgptProviderName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"chatgpt", "chatgpt"},
		{"codex", "chatgpt"},
		{"CODEX", "chatgpt"},
		{"  codex  ", "chatgpt"},
		{"ChatGPT", "ChatGPT"}, // only the codex alias is normalized
		{"claude-code", "claude-code"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := normalizeChatgptProviderName(tc.in); got != tc.want {
			t.Errorf("normalizeChatgptProviderName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- ensureChatgptProvider -------------------------------------------------------

// TestEnsureChatgptProvider_CreatesCanonicalRow verifies the provider row the
// connect flow creates: name "chatgpt", api_type codex, canonical ChatGPT
// backend URL, empty API key (design §4.4 step 1).
func TestEnsureChatgptProvider_CreatesCanonicalRow(t *testing.T) {
	database := newSetupTestDB(t)
	ctx := context.Background()

	providers := repository.NewProviderRepository(database)
	metadata := repository.NewProviderMetadataRepository(database)
	svc := service.NewProviderService(providers, metadata, testEncryptionKey)

	p, err := ensureChatgptProvider(ctx, providers, svc)
	if err != nil {
		t.Fatalf("ensureChatgptProvider: %v", err)
	}
	if p == nil || p.ID == 0 {
		t.Fatal("expected a created provider with an ID")
	}
	if p.Name != "chatgpt" {
		t.Errorf("Name = %q, want chatgpt", p.Name)
	}
	if p.APIType != models.APITypeCodex {
		t.Errorf("APIType = %q, want codex", p.APIType)
	}
	if p.BaseURL != "https://chatgpt.com/backend-api/codex" {
		t.Errorf("BaseURL = %q, want https://chatgpt.com/backend-api/codex", p.BaseURL)
	}

	// Empty API key: the provider authenticates via ChatGPT OAuth only.
	stored, err := providers.GetByName(ctx, "chatgpt")
	if err != nil || stored == nil {
		t.Fatalf("stored provider: %v, %v", stored, err)
	}
	key, err := svc.DecryptAPIKey(stored.APIKeyEncrypted)
	if err != nil {
		t.Fatalf("decrypt stored key: %v", err)
	}
	if key != "" {
		t.Errorf("stored API key = %q, want empty", key)
	}
}

// TestEnsureChatgptProvider_Idempotent verifies an existing chatgpt row is
// reused (not duplicated) and that a pre-existing "codex"-named row (created
// via add-provider --name codex) is adopted instead of creating a second
// provider.
func TestEnsureChatgptProvider_Idempotent(t *testing.T) {
	t.Run("existing_chatgpt_row_reused", func(t *testing.T) {
		database := newSetupTestDB(t)
		ctx := context.Background()
		providers := repository.NewProviderRepository(database)
		metadata := repository.NewProviderMetadataRepository(database)
		svc := service.NewProviderService(providers, metadata, testEncryptionKey)

		first, err := ensureChatgptProvider(ctx, providers, svc)
		if err != nil {
			t.Fatalf("first create: %v", err)
		}
		second, err := ensureChatgptProvider(ctx, providers, svc)
		if err != nil {
			t.Fatalf("second call: %v", err)
		}
		if first.ID != second.ID {
			t.Errorf("expected same provider row, got %d and %d", first.ID, second.ID)
		}
	})

	t.Run("existing_codex_alias_row_adopted", func(t *testing.T) {
		database := newSetupTestDB(t)
		ctx := context.Background()
		providers := repository.NewProviderRepository(database)
		metadata := repository.NewProviderMetadataRepository(database)
		svc := service.NewProviderService(providers, metadata, testEncryptionKey)

		alias := &models.Provider{Name: "codex", APIType: models.APITypeCodex, BaseURL: "https://chatgpt.com/backend-api/codex", ProviderKey: "codex"}
		if err := providers.Create(ctx, alias); err != nil {
			t.Fatalf("seed alias row: %v", err)
		}

		p, err := ensureChatgptProvider(ctx, providers, svc)
		if err != nil {
			t.Fatalf("ensureChatgptProvider: %v", err)
		}
		if p.ID != alias.ID {
			t.Errorf("expected alias row id %d, got %d", alias.ID, p.ID)
		}
	})
}

// --- runChatgptConnect (service flow orchestration, fake OAuthService) ----------

// TestRunChatgptConnect_CallbackServerPath verifies the happy path of the
// shared helper: StartAuthFlowWithCallback is called with the fixed loopback
// address, the callback server binds the same address, and the returned token
// is surfaced as connected.
func TestRunChatgptConnect_CallbackServerPath(t *testing.T) {
	ctx := context.Background()
	provider := &models.Provider{ID: 7, Name: "chatgpt", APIType: models.APITypeCodex, BaseURL: "https://chatgpt.com/backend-api/codex"}

	fake := &fakeOAuthService{
		startServerToken: &models.OAuthToken{
			ProviderID:  7,
			AccessToken: "at-1",
			ExpiresAt:   time.Now().Add(time.Hour),
			AccountID:   "acc-1",
			Email:       "me@example.com",
		},
	}

	suppressStdout(t, func() {
		if err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), false, "localhost:1455", 50*time.Millisecond, nil, nil); err != nil {
			t.Errorf("runChatgptConnectOnAddr: %v", err)
		}
	})

	if !fake.startFlowCalled || fake.startFlowAddr != "localhost:1455" {
		t.Errorf("StartAuthFlowWithCallback addr = %q (called=%v), want localhost:1455", fake.startFlowAddr, fake.startFlowCalled)
	}
	if !fake.startServerCalled || fake.startServerAddr != "localhost:1455" {
		t.Errorf("StartCallbackServerOnAddr addr = %q (called=%v), want localhost:1455 (same address the authorize URL advertises)", fake.startServerAddr, fake.startServerCalled)
	}
}

// TestRunChatgptConnect_ManualFallsBack verifies the paste fallback: with
// --manual, no callback server is started; the authorize URL is printed and
// HandleCallback is invoked with the code/state parsed from the pasted URL.
func TestRunChatgptConnect_ManualFallsBack(t *testing.T) {
	ctx := context.Background()
	provider := &models.Provider{ID: 7, Name: "chatgpt", APIType: models.APITypeCodex}

	fake := &fakeOAuthService{
		startFlowState: "state-manual",
		handleToken: &models.OAuthToken{
			ProviderID:  7,
			AccessToken: "at-2",
			ExpiresAt:   time.Now().Add(time.Hour),
			AccountID:   "acc-2",
		},
	}

	// Simulate the user pasting the codex callback URL (with trailing
	// newline, as typed in a terminal).
	pasted := "http://localhost:1455/auth/callback?code=code-manual&state=state-manual\n"
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString(pasted); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	suppressStdout(t, func() {
		if err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), true, "localhost:1455", 50*time.Millisecond, nil, nil); err != nil {
			t.Errorf("runChatgptConnectOnAddr: %v", err)
		}
	})

	if fake.startServerCalled {
		t.Error("manual mode must not start the callback server")
	}
	if !fake.handleCalled {
		t.Error("manual mode should exchange the pasted code via HandleCallback")
	}
}

// TestRunChatgptConnect_PasteErrorPropagates verifies a failed paste flow
// (empty input) returns an error instead of os.Exit — setup depends on that
// to still create the predefined models after a failed OAuth exchange.
func TestRunChatgptConnect_PasteErrorPropagates(t *testing.T) {
	ctx := context.Background()
	provider := &models.Provider{ID: 7, Name: "chatgpt", APIType: models.APITypeCodex}

	fake := &fakeOAuthService{startFlowState: "state-e"}

	// Empty stdin → the "no URL provided" error path.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	var gotErr error
	suppressStdout(t, func() {
		gotErr = runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), true, "localhost:1455", 50*time.Millisecond, nil, nil)
	})

	if gotErr == nil {
		t.Fatal("expected an error for empty paste input")
	}
	if fake.handleCalled {
		t.Error("HandleCallback should not run without a code")
	}
}

// TestRunCodexPasteFallback_ParsesPastedCodexURL exercises the paste fallback
// directly (its unit boundary): codex callback URL shape, state preference.
func TestRunCodexPasteFallback_ParsesPastedCodexURL(t *testing.T) {
	ctx := context.Background()
	fake := &fakeOAuthService{
		startFlowState: "state-flow",
		handleToken: &models.OAuthToken{
			ProviderID:  7,
			AccessToken: "at-paste",
			ExpiresAt:   time.Now().Add(time.Hour),
			Email:       "paste@example.com",
		},
	}

	pasted := "http://localhost:1455/auth/callback?code=code-paste&state=state-paste\n"
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString(pasted); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	var token *models.OAuthToken
	suppressStdout(t, func() {
		token, err = runCodexPasteFallback(ctx, fake, "https://auth.openai.com/oauth/authorize?x=1", "state-flow")
	})
	if err != nil {
		t.Fatalf("runCodexPasteFallback: %v", err)
	}
	if token == nil || token.AccessToken != "at-paste" {
		t.Fatalf("token = %+v", token)
	}
	if !fake.handleCalled {
		t.Error("HandleCallback should have been called with the pasted code")
	}
}

// TestRunChatgptConnect_PortBusyFallsBack verifies the port-busy fallback:
// when something already listens on the callback port, the helper skips the
// callback server and goes straight to the paste flow.
func TestRunChatgptConnect_PortBusyFallsBack(t *testing.T) {
	ctx := context.Background()
	provider := &models.Provider{ID: 7, Name: "chatgpt", APIType: models.APITypeCodex}

	// Occupy a port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	busyAddr := listener.Addr().String()

	fake := &fakeOAuthService{
		startFlowState: "state-busy",
		handleToken: &models.OAuthToken{
			ProviderID:  7,
			AccessToken: "at-3",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}

	pasted := "http://localhost:1455/auth/callback?code=code-busy&state=state-busy\n"
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString(pasted); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	suppressStdout(t, func() {
		if err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), false, busyAddr, 50*time.Millisecond, nil, nil); err != nil {
			t.Errorf("runChatgptConnectOnAddr: %v", err)
		}
	})

	if fake.startServerCalled {
		t.Errorf("port busy: callback server must not be started")
	}
	if !fake.handleCalled {
		t.Error("port busy: should fall back to the paste flow (HandleCallback)")
	}
}

// TestRunChatgptConnect_AlreadyConnected verifies the early return when the
// OAuth service reports an existing valid session.
func TestRunChatgptConnect_AlreadyConnected(t *testing.T) {
	ctx := context.Background()
	provider := &models.Provider{ID: 7, Name: "chatgpt", APIType: models.APITypeCodex}

	fake := &fakeOAuthService{
		connected: true,
		connectedToken: &models.OAuthToken{
			ProviderID:  7,
			AccessToken: "at-existing",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
			AccountID:   "acc-existing",
		},
	}

	suppressStdout(t, func() {
		if err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), false, "localhost:1455", 50*time.Millisecond, nil, nil); err != nil {
			t.Errorf("runChatgptConnectOnAddr: %v", err)
		}
	})

	if fake.startFlowCalled {
		t.Error("already connected: no new auth flow should start")
	}
}

// --- StartAuthFlowWithCallback failure propagation -------------------------------

func TestRunChatgptConnect_StartFlowError(t *testing.T) {
	ctx := context.Background()
	provider := &models.Provider{ID: 7, Name: "chatgpt", APIType: models.APITypeCodex}

	fake := &fakeOAuthService{
		startFlowErr: errors.New("provider not found: 7"),
	}

	err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), false, "localhost:1455", 50*time.Millisecond, nil, nil)

	if err == nil {
		t.Fatal("expected error when StartAuthFlowWithCallback fails")
	}
	if !strings.Contains(err.Error(), "start auth flow") {
		t.Errorf("error should identify the failing step: %v", err)
	}
}

// --- extractCodeAndState ------------------------------------------------------------

// TestExtractCodeAndState_CodexCallbackShape verifies the codex callback URL
// shape (?code=...&state=... on /auth/callback) parses correctly, alongside
// the legacy claude-code shapes.
func TestExtractCodeAndState_CodexCallbackShape(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		code  string
		state string
	}{
		{"codex standard", "http://localhost:1455/auth/callback?code=abc123&state=st456", "abc123", "st456"},
		{"codex with extra params", "http://localhost:1455/auth/callback?code=c1&state=s1&scope=openid", "c1", "s1"},
		{"codex encoded params", "http://localhost:1455/auth/callback?code=a%20b&state=s%202", "a b", "s 2"},
		{"claude standard", "http://localhost:8080/callback?code=x&state=y", "x", "y"},
		{"empty", "", "", ""},
		{"no query", "http://localhost:1455/auth/callback", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, state := extractCodeAndState(tc.raw)
			if code != tc.code || state != tc.state {
				t.Errorf("extractCodeAndState(%q) = (%q, %q), want (%q, %q)", tc.raw, code, state, tc.code, tc.state)
			}
		})
	}
}

// TestExtractCodeAndState_FragmentFallback keeps the legacy claude-code
// fragment handling intact (connect.go must not regress the anthropic flow).
func TestExtractCodeAndState_FragmentFallback(t *testing.T) {
	code, state := extractCodeAndState("https://claude.ai/oauth/callback?code=frag-code#frag-state")
	if code != "frag-code" || state != "frag-state" {
		t.Errorf("= (%q, %q), want (frag-code, frag-state)", code, state)
	}
}

// --- canBindLoopback ------------------------------------------------------------------

func TestCanBindLoopback(t *testing.T) {
	// A free port reports bindable.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	freePort := l.Addr().(*net.TCPAddr).Port
	l.Close()

	if !canBindLoopback(fmt.Sprintf("127.0.0.1:%d", freePort)) {
		t.Errorf("port %d should be detected as free", freePort)
	}

	// An occupied port reports not bindable.
	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l2.Close()
	occupied := l2.Addr().(*net.TCPAddr).Port

	if canBindLoopback(fmt.Sprintf("127.0.0.1:%d", occupied)) {
		t.Errorf("port %d should be detected as occupied", occupied)
	}
}

// --- Constants -------------------------------------------------------------------------

// TestCodexOAuthCallbackConstants pins the design §4.4 contract: fixed port
// 1455 loopback callback and a 5-minute wait for the browser redirect.
func TestCodexOAuthCallbackConstants(t *testing.T) {
	if codexOAuthCallbackAddr != "localhost:1455" {
		t.Errorf("codexOAuthCallbackAddr = %q, want localhost:1455", codexOAuthCallbackAddr)
	}
	if codexOAuthCallbackTimeout != 5*time.Minute {
		t.Errorf("codexOAuthCallbackTimeout = %v, want 5m", codexOAuthCallbackTimeout)
	}
}

// --- Remote-machine race: paste vs automatic callback ------------------------------

// TestRunChatgptConnect_PasteWinsWhileServerBound covers the REMOTE-machine
// bug: the callback server binds fine on the remote box (no port-busy
// fallback), the browser redirect lands on the user's laptop and can never
// reach it. The pasted URL must win the race, the flow must complete via
// HandleCallback, and the injected server wait must be cancelled (no leak).
func TestRunChatgptConnect_PasteWinsWhileServerBound(t *testing.T) {
	ctx := context.Background()
	provider := &models.Provider{ID: 7, Name: "chatgpt", APIType: models.APITypeCodex}

	fake := &fakeOAuthService{
		startFlowState: "state-remote",
		handleToken: &models.OAuthToken{
			ProviderID:  7,
			AccessToken: "at-remote",
			ExpiresAt:   time.Now().Add(time.Hour),
			Email:       "remote@example.com",
		},
	}

	// The injected server wait mirrors the real one's cancellation contract:
	// when the flow ctx is cancelled (paste won), it returns promptly.
	cancelObserved := make(chan struct{})
	serverWait := func(waitCtx context.Context) (*models.OAuthToken, error) {
		<-waitCtx.Done()
		close(cancelObserved)
		return nil, fmt.Errorf("timeout waiting for OAuth callback")
	}

	pasted := "http://localhost:1455/auth/callback?code=ac_remote123&scope=openid+profile+email+offline_access&state=state-remote\n"
	cli, output := cliIOFor(t, pasted)

	var gotErr error
	suppressStdout(t, func() {
		gotErr = runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), false, "localhost:1455", time.Minute, cli, serverWait)
	})
	if gotErr != nil {
		t.Fatalf("paste-wins flow failed: %v", gotErr)
	}

	// Flow completed via the pasted code, not the server wait.
	if !fake.handleCalled {
		t.Error("HandleCallback should run for the pasted code")
	}
	if fake.startServerToken != nil {
		t.Error("server wait must not deliver a token in the paste-wins scenario")
	}

	// The loser (server wait) was shut down via ctx cancellation.
	select {
	case <-cancelObserved:
	case <-time.After(2 * time.Second):
		t.Error("flow context was not cancelled after paste win (server wait leaked)")
	}

	// UX: the authorize URL and the remote-machine paste instructions printed
	// immediately; success output printed.
	out := output()
	for _, want := range []string{
		"Open this URL in your browser",
		"If llm-router runs on a different machine than your browser",
		"press Enter to keep waiting",
		"✓ Connected!",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

// TestRunChatgptConnect_RemoteScenarioHandleCallbackOnce is the end-to-end-ish
// remote scenario: server bound on addr X, pasted URL contains code+state
// matching the flow's state, HandleCallback is invoked exactly once with them.
func TestRunChatgptConnect_RemoteScenarioHandleCallbackOnce(t *testing.T) {
	ctx := context.Background()
	provider := &models.Provider{ID: 7, Name: "chatgpt", APIType: models.APITypeCodex}

	fake := &fakeOAuthService{
		startFlowState: "the-flow-state",
		handleToken: &models.OAuthToken{
			ProviderID:  7,
			AccessToken: "at-once",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	serverWait := func(waitCtx context.Context) (*models.OAuthToken, error) {
		<-waitCtx.Done()
		return nil, fmt.Errorf("timeout waiting for OAuth callback")
	}

	pasted := "http://localhost:1455/auth/callback?code=ac_xyz&scope=openid+profile+email+offline_access&state=the-flow-state\n"
	cli, _ := cliIOFor(t, pasted)

	suppressStdout(t, func() {
		if err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), false, "localhost:1455", time.Minute, cli, serverWait); err != nil {
			t.Errorf("flow: %v", err)
		}
	})

	if !fake.handleCalled {
		t.Fatal("HandleCallback not called")
	}
	if fake.handleCalledCount() != 1 {
		t.Errorf("HandleCallback called %d times, want exactly 1", fake.handleCalledCount())
	}
	if fake.handleCode() != "ac_xyz" || fake.handleState() != "the-flow-state" {
		t.Errorf("HandleCallback args = (%q, %q), want (ac_xyz, the-flow-state)", fake.handleCode(), fake.handleState())
	}
}

// TestRunChatgptConnect_CallbackWinsFirst verifies the same-machine case is
// untouched: the automatic callback delivers the token, the flow completes via
// the server wait, and HandleCallback (the paste exchange) never runs.
func TestRunChatgptConnect_CallbackWinsFirst(t *testing.T) {
	ctx := context.Background()
	provider := &models.Provider{ID: 7, Name: "chatgpt", APIType: models.APITypeCodex}

	fake := &fakeOAuthService{
		startFlowState: "state-cb",
		startServerToken: &models.OAuthToken{
			ProviderID:  7,
			AccessToken: "at-cb",
			ExpiresAt:   time.Now().Add(time.Hour),
			AccountID:   "acc-cb",
		},
	}
	// Release the token slightly later than the paste input is readable, so
	// the race genuinely has both sources alive; the token must still win
	// because the pasted line is EMPTY (keep waiting) — i.e. the flow may only
	// complete via the callback.
	serverWait := func(waitCtx context.Context) (*models.OAuthToken, error) {
		time.Sleep(150 * time.Millisecond)
		select {
		case <-waitCtx.Done():
			return nil, fmt.Errorf("cancelled")
		default:
			return fake.startServerToken, nil
		}
	}

	// Empty line (keep waiting), then EOF.
	cli, output := cliIOFor(t, "\n")

	suppressStdout(t, func() {
		if err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), false, "localhost:1455", 5*time.Second, cli, serverWait); err != nil {
			t.Errorf("flow: %v", err)
		}
	})

	if fake.handleCalled {
		t.Error("empty paste must not exchange a code")
	}
	out := output()
	if !strings.Contains(out, "✓ Connected!") {
		t.Errorf("callback-wins flow should print success, got: %q", out)
	}
	// Note: the "still waiting" note may legitimately appear when the empty
	// line was processed before the token arrived — pressing Enter told the
	// user we keep waiting, and then the callback landed. Only the completion
	// source matters here, and it was the server wait.
}

// TestRunChatgptConnect_EmptyLineKeepsWaiting verifies an empty paste line
// keeps the race alive: the callback arriving after the empty line still
// completes the flow.
func TestRunChatgptConnect_EmptyLineKeepsWaiting(t *testing.T) {
	ctx := context.Background()
	provider := &models.Provider{ID: 7, Name: "chatgpt", APIType: models.APITypeCodex}

	fake := &fakeOAuthService{
		startFlowState: "state-empty",
		startServerToken: &models.OAuthToken{
			ProviderID:  7,
			AccessToken: "at-after-empty",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	serverWait := func(waitCtx context.Context) (*models.OAuthToken, error) {
		// The token arrives only after the paste goroutine had time to deliver
		// the empty line first.
		time.Sleep(150 * time.Millisecond)
		select {
		case <-waitCtx.Done():
			return nil, fmt.Errorf("cancelled")
		default:
			return fake.startServerToken, nil
		}
	}

	cli, output := cliIOFor(t, "\n")

	suppressStdout(t, func() {
		if err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), false, "localhost:1455", 5*time.Second, cli, serverWait); err != nil {
			t.Errorf("flow: %v", err)
		}
	})

	if fake.handleCalled {
		t.Error("empty line must not trigger HandleCallback")
	}
	if !strings.Contains(output(), "still waiting for the automatic callback") {
		t.Error("expected the keep-waiting note after an empty line")
	}
}

// TestRunChatgptConnect_InvalidPasteReprompts verifies garbage input
// re-prompts (flow continues) and a subsequent valid paste completes.
func TestRunChatgptConnect_InvalidPasteReprompts(t *testing.T) {
	ctx := context.Background()
	provider := &models.Provider{ID: 7, Name: "chatgpt", APIType: models.APITypeCodex}

	fake := &fakeOAuthService{
		startFlowState: "state-garbage",
		handleToken: &models.OAuthToken{
			ProviderID:  7,
			AccessToken: "at-after-garbage",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	serverWait := func(waitCtx context.Context) (*models.OAuthToken, error) {
		<-waitCtx.Done()
		return nil, fmt.Errorf("timeout waiting for OAuth callback")
	}

	// Garbage (no code), then a valid paste.
	stdin := "not-a-callback-url\nhttp://localhost:1455/auth/callback?code=ac_ok&state=state-garbage\n"
	cli, output := cliIOFor(t, stdin)

	suppressStdout(t, func() {
		if err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), false, "localhost:1455", time.Minute, cli, serverWait); err != nil {
			t.Errorf("flow: %v", err)
		}
	})

	if fake.handleCalledCount() != 1 {
		t.Errorf("HandleCallback called %d times, want 1 (only the valid paste)", fake.handleCalledCount())
	}
	if fake.handleCode() != "ac_ok" {
		t.Errorf("HandleCallback code = %q, want ac_ok", fake.handleCode())
	}
	if n := strings.Count(output(), "could not extract a code"); n != 1 {
		t.Errorf("expected exactly 1 invalid-paste notice, got %d", n)
	}
	if prompts := strings.Count(output(), "Paste callback URL (Enter to keep waiting): "); prompts < 2 {
		t.Errorf("expected re-prompt after invalid paste, prompt count = %d", prompts)
	}
	if !strings.Contains(output(), "✓ Connected!") {
		t.Error("valid paste after garbage should complete the flow")
	}
}

// TestRunChatgptConnect_ManualNoServerBind asserts manual mode never touches
// the callback server and runs the paste-only loop (the success path).
func TestRunChatgptConnect_ManualNoServerBind(t *testing.T) {
	ctx := context.Background()
	provider := &models.Provider{ID: 7, Name: "chatgpt", APIType: models.APITypeCodex}

	fake := &fakeOAuthService{
		startFlowState: "state-manual2",
		handleToken: &models.OAuthToken{
			ProviderID:  7,
			AccessToken: "at-manual2",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	serverWait := func(waitCtx context.Context) (*models.OAuthToken, error) {
		t.Error("manual mode must not start the callback server wait")
		return nil, fmt.Errorf("should not be called")
	}

	pasted := "http://localhost:1455/auth/callback?code=ac_m&state=state-manual2\n"
	cli, output := cliIOFor(t, pasted)

	suppressStdout(t, func() {
		if err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), true, "localhost:1455", time.Minute, cli, serverWait); err != nil {
			t.Errorf("flow: %v", err)
		}
	})

	if !fake.handleCalled {
		t.Error("manual mode should complete via the pasted code")
	}
	if strings.Contains(output(), "Manual mode: automatic callback disabled.") == false {
		t.Error("manual banner missing")
	}
	// No "press Enter to keep waiting" in manual mode: nothing to wait for.
	if strings.Contains(output(), "press Enter to keep waiting") {
		t.Error("manual mode must not advertise waiting for the automatic callback")
	}
}
