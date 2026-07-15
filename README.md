# llm-router

OpenAI-compatible LLM proxy with dynamic virtual models, provider failover, and metadata-based routing.

## Why llm-router?

Most LLM proxies (OpenRouter, LiteLLM, etc.) route by model name. You configure `gpt-4o` → OpenAI, `claude-3` → Anthropic. Static.

llm-router is different: **models are tagged with metadata, and virtual models select dynamically based on filters and sorting.**

| Feature | OpenRouter / LiteLLM | llm-router |
|---------|---------------------|------------|
| Routing | Static model → provider mapping | Dynamic: filter by metadata, sort by priority |
| Virtual models | No | Yes: `"smart-free"` = best free model with intel≥50 |
| Model tagging | No | Arbitrary key-value: `intel=85`, `hallucination=12`, `cost-type=free` |
| Metadata import | No | CSV import from benchmarks (intelligence, speed, cost) |
| Failover | Basic retry | Silent failover through sorted model list |
| Token tracking | Basic | Cached tokens, reasoning tokens, headroom, cost estimation |
| UI | Web dashboard | Built-in TUI with live log + stats |

**Example:** Create a virtual model `smart-cheap` that always picks the cheapest model with intelligence≥70. No code changes needed when new models are added — just tag them.

```
Filter: {"and":[{"key":"intel","op":"gte","value":"70"},{"key":"cost-type","op":"eq","value":"free"}]}
Sort:   [{"key":"intel","direction":"desc"}]
```

## Quick Start

```bash
go build -o llm-router ./cmd/llm-router

# Start proxy with TUI (single-instance mode)
./llm-router proxy --port 8080 --db ./data/router.db

# Or start proxy without TUI (daemon mode)
./llm-router proxy --port 8080 --db ./data/router.db --no-tui

# In another terminal: admin dashboard
./llm-router admin --connect http://localhost:8080 --key <admin-key>
```

## Multi-Instance

Run proxy and admin dashboard separately:

```bash
# Terminal 1: Proxy (no TUI, just HTTP server)
./llm-router proxy --port 8080 --db ./data/router.db --no-tui

# Terminal 2: Admin dashboard (connects to proxy via HTTP)
./llm-router admin --connect http://localhost:8080 --key lmr_...

# Terminal 3: Another admin (multiple viewers supported)
./llm-router admin --connect http://localhost:8080 --key lmr_...
```

Admin connects via HTTP — no direct DB access needed. Can run on different machines.

## CLI Commands

```
llm-router [flags]              Start proxy (default, same as 'proxy')
llm-router proxy [flags]        Start proxy server
llm-router admin [flags]        Connect to running proxy as admin viewer
llm-router import [flags]       Import CSV metadata
llm-router help                 Show help
```

### Proxy flags

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--port` | `LLM_ROUTER_PORT` | `8080` | HTTP port |
| `--db` | `LLM_ROUTER_DB` | `./data/llm-router.db` | SQLite path |
| `--encryption-key` | `LLM_ROUTER_ENCRYPTION_KEY` | auto-generated | 32-byte hex key |
| `--no-tui` | | `false` | Disable terminal UI |

### Admin flags

| Flag | Default | Description |
|------|---------|-------------|
| `--connect` | `http://localhost:8080` | Proxy URL |
| `--key` | | Proxy API key (required) |

### Import flags

| Flag | Default | Description |
|------|---------|-------------|
| `--file` | | CSV file path (required) |
| `--mode` | `merge` | `merge` or `replace` |
| `--db` | `./data/llm-router.db` | SQLite path |

# Create virtual model
curl -X POST http://localhost:8080/api/v1/virtual-models \
  -H "Authorization: Bearer <admin-key>" \
  -H "Content-Type: application/json" \
  -d '{"name":"smart-free","filter_expr":{"and":[{"key":"cost-type","op":"eq","value":"free"},{"key":"intel","op":"gte","value":"50"}]},"sort_expr":[{"key":"intel","direction":"desc"}]}'

# Use it
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer <proxy-key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"smart-free","messages":[{"role":"user","content":"Hello"}]}'
```

## Architecture

```
Client (OpenAI SDK) → /v1/chat/completions → Routing Engine → Provider A (OpenAI)
                       model="smart-free"       ↓ fail       → Provider B (Anthropic)
                                                ↓ retry      → Provider C (OpenAI)
```

Silent failover: if the best model fails, the next one in the sorted list is tried transparently.

## Metadata Filter Operators

| Op | Example | Description |
|----|---------|-------------|
| `eq` | `{"key":"cost-type","op":"eq","value":"free"}` | Equals |
| `neq` | `{"key":"cost-type","op":"neq","value":"free"}` | Not equals |
| `gt` | `{"key":"intel","op":"gt","value":"80"}` | Greater than |
| `gte` | `{"key":"intel","op":"gte","value":"80"}` | Greater or equal |
| `lt` | `{"key":"intel","op":"lt","value":"80"}` | Less than |
| `lte` | `{"key":"intel","op":"lte","value":"80"}` | Less or equal |
| `in` | `{"key":"cost-type","op":"in","value":["free","sub"]}` | In list |
| `contains` | `{"key":"name","op":"contains","value":"gpt"}` | String contains |

## Sort

```json
[
  {"key": "intel", "direction": "desc"},
  {"key": "cost-type", "order": ["subscription", "free", "api-creds"]}
]
```

`direction`: numeric/string sort. `order`: explicit categorical priority.

## CSV Import

```csv
model_name,key,value
gpt-4o,intelligence,85.2
gpt-4o,speed,78.5
gpt-4o,cost_per_1m_input,2.50
gpt-4o,hallucination,12
claude-3.5-sonnet,intelligence,88.1
```

```bash
# CLI import
./llm-router import --file data.csv --mode merge

# API import
curl -X POST "http://localhost:8080/api/v1/import/csv?mode=merge" \
  -H "Authorization: Bearer <admin-key>" \
  -H "Content-Type: text/csv" \
  --data-binary @data.csv
```

## API Endpoints

### Management (Bearer: admin key)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/providers` | Create provider |
| `GET` | `/api/v1/providers` | List providers |
| `POST` | `/api/v1/providers/:id/discover` | Discover models |
| `PUT` | `/api/v1/models/:id/tags` | Set model tags |
| `POST` | `/api/v1/virtual-models` | Create virtual model |
| `GET` | `/api/v1/virtual-models` | List virtual models |
| `POST` | `/api/v1/keys` | Create proxy key |
| `POST` | `/api/v1/import/csv` | Import CSV metadata |
| `GET` | `/api/v1/stats` | Get aggregated statistics |
| `GET` | `/api/v1/stats/logs` | Get recent request logs |
| `GET` | `/api/v1/stats/logs/stream` | SSE stream of live logs |

### Proxy (Bearer: proxy key)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/chat/completions` | Chat completion (streaming supported) |
| `GET` | `/v1/models` | List virtual models |

## Provider Types

- `openai`: OpenAI-compatible APIs (OpenAI, Groq, Together, vLLM, etc.)
- `anthropic`: Anthropic API (Claude models). Request/response auto-translated.

## Project Structure

```
cmd/llm-router/         # Entry point
internal/
  api/                   # HTTP handlers, middleware, router
  config/                # Configuration
  db/                    # SQLite connection, migrations
  models/                # Domain structs
  repository/            # Data access layer
  service/               # Business logic
  proxy/                 # Routing engine, provider clients, format translation
  tui/                   # Terminal UI (bubbletea)
specs/                   # Design specs (spec-query)
```

## Development

```bash
go build ./...          # Build
go test ./...           # Test
go vet ./...            # Lint
```
