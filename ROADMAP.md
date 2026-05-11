# Mini LLM Gateway — Roadmap

A lightweight, OpenAI-compatible LLM gateway in Go. The goal is to demonstrate
backend engineering, LLM application infrastructure, streaming APIs,
observability, and cloud deployment — *not* to reimplement transformer
internals. Requests are routed to existing providers (Ollama, OpenAI-compatible
APIs, plus a built-in `fake` provider for tests).

## Milestones

### M1 — Skeleton ✅
- Go module: `mini-llm-gateway` (bare path, retarget to GitHub when published)
- Package layout: `cmd/server`, `internal/{config,provider,httpapi}`
- `config.FromEnv` for runtime configuration
- `Provider` interface + in-process `Fake` provider (echoes last user message)
- `Registry` for name → provider lookup
- HTTP server using `net/http` (Go 1.22 method-based ServeMux, no chi yet)
- `GET /health`
- `POST /v1/chat/completions` returning OpenAI-compatible JSON via the fake provider
- Per-request context timeout, graceful shutdown on SIGINT/SIGTERM
- Unit tests for the fake provider; handler tests for `/health` and
  `/v1/chat/completions` (happy path, default model, malformed body, empty
  messages, stream rejected, unknown provider, wrong method)

### M2 — Real providers (non-streaming) ✅
- `internal/provider/ollama.go` — `POST /api/chat`, maps `prompt_eval_count`/`eval_count` to token usage
- `internal/provider/openai.go` — `POST /v1/chat/completions` with `Authorization: Bearer <key>`; works against api.openai.com, Groq, Together, local llama.cpp, etc.
- `internal/provider/loader.go` (`BuildRegistry`) builds the registry from env;
  fails fast on missing config (e.g. `OPENAI_API_KEY` required when `openai` is enabled, default port now `8090`)
- `cmd/server/main.go` validates that `GATEWAY_DEFAULT_PROVIDER` is actually in the registry
- Upstream non-2xx → `502 provider_error` with status + body snippet (snippet capped at 512 bytes)
- API keys never appear in error messages or logs (guarded by `TestOpenAIErrorDoesNotLeakAPIKey`)
- 30 tests total (config + provider + httpapi); upstream calls use `httptest.Server` fakes — no real network

### M3 — Persistence & admin ✅
- `internal/store` package: `Repository` interface, `Noop` impl for tests,
  `SQLite` impl backed by `modernc.org/sqlite` (pure Go, no CGO)
- `internal/store/migrations/0001_init.sql` embedded via `embed.FS` and applied
  on `OpenSQLite`; idempotent across restarts (verified by `TestSQLiteOpenIsIdempotent`)
- WAL mode + busy_timeout=5s so admin reads don't block writes
- `/v1/chat/completions` logs every request — happy path *and* every validation
  / provider-error path — with id, timestamp, provider, model, latency_ms,
  status_code, error_text, prompt_chars, completion_chars, and (when upstream
  reports them) prompt_tokens / completion_tokens. Logging runs in a `defer`
  with its own 2s background context so it survives client disconnects.
- `GET /admin/requests?limit=&before=` — newest-first; cursor-paginated via
  `next_before` (RFC3339Nano) when a full page is returned
- `GET /admin/providers` — sorted provider list plus `default_provider` /
  `default_model`
- 12 new tests (4 store + 8 admin/handler) — total 42 pass

### M4 — Streaming ✅
- `provider.Streamer` sibling interface — non-streaming providers stay simple.
  Callback-based (`onChunk func(string) error`) so there's no goroutine or channel lifecycle to manage.
- Implementations on `Fake` (word-by-word echo), `Ollama` (NDJSON parser over `/api/chat?stream=true`), and `OpenAI` (SSE parser, sets `stream_options.include_usage:true` so the upstream emits a final usage chunk we can log)
- Handler dispatch: `stream=true` → `streamChat`. Headers are deferred until the *first* chunk arrives, so an upstream failure before any output still surfaces as a JSON 502/504. Mid-stream failures emit an `error` event, leave the response status at 200, and write the failure into `error_text` in the request log.
- OpenAI-shaped wire format: `data: {…chat.completion.chunk…}\n\n` per delta, a final chunk with `finish_reason:"stop"`, then `data: [DONE]\n\n`. Sets `Cache-Control: no-cache` and `X-Accel-Buffering: no` to defeat intermediary buffering.
- 9 new tests (3 fake-stream + 2 ollama-stream + 2 openai-stream + 3 SSE handler integration). Total now 51.

### M5 — Web UI ✅
- `web/` package — `index.html` + `app.js` + `style.css` + `embed.go` (single
  `embed.FS` so the binary stays self-contained)
- Mounted at `GET /` via Go 1.22's `http.FileServerFS`. Method-based routing
  means API routes (`POST /v1/chat/completions`, `GET /admin/*`, etc.) take
  precedence and a non-matching API path returns 405, so the static handler
  doesn't shadow them.
- Frontend (no build step, no framework, no dependencies):
  - Provider dropdown populated from `/admin/providers`; model is a free-text
    input pre-filled from the configured default
  - Chat history kept in JS, sent verbatim to `/v1/chat/completions` so the
    conversation accumulates
  - Streaming on by default — uses `fetch` + `ReadableStream` to read the SSE
    body chunk by chunk, parsing `data:` lines and appending content as it
    arrives. `[DONE]` ends the stream; mid-stream `error` events are inlined
    into the assistant message.
  - `Stream` checkbox lets you flip to non-streaming for comparison
  - Request log table at the bottom, refreshed automatically after each chat
    plus a manual Refresh button
- 4 new handler tests (index served, css/js mime types, 404 for unknown asset,
  API routes still win). Total now 55.

### M6 — Deployment & docs ✅
- Multi-stage `Dockerfile` (alpine builder + alpine runtime, non-root user, `CGO_ENABLED=0` static build, `HEALTHCHECK`). Final image: **32.5 MB**.
- `.dockerignore` keeps the build context lean (no `.git`, `.env`, `.db` files).
- `docker-compose.yml` runs three services: `ollama` (the official image), one-shot `ollama-init` that pulls `llama3.2:1b` on first boot into a named volume, and `gateway` wired to `http://ollama:11434`. Volumes for both Ollama models and the request log so state survives `docker compose down`.
- `run-ollama.sh` + `.env.example` for the no-Docker path.
- README rewritten as an interview artifact: Docker-first quick start, ASCII architecture diagram, **Design tradeoffs** section with cost called out for every key decision, **Future improvements** section, layout map, dev/test commands.

### M7 — RAG ✅
- `internal/embed`: `Embedder` interface (mirrors `Provider`) + Fake / OpenAI / Ollama implementations + env-driven loader. `Probe()` discovers vector dimensionality at startup.
- `internal/rag`: `Chunker` (rune-based sliding window with overlap), `Service` (ties chunker + embedder + vector store + document store, with rollback on partial failure), and two `VectorStore` implementations:
  - `Qdrant` — REST client (EnsureCollection, Upsert, Search, DeleteByDocumentID); idempotent collection setup; `?wait=true` on writes for synchronous behavior.
  - `InMemoryStore` — brute-force cosine similarity, `sync.RWMutex`, good for tests + small (<10k chunks) deployments.
- `internal/store/migrations/0002_documents.sql` and `0003_request_rag.sql`. Migration tracking via a new `schema_migrations` table so non-idempotent migrations (`ALTER TABLE`) don't re-run.
- New endpoints: `POST /admin/documents`, `GET /admin/documents`, `DELETE /admin/documents/{id}`. All return 503 when RAG is disabled.
- Modified `POST /v1/chat/completions`: with `"rag":true`, embeds the user's last message, retrieves top-K from the vector store, and prepends the chunks as a `system` message. Retrieved chunk IDs are recorded in the request log.
- UI gains a "Knowledge Base" panel (upload + list + delete) and a `RAG` checkbox on the chat input.
- Compose adds a `qdrant` service and pulls `nomic-embed-text` alongside the chat model.
- 28 new tests across `internal/embed`, `internal/rag`, `internal/store`, `internal/httpapi`. Total now 98.

### M8 — Auth + cost dashboard ✅
- `internal/auth`: `RequireBearer(token)` middleware. Empty token = no-op so existing setups keep working. Constant-time compare via `crypto/subtle.ConstantTimeCompare`. Sends `WWW-Authenticate: Bearer realm="..."` on 401.
- Routes wired so `/health` and `GET /` (web UI + assets) stay open; `POST /v1/chat/completions` and every `/admin/*` route require the token when configured.
- `web/app.js`: `apiFetch` wrapper sends `Authorization: Bearer …` from `localStorage`; on 401 it `window.prompt`s the user, stores the new token, and retries once.
- `internal/pricing`: hardcoded USD/million-token table (OpenAI, Anthropic, common Ollama models). `USD(model, in, out)` returns 0 for unknown models, flagged via `Known()`.
- `internal/store/stats.go`: `StatsByModel(ctx, since)` and `StatsByDay(ctx, days)` aggregate the request log; failed requests excluded from token totals.
- New `GET /admin/stats` endpoint returns `total_usd`, `today_usd` (computed via two queries — all-time + start-of-today — instead of a wrong proration), per-model breakdown with `pricing_known` flag, per-day request volume.
- `GET /admin/requests` rows now carry a `usd` field per row.
- New "Cost" panel in the UI: today/all-time totals + per-model SVG bar chart (no Chart.js dependency).
- 21 new tests (auth 6, pricing 6, stats SQL 3, stats HTTP 3, server-level auth 3). Total now 120.
- Training or running transformer internals from scratch
- Implementing a real tokenizer (token counts stay approximate)
- Multi-tenant auth / billing (one gateway-level API key is enough)

## Notable design choices
- Package directory `internal/httpapi` (not `internal/http`) so the package
  name doesn't shadow stdlib `net/http`.
- Standard library router (`http.ServeMux` with Go 1.22 method patterns) until
  routing complexity justifies pulling in `chi`.
- The gateway extends the OpenAI request schema with an optional `"provider"`
  field so a single endpoint can route across backends without separate URLs.
- The `fake` provider lives in the same `provider` package as the interface so
  tests can use it without an extra adapter; real providers will sit beside it.
