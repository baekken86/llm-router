package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

type OAuthHandler struct {
	mu          sync.RWMutex
	codes       map[string]*authCode
	tokens      map[string]*oauthToken
	clientKeys  map[string]string // client_id -> proxy_key
	proxyKeyVal func(key string) bool
}

type authCode struct {
	Code        string
	ClientID    string
	RedirectURI string
	ExpiresAt   time.Time
	ProxyKeyID  int64
}

type oauthToken struct {
	AccessToken  string
	RefreshToken string
	ClientID     string
	ExpiresAt    time.Time
	ProxyKeyID   int64
}

func NewOAuthHandler(validateProxyKey func(string) int64) *OAuthHandler {
	return &OAuthHandler{
		codes:      make(map[string]*authCode),
		tokens:     make(map[string]*oauthToken),
		clientKeys: make(map[string]string),
		proxyKeyVal: func(key string) bool {
			return validateProxyKey(key) > 0
		},
	}
}

func (h *OAuthHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/authorize", h.Authorize)
	r.Post("/token", h.Token)
	r.Post("/revoke", h.Revoke)
	return r
}

func (h *OAuthHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")
	responseType := r.URL.Query().Get("response_type")
	state := r.URL.Query().Get("state")

	if responseType != "code" {
		http.Error(w, "unsupported response_type", http.StatusBadRequest)
		return
	}

	if clientID == "" {
		clientID = "claude-code"
	}

	if redirectURI == "" {
		redirectURI = "http://127.0.0.1:" + extractPort(r) + "/callback"
	}

	proxyKey := r.URL.Query().Get("proxy_key")
	if proxyKey == "" {
		proxyKey = r.Header.Get("X-Proxy-Key")
	}

	if proxyKey != "" {
		code := h.generateCode(clientID, redirectURI, 0)
		h.redirectWithCode(w, r, redirectURI, code, state)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>llm-router - Authorize</title>
<style>
body { font-family: system-ui; max-width: 400px; margin: 80px auto; padding: 20px; }
h1 { font-size: 1.5em; color: #333; }
input { width: 100%%; padding: 10px; margin: 10px 0; border: 1px solid #ccc; border-radius: 4px; box-sizing: border-box; }
button { width: 100%%; padding: 12px; background: #6366f1; color: white; border: none; border-radius: 4px; cursor: pointer; font-size: 1em; }
button:hover { background: #4f46e5; }
.info { color: #666; font-size: 0.9em; margin-top: 10px; }
</style>
</head>
<body>
<h1>llm-router</h1>
<p>Enter your proxy API key to authorize Claude Code:</p>
<form method="POST" action="/v1/oauth/authorize">
  <input type="hidden" name="client_id" value="%s">
  <input type="hidden" name="redirect_uri" value="%s">
  <input type="hidden" name="state" value="%s">
  <input type="password" name="proxy_key" placeholder="lmr_..." autofocus>
  <button type="submit">Authorize</button>
</form>
<p class="info">Get your API key from: <code>llm-router admin</code> or check the proxy startup logs.</p>
</body>
</html>`, clientID, redirectURI, state)
}

func (h *OAuthHandler) AuthorizePost(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	state := r.FormValue("state")
	proxyKey := r.FormValue("proxy_key")

	if proxyKey == "" {
		http.Error(w, "proxy_key required", http.StatusBadRequest)
		return
	}

	code := h.generateCode(clientID, redirectURI, 0)
	h.redirectWithCode(w, r, redirectURI, code, state)
}

func (h *OAuthHandler) Token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	grantType := r.FormValue("grant_type")

	switch grantType {
	case "authorization_code":
		h.handleAuthorizationCode(w, r)
	case "refresh_token":
		h.handleRefreshToken(w, r)
	default:
		http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
	}
}

func (h *OAuthHandler) handleAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")

	h.mu.Lock()
	authCode, exists := h.codes[code]
	if exists {
		delete(h.codes, code)
	}
	h.mu.Unlock()

	if !exists {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}

	if time.Now().After(authCode.ExpiresAt) {
		http.Error(w, `{"error":"invalid_grant","error_description":"code expired"}`, http.StatusBadRequest)
		return
	}

	if clientID != "" && authCode.ClientID != clientID {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}

	if redirectURI != "" && authCode.RedirectURI != redirectURI {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}

	accessToken := h.generateToken()
	refreshToken := h.generateToken()

	h.mu.Lock()
	h.tokens[accessToken] = &oauthToken{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ClientID:     authCode.ClientID,
		ExpiresAt:    time.Now().Add(24 * time.Hour),
		ProxyKeyID:   authCode.ProxyKeyID,
	}
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    86400,
		"refresh_token": refreshToken,
		"scope":         "user:inference",
	})
}

func (h *OAuthHandler) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.FormValue("refresh_token")

	h.mu.Lock()
	var oldToken *oauthToken
	for _, t := range h.tokens {
		if t.RefreshToken == refreshToken {
			oldToken = t
			break
		}
	}

	if oldToken == nil {
		h.mu.Unlock()
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}

	delete(h.tokens, oldToken.AccessToken)

	newAccessToken := h.generateToken()
	newRefreshToken := h.generateToken()

	h.tokens[newAccessToken] = &oauthToken{
		AccessToken:  newAccessToken,
		RefreshToken: newRefreshToken,
		ClientID:     oldToken.ClientID,
		ExpiresAt:    time.Now().Add(24 * time.Hour),
		ProxyKeyID:   oldToken.ProxyKeyID,
	}
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"access_token":  newAccessToken,
		"token_type":    "Bearer",
		"expires_in":    86400,
		"refresh_token": newRefreshToken,
		"scope":         "user:inference",
	})
}

func (h *OAuthHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")

	h.mu.Lock()
	delete(h.tokens, token)
	h.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

func (h *OAuthHandler) ValidateToken(token string) int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	t, exists := h.tokens[token]
	if !exists {
		return 0
	}

	if time.Now().After(t.ExpiresAt) {
		return 0
	}

	return t.ProxyKeyID
}

func (h *OAuthHandler) generateCode(clientID, redirectURI string, proxyKeyID int64) string {
	b := make([]byte, 16)
	rand.Read(b)
	code := hex.EncodeToString(b)

	h.mu.Lock()
	h.codes[code] = &authCode{
		Code:        code,
		ClientID:    clientID,
		RedirectURI: redirectURI,
		ExpiresAt:   time.Now().Add(5 * time.Minute),
		ProxyKeyID:  proxyKeyID,
	}
	h.mu.Unlock()

	return code
}

func (h *OAuthHandler) generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "llmr_" + hex.EncodeToString(b)
}

func (h *OAuthHandler) redirectWithCode(w http.ResponseWriter, r *http.Request, redirectURI, code, state string) {
separator := "?"
	if strings.Contains(redirectURI, "?") {
	separator = "&"
	}

	location := redirectURI + separator + "code=" + url.QueryEscape(code)
	if state != "" {
		location += "&state=" + url.QueryEscape(state)
	}

	http.Redirect(w, r, location, http.StatusFound)
}

func extractPort(r *http.Request) string {
	host := r.Host
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		return host[idx+1:]
	}
	return "8080"
}
