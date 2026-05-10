# mini-llm-gateway

[![test](https://github.com/hsuanchenlin/mini-llm-gateway/actions/workflows/test.yml/badge.svg)](https://github.com/hsuanchenlin/mini-llm-gateway/actions/workflows/test.yml)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)

A lightweight, OpenAI-compatible LLM gateway in Go. Routes chat completion
requests to pluggable backends (Ollama, OpenAI-compatible APIs, or a built-in
`fake` provider) and ships with a small chat UI plus a SQLite-backed request
log.

This is a portfolio project. Goal: demonstrate backend engineering, LLM
application infrastructure, streaming APIs, observability, and cloud
deployment — *not* to reimplement transformer internals.

**Status:** M1 skeleton ✅ · M2 real providers ✅ · M3 SQLite logging + admin ✅ · M4 SSE streaming ✅ · M5 web UI ✅ · M6 Docker + docs ✅ · M7 RAG ✅. See [ROADMAP.md](./ROADMAP.md) for the full milestone breakdown.

---

## Quick start (Docker, recommended)

One command brings up the gateway, a local Ollama, and pulls a small model:

```sh
docker compose up --build
# wait for ollama-init to finish pulling llama3.2:1b (~1.3 GB, one time)
open http://localhost:8090/
```

Then chat in the browser, or curl:

```sh
curl -N http://localhost:8090/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{"stream":true,"messages":[{"role":"user","content":"hello"}]}'
```

State persists across restarts (Ollama models in the `ollama-data` volume, request log in `gateway-data`).

## Quick start (no Docker)

```sh
go run ./cmd/server
```

Listens on `:8090`, uses the built-in `fake` provider (deterministic echo), no setup. Open <http://localhost:8090/>.

## Run against a real LLM (without Docker)

### Local Ollama

```sh
brew install ollama
ollama serve &
ollama pull llama3.2:1b

./run-ollama.sh   # or set the env vars yourself; see .env.example
```

### OpenAI / Groq / Together / any OpenAI-compatible upstream

```sh
GATEWAY_PROVIDERS=openai \
GATEWAY_DEFAULT_PROVIDER=openai \
GATEWAY_DEFAULT_MODEL=gpt-4o-mini \
OPENAI_BASE_URL=https://api.openai.com \
OPENAI_API_KEY=sk-... \
go run ./cmd/server
```

### Multiple providers, route per request

```sh
GATEWAY_PROVIDERS=fake,ollama,openai \
OLLAMA_BASE_URL=http://localhost:11434 \
OPENAI_BASE_URL=https://api.openai.com \
OPENAI_API_KEY=sk-... \
go run ./cmd/server

# Pick the backend at request time using the gateway's "provider" extension field:
curl -s http://localhost:8090/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{"provider":"ollama","model":"llama3.2:1b","messages":[{"role":"user","content":"hi"}]}'
```

---

## Architecture

```
       ┌──────────────────────────────────────────────────────────┐
       │                  mini-llm-gateway                        │
       │                                                          │
client │  ┌─────────────────┐    ┌─────────────────────┐          │
──────►│  │ internal/httpapi│    │ internal/provider/  │          │   Ollama
POST   │  │  router         │    │  Provider iface     │──HTTP───►│ ──────►
/v1/   │  │  /v1/chat/...   │───►│  ├─ Fake            │          │
chat/  │  │  /admin/...     │    │  ├─ Ollama          │──HTTPS──►│   OpenAI /
comple-│  │  GET / (web UI) │    │  └─ OpenAI          │          │   Groq / ...
tions  │  └────────┬────────┘    └─────────────────────┘          │
       │           │                                              │
       │           ▼ defer                                        │
       │  ┌─────────────────┐                                     │
       │  │ internal/store/ │                                     │
       │  │  Repository     │                                     │
       │  │  └─ SQLite      │──► mini-llm-gateway.db              │
       │  └─────────────────┘    (or /data inside container)      │
       └──────────────────────────────────────────────────────────┘
```

**Request flow.** Browser or HTTP client sends an OpenAI-shaped request to the gateway. The HTTP layer parses it, looks up the named provider in the registry, and either calls `Provider.Chat` (returns a full response) or `Streamer.Stream` (calls a callback per delta and emits SSE chunks). A `defer` writes the request log to SQLite — happy path *and* every error path.

**Why a gateway?** A client talks to *one* URL. Adding OpenAI, swapping to Groq, or running a local model becomes a config change, not a code change. Cross-cutting concerns (logging, redaction, future rate-limiting/retries/auth) live in one place instead of every client.

---

## RAG

The gateway can retrieve from a knowledge base before answering. Upload a document, then add `"rag": true` to a chat request — the user's last message is embedded, the top-K nearest chunks are pulled from the vector store, and they're prepended as a `system` message before the provider is called.

```sh
# 1. Upload a document
curl -s http://localhost:8090/admin/documents \
  -H 'content-type: application/json' \
  -d '{"title":"Pricing","body":"The Pro plan is $29/month and includes 50 GB of storage."}'

# 2. Ask a question with RAG enabled
curl -sN http://localhost:8090/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{"rag":true,"stream":true,"messages":[{"role":"user","content":"How much is the Pro plan?"}]}'
```

The retrieved chunk IDs are recorded in the request log (`/admin/requests` shows them in the `rag_chunk_ids` field), so you can trace exactly which docs influenced each answer.

**Vector store options** (set `RAG_VECTOR_STORE`):
- `inmemory` — brute-force cosine similarity, no extra service. Fine up to ~10k chunks. Data is lost on restart.
- `qdrant` — production-grade vector DB. Add `qdrant` service to compose (default: `http://localhost:6333`).

**Embedding options** (set `GATEWAY_EMBEDDER`):
- `ollama` — local, free, uses `nomic-embed-text` (768-dim) by default.
- `openai` — uses `text-embedding-3-small` (1536-dim) by default.
- *(unset)* — RAG is disabled; admin doc endpoints return 503; chat with `rag:true` returns 400.

## Streaming

`stream=true` returns Server-Sent Events in OpenAI's `chat.completion.chunk` shape:

```sh
curl -N http://localhost:8090/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{"stream":true,"messages":[{"role":"user","content":"hello"}]}'

# data: {"id":"chatcmpl-...","object":"chat.completion.chunk", ... "delta":{"content":"hello"}}
# data: {"id":"chatcmpl-...", ... "delta":{"content":" there"}}
# data: {"id":"chatcmpl-...", ... "delta":{}, "finish_reason":"stop"}
# data: [DONE]
```

If the upstream fails *before* the first chunk, the gateway returns a JSON `502`/`504` instead of an empty SSE stream. If it fails *mid-stream*, an `error` event is emitted and the failure is logged with `status_code=200` plus the error text in `error_text`.

If a provider doesn't implement streaming, the gateway returns a `400` *before* writing any SSE bytes — clients can fall back to non-streaming.

## Admin

```sh
# Configured providers + defaults
curl -s http://localhost:8090/admin/providers

# Most recent requests (newest first)
curl -s 'http://localhost:8090/admin/requests?limit=20'

# Next page: feed the next_before cursor from the previous response
curl -s 'http://localhost:8090/admin/requests?limit=20&before=2026-05-08T21:01:44.529Z'
```

Each entry has `id`, `ts`, `provider`, `model`, `latency_ms`, `status_code`, `error` (on failure), `prompt_chars`, `completion_chars`, and `prompt_tokens` / `completion_tokens` when the upstream reports them.

## Configuration

| Env var                            | Default                       | Purpose                                                    |
| ---------------------------------- | ----------------------------- | ---------------------------------------------------------- |
| `GATEWAY_PORT`                     | `8090`                        | TCP port to listen on                                      |
| `GATEWAY_PROVIDERS`                | `fake`                        | Comma-separated providers to enable (`fake`, `ollama`, `openai`) |
| `GATEWAY_DEFAULT_PROVIDER`         | `fake`                        | Used when a request omits `"provider"`. Must be in `GATEWAY_PROVIDERS`. |
| `GATEWAY_DEFAULT_MODEL`            | `fake-1`                      | Used when a request omits `"model"`                        |
| `GATEWAY_REQUEST_TIMEOUT_SECONDS`  | `60`                          | Per-request context timeout                                |
| `OLLAMA_BASE_URL`                  | `http://localhost:11434`      | Required if `ollama` is enabled                            |
| `OPENAI_BASE_URL`                  | `https://api.openai.com`      | Required if `openai` is enabled                            |
| `OPENAI_API_KEY`                   | *(unset)*                     | Required if `openai` is enabled. Set to any non-empty string for local OpenAI-compat servers without auth. |
| `GATEWAY_DB_PATH`                  | `mini-llm-gateway.db`         | SQLite file used for the request log                       |
| `GATEWAY_EMBEDDER`                 | *(unset)*                     | `fake` / `ollama` / `openai`. Empty disables RAG.          |
| `OLLAMA_EMBED_MODEL`               | `nomic-embed-text`            | Embedding model name on the Ollama backend                 |
| `OPENAI_EMBED_MODEL`               | `text-embedding-3-small`      | Embedding model name on the OpenAI-compatible backend      |
| `RAG_VECTOR_STORE`                 | `inmemory`                    | `inmemory` or `qdrant`                                     |
| `QDRANT_URL`                       | `http://localhost:6333`       | Qdrant base URL (used only when RAG_VECTOR_STORE=qdrant)   |
| `QDRANT_COLLECTION`                | `chunks`                      | Qdrant collection name                                     |
| `RAG_TOP_K`                        | `4`                           | Number of chunks retrieved per query                       |
| `RAG_CHUNK_SIZE`                   | `1000`                        | Chunk window size in runes                                 |
| `RAG_OVERLAP`                      | `100`                         | Rune overlap between consecutive chunks                    |

The server fails fast at startup if a configured provider is missing required values, or if `GATEWAY_DEFAULT_PROVIDER` isn't in `GATEWAY_PROVIDERS`. See [`.env.example`](./.env.example) for a copy-pasteable template.

---

## Deploy on a Mac mini via Tailscale

Use any always-on Mac as a personal LLM gateway reachable from every device on your tailnet. Tailscale gives each device a stable encrypted route to the gateway with no port-forwarding, no dynamic DNS, and no public exposure.

**Prereqs:** Docker Desktop, Tailscale, git on the Mac mini (`brew install --cask docker tailscale && brew install git` if needed).

```sh
# On the Mac mini (in person or via SSH):
git clone https://github.com/hsuanchenlin/mini-llm-gateway.git
cd mini-llm-gateway
docker compose up -d --build           # ~5 min first time (pulls ~1.6 GB of models)
curl -s http://localhost:8090/health   # verify locally first

# Make sure Tailscale is up
sudo tailscale up
tailscale status                       # note the "self" hostname (e.g. "mac-mini")
```

From any other tailnet device, the gateway is now reachable at `http://<machine-name>:8090/`.

### HTTPS via Tailscale Serve (recommended)

Cleaner URL, no port to remember, mobile keychains autofill:

```sh
# One-time: enable HTTPS in your Tailscale admin console
# https://login.tailscale.com/admin/dns → "HTTPS Certificates" → Enable

sudo tailscale serve --bg --https=443 http://localhost:8090
```

The gateway is now at `https://<machine-name>.<tailnet>.ts.net/`. Tailscale terminates TLS in front of it; the gateway itself stays plain HTTP on `localhost:8090`. To stop serving: `sudo tailscale serve --https=443 off`.

### Persistence (survive reboots and sleep)

| Setting | Where |
|---|---|
| Auto-start Docker on login | Docker Desktop → Settings → General → "Start Docker Desktop when you sign in" |
| Don't sleep when display off | System Settings → Lock Screen → "Prevent automatic sleeping when display is off" |
| Auto-start after power outage | System Settings → General → Login Items → "Start up automatically after a power failure" |
| Restart containers on crash | Already in `docker-compose.yml` (`restart: unless-stopped`) |

Tailscale's macOS client auto-starts at login by default, so the tailnet route comes back automatically too.

### Security

- **Tailnet membership = auth.** Only your Tailscale-logged-in devices can reach the gateway. Sufficient for personal use.
- **Do NOT enable `tailscale funnel`** (the public-internet variant). The `/admin/*` endpoints have no auth — anyone who finds the URL can read or delete your documents and request log.
- **Do NOT port-forward 8090** on your home router. Same reason.
- If you share your tailnet with anyone, restrict access via [Tailscale ACLs](https://tailscale.com/kb/1018/acls).
- Want to expose this beyond the tailnet later? Add a Bearer-token check to `/admin/*` and the chat endpoint first.

## Design tradeoffs

Each entry: **decision · why · cost.**

- **Pure-Go SQLite (`modernc.org/sqlite`) instead of CGO `mattn/go-sqlite3`.** No C toolchain in the runtime image, so the Docker image is ~32 MB instead of ~80 MB and cross-compiling is trivial. *Cost:* ~5x slower than CGO sqlite under load. Fine for an admin log; revisit if it ever becomes the bottleneck.
- **`Provider` as a Go interface.** Each backend is one file implementing one method (`Chat`); the HTTP layer never names a specific backend. *Cost:* one indirection per request.
- **`Streamer` as a sibling interface, not a method on `Provider`.** Non-streaming providers (the `fake`, hypothetical batch providers) stay simple. *Cost:* the handler has to type-assert.
- **Embedded migrations + embedded web assets via `embed.FS`.** Single binary, no runtime file dependencies. The gateway will start in any working directory. *Cost:* changing a CSS line requires a rebuild.
- **OpenAI request shape as our own wire format.** Every existing client SDK works against us with zero code changes. *Cost:* locked into their schema; awkward when we want to expose capabilities they don't.
- **Method-routed stdlib `http.ServeMux` (Go 1.22)** instead of chi. One less dependency. *Cost:* less ergonomic for path parameters (we don't use any yet).
- **Streaming headers deferred until first chunk.** If the upstream errors before producing output, we can still return a JSON `502` instead of an empty SSE stream. *Cost:* ~30 lines of subtle handler logic.
- **SQLite, single-instance.** No external DB to operate; the whole stack is the binary plus a file. *Cost:* horizontal scaling needs a real DB (admin reads on instance B can't see writes from instance A).
- **API keys live only on the provider struct + Authorization header.** Never logged, never returned in error messages — there's a regression test (`TestOpenAIErrorDoesNotLeakAPIKey`) guarding that.

## Future improvements

- **Rate limiting** — token bucket middleware (per IP, per provider).
- **Multi-tenant API keys** — gateway-level keys, per-tenant request log.
- **Named provider instances** — so `openai-prod` and `groq-fast` can be separate routes on the same gateway with different bases/keys.
- **Retries with exponential backoff** for transient upstream errors (5xx, network timeouts).
- **Tool / function calling** passthrough — proxy OpenAI's `tools` field to upstreams that support it.
- **Multimodal input** — image messages for vision models.
- **OpenTelemetry traces** + **Prometheus metrics endpoint** for production observability.
- **Postgres backend** for the request log + multi-instance deploy behind a load balancer.
- **SSE reconnection** via `Last-Event-ID` for long-running streams.

---

## Layout

```
cmd/server/        program entrypoint; loads config, builds registry, starts HTTP server, graceful shutdown
internal/config/   env-driven configuration
internal/provider/ Provider + Streamer interfaces, Registry, BuildRegistry, and Fake/Ollama/OpenAI implementations
internal/store/    Repository interface + SQLite request log + documents table; embedded migrations
internal/embed/    Embedder interface + Fake/Ollama/OpenAI implementations
internal/rag/      Chunker + Qdrant + InMemoryStore + Service that ties it all together
internal/httpapi/  HTTP handlers (chat completions, streaming, admin, RAG, static UI mount), OpenAI-compatible wire types
web/               Vanilla HTML/JS chat UI + knowledge panel; embedded into the binary via embed.FS
Dockerfile         Multi-stage; builds a static binary, runs as non-root on alpine
docker-compose.yml gateway + ollama (chat + embeddings) + ollama-init + qdrant
run-ollama.sh      Convenience launcher for the no-Docker Ollama path
.env.example       All env vars, documented
```

## Development

```sh
go test ./...                    # all 55 tests
go test ./internal/provider -v   # one package, verbose
go test ./... -run Stream        # one suite (e.g. all streaming tests)
go vet ./...                     # static analysis
```

Tests use `httptest.Server` to fake upstream LLM APIs — no real network calls in the test suite. SQLite tests use `t.TempDir()` so each test gets an isolated database. The streaming integration tests use `httptest.NewServer` (not `ResponseRecorder`) because the SSE handler needs `http.Flusher`.
