# Admin Password Authentication

## Overview

The web UI should authenticate using an admin password instead of API keys. This separates web UI authentication from proxy authentication, making the system clearer and more secure.

## Current State

- Web UI asks for "Admin API Key" in LoginPrompt.svelte
- API keys are stored in the database via `proxy_keys` table
- `createAdminKeyIfEmpty()` creates an admin key if no keys exist
- Proxy requests authenticate via Bearer token (API key or OAuth)
- The same API keys are used for both web UI and proxy access

## Goal

- Web UI authenticates with a password (separate from API keys)
- Password is configured via CLI flag or environment variable
- API keys remain for proxy authentication only

## Design

### 1. Configuration

Add two new options for the admin password:

- `--admin-password` CLI flag
- `LLM_ROUTER_ADMIN_PASSWORD` environment variable

If neither is set, generate a random password on first start and log it (similar to current encryption key behavior).

### 2. New API Endpoint

```
POST /api/v1/admin/auth
Content-Type: application/json

Request:  {"password": "the-admin-password"}
Response: {"token": "<session-token>"}
```

The session token is a JWT-like opaque string that expires after 24 hours. The token grants access to admin endpoints (virtual models, keys, etc.).

### 3. Authentication Middleware

Update `middlewareAuthOrOAuth` to accept:
1. Valid API key (for proxy requests)
2. Valid admin session token (for web UI)
3. Valid OAuth token

### 4. Web UI Changes

**LoginPrompt.svelte:**
- Label changes from "Admin API Key" to "Admin Password"
- Placeholder changes from "lmr_..." to "Enter password"
- Calls `POST /api/v1/admin/auth` with password
- Stores returned token in localStorage as `adminToken`

**App.svelte:**
- Uses `adminToken` instead of `adminKey`
- Sends token as Bearer token in requests

### 5. Admin Key Lifecycle

- Remove `createAdminKeyIfEmpty()` - no longer needed
- Existing API keys remain for proxy use
- `create-key` command still works for creating proxy API keys

## Files to Modify

1. `cmd/llm-router/main.go` - Add `--admin-password` flag, remove `createAdminKeyIfEmpty`
2. `internal/api/handlers/admin_handler.go` - New file for auth endpoint
3. `internal/api/router.go` - Mount admin routes
4. `internal/service/admin_service.go` - New file for password validation and token generation
5. `web/src/components/LoginPrompt.svelte` - Update UI
6. `web/src/App.svelte` - Use adminToken
7. `web/src/lib/stores.js` - Rename adminKey to adminToken

## Security Considerations

- Password is never sent to the frontend after authentication
- Session tokens expire after 24 hours
- Tokens are stored in localStorage (acceptable for local admin UI)
- Password should be at least 8 characters

## Migration

- Existing API keys continue to work for proxy
- No database migration needed
- Users need to set admin password via flag/env on next deploy
