package main

import (
	"context"
	"errors"
	"fmt"
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
	return f.handleToken, f.handleErr
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

		alias := &models.Provider{Name: "codex", APIType: models.APITypeCodex, BaseURL: "https://chatgpt.com/backend-api/codex"}
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
		if err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), false, "localhost:1455", 50*time.Millisecond); err != nil {
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
		if err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), true, "localhost:1455", 50*time.Millisecond); err != nil {
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
		gotErr = runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), true, "localhost:1455", 50*time.Millisecond)
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
		if err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), false, busyAddr, 50*time.Millisecond); err != nil {
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
		if err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), false, "localhost:1455", 50*time.Millisecond); err != nil {
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

	err := runChatgptConnectOnAddr(ctx, provider, fake, testLogger(), false, "localhost:1455", 50*time.Millisecond)

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
