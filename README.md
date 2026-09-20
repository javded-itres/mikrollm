<p align="center">
  <img src="docs/assets/banner.jpg" alt="banner" width="920">
</p>

# MikroLLM

**English** · [Русский](README.ru.md)

A small LLM gateway for [Ollama](https://ollama.com) (local and Cloud), [OpenRouter](https://openrouter.ai), [vLLM](https://docs.vllm.ai), and [LM Studio](https://lmstudio.ai), in the spirit of LiteLLM: OpenAI-compatible API, virtual `sk-…` keys, HTML admin, playground chat, and pull/load where the backend API allows it.

Written in Go, no Python, no CGO. Binary ~12 MB, typically 8–20 MB RAM in use. Fits:

- a **RouterOS 7 container** on MikroTik (hAP ax³ and other ARM64);
- a **normal server** (Linux amd64/arm64, Docker or systemd);
- local development.

Docs: [docs/](docs/README.md) (English default). Russian: [docs/ru/](docs/ru/README.md).

## Features

- Multiple backends: **Ollama**, **Ollama Cloud**, **OpenRouter**, **vLLM**, **LM Studio**; health check every 10 s. Disable a provider in admin or MCP without deleting it.
- Client aliases, `least_conn` / `round_robin` / `failover`, credit fallback when the primary runs out of quota.
- Queues: steps (local → free cloud → paid), dashboard visualization, disk-backed wait without Kafka/Redis. Clients may send an alias, queue name, or `name-alias`.
- Virtual keys with model allowlists and RPM.
- Admin: servers, models (provider and price filters), keys, queues, filtered logs, chat, **billing** (hour/day/week/month/year), **security** (system prompt, injection, PII).
- **MCP** at `POST /mcp`: an agent can configure providers, models, queues, and keys, and read logs/status. [docs/mcp.md](docs/mcp.md)
- Model download (`pull`) with a progress bar on Ollama and LM Studio; refresh does not abort the job.
- Load / unload weights in RAM (Ollama `keep_alive`, LM Studio `/api/v1/models/load|unload`).
- vLLM: the model is bound to the process (`vllm serve <HuggingFace-id>`) — [guide](docs/providers.md#vllm).
- OpenRouter and Ollama Cloud over HTTPS, no extra GPU server; API key required. $/1M prices in the catalog: OpenRouter from `/models`, Ollama Cloud from [ollama.com/pricing](https://ollama.com/pricing).
- OpenRouter prompt cache: `auto` injects Claude `cache_control`; logs show `cached_tokens` and a $ estimate. [docs/providers.md](docs/providers.md#prompt-cache).
- Playground: pick an alias or upstream name and chat without an API key (admin session required).
- **Hub network**: toggle **Hub network member** on Status; the gateway registers itself at the compiled hub URL (`https://hub.mikrollm.ru`, override `MIKROLLM_HUB_URL`) and shares marked aliases through an outbound long-poll. Not P2P. The hub **service** is a separate repo. [docs/hub.md](docs/hub.md)

## Quick start (local)

**One line** (Linux or macOS): installs Ollama if needed, MikroLLM, a user service, and connects local Ollama models. [docs/install-desktop.md](docs/install-desktop.md)

```bash
curl -fsSL https://raw.githubusercontent.com/javded-itres/mikrollm/main/scripts/install.sh | sh
```

Admin: http://127.0.0.1:4000/admin — password is printed by the script (`~/.mikrollm/admin.pass`).

From source you need Go 1.23+ and at least one Ollama on `localhost:11434` or on the LAN:

```bash
git clone https://github.com/javded-itres/mikrollm.git
cd mikrollm
go test ./...
make run
```

`make run` listens on `:4000`, admin password `admin`, data dir `./data`.

Open http://127.0.0.1:4000/admin → **Status** → add Ollama / vLLM / LM Studio → **Models** → connect what you need → **Keys** → issue `sk-…`.

```bash
curl http://127.0.0.1:4000/v1/chat/completions \
  -H "Authorization: Bearer sk-…" \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3.2","messages":[{"role":"user","content":"hello"}],"stream":false}'
```

An empty data dir **seeds** two backends `mac-82` / `mac-80` at `192.168.88.80/82` on first start. If those are not your hosts, delete them in admin and add your own. Existing databases are not seeded.

## Where to install

| Scenario | Doc |
|---|---|
| Laptop / desktop, one command | [docs/install-desktop.md](docs/install-desktop.md) |
| Dev machine with Go | [docs/install-local.md](docs/install-local.md) |
| Linux server, Docker or systemd | [docs/install-docker.md](docs/install-docker.md) |
| MikroTik RouterOS 7 container | [docs/install-mikrotik.md](docs/install-mikrotik.md) |
| KeeneticOS (Entware) | [docs/install-keenetic.md](docs/install-keenetic.md) |
| HTTPS | [docs/tls.md](docs/tls.md) |
| Admin: models, RAM, keys, chat | [docs/admin.md](docs/admin.md) |
| Ollama / Cloud / OpenRouter / vLLM / LM Studio | [docs/providers.md](docs/providers.md) |
| HTTP API | [docs/api.md](docs/api.md) |
| MCP for an agent | [docs/mcp.md](docs/mcp.md) |
| Code layout | [docs/architecture.md](docs/architecture.md) |
| Hub network (outbound client) | [docs/hub.md](docs/hub.md) |

## Requirements

- At least one backend: local Ollama / vLLM / LM Studio **or** OpenRouter / Ollama Cloud (HTTPS + API key). The MikroLLM process must be able to reach it.
- For the RouterOS image: Docker Buildx, Python 3; USB storage on the router is recommended.
- Port **4000/tcp** (override with `-listen` / `MIKROLLM_LISTEN`). HTTPS — [docs/tls.md](docs/tls.md).

## Configuration

| Flag | Env | Default |
|---|---|---|
| `-seed` | `MIKROLLM_SEED` | empty = LAN demo `mac-80`/`mac-82`; `local` = `http://127.0.0.1:11434` and connect Ollama models |
| `-listen` | `MIKROLLM_LISTEN` | `:4000` |
| `-data` | `MIKROLLM_DATA` | `./data` |
| `-admin-password` | `ADMIN_PASSWORD` | empty: generated and printed on **first** start |
| `-admin-password-reset` | `ADMIN_PASSWORD_RESET=1` | do not reset |
| `-mcp-token` | `MIKROLLM_MCP_TOKEN` | Bearer for `/mcp`; empty → generated on first start |
| `-mcp-token-reset` | `MIKROLLM_MCP_TOKEN_RESET=1` | issue a new MCP token |
| `-tls-cert` | `MIKROLLM_TLS_CERT` | certificate PEM; with `-tls-key` enables HTTPS on `-listen` |
| `-tls-key` | `MIKROLLM_TLS_KEY` | key PEM |
| `-tls-auto` | `MIKROLLM_TLS_AUTO=1` | self-signed in `<data>/tls` if no files |
| `-tls-hosts` | `MIKROLLM_TLS_HOSTS` | SANs for `-tls-auto` (comma-separated IPs and names) |
| `-acme-hosts` | `MIKROLLM_ACME_HOSTS` | Let's Encrypt FQDNs; issue and renew (needs port 80) |
| `-acme-email` | `MIKROLLM_ACME_EMAIL` | LE account email |
| `-acme-http` | `MIKROLLM_ACME_HTTP` | HTTP-01, default `:80`; `off` disables it |
| `-acme-staging` | `MIKROLLM_ACME_STAGING=1` | staging CA |
|  | `MIKROLLM_QUEUE_MAX_BYTES` | `16777216` — max queued body on disk; older waiters dropped |
|  | `MIKROLLM_QUEUE_MAX_JOBS` | `200` — max waiting+running |
|  | `MIKROLLM_QUEUE_MAX_WAIT` | `3m` — how long an HTTP connection may wait for a slot |
|  | `MIKROLLM_HUB_URL` | compiled `https://hub.mikrollm.ru` — outbound hub for **Hub network member** |

The password is bcrypt in SQLite (`data/mikrollm.db`). Reset only with `ADMIN_PASSWORD_RESET=1`.

## Build

```bash
make test
make build-arm64          # dist/mikrollm (linux/arm64)
make tar-ros              # dist/mikrollm-ros-legacy.tar for RouterOS
```

GitHub Releases ship binaries and the MikroTik tar. Release notes are **English** — see [docs/releasing.md](docs/releasing.md).

## License

[MIT](LICENSE)
