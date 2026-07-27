# llm-router

OpenAI-compatible LLM proxy that routes requests to the best provider using metadata, not model names.

## What it does

llm-router sits between your AI tools and LLM providers (OpenAI, Anthropic, Ollama, etc.). Instead of hardcoding "gpt-4o goes to OpenAI", you tag models with metadata like `intelligence=85`, `cost=free`, `speed=high` — then create virtual models that pick the best match at runtime.

**Example:** A virtual model `smart-free` picks the cheapest model with intelligence ≥ 70. When a new model is tagged and uploaded, it's automatically included — no config changes needed.

## How it's different

| | OpenRouter / LiteLLM | llm-router |
|-|----------------------|------------|
| Routing | Static: model name → provider | Dynamic: filter metadata, sort by priority |
| Virtual models | No | Yes — `"smart-free"` = best match at runtime |
| Model tagging | No | Any key-value pairs: `intel=85`, `cost=free` |
| Failover | Basic retry | Silent: tries next model if one fails |
| Token savings | No | RTK compresses tool outputs, Caveman trims verbose responses |
| Circuit breaker | No | Auto-disables failing models/providers, re-enables after cooldown |
| Claude Code cloaking | No | Proxies Claude Code through non-Anthropic backends |
| Deploy | Docker / cloud | Single Go binary, SQLite database |

## Quick start

```bash
# Build
go build -o llm-router ./cmd/llm-router

# Set up a provider (e.g. Claude Code)
./llm-router setup --provider claude-code

# Start proxy
./llm-router proxy --port 8080

# Connect Claude Code
export ANTHROPIC_BASE_URL=http://localhost:8080/v1
claude
```

### Setup options

```bash
./llm-router setup --provider openai --key sk-...
./llm-router setup --provider anthropic --key sk-ant-...
./llm-router setup --provider ollama
./llm-router setup --provider cloudflare --account-id XXX --key XXX
./llm-router setup --provider claude-code --url https://your-custom-url  # OAuth
```

## Virtual models

Create virtual models via the API:

```bash
curl -X POST http://localhost:8080/api/v1/virtual-models \
  -H "Authorization: Bearer <admin-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "smart-free",
    "filter_expr": {
      "and": [
        {"key": "cost-type", "op": "eq", "value": "free"},
        {"key": "intel", "op": "gte", "value": "50"}
      ]
    },
    "sort_expr": [{"key": "intel", "direction": "desc"}]
  }'
```

Then use it like any model:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer <proxy-key>" \
  -H "Content-Type: application/json" \
  -d '{"model": "smart-free", "messages": [{"role": "user", "content": "Hello"}]}'
```

### Filter operators

| Op | Description | Example |
|----|-------------|---------|
| `eq` | Equals | `{"key":"cost-type","op":"eq","value":"free"}` |
| `neq` | Not equals | `{"key":"cost-type","op":"neq","value":"paid"}` |
| `gt` / `gte` | Greater than / or equal | `{"key":"intel","op":"gte","value":"80"}` |
| `lt` / `lte` | Less than / or equal | `{"key":"intel","op":"lt","value":"80"}` |
| `in` | In list | `{"key":"cost-type","op":"in","value":["free","sub"]}` |
| `contains` | String contains | `{"key":"name","op":"contains","value":"gpt"}` |

### Sort

```json
[
  {"key": "intel", "direction": "desc"},
  {"key": "cost-type", "order": ["subscription", "free", "api-creds"]}
]
```

## Tags & metadata

Tag models with any key-value pairs. Import from CSV benchmarks or set via API:

```bash
# CSV import
./llm-router import --file data.csv --mode merge

# API — set tags on a model
curl -X PUT http://localhost:8080/api/v1/models/:id/tags \
  -H "Authorization: Bearer <admin-key>" \
  -H "Content-Type: application/json" \
  -d '{"tags": [{"key": "intel", "value": "85"}, {"key": "cost-type", "value": "free"}]}'
```

## Architecture

```
Client (OpenAI SDK) → /v1/chat/completions → Routing Engine → Provider A
                       model="smart-free"       ↓ fail       → Provider B
                                                ↓ fail       → Provider C
```

Silent failover: if the best model fails, the next one in the sorted list is tried transparently. No client changes needed.

## Integrating tools

### Claude Code

```bash
# Option 1: API key
export ANTHROPIC_BASE_URL=http://localhost:8080/v1
export ANTHROPIC_API_KEY=lmr_...

# Option 2: OAuth (browser-based)
export ANTHROPIC_BASE_URL=http://localhost:8080/v1
claude  # opens browser for auth
```

### Cursor / Codex CLI

```bash
export OPENAI_BASE_URL=http://localhost:8080/v1
export OPENAI_API_KEY=lmr_...
```

## Multi-instance

Run proxy and admin separately:

```bash
# Terminal 1: Proxy (headless)
./llm-router proxy --port 8080 --no-tui

# Terminal 2: Admin dashboard (connects via HTTP)
./llm-router admin --connect http://localhost:8080 --key lmr_...
```

Multiple admin viewers can connect simultaneously.

## CLI

```
llm-router                    Start proxy (default)
llm-router proxy [flags]      Start proxy server
llm-router admin [flags]      Connect as admin viewer
llm-router setup [flags]      Initialize a provider
llm-router add-provider       Add custom provider
llm-router discover           Discover models from provider
llm-router tag                Set model metadata
llm-router import             Import CSV metadata
llm-router toggle-provider    Enable/disable provider
llm-router toggle-model       Enable/disable model
llm-router create-key         Create proxy API key
```

### Key flags

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--port` | `LLM_ROUTER_PORT` | `8080` | HTTP port |
| `--db` | `LLM_ROUTER_DB` | `~/.local/share/llm-router/llm-router.db` | SQLite path |
| `--encryption-key` | `LLM_ROUTER_ENCRYPTION_KEY` | auto-generated | AES key |
| `--no-tui` | | `false` | Disable terminal UI |

## API endpoints

### Management (admin key)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/providers` | Create provider |
| `GET` | `/api/v1/providers` | List providers |
| `POST` | `/api/v1/providers/:id/discover` | Discover models |
| `PUT` | `/api/v1/models/:id/tags` | Set model tags |
| `POST` | `/api/v1/virtual-models` | Create virtual model |
| `GET` | `/api/v1/virtual-models` | List virtual models |
| `GET` | `/api/v1/virtual-models/:id/resolved` | Show resolved models |
| `POST` | `/api/v1/keys` | Create proxy key |
| `POST` | `/api/v1/import/csv` | Import CSV metadata |
| `GET` | `/api/v1/stats` | Aggregated statistics |
| `GET` | `/api/v1/stats/logs` | Recent request logs |
| `GET` | `/api/v1/stats/logs/stream` | SSE live log stream |

### Proxy (proxy key)

| Method | Path | Format | Description |
|--------|------|--------|-------------|
| `POST` | `/v1/chat/completions` | OpenAI | Chat completion (streaming) |
| `POST` | `/v1/messages` | Anthropic | Messages (non-streaming) |
| `POST` | `/v1/messages/stream` | Anthropic | Messages (streaming) |
| `GET` | `/v1/models` | OpenAI | List virtual models |

## Provider types

- `openai` — OpenAI-compatible APIs (OpenAI, Groq, Together, vLLM, etc.)
- `anthropic` — Anthropic API (auto-translated to/from OpenAI format)
- `cloudflare` — Cloudflare Workers AI
- `ollama` — Local Ollama
- `ollama-cloud` — Ollama Cloud (ollama.com)

## Tech stack

- **Backend:** Go, chi router, SQLite (pure Go via modernc.org/sqlite)
- **Frontend:** Svelte 5, Tailwind CSS 4, Vite (embedded in Go binary)
- **TUI:** Bubble Tea + Lip Gloss
- **Encryption:** AES-256 at rest
