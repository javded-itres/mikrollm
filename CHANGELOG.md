# Changelog

**English** · [Русский](CHANGELOG.ru.md)

GitHub Releases use this English file. Russian copy: [CHANGELOG.ru.md](CHANGELOG.ru.md).

## Unreleased

## 0.0.4 — 2026-09-19

- Documentation is bilingual; **English is default** (`README.md`, `docs/`, GitHub Releases). Russian: `README.ru.md`, `docs/ru/`. [docs/releasing.md](docs/releasing.md)
- **Billing** admin tab: `$` totals for hour / day / week / month / year, bar chart, no extra buttons. History lives in `billing_hour` and is not trimmed with the request log.
- OpenRouter prompt cache: `system_prompt` no longer flattens multipart/`cache_control`; `X-Session-Id` is forwarded to OpenRouter; default `auto` injects top-level `cache_control` only for Claude (first turn 1.25× write — toggle on **Models**). Logs/dashboard/MCP: `cached_tokens`, $ estimate, `usage.cost` separately. [docs/providers.md](docs/providers.md#prompt-cache)
- In-process HTTPS: `-tls-cert`/`-tls-key` (PEM reloaded without restart) or `-tls-auto` (self-signed in `<data>/tls`). Let's Encrypt: `-acme-hosts` (HTTP-01 on `:80`, auto-renew). [docs/tls.md](docs/tls.md)
- Providers can be disabled without delete: Status button, MCP `upsert_provider` with `enabled`. Health and routing skip a disabled backend; omitted `enabled`/`weight` are not reset.
- **Security** tab: system prompt, stop words, PII, prompt injection (LiteLLM-style heuristic), categories, regex. Attach to alias, queue, and/or upstream; each id runs once. [docs/security.md](docs/security.md)
- NSFW / 18+ as category plugins (LiteLLM `content_filter`): `nsfw`, `adult`, separate `csam`. Policy kind `nsfw` enables both; MCP `list_plugins`.
- Media: `POST /v1/images/generations` and `POST /v1/videos` like LiteLLM/OpenAI. OpenRouter images go through chat + `modalities`. Catalog and playground mark image/video models; chat can pick content type or endpoint.
- Playground: generation history, edits on the original request (thread + previous frame for OpenRouter).
- Force-refresh a provider catalog (card button and “Refresh catalogs”). OpenRouter pulls chat+image+video (`/videos/models`). Model list pagination: 10 / 20 / 50 / 100 / all.
- OpenRouter image/video prices: `image_output` / `image_token` and `pricing_skus` (per frame, per 1M img, per video second), not only prompt/completion per token.

## 0.0.3 — 2026-09-08

Request queues, MCP for an agent, Ollama Cloud prices, log filters, tighter admin.

- **Queues**: steps (local → cloud), slots, disk wait (SQLite WAL, no Kafka/Redis), sticky HTTP — the response always returns on the same connection. Overflow threshold, limits `MIKROLLM_QUEUE_MAX_BYTES` / `MIKROLLM_QUEUE_MAX_JOBS` / `MIKROLLM_QUEUE_MAX_WAIT`. `/admin/queues` and a live strip on the dashboard
- Client `model` may be a queue alias, its **name**, `name-alias`, or `name/alias` (e.g. `coder`, `itres`, `itres-coder`). Extra aliases attach to the same queue. Upstream gets the real model name, not the client alias
- **MCP** in-process: `POST /mcp` (Streamable HTTP, JSON-RPC). An agent configures providers, models, queues, and keys, and reads logs/status. Token `mcp-…` (or the admin password). [docs/mcp.md](docs/mcp.md)
- Ollama Cloud: $/1M in/out from [ollama.com/pricing](https://ollama.com/pricing) and `/library/<model>` pages, in catalog, keys, and chat, like OpenRouter
- Log: search, 2xx/4xx/5xx filters, model, backend, key, latency; up to 500 rows
- Keys: named model list with scroll, not a tight unlabeled grid
- Security: CSRF, login limit by IP (no port), cookie only on `/admin`, key not in URL, backend URL http(s) only, proxy does not copy Set-Cookie/CORS/Location, CSP/nosniff, no following 3xx upstream, password change rotates the session secret
- Logo in the header, on login, and as favicon

## 0.0.2 — 2026-09-08

vLLM, LM Studio, OpenRouter, and Ollama Cloud backends; admin, keys, catalog, fallback models.

- **vLLM** and **LM Studio** next to Ollama (kind + optional Bearer). vLLM loads the model via `vllm serve`; LM Studio — download / load / unload over REST v1. Guide: [docs/providers.md](docs/providers.md)
- **OpenRouter** and **Ollama Cloud** over HTTPS, no GPU server of your own. RouterOS image includes root CAs
- OpenRouter: health via `GET /key` (catalog cached), 403 body parse; key stored without a spare `Bearer`
- 403 from a Russian ISP: send container `192.168.254.5` through the same VPN as LAN — [docs/install-mikrotik.md](docs/install-mikrotik.md#openrouter-403)
- Keys: full alias and model list at create, allowlist editable after issue
- Model context (`max_input_tokens`) in admin and in `GET /v1/models`, `GET /v1/model/info`
- Catalog and keys: filter by provider and price, provider name and $/1M (in/out)
- Alias: fallback model on 402 / end of credits or subscription (`X-MikroLLM-Fallback`)
- `/api/chat` to a non-Ollama backend is rewritten to the provider’s OpenAI chat completions
- Admin: server list, chat with scroll and pinned input, pull banner hides after 5 s

## 0.0.1 — 2026-09-07

First public release.

- OpenAI `/v1/chat/completions` and Ollama `/api/chat` / `/api/tags`
- Multiple Ollama backends, health check, LB
- Virtual keys, RPM, allowlist
- Admin: models, RAM, pull with progress, keys, playground chat, log
- scratch linux/arm64 image for RouterOS 7
- SQLite without CGO
