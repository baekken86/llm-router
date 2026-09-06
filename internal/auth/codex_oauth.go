package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	CodexAuthorizeURL       = "https://auth.openai.com/oauth/authorize"
	CodexTokenURL           = "https://auth.openai.com/oauth/token"
	CodexClientID           = "app_EMoamEEZ73f0CkXaXp7hrann"
	CodexScope              = "openid profile email offline_access"
	CodexDefaultRedirectURI = "http://localhost:1455/auth/callback"
)

// codexTokenURL is a var (not a const) so tests can point it at an httptest
// server; production code always sees the CodexTokenURL default.
var codexTokenURL = CodexTokenURL

type CodexOAuth struct {
	clientID     string
	redirectURI  string
	httpClient   *http.Client
	codeVerifier string
}

type CodexTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func NewCodexOAuth(clientID, redirectURI string) *CodexOAuth {
	if clientID == "" {
		clientID = CodexClientID
	}
	if redirectURI == "" {
		redirectURI = CodexDefaultRedirectURI
	}
	return &CodexOAuth{
		clientID:    clientID,
		redirectURI: redirectURI,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// SetRedirectURI overrides the default redirect URI (e.g. for the
// callback-server flow). Mirrors AnthropicOAuth.SetRedirectURI.
func (o *CodexOAuth) SetRedirectURI(uri string) {
	o.redirectURI = uri
}

func (o *CodexOAuth) GetAuthorizationURL(state string) string {
	o.codeVerifier = generateCodexCodeVerifier()
	codeChallenge := generateCodexCodeChallenge(o.codeVerifier)

	params := url.Values{
		"response_type":              {"code"},
		"client_id":                  {o.clientID},
		"redirect_uri":               {o.redirectURI},
		"state":                      {state},
		"scope":                      {CodexScope},
		"code_challenge":             {codeChallenge},
		"code_challenge_method":      {"S256"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {"codex_cli_rs"},
	}
	// url.Values.Encode() encodes spaces as "+"; the OpenAI authorize endpoint
	// (and the reference Codex CLI) use %20 for the scope list.
	return CodexAuthorizeURL + "?" + strings.ReplaceAll(params.Encode(), "+", "%20")
}

func (o *CodexOAuth) ExchangeCode(ctx context.Context, code string) (*CodexTokens, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {o.clientID},
		"code":          {code},
		"redirect_uri":  {o.redirectURI},
		"code_verifier": {o.codeVerifier},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", codexTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return o.doTokenRequest(req)
}

func (o *CodexOAuth) RefreshToken(ctx context.Context, refreshToken string) (*CodexTokens, error) {
	// The refresh endpoint expects a JSON body (unlike the form-encoded
	// exchange) — matching the reference Codex CLI implementation.
	jsonBody, err := json.Marshal(map[string]string{
		"client_id":     o.clientID,
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", codexTokenURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	return o.doTokenRequest(req)
}

func (o *CodexOAuth) doTokenRequest(req *http.Request) (*CodexTokens, error) {
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("codex token request failed: %d %s", resp.StatusCode, codexTokenErrorMessage(respBody))
	}

	var tokens CodexTokens
	if err := json.Unmarshal(respBody, &tokens); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &tokens, nil
}

// codexTokenErrorMessage renders a token-endpoint error body. The endpoint may
// return the flat OAuth form ({"error":"invalid_grant",...}) or a nested
// object ({"error":{"type":...,"message":...}}); the raw body is always kept
// in the message.
func codexTokenErrorMessage(body []byte) string {
	raw := string(body)

	var flat struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &flat); err == nil && flat.Error != "" {
		msg := flat.Error
		if flat.ErrorDescription != "" {
			msg += ": " + flat.ErrorDescription
		}
		return msg + " (body: " + raw + ")"
	}

	var nested struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &nested); err == nil && (nested.Error.Type != "" || nested.Error.Message != "") {
		msg := nested.Error.Type
		if nested.Error.Message != "" {
			if msg != "" {
				msg += ": "
			}
			msg += nested.Error.Message
		}
		return msg + " (body: " + raw + ")"
	}

	return raw
}

// ParseIDToken extracts account/plan/email claims from a Codex id_token JWT.
// The token is trusted as delivered by the OAuth token endpoint (it comes over
// TLS from auth.openai.com); the signature is NOT verified — same trust model
// as the Anthropic flow.
func ParseIDToken(idToken string) (accountID, planType, email string, err error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("malformed id_token: expected 3 segments, got %d", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some issuers pad the payload; retry with padded base64url.
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return "", "", "", fmt.Errorf("decode id_token payload: %w", err)
		}
	}

	var claims struct {
		Email string `json:"email"`
		Auth  struct {
			ChatGPTAccountID string `json:"chatgpt_account_id"`
			ChatGPTPlanType  string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", "", "", fmt.Errorf("decode id_token claims: %w", err)
	}

	return claims.Auth.ChatGPTAccountID, claims.Auth.ChatGPTPlanType, claims.Email, nil
}

// generateCodexCodeVerifier returns a PKCE verifier: 32 crypto-random bytes,
// base64url-encoded (43 chars, RFC 7636). Local sibling of the anthropic
// helpers — kept separate so anthropic_oauth.go stays untouched.
func generateCodexCodeVerifier() string {
	b := make([]byte, 32)
	// crypto/rand.Read is documented to always succeed; the error return
	// exists only for signature compatibility.
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func generateCodexCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
