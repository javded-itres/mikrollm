# Changelog

**English** · [Русский](CHANGELOG.ru.md)

GitHub Releases use this English file. Russian copy: [CHANGELOG.ru.md](CHANGELOG.ru.md).

## Unreleased

## 0.0.9 — 2026-09-25

- Hub alias **`auto`** can follow a pool of chat models. The hub picks fast / balanced / strong from the prompt; `X-MikroLLM-Routed-Model` names the node and alias that answered. Images and video stay on explicit aliases. [docs/hub.md](docs/hub.md)

- Ollama RAM load pins the alias context: «in RAM» now sends `options.num_ctx` = the largest `num_ctx` across alias profiles on that model (fallback: alias Context). Ollama default is 4096 and a per-request num_ctx triggers a reload — set it in alias Params, then reload. MCP `host_action` load accepts explicit `num_ctx`.

- **Params profile per alias**: default generation knobs (`think` true/false/low/medium/high/max, `temperature`, `num_predict`, `num_ctx`, `top_p`, `seed`, `x_*`) stored on the alias and injected into every proxied chat request — admin Models column plus MCP `save_model` `params`. Client fields win; per-knob `lock` forces the profile value. Provider translation: Ollama `/v1` → `reasoning_effort`/`max_tokens`, native `/api/chat` → `options.*` + `think` (only path where `num_ctx` works), OpenRouter → `reasoning`, vLLM / LM Studio → `chat_template_kwargs`. Playground panel shows the profile as defaults.

## 0.0.8 — 2026-09-21

- **Tool calling works over hub aliases**: relayed chat completions now keep `tool_calls`, `finish_reason`, `id`, `model` and `usage` (previously only `content`/`reasoning` survived, so agents saw empty replies). Stream clients get a proper two-chunk SSE with `finish_reason`. Non-chat relay payloads still pass through untouched.
- Admin chat: live queue is a collapsible panel at the top (arrow). Video/image generation stays on its tab after switching away.
- Chat «Генерация» lists this session’s in-flight prompts, not gateway step queues. GET video status/content does not consume API-key RPM.

## 0.0.7 — 2026-09-21

- Queue wait timeout no longer cancels a job that already started (fixes `context deadline exceeded` on long chat/video).
- Hub node concurrency: chat / images / videos slots (default 4 / 2 / 1) on Status; extra jobs wait in the network queue instead of `peer busy`.
- Admin chat tabs; live queue list shows the prompt currently generating.
- **Swagger UI** (light theme) at `/docs` and `/admin/docs`. Spec: `GET /openapi.json`. Authorize with an `sk-` key to try chat / images / videos.

## 0.0.6 — 2026-09-21

- Hub member mode grows a reserved alias **`auto`**: it follows the hub operator’s default node+model from `GET /v1/catalog` `defaults`. [docs/hub.md](docs/hub.md)
- **iOS first slice:** `mobile` package (`Start`/`Stop`) + SwiftUI WKWebView shell (`ios/`). `make ios` builds the gomobile xcframework. No on-device LLM yet. [docs/install-ios.md](docs/install-ios.md)
- Admin is a **Chrome installable app** (PWA): `/admin/manifest.webmanifest`, icons, service worker, **В приложение** when Chrome offers `beforeinstallprompt`. Needs HTTPS or localhost.
- **KeeneticOS:** Entware install (no Docker). `scripts/install-keenetic.sh` + `make build-keenetic` (`linux/arm64`: Peak / Ultra KN-1811 / Giga KN-1012 / Hopper KN-3811). MIPS waits on SQLite. [docs/install-keenetic.md](docs/install-keenetic.md)
- Hub share **schedule** on Status (daily or selected weekdays + time window + timezone). Catalog shows the window and “not now”; relay is 503 outside it. **Rating** is lifetime minutes seen on the hub; Hub UI and Models filters **Top 10 / Top 100**.

## 0.0.5 — 2026-09-20

- Compiled hub URL is `https://hub.mikrollm.ru` (`MIKROLLM_HUB_URL` still overrides). Hub guide: [docs/hub.md](docs/hub.md).
- Admin chat: right-hand **model parameters** panel (`GET /admin/model-params`). OpenComfy workflows expose required fields (e.g. `input_image`); the playground blocks send until a reference is attached. Photos go as `input_image` / `input_images` as well as `input_references`.
- Admin playground: picking a video-only model (e.g. Hailuo) switches the type to **Video** so chat does not hit OpenComfy’s `video models use POST /v1/videos`. Chat on a video-only alias returns 400 with that hint.
- Admin playground: in **image** / **video** mode you can attach several reference photos (`+ фото`, paste, drop). They go as `input_references` (JPEG, max 6, downscaled). Hub media relay body cap is 8 MiB.
- **OpenRouter images:** `POST /v1/images/generations` goes to OpenRouter `POST /images` (not chat + `modalities: ["image","text"]`). Image-only models (Flux, Seedream, …) no longer 404 with “No endpoints found that support the requested output modalities”. Previous-frame edits become `input_references`.
- **Hub media:** shared image/video aliases are announced and relayed (`POST /v1/relay/{node}/images` and `/videos`, plus status/content). Binary clips come back as `b64` (cap 6 MiB). Chat on a video-only alias still 400. [docs/hub.md](docs/hub.md)
- **OpenComfy pictures in admin chat:** `POST /v1/images/generations` fetches same-host `data[].url` files (OpenComfy `/v1/files/…`) and returns `b64_json`. The playground CSP is `img-src 'self' data: blob:` and cannot load `http://gpu:8788/…`. Foreign hosts are not fetched.
- Desktop one-liner: `curl -fsSL https://raw.githubusercontent.com/javded-itres/mikrollm/main/scripts/install.sh | sh` installs Ollama (if needed) and MikroLLM on Linux/macOS, starts a user service, and with `-seed local` connects local Ollama models. [docs/install-desktop.md](docs/install-desktop.md)

- **OpenComfy** backend kind: local ComfyUI image/video gateway (`:8788`). Admin **Add server** → OpenComfy, URL without `/v1`, API key `sk-…`. Catalog from `/v1/models` + `/v1/images/models` + `/v1/videos/models`; `POST /v1/images/generations` and `POST /v1/videos` proxy as with OpenAI (not OpenRouter chat+modalities). [docs/providers.md](docs/providers.md#opencomfy)
- **Hub network** (not P2P): Status toggle **Hub network member** makes the gateway register at the compiled URL `https://hub.mikrollm.ru` (`MIKROLLM_HUB_URL` to override). Shared aliases are announced; the hub long-polls the node and relays chat, images, and videos. Client only in this repo (`internal/hubclient`). Hub **service** lives in [`mikrollm_hub`](https://github.com/javded-itres/mikrollm_hub). [docs/hub.md](docs/hub.md)

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
