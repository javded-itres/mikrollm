# HTTP API

**English** · [Русский](ru/api.md)

Base: `http://<host>:4000` or `https://<host>:4000` if TLS is on ([tls.md](tls.md)).

Key: `Authorization: Bearer sk-…` or `X-Api-Key: sk-…`.

Without a key (except health/ready and admin) — `401`. Model not on the key allowlist — `403`. RPM exceeded — `429`.

## Endpoints

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/health` | none | process is up |
| GET | `/ready` | none | at least one healthy backend |
| GET | `/v1/models` | key | alias list; `context_length` / `max_input_tokens` |
| GET | `/v1/model/info` | key | LiteLLM-style `model_info.max_input_tokens` |
| GET | `/model/info` | key | same, without `/v1` |
| POST | `/v1/chat/completions` | key | OpenAI Chat Completions, including `stream: true`. Optional `X-Session-Id` (OpenRouter). Non-stream: `X-MikroLLM-Cache-Tokens` |
| POST | `/v1/images/generations` | key | OpenAI Images API. OpenRouter: native `POST /images`. OpenComfy / OpenAI-style backends: native `/v1/images/generations`; same-host `data[].url` is inlined as `b64_json` |
| POST | `/v1/videos` | key | OpenAI Videos API (Sora-style). `GET /v1/videos/{id}` and `/content` — status and file (`?model=` if the id is unknown to the gateway) |
| POST | `/api/chat` | key | Ollama `/api/chat` |
| GET | `/api/tags` | key | model names |
| GET | `/admin` | cookie | HTML admin |
| POST | `/mcp` | MCP token | MCP JSON-RPC (models, queues, keys, logs). [mcp.md](mcp.md) |

`/` redirects to `/admin`.

## Chat Completions

Body like OpenAI. `model` is a **gateway alias** or an upstream name if there is no alias but health has seen it.

```bash
curl http://192.168.88.1:4000/v1/chat/completions \
  -H "Authorization: Bearer sk-…" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.8:27b-mlx",
    "messages": [{"role": "user", "content": "hello"}],
    "stream": false,
    "temperature": 0.7,
    "max_tokens": 512
  }'
```

Stream is SSE (`data: {…}` / `data: [DONE]`), like OpenAI. The gateway proxies upstream bytes and does not buffer the whole response.

The gateway rewrites `model` to the alias `upstream_name` when they differ.

**Prompt cache (OpenRouter).** The client may send `X-Session-Id` and `session_id` / `cache_control` / `prompt_cache_key` — they reach upstream. With `prompt_cache=auto` the gateway injects top-level `cache_control` for Claude. Non-stream: `X-MikroLLM-Cache-Tokens` (and `X-MikroLLM-Cache-Write-Tokens` if write > 0). Stream: tokens only in the last SSE `usage` and in the log. Fallback to another model does not carry `cache_control`. Details: [providers.md](providers.md#prompt-cache).

If `model` is a **queue** (its alias, name, `name-alias`, `name/alias`, or extra alias), the request walks the steps. While slots are busy the connection waits; the reply always uses that connection. Overflow (waiters ≥ N) goes to the overflow alias; if none — `503`. `X-MikroLLM-Queue` is optional — watch the dashboard strip. The upstream body is rewritten: `"model"` becomes the chosen step’s model name, not the client queue alias.

If an alias has a **fallback model** and upstream returned 402 or credit/quota/subscription text, the request is retried on the fallback alias. Response header: `X-MikroLLM-Fallback: original -> fallback`.

`GET /v1/models` adds `context_length`, `max_model_len`, `max_tokens`, `max_input_tokens` (if known), `provider`, `owned_by`, and OpenRouter prices as `input_cost_per_token` / `output_cost_per_token`.

`GET /v1/model/info` (and `/model/info`) — LiteLLM shape:

```json
{
  "data": [{
    "model_name": "fast",
    "litellm_params": { "model": "qwen3.8:27b-mlx", "custom_llm_provider": "mikrollm" },
    "model_info": {
      "id": "fast",
      "key": "qwen3.8:27b-mlx",
      "max_tokens": 32768,
      "max_input_tokens": 32768,
      "max_output_tokens": 32768
    }
  }]
}
```

## Ollama Chat

```bash
curl http://192.168.88.1:4000/api/chat \
  -H "Authorization: Bearer sk-…" \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3.2","messages":[{"role":"user","content":"hi"}],"stream":false}'
```

Proxied to `POST <ollama>/api/chat` for local Ollama and Ollama Cloud. Otherwise the path becomes the provider’s OpenAI chat (`/v1/chat/completions` or `/chat/completions` on OpenRouter).

## Backend selection

1. Look up an enabled alias with that name.
2. Take its servers, keep **healthy** ones.
3. Apply the LB policy (see [admin.md](admin.md)).
4. If there is no alias — any healthy backend that has the name in its catalog (or whose model list has not loaded yet).
5. If the backend has a token, the gateway sends `Authorization: Bearer …` upstream.

## Errors

OpenAI-shaped JSON:

```json
{"error": {"message": "no healthy backend for model llama3.2"}}
```

Typical codes: `400` no `model`, `401` key, `403` allowlist, `429` RPM, `502` upstream down, `503` queue full / dropped waiters.

## Clients

Any OpenAI SDK: `base_url=http://<host>:4000/v1`, `api_key=sk-…`.

Open WebUI, Holix, Cursor, etc. — same base URL and a key from admin.
