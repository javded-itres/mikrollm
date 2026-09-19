# Backends: local and cloud

**English** · [Русский](ru/providers.md)

MikroLLM is a gateway. A backend can be a **local process** on the LAN or a **cloud API** (OpenRouter, Ollama Cloud) — no GPU server of your own. In admin **Status → Add server** set kind, URL, and an API key if needed.

| Kind | Talks to | Chat | Model list | Download / RAM |
|---|---|---|---|---|
| **Ollama** (local) | `http://<host>:11434` | `/api/chat`, `/v1/chat/completions` | `/api/tags`, `/api/ps` | yes, from admin |
| **Ollama Cloud** | `https://ollama.com` | `/api/chat`, `/v1/chat/completions` | `/api/tags` | no: models already in the cloud |
| **OpenRouter** | `https://openrouter.ai/api/v1` | `/chat/completions` | `/models` | no: models already in the cloud |
| **vLLM** | `http://<host>:8000` | `/v1/chat/completions` | `/health`, `/v1/models` | no API: the model is the process |
| **LM Studio** | `http://<host>:1234` | `/v1/chat/completions` | `/api/v1/models` | yes, from admin |

Clients always talk to MikroLLM (`/v1/chat/completions`). If the client sends `/api/chat` and the backend is not Ollama / Ollama Cloud, the gateway rewrites the path to the provider’s OpenAI-compatible chat.

Cloud backends need **HTTPS**. The RouterOS image puts root CAs in the container (`ca-certificates`). On normal Linux/macOS, system certs are used.

## Ollama

1. Install [Ollama](https://ollama.com) on the machine that holds the model.
2. In admin: kind **Ollama**, URL `http://<host>:11434`.
3. On **Models** download (`llama3.2`, `qwen2.5:32b`, …), load into RAM, connect to the gateway.

More: [admin.md](admin.md).

## vLLM

vLLM holds **one** model for the life of the process. Changing weights = stop the server and start with another id. That cannot be done over HTTP — so admin has no “download” / “into RAM” buttons.

### Install

Need Python 3.9+, NVIDIA GPU (CUDA) or a supported backend. Docs: [docs.vllm.ai](https://docs.vllm.ai/en/latest/getting_started/installation.html).

```bash
pip install vllm
# or
uv pip install vllm
```

Docker:

```bash
docker run --gpus all --ipc=host -p 8000:8000 \
  vllm/vllm-openai:latest \
  --model Qwen/Qwen2.5-7B-Instruct \
  --host 0.0.0.0 --port 8000
```

### Load a model on the server

The model name is a Hugging Face id (`org/name`).

```bash
vllm serve Qwen/Qwen2.5-7B-Instruct \
  --host 0.0.0.0 \
  --port 8000
```

With a key (put the same token in MikroLLM):

```bash
vllm serve Qwen/Qwen2.5-7B-Instruct \
  --host 0.0.0.0 --port 8000 \
  --api-key supersecret
```

Private Hugging Face model:

```bash
export HF_TOKEN=hf_...
vllm serve meta-llama/Meta-Llama-3.1-8B-Instruct \
  --host 0.0.0.0 --port 8000
```

The first start downloads weights into the Hugging Face cache (`~/.cache/huggingface`). That is “loading the model on the server”.

Check:

```bash
curl http://127.0.0.1:8000/health
curl http://127.0.0.1:8000/v1/models
```

### Change model

```bash
# stop the vllm process / container
vllm serve mistralai/Mistral-7B-Instruct-v0.3 --host 0.0.0.0 --port 8000
```

In MikroLLM click **Refresh status** — the new name appears in the catalog.

### Connect to the gateway

Admin → **Status** → kind **vLLM**, URL `http://<host>:8000`, token if you set `--api-key`. Then **Models → To gateway**.

systemd example:

```ini
[Service]
ExecStart=/usr/bin/vllm serve Qwen/Qwen2.5-7B-Instruct --host 0.0.0.0 --port 8000
Restart=on-failure
```

## LM Studio

[LM Studio](https://lmstudio.ai) is a desktop app (macOS / Windows / Linux) with a local HTTP server. From 0.4 there is REST `/api/v1/*`: list, download, load, unload.

### Enable the server

1. Open LM Studio → **Developer**.
2. Start server, bind `0.0.0.0`, port `1234` (so the gateway on the LAN can see the host).
3. If you enabled an API token — paste it into MikroLLM’s Token field.

Headless (no window): see [Run as a service](https://lmstudio.ai/docs/developer/core/headless).

Check:

```bash
curl http://127.0.0.1:1234/v1/models
curl http://127.0.0.1:1234/api/v1/models \
  -H "Authorization: Bearer $LM_API_TOKEN"
```

### Download and load from MikroLLM

In admin kind **LM Studio**, URL `http://<host>:1234`.

- **Download**: id from the LM Studio catalog (`ibm/granite-4-micro`) or a Hugging Face URL. Progress like Ollama pull.
- **Into RAM / unload**: buttons on **Models** and the server card. API: `POST /api/v1/models/load` and `/unload`.
- Admin does not delete the file from disk — remove the model in the LM Studio UI.

You can still load a model by hand in LM Studio (Chat / Developer → load). The gateway sees it after **Refresh**.

### Old server

Before v1 REST, LM Studio only serves `/v1/models` (already loaded). MikroLLM then shows them as “in RAM” and may hide download/load — upgrade LM Studio.

## OpenRouter

Direct cloud: MikroLLM talks to `https://openrouter.ai/api/v1`, no local LLM server.

1. Key: [openrouter.ai/keys](https://openrouter.ai/settings/keys) (`sk-or-v1-…`).
2. Admin → **Status** → kind **OpenRouter**. URL is filled (`https://openrouter.ai/api/v1`). Paste the key.
3. **Refresh status** — catalog from OpenRouter (`GET /models`).
4. **Models → To gateway** for the ids you need (`openai/gpt-4o-mini`, `anthropic/claude-sonnet-4`, …).

Chat: `POST https://openrouter.ai/api/v1/chat/completions`, `Authorization: Bearer <key>`.

Nothing to download or load into RAM — weights live at the provider. RouterOS needs an image with CA certs (the current Dockerfile copies them).

**403 Forbidden.** OpenRouter (Cloudflare) often 403s API from a Russian IP. The key can still be valid: `GET /api/v1/key` via VPN is 200, via ISP is 403. The MikroLLM container lives in `192.168.254.0/24` and **does not** match the “LAN 88 → AMS WG” rule. You need a separate mark-routing on `192.168.254.5` into table `vpn` and src-nat to a free LAN address (not the router `.1`), or the VPN reply hits INPUT and the session hangs. Leave `ru-domains` / `novpn` on the ISP. Details: [install-mikrotik.md](install-mikrotik.md#openrouter-403).

In the token field paste the raw key `sk-or-v1-…`, without a `Bearer` prefix.

## Prompt cache

Clouds (via OpenRouter) discount a **repeated prompt prefix**: same system + long context, new question at the tail. This is not a full-response cache. Ollama / vLLM / LM Studio / **Ollama Cloud** are a no-op.

On **Models**: global `auto` | `off` | `on`, plus `inherit` per alias. Default **`auto`**: the gateway sets top-level `"cache_control":{"type":"ephemeral"}` **only** if the hop is Anthropic (id `anthropic/…` or catalog provider `Anthropic`) and the client did not send `cache_control` / a breakpoint. Claude first turn is a **1.25×** input write, later reads ~0.10×. The gateway does not set `"ttl":"1h"`.

`on` — the same field on any OpenRouter request without hints. `off` — inject nothing.

Client `cache_control`, `session_id`, `prompt_cache_key`, `X-Session-Id` are passed through. Agents should send a stable `X-Session-Id` / `session_id` (≤256) so OpenRouter keeps a sticky route.

**Qwen / `deepseek/deepseek-v3.2` / Gemini-explicit** do not turn on from Claude’s top-level field. You need a per-block breakpoint:

```json
{
  "model": "qwen/qwen3-max",
  "messages": [
    {
      "role": "user",
      "content": [
        { "type": "text", "text": "Reference:" },
        { "type": "text", "text": "HUGE TEXT BODY", "cache_control": { "type": "ephemeral" } },
        { "type": "text", "text": "New question" }
      ]
    }
  ]
}
```

In logs: `cached_tokens`, `$` estimate (`cache_discount` or a multiplier). `usage.cost` is billed amount, not savings. Non-stream headers: `X-MikroLLM-Cache-Tokens`, and `X-MikroLLM-Cache-Write-Tokens` on a write.

Check:

```bash
curl https://openrouter.ai/api/v1/key \
  -H "Authorization: Bearer $OPENROUTER_API_KEY"
```

## Ollama Cloud

**Cloud** models on [ollama.com](https://ollama.com) only — not the local daemon `:11434` and not `:cloud` through your GPU.

1. Key: [ollama.com/settings/keys](https://ollama.com/settings/keys).
2. Admin → kind **Ollama Cloud**. URL `https://ollama.com`. Paste the key.
3. List: `GET https://ollama.com/api/tags` (names **without** a `-cloud` suffix, e.g. `gpt-oss:120b`).
4. $/1M (in / out) from [ollama.com/pricing](https://ollama.com/pricing); if the model is missing, from `/library/<model>` (e.g. [glm-5.3](https://ollama.com/library/glm-5.3)). Tags may include a tag (`gemma4:31b`); the table is the family (`gemma4`).
5. Connect what you need to the gateway. Pull/load/delete are hidden in admin: there is nowhere to download.

Chat via MikroLLM:

- OpenAI client → gateway sends `POST https://ollama.com/v1/chat/completions`;
- Ollama client → `POST https://ollama.com/api/chat`.

Do not confuse with local **Ollama**: that talks to your LAN host and can pull/RAM. Cloud is a separate card.

```bash
curl https://ollama.com/api/tags \
  -H "Authorization: Bearer $OLLAMA_API_KEY"

curl https://ollama.com/api/chat \
  -H "Authorization: Bearer $OLLAMA_API_KEY" \
  -d '{"model":"gpt-oss:120b","messages":[{"role":"user","content":"hi"}],"stream":false}'
```

## Mixed pool

One alias can point at several servers of different kinds. LB is the same (`least_conn` / `round_robin` / `failover`). The client name is the alias; upstream gets `upstream_name`.

Make sure **the same name** exists on every host in the pool, or use separate aliases (`qwen-ollama`, `qwen-vllm`).
