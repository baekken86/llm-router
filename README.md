# llm-router

OpenAI-compatible LLM proxy that routes requests to the best provider using metadata, not model names.

## 🔍 What it does

llm-router sits between your AI tools and LLM providers (OpenAI, Anthropic, Ollama, etc.). Like other proxies, it lets you group models into virtual models and use them as fallbacks. The difference: llm-router **enriches models with metadata** (intelligence, hallucination rate, cost, speed) — imported from benchmarks or set manually — and virtual models **automatically pick the best match** based on rules you define.

**Example:** A virtual model `smart-free` picks the cheapest model with intelligence ≥ 70 and hallucination ≤ 15. When a new model is tagged and added, it's automatically included — no manual curation needed.

## ⚡ How it's different

Most LLM proxies let you create virtual models, but you have to curate them by hand — pick specific models, rearrange them when priorities change, add new ones manually.

llm-router automates this. You tag models with metadata (from CSV benchmarks or manually), then define rules. The routing engine picks the best model at request time.

| | Other proxies (OpenRouter, LiteLLM) | llm-router |
|-|--------------------------------------|------------|
| Virtual models | Yes, but manually curated | Yes, auto-resolved by metadata rules |
| Model selection | You pick and order models yourself | Filter by metadata (`intel >= 70`, `hallucination <= 15`, `cost <= 0.50`) |
| Adding new models | Update virtual model config manually | Tag it — rules pick it up automatically |
| Metadata | No | Import from benchmarks or set manually |
| Failover | Basic retry | Silent failover through sorted model list |
| Token savings | No | RTK compresses tool outputs, Caveman trims verbose responses |
| Circuit breaker | No | Auto-disables failing models/providers, re-enables after cooldown |
| Claude Code cloaking | No | Proxies Claude Code through non-Anthropic backends |
| Deploy | Docker / cloud | Single Go binary, SQLite database |

## 🚀 Quick start

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

### 📦 Setup options

```bash
./llm-router setup --provider openai --key sk-...
./llm-router setup --provider anthropic --key sk-ant-...
./llm-router setup --provider ollama
./llm-router setup --provider cloudflare --account-id XXX --key XXX
./llm-router setup --provider claude-code --url https://your-custom-url  # OAuth
```

## 🎯 Virtual models

Create virtual models via the API. Define rules that filter and sort models by metadata:

```bash
curl -X POST http://localhost:8080/api/v1/virtual-models \
  -H "Authorization: Bearer <admin-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "smart-cheap",
    "filter_expr": {
      "and": [
        {"key": "intelligence", "op": "gte", "value": "70"},
        {"key": "hallucination", "op": "lte", "value": "15"},
        {"key": "cost_per_task", "op": "lte", "value": "0.50"}
      ]
    },
    "sort_expr": [{"key": "intelligence", "direction": "desc"}]
  }'
```

Then use it like any model:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer <proxy-key>" \
  -H "Content-Type: application/json" \
  -d '{"model": "smart-free", "messages": [{"role": "user", "content": "Hello"}]}'
```

### 🔧 Filter operators

| Op | Description | Example |
|----|-------------|---------|
| `eq` | Equals | `{"key":"cost-type","op":"eq","value":"free"}` |
| `neq` | Not equals | `{"key":"cost-type","op":"neq","value":"paid"}` |
| `gt` / `gte` | Greater than / or equal | `{"key":"intel","op":"gte","value":"80"}` |
| `lt` / `lte` | Less than / or equal | `{"key":"intel","op":"lt","value":"80"}` |
| `in` | In list | `{"key":"cost-type","op":"in","value":["free","sub"]}` |
| `contains` | String contains | `{"key":"name","op":"contains","value":"gpt"}` |

### 📊 Sort

```json
[
  {"key": "intel", "direction": "desc"},
  {"key": "cost-type", "order": ["subscription", "free", "api-creds"]}
]
```

## 🏷️ Tags & metadata

Tag models with any key-value pairs — intelligence scores, hallucination rates, cost per task, speed ratings, or whatever matters for your use case. Import from CSV benchmarks or set via API:

```bash
# CSV import
./llm-router import --file data.csv --mode merge

# API — set tags on a model
curl -X PUT http://localhost:8080/api/v1/models/:id/tags \
  -H "Authorization: Bearer <admin-key>" \
  -H "Content-Type: application/json" \
  -d '{"tags": [{"key": "intelligence", "value": "85"}, {"key": "hallucination", "value": "12"}, {"key": "cost_per_task", "value": "0.30"}]}'
```

## 🏗️ Architecture

```
Client (OpenAI SDK) → /v1/chat/completions → Routing Engine → Provider A
                       model="smart-cheap"    ↓ fail       → Provider B
                                              ↓ fail       → Provider C
```

Silent failover: if the best model fails, the next one in the sorted list is tried transparently. No client changes needed.

## 🔌 Integrating tools

### 🤖 Claude Code

```bash
# Option 1: API key
export ANTHROPIC_BASE_URL=http://localhost:8080/v1
export ANTHROPIC_API_KEY=lmr_...

# Option 2: OAuth (browser-based)
export ANTHROPIC_BASE_URL=http://localhost:8080/v1
claude  # opens browser for auth
```

### ✏️ Cursor / Codex CLI

```bash
export OPENAI_BASE_URL=http://localhost:8080/v1
export OPENAI_API_KEY=lmr_...
```

## 🖥️ Multi-instance

Run proxy and admin separately:

```bash
# Terminal 1: Proxy (headless)
./llm-router proxy --port 8080 --no-tui

# Terminal 2: Admin dashboard (connects via HTTP)
./llm-router admin --connect http://localhost:8080 --key lmr_...
```

Multiple admin viewers can connect simultaneously.

## ⌨️ CLI

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

### 🚩 Key flags

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--port` | `LLM_ROUTER_PORT` | `8080` | HTTP port |
| `--db` | `LLM_ROUTER_DB` | `~/.local/share/llm-router/llm-router.db` | SQLite path |
| `--encryption-key` | `LLM_ROUTER_ENCRYPTION_KEY` | auto-generated | AES key |
| `--no-tui` | | `false` | Disable terminal UI |

## 📡 API endpoints

### 🔐 Management (admin key)

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

### 🔑 Proxy (proxy key)

| Method | Path | Format | Description |
|--------|------|--------|-------------|
| `POST` | `/v1/chat/completions` | OpenAI | Chat completion (streaming) |
| `POST` | `/v1/messages` | Anthropic | Messages (non-streaming) |
| `POST` | `/v1/messages/stream` | Anthropic | Messages (streaming) |
| `GET` | `/v1/models` | OpenAI | List virtual models |

## 🌐 Provider types

- `openai` — OpenAI-compatible APIs (OpenAI, Groq, Together, vLLM, etc.)
- `anthropic` — Anthropic API (auto-translated to/from OpenAI format)
- `cloudflare` — Cloudflare Workers AI
- `ollama` — Local Ollama
- `ollama-cloud` — Ollama Cloud (ollama.com)
- `codex` — ChatGPT Plus/Pro subscription (OAuth login, Codex Responses API, auto-translated to/from OpenAI format)

## 🛠️ Tech stack

- **Backend:** Go, chi router, SQLite (pure Go via modernc.org/sqlite)
- **Frontend:** Svelte 5, Tailwind CSS 4, Vite (embedded in Go binary)
- **TUI:** Bubble Tea + Lip Gloss
- **Encryption:** AES-256 at rest
