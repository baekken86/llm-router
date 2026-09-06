# ChatGPT Subscription Provider (Codex OAuth) — Design

**Date:** 2026-09-05 · **Spec:** specs/007-chatgpt-subscription-provider-(codex-oauth) · **Status:** Draft

## 1. Goal

Let llm-router use a **ChatGPT Plus/Pro subscription** (OAuth login, no API key) as a provider, the same way it already supports Claude subscription auth via `claude-code`. Clients keep speaking OpenAI `chat/completions`; llm-router translates to the ChatGPT backend's **Responses API** and back.

Reference implementation studied: `decolua/9router` (files `open-sse/providers/registry/codex.js`, `open-sse/executors/codex.js`, `open-sse/services/tokenRefresh/providers.js`). The flow originates from the official Codex CLI (`codex_cli_rs`).

## 2. Non-Goals (v1)

- No usage/quota dashboard (`backend-api/wham/usage`) — later spec.
- No review/spark/image model families, no `@compact` RTK support.
- No replay of encrypted reasoning items across requests (stateless history translation only).
- No web-UI changes (provider is created via CLI, visible in UI read-only).

## 3. How 9router does it (reference constants)

### 3.1 OAuth (PKCE)

| Item | Value |
|---|---|
| client_id | `app_EMoamEEZ73f0CkXaXp7hrann` |
| authorize URL | `https://auth.openai.com/oauth/authorize` |
| token URL | `https://auth.openai.com/oauth/token` |
| scope | `openid profile email offline_access` |
| redirect URI | `http://localhost:1455/auth/callback` (fixed port 1455, loopback only) |
| extra authorize params | `id_token_add_organizations=true`, `codex_cli_simplified_flow=true`, `originator=codex_cli_rs` |
| PKCE | verifier = 32 random bytes base64url; challenge = S256(verifier); state = 32 random bytes base64url |
| token exchange | form-encoded: `grant_type=authorization_code&client_id=…&code=…&redirect_uri=…&code_verifier=…` |
| refresh | JSON body: `{"client_id":"…","grant_type":"refresh_token","refresh_token":"…"}` (no secret) |

JWT: `id_token` payload carries a custom claim namespace `https://api.openai.com/auth` with `chatgpt_account_id` and `chatgpt_plan_type`.

Refresh policy in 9router: refresh when `< 5 days` to expiry **or** last refresh `> 8 days` ago (stays inside ChatGPT's refresh-token reuse/rotation window). Unrecoverable refresh errors → re-login: `invalid_grant`, `refresh_token_reused`, `refresh_token_expired`, `refresh_token_invalidated`.

### 3.2 Chat endpoint

```
POST https://chatgpt.com/backend-api/codex/responses
Authorization: Bearer <OAuth access_token>
Originator: codex_cli_rs
User-Agent: codex_cli_rs/0.136.0
ChatGPT-Account-ID: <chatgpt_account_id>   (when known)
session_id: <stable per-conversation id>
Accept: text/event-stream
```

Body is **Responses API** shape, forced: `stream:true`, `store:false`, `instructions` (Codex default instructions when client sends none), `prompt_cache_key` = conversation-stable id, `reasoning:{effort, summary:"auto"}`, `include:["reasoning.encrypted_content"]` (when effort ≠ none). `service_tier:"fast"` → `"priority"`.

**Deleted** from translated requests: `temperature`, `top_p`, `frequency_penalty`, `presence_penalty`, `logprobs`, `top_logprobs`, `n`, `seed`, `max_tokens`, `max_completion_tokens`, `max_output_tokens`, `user`, `metadata`, `stream_options`, `safety_identifier`, `reasoning_effort`.

`input` item types: `{type:"message", role, content:[{type:"input_text"|"input_image",…}]}`; `function_call`/`function_call_output` for tool history; system messages become `role:"developer"`. Tools are flattened to `{type:"function", name, description, parameters}`.

SSE events consumed: `response.output_text.delta`, `response.function_call_arguments.delta`, `response.reasoning_summary_text.delta`, `response.completed`, `error`. `response.completed` carries final usage (`input_tokens`, `output_tokens`, `output_tokens_details.reasoning_tokens`).

Error shapes: 429 `{"error":{"type":"usage_limit_reached","resets_at":…}}`; `"Selected model is at capacity…"` (treat as 503-class, retryable); `server_is_overloaded` / `service_unavailable_error` (retry inside 200-OK stream body).

### 3.3 Models

`gpt-5.1`, `gpt-5.1-codex`, `gpt-5.1-codex-max`, `gpt-5.1-codex-mini`, `gpt-5`, `gpt-5-codex`, `gpt-5-codex-mini` (list rotates; keep it a single editable slice). `-high/-xhigh/…` suffixes and `-max` (→ `xhigh`) map to `reasoning.effort`; default effort `medium`. Thinking cannot be fully disabled for codex models.

⚠️ **Fragility:** the client_id and endpoints are undocumented; OpenAI can rotate/revoke them anytime. Mitigation: all constants in one file, clear error surfaced when login/refresh fails.

### 3.4 Model discovery (possible — unlike claude-code)

The official Codex CLI fetches a **live model catalog** for ChatGPT-OAuth sessions (verified in `openai/codex` `codex-rs`: `models_endpoint.rs` → `GET {CHATGPT_CODEX_BASE_URL}/models?client_version=<ver>`, ETag cache, 5 s timeout; telemetry fixture confirms the literal URL `https://chatgpt.com/backend-api/codex/models`):

```
GET https://chatgpt.com/backend-api/codex/models?client_version=0.136.0
Authorization: Bearer <access_token>
ChatGPT-Account-ID: <account_id>        (when known)
Originator: codex_cli_rs
User-Agent: codex_cli_rs/0.136.0
```

Response shape (`ModelsResponse`, top-level `models`, **not** `/v1/models` format):

```json
{"models": [{
  "slug": "gpt-5.1-codex", "display_name": "…", "description": "…",
  "default_reasoning_level": "medium",
  "supported_reasoning_levels": [{"effort":"low","description":"low"}, …],
  "visibility": "list", "supported_in_api": true, "priority": 1,
  "context_window": 272000, "minimal_client_version": [0,99,0], "upgrade": null
}]}
```

Hard rules learned from the ecosystem:
- **Never** call `api.openai.com/v1/models` with an OAuth token — rejected (billing mismatch).
- `chatgpt.com/backend-api/models` is unreliable with OAuth bearer tokens — do not use.
- `wham/usage` is quota only, no models.

## 4. Design in llm-router

### 4.1 New API type `codex`

- `internal/models/provider.go`: `APITypeCodex APIType = "codex"`.
- Migration `028_add_codex_api_type.sql` using the established recreate pattern (`018`/`019`/`025`): SQLite cannot alter a CHECK constraint, so recreate `providers` with `api_type IN ('openai','anthropic','cloudflare','ollama','ollama-cloud','codex')` + copy data. Down restores the 5-type list.
- Migration `029_add_oauth_token_refresh_tracking.sql`: `ALTER TABLE oauth_tokens ADD COLUMN last_refresh_at DATETIME` (proactive 8-day rotation) and `ADD COLUMN id_token TEXT` (kept for account extraction + future claims).
- Registration points (full list from repo recon): `provider_handler.go:51`, `addprovider.go`, `main.go` usage text, `model_service.go` discovery, `engine.go` dispatch, `setup.go` supportedProviders, tests + `migrations_test.go` CHECK assertions, inline DDL in `provider_repo_test.go` / `model_mapping_repo_test.go`.

### 4.2 OAuth client — `internal/auth/codex_oauth.go`

Sibling of `anthropic_oauth.go`, same interface shape:

```go
type CodexOAuth struct{ clientID, redirectURI, codeVerifier string }
func (o *CodexOAuth) GetAuthorizationURL(state string) string
func (o *CodexOAuth) ExchangeCode(ctx, code) (*CodexTokens, error)      // form-encoded POST
func (o *CodexOAuth) RefreshToken(ctx, refreshToken) (*CodexTokens, error) // JSON POST
func ParseIDToken(idToken string) (accountID, planType, email string, err error) // JWT payload, claims["https://api.openai.com/auth"]
```

Constants block mirrors §3.1 verbatim. `ParseIDToken` is base64-decode + `encoding/json`, **no signature verification** (same trust model as reading Codex CLI's `auth.json`).

### 4.3 OAuth service — parameterize `OAuthService`

Today `internal/service/oauth_service.go:47` is hardwired to `NewAnthropicOAuth`. Change to a small strategy:

```go
type oauthClient interface {
    GetAuthorizationURL(state string) string
    ExchangeCode(ctx, code string) (*models.OAuthToken, error)
    RefreshToken(ctx, refresh string) (*models.OAuthToken, error)
}
// NewOAuthService(provider) returns anthropic or codex strategy by provider.APIType/name
```

- `StartAuthFlowWithCallback` reused for codex with redirect port **1455** and callback path `/auth/callback` (add configurable port/path; current code has `StartCallbackServerOnAddr` already).
- `GetValidToken` gains codex-specific policy: refresh when `expiresAt - now < 5d` **or** `lastRefreshAt > 8d`; stamp `lastRefreshAt` + upsert rotated `refresh_token` after each refresh. `invalid_grant`-family errors → mark token invalid, return "re-run `llm-router connect --provider chatgpt`" error.
- `chatgpt_account_id` → existing `oauth_tokens.account_id` column (no schema change); surfaced as the `ChatGPT-Account-ID` header. `plan_type` is logged at connect time only.

### 4.4 CLI — `llm-router connect --provider chatgpt`

- Extend existing `connect` command (currently claude-code-specific) to accept `--provider chatgpt`.
- Flow mirrors `connect.go runManual` + `OAuthService.StartAuthFlowWithCallback`:
  1. Create provider row: `{name:"chatgpt", api_type:"codex", base_url:"https://chatgpt.com/backend-api/codex", api_key:""}` (empty key, like claude-code).
  2. Generate PKCE + state, print authorize URL, **and** try to bind 127.0.0.1:1455 (5-min timeout) to auto-catch the callback; fallback = paste the callback URL (`extractCodeAndState`).
  3. Exchange code, `ParseIDToken`, upsert `oauth_tokens` (+ last_refresh_at).
  4. Predefine models (static list, §3.3) — no discovery call (ChatGPT backend has no models endpoint).
- `setup --provider chatgpt` routes to the same flow (no `--key` required). Provider handler validation exempts `codex` from the api_key requirement (like ollama types).

### 4.5 Codex client — `internal/proxy/codex_client.go` + `codex_translator.go`

Follows the `anthropic_client.go` + `translator.go` pattern:

```go
func (c *CodexClient) ChatCompletion(ctx, baseURL, accessToken, accountID, sessionID string, req ChatCompletionRequest) (*ChatCompletionResponse, error)
func (c *CodexClient) ChatCompletionStream(...) (io.ReadCloser, *http.Response, error)
```

- `codex_translator.go`:
  - `ChatToResponses(req, defaultInstructions) → ResponsesBody` (§3.2 rules: drop list, system→developer, tool flattening, `store:false`, `stream:true`, `prompt_cache_key` = sessionID, reasoning mapping incl. effort-suffix models, `include`).
  - `ResponsesChunkToChatSSE(event) → ChatCompletionStreamChunk` (`output_text.delta` → `delta.content`; `function_call_arguments.delta` → tool-call deltas; `reasoning_summary_text.delta` → same reasoning field convention used by the anthropic translator; `response.completed` → finish_reason + usage).
  - Non-streaming: still `stream:true` upstream, aggregate deltas into one `ChatCompletionResponse` (the backend always streams).
  - Codex default instructions constant (short "You are Codex…" preamble) — stored as a Go const, only injected when the request has no system message.
- Headers per §3.2 (`session_id` = `sessionIDForRequest(r)` — the same X-Request-ID-derived id used for opencode; keeps `prompt_cache_key` stable per conversation).
- Errors: map HTTP/stream errors through `newProviderError` so `processProviderError` (engine.go:933) gets 402 → `SubscriptionRequiredError`, 429 + `resets_at` → rate-limit cooldown with RetryAfter, 5xx/capacity → circuit-breaker 5xx. `usage_limit_reached` maps `resets_at`/`resets_in_seconds` to `RetryAfter`.
- On 401: attempt one synchronous `RefreshToken` then retry once; on unrecoverable refresh error, return a provider error telling the user to reconnect (model will not be disabled — this is auth, not quota).

### 4.6 Engine integration

- `NewEngine`: add `codexClient *CodexClient`.
- `sendRequest` (engine.go ~640) / `sendStreamRequest` (~720): explicit else-if branch for `APITypeCodex` (documented per-provider-hook pattern) — resolve token via `getAPIKey` (OAuth-first path already works: an `oauth_tokens` row for the provider wins; empty API key allowed).
- Stream branch at engine.go ~557: `case APITypeCodex: usage = e.streamCodexToOpenAI(...)` (parses translated SSE like the anthropic path does, then re-serializes OpenAI chunks to the client — reusing the anthropic stream plumbing pattern).
- baseURL convention: provider `base_url = https://chatgpt.com/backend-api/codex`, client appends `/responses` (mockable for tests).

### 4.7 Model discovery — live (this is the differentiator vs claude-code)

Unlike claude-code, codex has an official upstream catalog endpoint (§3.4). `model_service.fetchModels` gets a codex branch:

1. `GET {base_url}/models?client_version=0.136.0` with `Authorization: Bearer <token>` (OAuth-first via `getAPIKey`-equivalent path), `Originator`, `User-Agent`, `ChatGPT-Account-ID` when known. 5 s timeout.
2. On 401: one synchronous token refresh → retry once (same pattern as the chat client).
3. Parse `models[]`: keep entries with `visibility == "list"` && `supported_in_api != false`; map `slug → model name`, `display_name → display`.
4. **Metadata bonus:** import `context_window` as the `context-window` tag and `default_reasoning_level`/`supported_reasoning_levels` as metadata — this feeds llm-router's metadata-driven virtual models for free.
5. Fallback: on any failure (non-200, parse error, refresh exhausted) return the static seed list (`var codexModelFallback = []string{…}` shared with the CLI's predefined models) and log a warning — discovery must never be worse than the claude-code status quo.
6. No ETag cache in v1: discovery is user-triggered (`POST /api/v1/providers/:id/discover`), rare, and cheap.

## 5. Test Plan

- `codex_translator_test.go`: chat→responses (param stripping, system→developer, tools, reasoning suffix), responses→chat SSE golden tests, non-stream aggregation.
- `codex_oauth_test.go`: auth URL literal comparison, PKCE shape, token exchange/refresh request bodies, JWT claim parsing (sample id_token fixture).
- `codex_client_test.go` (httptest): headers, stream parsing, 401→refresh→retry, 402/429/5xx mapping to ProviderError/SubscriptionRequiredError with RetryAfter.
- `model_service` discovery tests (httptest): catalog parsing (`visibility`/`supported_in_api` filter, `slug` mapping, `context_window` tag import), 401→refresh→retry-once, fallback to static seed on non-200/parse failure; assertion that `api.openai.com` is never called.
- Engine tests: codex dispatch stream + non-stream (pattern of `engine_test.go`).
- Migration tests: 028/029 up+down, CHECK contents, `migrations_test.go` insert fixtures updated.

## 6. Risks

| Risk | Mitigation |
|---|---|
| client_id/endpoint revoked by OpenAI | constants isolated in `codex_oauth.go`; clear reconnect errors |
| refresh token rotation loss on crash between refresh & upsert | upsert immediately after successful refresh; single-flight refresh (existing dedup pattern) |
| effort/thinking param drift as models change | model list + suffix map in one file, table-tested |
| rate-limit semantics differ from API 429s | map `usage_limit_reached` + `resets_at` into existing cooldown machinery |
