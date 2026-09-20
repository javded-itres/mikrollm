# Architecture

**English** · [Русский](ru/architecture.md)

One process, one binary, SQLite. Composition root is `internal/app`: implementations are constructed and passed into constructors.

```
cmd/mikrollm          flags, http.Server (optional TLS)
internal/app          dependency graph
internal/domain       entities (Backend, Model, APIKey, Job…)
internal/ports        Store, Health, Auth, Host, Jobs, ChatGateway
internal/store        SQLite (modernc.org/sqlite, no CGO)
internal/health       poll Ollama / Ollama Cloud / OpenRouter / vLLM / LM Studio
internal/auth         bcrypt, cookie+CSRF, SHA-256 keys
internal/host         pull / delete / load / unload by backend kind
internal/ollama       Ollama HTTP (pull NDJSON, generate keep_alive)
internal/jobs         background pull/load + progress
internal/proxy        OpenAI/Ollama API, LB
internal/queue        disk queue (SQLite WAL), step slots, sticky HTTP
internal/admin        HTML admin
internal/mcp          MCP Streamable HTTP (`/mcp`), JSON-RPC, no SDK
internal/guard        request/response filters, system prompt, prompt injection
internal/promptcache  OpenRouter prefix cache: cache_control, usage, SSE tail
internal/tlsconf      PEM / self-signed / Let's Encrypt ACME (autocert)
internal/web          templates and static (embed)
internal/hubclient    outbound hub client (register / announce / long-poll)
```

## Principles

- **S** — proxy does not know HTML; host adapter does not know SQLite.
- **O / L** — a new adapter hangs on a port without changing `proxy`/`admin`.
- **I** — health sees only `BackendQuery`, auth only `AuthStore`.
- **D** — HTTP layers depend on `ports`, not concrete packages. `*http.Client` is injected too.

Go DI: constructors, no Wire/Fx.

```go
ui := admin.New(admin.Deps{
    Store: st, Health: checker, Auth: keys, Host: host, Jobs: tracker, Chat: px, Queues: queues,
})
mcp.New(mcp.Deps{Store: st, Health: checker, Auth: keys, Host: host, Jobs: tracker, Queues: queues}).Mount(mux)
```

MCP token — SHA-256 in `admin_meta` (`mcp_token_hash` / prefix). Constant-time compare; plaintext is not stored.

## Data

File `<data>/mikrollm.db`, WAL. `MaxOpenConns=1` (modernc/sqlite limit). The model list **must not** hold a cursor during a second query — login and API deadlock otherwise.

The `/data` volume on RouterOS survives `container remove`. Queues are tables `queues` / `queue_steps` / `queue_aliases` / `queue_jobs`; waiting bodies on disk, live HTTP `ResponseWriter` in memory. Billing rollup: `billing_hour`.

## Background work

`POST /admin/ollama/{id}/pull` returns immediately and downloads in a goroutine with `context.Background()`. Progress in memory and in `ollama_jobs`. UI polls `GET /admin/ollama/jobs`. After process restart a running pull resumes.

Load into RAM: Ollama — `POST /api/generate` with empty prompt and `keep_alive: -1`; LM Studio — `POST /api/v1/models/load`. vLLM does not load via API (see [providers.md](providers.md)).

## RouterOS image

`docker buildx` → OCI tar → `scripts/oci_to_legacy_docker.py` → docker-save v1. Otherwise RouterOS answers `no config found in manifest`.
