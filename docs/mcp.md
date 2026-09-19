# MCP

**English** · [Русский](ru/mcp.md)

MikroLLM runs [MCP](https://modelcontextprotocol.io) in the same process as the gateway: `POST /mcp`, no extra binary and no second SQLite.

An agent (Grok, Cursor, Claude) can read status, configure providers, models, queues, and keys, and read logs. `save_model.prompt_cache` = `inherit|off|auto|on`. `list_logs` / `log_stats` return `cached_tokens` and `saved_usd`.

## Transport

Streamable HTTP, JSON-RPC 2.0.

| Method | Path | Purpose |
|---|---|---|
| POST | `/mcp` | initialize, tools/list, tools/call, ping |
| GET | `/mcp` | `405` (no server SSE notifications — RAM on MikroTik) |
| DELETE | `/mcp` | close session (no-op, 200) |

`Accept: application/json, text/event-stream`. Reply is usually `application/json`. If the client asks for SSE only — one `message` event.

Sessions are **stateless**: `Mcp-Session-Id` is issued on initialize and mirrored, but the server does not store it. After a container restart, reconnect without lost state.

Request body max 1 MB.

## Auth

Bearer only. The admin cookie does not go to `/mcp` (`Path=/admin`).

```
Authorization: Bearer mcp-…
```

Accepted:

1. MCP token (SHA-256 in `admin_meta`, like `sk-` keys).
2. Admin password — to recover if the token is lost. bcrypt, same per-IP login limit.

Ordinary `sk-` keys **do not** open MCP: they are chat-client rights, not admin.

Bad token — `401` and `WWW-Authenticate: Bearer`.

## How to get a token

On **first** start, if none exists, one is generated and logged:

```
generated MCP token: mcp-…
```

Later — admin **Status → MCP for agents**: issue / rotate. Secret shown once.

Or flag / env (overwrites the stored hash if set):

| Flag | Env | Meaning |
|---|---|---|
| `-mcp-token` | `MIKROLLM_MCP_TOKEN` | set the token (hash stored) |
| `-mcp-token-reset` | `MIKROLLM_MCP_TOKEN_RESET=1` | generate a new one even if one exists |

Do not commit the token. In a RouterOS envlist it lands in the router config — prefer issuing from admin.

## Grok

`~/.grok/config.toml` or `.grok/config.toml` in the repo:

```toml
[mcp_servers.mikrollm]
url = "http://192.168.88.1:4000/mcp"   # or https://… after TLS, see tls.md
enabled = true
headers = { "Authorization" = "Bearer ${MIKROLLM_MCP_TOKEN}" }
```

Paste the token or export `MIKROLLM_MCP_TOKEN`. LAN, no hairpin WSS: the agent should hit `192.168.88.1:4000` from a LAN host / split-tunnel, not public hairpin.

Check:

```bash
curl -sS http://192.168.88.1:4000/mcp \
  -H "Authorization: Bearer mcp-…" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1"}}}'
```

Then `"method":"tools/list"` and `"method":"tools/call","params":{"name":"get_status","arguments":{}}`.

CLI:

```bash
grok mcp add --transport http mikrollm http://192.168.88.1:4000/mcp \
  --header "Authorization: Bearer mcp-…"
```

## Tools

Start with `get_status`. Names are stable; descriptions may be Russian in the live schema.

| Tool | Action |
|---|---|
| `get_status` | version, process RAM, backends, aliases, live queues, jobs |
| `refresh_health` | full catalog refetch without cache (OpenRouter: chat + image + video) + summary |
| `refresh_provider` | same for one backend by `id` |
| `list_providers` / `upsert_provider` / `delete_provider` | Ollama, vLLM, LM Studio, OpenRouter, Ollama Cloud. Token is masked; empty `token` does not wipe the key. Omitted `enabled` and `weight` stay. Disable: `id` + `enabled: false` |
| `list_models` / `list_catalog` / `connect_model` / `save_model` / `delete_model` | gateway aliases and health catalog |
| `host_action` | `pull` / `load` (background, see `list_jobs`) or `unload` / `delete` |
| `list_jobs` | pull/load progress |
| `list_queues` / `save_queue` / `delete_queue` | queue: `steps` (`model_alias`, `max_concurrent`), `extra_aliases`, overflow |
| `list_keys` / `create_key` / `update_key` / `delete_key` | virtual `sk-`. Secret only in `create_key` response |
| `list_logs` / `log_stats` | filters q / model / backend / key / 2xx·4xx·5xx / latency; p50/p95 |
| `rotate_mcp_token` | new Bearer, old one dies immediately |
| `list_policies` / `list_plugins` / `save_policy` / `delete_policy` | filters; `kind=nsfw` or `plugins: ["nsfw","adult"]`. Targets alias/queue/model, each id once |

`save_queue` with `steps` and `extra_aliases` replaces those lists. If the field is omitted, the old value stays.

Summary:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": { "name": "get_status", "arguments": {} }
}
```

Queue “local → cloud”:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "save_queue",
    "arguments": {
      "name": "itres",
      "alias": "coder",
      "overflow_after": 2,
      "steps": [
        {"model_alias": "ornith-1.5:35b", "max_concurrent": 2},
        {"model_alias": "smart-kimi-k2.7", "max_concurrent": 3}
      ],
      "extra_aliases": ["itres-coder"]
    }
  }
}
```

A chat client can then send `model: "coder"`, `itres`, or `itres-coder`.

## Security

MCP is full admin. Whoever has the token can issue keys and change providers.

- Do not publish `/mcp` to the internet without TLS and a filter.
- On RouterOS keep dst-nat :4000 on the LAN.
- Backend tokens and `sk-` are not repeated in tool replies except one-shot `create_key` / `rotate_mcp_token`.
