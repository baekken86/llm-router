# Admin Password Authentication - Implementation Plan

## Overview

Implement admin password authentication for the web UI, separate from proxy API keys.

## Implementation Steps

### Step 1: Backend - Admin Service

Create `internal/service/admin_service.go`:

```go
package service

import (
    "crypto/rand"
    "encoding/hex"
    "sync"
    "time"
)

type AdminService interface {
    ValidatePassword(password string) bool
    CreateSession() string
    ValidateSession(token string) bool
}

type adminSession struct {
    token     string
    expiresAt time.Time
}

type adminService struct {
    password string
    mu       sync.RWMutex
    sessions map[string]adminSession
}

func NewAdminService(password string) AdminService {
    return &adminService{
        password: password,
        sessions: make(map[string]adminSession),
    }
}

func (s *adminService) ValidatePassword(password string) bool {
    return s.password == password
}

func (s *adminService) CreateSession() string {
    b := make([]byte, 32)
    rand.Read(b)
    token := hex.EncodeToString(b)

    s.mu.Lock()
    s.sessions[token] = adminSession{
        token:     token,
        expiresAt: time.Now().Add(24 * time.Hour),
    }
    s.mu.Unlock()

    // Cleanup expired sessions periodically
    go s.cleanup()

    return token
}

func (s *adminService) ValidateSession(token string) bool {
    s.mu.RLock()
    sess, ok := s.sessions[token]
    s.mu.RUnlock()

    if !ok {
        return false
    }

    if time.Now().After(sess.expiresAt) {
        s.mu.Lock()
        delete(s.sessions, token)
        s.mu.Unlock()
        return false
    }

    return true
}

func (s *adminService) cleanup() {
    s.mu.Lock()
    defer s.mu.Unlock()

    now := time.Now()
    for token, sess := range s.sessions {
        if now.After(sess.expiresAt) {
            delete(s.sessions, token)
        }
    }
}
```

### Step 2: Backend - Admin Handler

Create `internal/api/handlers/admin_handler.go`:

```go
package handlers

import (
    "encoding/json"
    "net/http"

    "github.com/chris/llm-router/internal/service"
)

type AdminHandler struct {
    adminService service.AdminService
}

func NewAdminHandler(adminService service.AdminService) *AdminHandler {
    return &AdminHandler{adminService: adminService}
}

func (h *AdminHandler) Routes() *http.ServeMux {
    mux := http.NewServeMux()
    mux.HandleFunc("POST /api/v1/admin/auth", h.Auth)
    return mux
}

func (h *AdminHandler) Auth(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Password string `json:"password"`
    }

    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
        return
    }

    if !h.adminService.ValidatePassword(req.Password) {
        http.Error(w, `{"error":"invalid password"}`, http.StatusUnauthorized)
        return
    }

    token := h.adminService.CreateSession()

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{
        "token": token,
    })
}
```

### Step 3: Backend - Router Changes

Update `internal/api/router.go` to mount admin routes without auth middleware:

```go
func NewRouter(
    // ... existing params ...
    adminHandler *handlers.AdminHandler,
) chi.Router {
    r := chi.NewRouter()

    // ... existing setup ...

    // Admin auth endpoint (no auth required)
    r.Mount("/", adminHandler.Routes())

    r.Route("/api/v1", func(r chi.Router) {
        r.Use(middleware.AuthMiddleware(keyService, adminService))

        // ... existing routes ...
    })

    // ... rest of router ...
}
```

### Step 4: Backend - Middleware Changes

Update `internal/api/middleware/auth.go` to accept admin sessions:

```go
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
```

### Step 5: Backend - Main Changes

Update `cmd/llm-router/main.go`:

1. Add `--admin-password` flag
2. Add `LLM_ROUTER_ADMIN_PASSWORD` env var support
3. Generate password if not set
4. Create AdminService
5. Pass to router
6. Remove `createAdminKeyIfEmpty()`

### Step 6: Frontend - LoginPrompt Changes

Update `web/src/components/LoginPrompt.svelte`:

- Change label to "Admin Password"
- Change placeholder to "Enter password"
- Call `POST /api/v1/admin/auth` with password
- Store token in localStorage as `adminToken`

### Step 7: Frontend - App Changes

Update `web/src/App.svelte`:

- Use `adminToken` instead of `adminKey`
- Update `checkAuth` to use token
- Update `apiFetch` to use token

### Step 8: Frontend - Stores Changes

Update `web/src/lib/stores.js`:

- Rename `adminKey` to `adminToken`

## Testing

1. Start proxy without admin password flag - should generate and log one
2. Start proxy with `--admin-password test123`
3. Open web UI - should show "Admin Password" field
4. Enter wrong password - should show error
5. Enter correct password - should login and show virtual models
6. Test API key still works for proxy requests
7. Test session expiry (set short expiry for testing)

## Security Notes

- Password is stored in memory only (not in database)
- Session tokens are stored in memory (not persisted)
- Sessions expire after 24 hours
- Password should be at least 8 characters (validate on startup)
