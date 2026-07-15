package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	AnthropicAuthURL  = "https://console.anthropic.com/oauth/authorize"
	AnthropicTokenURL = "https://console.anthropic.com/oauth/token"
	AnthropicClientID = "9d1c27c1-43be-4399-86c4-6252263f29b3"
	DefaultRedirectURI = "http://localhost:8080/v1/oauth/callback"
)

type AnthropicOAuth struct {
	clientID     string
	redirectURI  string
	httpClient   *http.Client
	codeVerifier string
}

type AnthropicTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

type AnthropicTokenInfo struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	AccountID    string
	Email        string
}

func NewAnthropicOAuth(clientID, redirectURI string) *AnthropicOAuth {
	if clientID == "" {
		clientID = AnthropicClientID
	}
	if redirectURI == "" {
		redirectURI = DefaultRedirectURI
	}
	return &AnthropicOAuth{
		clientID:    clientID,
		redirectURI: redirectURI,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *AnthropicOAuth) SetRedirectURI(uri string) {
	a.redirectURI = uri
}

func (a *AnthropicOAuth) GetAuthorizationURL(state string) string {
	a.codeVerifier = generateCodeVerifier()
	codeChallenge := generateCodeChallenge(a.codeVerifier)

	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {a.clientID},
		"redirect_uri":          {a.redirectURI},
		"state":                 {state},
		"scope":                 {"user:inference user:profile"},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}
	return AnthropicAuthURL + "?" + params.Encode()
}

func (a *AnthropicOAuth) ExchangeCode(ctx context.Context, code string) (*AnthropicTokenInfo, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {a.redirectURI},
		"client_id":     {a.clientID},
		"code_verifier": {a.codeVerifier},
	}

	return a.doTokenRequest(ctx, data)
}

func (a *AnthropicOAuth) RefreshToken(ctx context.Context, refreshToken string) (*AnthropicTokenInfo, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {a.clientID},
	}

	return a.doTokenRequest(ctx, data)
}

func (a *AnthropicOAuth) doTokenRequest(ctx context.Context, data url.Values) (*AnthropicTokenInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", AnthropicTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token request failed: %d %s", resp.StatusCode, string(body))
	}

	var tokenResp AnthropicTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	info := &AnthropicTokenInfo{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}

	return info, nil
}

func (a *AnthropicOAuth) GetUserInfo(ctx context.Context, accessToken string) (accountID, email string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://console.anthropic.com/api/v1/me", nil)
	if err != nil {
		return "", "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("get user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("get user info failed: %d", resp.StatusCode)
	}

	var result struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("decode response: %w", err)
	}

	return result.ID, result.Email, nil
}

func (a *AnthropicOAuth) ValidateToken(ctx context.Context, accessToken string) bool {
	_, _, err := a.GetUserInfo(ctx, accessToken)
	return err == nil
}

func generateCodeVerifier() string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	b := make([]byte, 64)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func generateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
