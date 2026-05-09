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

**Status:** M1 skeleton ✅ · M2 real providers ✅ · M3 SQLite logging + admin ✅ · M4 SSE streaming ✅ · M5 web UI ✅ · M6 Docker + docs ✅. See [ROADMAP.md](./ROADMAP.md) for the full milestone breakdown.

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

The server fails fast at startup if a configured provider is missing required values, or if `GATEWAY_DEFAULT_PROVIDER` isn't in `GATEWAY_PROVIDERS`. See [`.env.example`](./.env.example) for a copy-pasteable template.

---

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
internal/store/    Repository interface + SQLite-backed request log; embedded migrations
internal/httpapi/  HTTP handlers (chat completions, streaming, admin, static UI mount), OpenAI-compatible wire types
web/               Vanilla HTML/JS chat UI; embedded into the binary via embed.FS
Dockerfile         Multi-stage; builds a static binary, runs as non-root on alpine
docker-compose.yml gateway + ollama + one-shot ollama-init that pulls llama3.2:1b
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
