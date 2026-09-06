package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testTokenJSON = `{
	"access_token": "at_test_123",
	"refresh_token": "rt_test_456",
	"id_token": "idt.test.sig",
	"expires_in": 28800
}`

func TestCodexGetAuthorizationURL(t *testing.T) {
	o := NewCodexOAuth("", "")
	state := "test-state-abc123"
	got := o.GetAuthorizationURL(state)

	if !strings.HasPrefix(got, CodexAuthorizeURL+"?") {
		t.Fatalf("authorize URL should start with %q, got %q", CodexAuthorizeURL, got)
	}

	assertContains(t, got,
		"response_type=code",
		"client_id="+CodexClientID,
		"redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback",
		"state=test-state-abc123",
		"scope=openid%20profile%20email%20offline_access",
		"code_challenge=",
		"code_challenge_method=S256",
		"id_token_add_organizations=true",
		"codex_cli_simplified_flow=true",
		"originator=codex_cli_rs",
	)
}

func TestCodexGetAuthorizationURLCustomClientAndRedirect(t *testing.T) {
	o := NewCodexOAuth("custom_client", "http://127.0.0.1:9999/cb")
	got := o.GetAuthorizationURL("state")

	assertContains(t, got, "client_id=custom_client", "redirect_uri=http%3A%2F%2F127.0.0.1%3A9999%2Fcb")
}

func TestCodexExchangeCode(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testTokenJSON))
	}))
	defer srv.Close()

	restore := setCodexTokenURL(t, srv.URL)
	defer restore()

	o := NewCodexOAuth("", "")
	o.GetAuthorizationURL("some-state") // sets codeVerifier

	if o.codeVerifier == "" {
		t.Fatal("codeVerifier should be set by GetAuthorizationURL")
	}

	tokens, err := o.ExchangeCode(context.Background(), "auth-code-xyz")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}

	assertContains(t, gotBody,
		"grant_type=authorization_code",
		"client_id="+CodexClientID,
		"code=auth-code-xyz",
		"redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback",
		"code_verifier="+o.codeVerifier,
	)

	if tokens.AccessToken != "at_test_123" {
		t.Errorf("AccessToken = %q, want %q", tokens.AccessToken, "at_test_123")
	}
	if tokens.RefreshToken != "rt_test_456" {
		t.Errorf("RefreshToken = %q, want %q", tokens.RefreshToken, "rt_test_456")
	}
	if tokens.IDToken != "idt.test.sig" {
		t.Errorf("IDToken = %q, want %q", tokens.IDToken, "idt.test.sig")
	}
	if tokens.ExpiresIn != 28800 {
		t.Errorf("ExpiresIn = %d, want %d", tokens.ExpiresIn, 28800)
	}
}

func TestCodexRefreshToken(t *testing.T) {
	var gotBody string
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testTokenJSON))
	}))
	defer srv.Close()

	restore := setCodexTokenURL(t, srv.URL)
	defer restore()

	o := NewCodexOAuth("", "")
	tokens, err := o.RefreshToken(context.Background(), "rt_existing_789")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}

	if !strings.Contains(gotContentType, "application/json") {
		t.Errorf("refresh request Content-Type = %q, want application/json", gotContentType)
	}

	// Refresh uses a JSON body (unlike the form-encoded exchange).
	var parsed map[string]string
	if err := json.Unmarshal([]byte(gotBody), &parsed); err != nil {
		t.Fatalf("refresh body is not JSON: %v (body: %q)", err, gotBody)
	}
	want := map[string]string{
		"client_id":     CodexClientID,
		"grant_type":    "refresh_token",
		"refresh_token": "rt_existing_789",
	}
	for k, v := range want {
		if parsed[k] != v {
			t.Errorf("refresh body[%q] = %q, want %q", k, parsed[k], v)
		}
	}

	if tokens.AccessToken != "at_test_123" || tokens.RefreshToken != "rt_test_456" {
		t.Errorf("parsed tokens wrong: %+v", tokens)
	}
}

func TestCodexTokenRequestError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"code expired"}`))
	}))
	defer srv.Close()

	restore := setCodexTokenURL(t, srv.URL)
	defer restore()

	o := NewCodexOAuth("", "")
	_, err := o.ExchangeCode(context.Background(), "bad-code")
	if err == nil {
		t.Fatal("expected error on 400 response")
	}
	for _, part := range []string{"400", "invalid_grant", "code expired"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q should contain %q", err.Error(), part)
		}
	}
}

func TestCodexTokenRequestNestedErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"type":"server_error","message":"boom"}}`))
	}))
	defer srv.Close()

	restore := setCodexTokenURL(t, srv.URL)
	defer restore()

	o := NewCodexOAuth("", "")
	_, err := o.RefreshToken(context.Background(), "rt_x")
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
	for _, part := range []string{"500", "server_error", "boom"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q should contain %q", err.Error(), part)
		}
	}
}

func TestParseIDToken(t *testing.T) {
	const (
		accountID = "0f8a9c1b-2f4e-4c8a-9d3e-5b6a7c8d9e0f"
		planType  = "pro"
		email     = "user@example.com"
	)
	jwt := buildTestIDToken(t, accountID, planType, email)

	gotAccount, gotPlan, gotEmail, err := ParseIDToken(jwt)
	if err != nil {
		t.Fatalf("ParseIDToken: %v", err)
	}
	if gotAccount != accountID {
		t.Errorf("accountID = %q, want %q", gotAccount, accountID)
	}
	if gotPlan != planType {
		t.Errorf("planType = %q, want %q", gotPlan, planType)
	}
	if gotEmail != email {
		t.Errorf("email = %q, want %q", gotEmail, email)
	}
}

func TestParseIDTokenPaddedPayload(t *testing.T) {
	claims := map[string]any{
		"email": "padded@example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acc-pad",
			"chatgpt_plan_type":  "plus",
		},
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	padded := base64.URLEncoding.EncodeToString(payload) // includes "=" padding
	jwt := "header." + padded + ".sig"

	accountID, planType, email, err := ParseIDToken(jwt)
	if err != nil {
		t.Fatalf("ParseIDToken (padded): %v", err)
	}
	if accountID != "acc-pad" || planType != "plus" || email != "padded@example.com" {
		t.Errorf("got (%q, %q, %q), want (acc-pad, plus, padded@example.com)", accountID, planType, email)
	}
}

func TestParseIDTokenErrors(t *testing.T) {
	t.Run("garbage payload", func(t *testing.T) {
		if _, _, _, err := ParseIDToken("header.!!!not-base64!!.sig"); err == nil {
			t.Error("expected error for undecodable payload")
		}
	})
	t.Run("payload not json", func(t *testing.T) {
		payload := base64.RawURLEncoding.EncodeToString([]byte("not json"))
		if _, _, _, err := ParseIDToken("header." + payload + ".sig"); err == nil {
			t.Error("expected error for non-JSON payload")
		}
	})
	t.Run("wrong segment count", func(t *testing.T) {
		if _, _, _, err := ParseIDToken("only.two"); err == nil {
			t.Error("expected error for 2-segment token")
		}
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"x"}`))
		if _, _, _, err := ParseIDToken("a." + payload + ".b.c"); err == nil {
			t.Error("expected error for 4-segment token")
		}
	})
	t.Run("empty token", func(t *testing.T) {
		if _, _, _, err := ParseIDToken(""); err == nil {
			t.Error("expected error for empty token")
		}
	})
}

// buildTestIDToken assembles a fake JWT (header.payload.signature,
// base64url, unpadded) with the OpenAI auth claim namespace.
func buildTestIDToken(t *testing.T, accountID, planType, email string) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))

	claims := map[string]any{
		"email": email,
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": accountID,
			"chatgpt_plan_type":  planType,
		},
	}
	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)

	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
}

func TestPKCEShape(t *testing.T) {
	o := NewCodexOAuth("", "")
	o.GetAuthorizationURL("state")

	// 32 crypto-random bytes -> 43-char base64url verifier (RFC 7636).
	if len(o.codeVerifier) != 43 {
		t.Errorf("verifier length = %d, want 43", len(o.codeVerifier))
	}

	// Challenge must be S256(verifier): sha256 digest, base64url.
	challenge := generateCodexCodeChallenge(o.codeVerifier)
	if len(challenge) != 43 {
		t.Errorf("challenge length = %d, want 43", len(challenge))
	}
	if strings.Contains(challenge, "=") {
		t.Error("challenge should be unpadded base64url")
	}
}

// setCodexTokenURL points the package token-URL var at a test server and
// returns a restore func for defer.
func setCodexTokenURL(t *testing.T, url string) func() {
	t.Helper()
	prev := codexTokenURL
	codexTokenURL = url
	return func() { codexTokenURL = prev }
}

func assertContains(t *testing.T, got string, substrings ...string) {
	t.Helper()
	for _, s := range substrings {
		if !strings.Contains(got, s) {
			t.Errorf("URL/body %q should contain %q", got, s)
		}
	}
}
