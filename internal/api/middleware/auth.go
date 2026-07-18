package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/chris/llm-router/internal/service"
)

type contextKey string

const ProxyKeyIDContextKey contextKey = "proxy_key_id"

func AuthMiddleware(keyService service.KeyService, adminService service.AdminService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			// Check admin session first
			if adminService != nil && adminService.ValidateSession(token) {
				next.ServeHTTP(w, r)
				return
			}

			// Check API key
			pk, err := keyService.ValidateKey(r.Context(), token)
			if err != nil {
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}
			if pk == nil {
				http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), ProxyKeyIDContextKey, pk.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}
	return ""
}
