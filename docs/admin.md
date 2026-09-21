# Admin

**English** · [Русский](ru/admin.md)

URL: `http://<host>:4000/admin` (or `https://…`, [tls.md](tls.md)).

Chrome can **install MikroLLM as an app** (PWA): address-bar install icon, or **В приложение** in the header. Needs a [secure context](tls.md) (HTTPS or localhost). Scope is `/admin`.  
Login is a password (cookie `mikrollm_session`, 12 hours, HttpOnly, SameSite=Lax, path `/admin`; on HTTPS also `Secure`). After five failed attempts from one IP, a 10-minute pause. State changes only via POST with a CSRF token. A new API key is shown once and never put in the URL.

Menu: **Status · Queues · Models · Security · Keys · Chat · Log · Billing**.

## Status

Server cards: online/offline, kind (Ollama / vLLM / LM Studio), latency, on-disk models, what is in RAM now.

- **Enable / Disable** — the card stays; health is not polled; requests and catalog skip this backend. No need to delete.
- **Refresh models** on a card — force-download that provider’s catalog (no cache). OpenRouter: `/models` + `/images/models` + video (`/models?output_modalities=video`, `/videos/models`). OpenComfy: `/v1/models` + `/v1/images/models` + `/v1/videos/models`.
- On **Models**, **Refresh catalogs** does the same for every backend. Catalog list: 10 rows by default, options 20 / 50 / 100 / all.
  - Local Ollama: `http://192.168.88.82:11434`
  - Ollama Cloud: `https://ollama.com` + key from [ollama.com/settings/keys](https://ollama.com/settings/keys)
  - OpenRouter: `https://openrouter.ai/api/v1` + key from [openrouter.ai/keys](https://openrouter.ai/settings/keys)
  - vLLM: `http://192.168.88.82:8000`
  - LM Studio: `http://192.168.88.82:1234`
  - OpenComfy: `http://192.168.88.252:8788` + key from OpenComfy `keys.yaml`
- **Refresh status** — extra health poll for the chosen kind.
- Change admin password at the bottom (min 8 characters). Old sessions die immediately.
- **MCP for agents** — issue a Bearer token for `POST /mcp`. Secret shown once. Details: [mcp.md](mcp.md).

Health itself repeats every 10 seconds.

Dashboard **Request queues**: steps (busy/cap), strip “waiting / running / done” with the provider the job went to. Updates once a second.

**Hub network member** — the gateway registers itself at the compiled hub URL and long-polls for chat jobs. Mark aliases **in hub** on Models to publish them. Not P2P; no inbound port. [hub.md](hub.md).

## Queues

Hub **Status** form: concurrent slots the node accepts (chat / images / videos, default 4 / 2 / 1). Extra network jobs wait on the hub until a slot frees; they are not rejected as busy.

`/admin/queues`. The client puts one of these in `model`:

| Client sends | Example (queue name `itres`, alias `coder`) |
|---|---|
| primary alias | `coder` |
| queue name | `itres` |
| `name-alias` / `name/alias` | `itres-coder`, `itres/coder` |
| extra alias | any alias attached to the queue |

1. Create a queue (name + alias, overflow threshold, overflow alias).
2. Add steps **in order**: model alias + how many concurrent (e.g. 2 on local).
3. Extra aliases can hang on the same queue.

Routing: a free slot on step 1, else step 2… If every slot is busy the request waits in SQLite, the HTTP connection is held, and the reply always uses it. If waiters ≥ threshold, a new request goes straight to the overflow alias. Disk caps: `MIKROLLM_QUEUE_MAX_BYTES` / `MIKROLLM_QUEUE_MAX_JOBS` (old waiters dropped with 503). Upstream gets the **server model name**, not the client queue alias.

Loading a model on the GPU host (especially vLLM, which has no load API): [providers.md](providers.md).

## Security

`/admin/security`. Named filters (LiteLLM-style guardrails): system prompt, stop words, PII, prompt injection, categories, regex. Attach to an **alias**, a **queue**, and/or an **upstream model name**. The same filter on several layers still runs once. Details: [security.md](security.md).

## Models

One list from the disks of all live backends.

Catalog filters: name, **provider** (`openai/…` → OpenAI, local → Ollama / vLLM / …), **input price** per 1M tokens. OpenRouter from `GET /models` (`pricing.prompt` / `pricing.completion`). Ollama Cloud from [ollama.com/pricing](https://ollama.com/pricing) (and `/library/<model>` if missing from the table). Each row shows provider and “in / out”.

1. Check models → **To gateway** — an alias with the same name and the chosen LB policy.
2. **RAM**: `82 · in RAM` loads weights (Ollama `keep_alive: -1`, LM Studio `/api/v1/models/load`); unload takes them out. A large model can take minutes. vLLM has no buttons: the model is the `vllm serve` process.
3. **Disk**: `✕` deletes the file on Ollama only. LM Studio — its UI; vLLM — change the serve command.
4. **Download model** — Ollama `pull` or LM Studio `download`. The progress bar **survives** a page refresh: the job runs on the gateway and is stored in SQLite. After a container restart an unfinished pull resumes.
5. **Custom alias** — another name for clients (e.g. `fast` → `qwen3.8:27b-mlx`).
6. **Context** — catalog column (from the server: OpenRouter `context_length`, Ollama `/api/show`). An alias can override the token count; `0` = take from the server. Clients: `GET /v1/models` and `GET /v1/model/info` (`max_input_tokens`).
7. **Fallback model** on an alias: if upstream returns 402 / “out of credits / subscription / quota”, the gateway retries the chosen alias (the key must allow it). Chain up to 4 hops, no cycles.
8. **Prompt cache** (global above the alias table and the Cache column): `auto` injects Claude `cache_control` (first turn 1.25× write). `off` disables inject. Dashboard sums `cached_tokens` and a $ estimate. [providers.md](providers.md#prompt-cache).
9. **Hub** on an alias: **in hub** publishes it to the cloud catalog when **Hub network member** is on (chat, image, and video). Connect remote rows with **To gateway**. [hub.md](hub.md).
10. **Params** on an alias — default generation knobs injected into every proxied chat request: `think` (`true`/`false`/`low`/`medium`/`high`/`max`), `temperature`, `num_predict`, `num_ctx`, plus free-form extra JSON (`top_p`, `seed`, `x_…`). Client-sent fields win; a `lock` checkbox per knob overrides even those. Translation is per provider: Ollama `/v1/chat/completions` gets `reasoning_effort`/`max_tokens`, native `/api/chat` gets `options.*` + top-level `think` (only there `num_ctx` works), OpenRouter gets `reasoning`, vLLM/LM Studio `chat_template_kwargs`. MCP: `save_model` accepts `params` as an object.

LB policies:

| Policy | Behavior |
|---|---|
| `least_conn` | fewer active requests (default) |
| `round_robin` | round-robin |
| `failover` | first healthy in the list |

Removing an alias from the gateway ≠ deleting the file on the server.

## Keys

Clients send `Authorization: Bearer sk-…`.

- Name, RPM (`0` = no limit), list of **aliases and server models** (provider, context, price) or “All models”. Same filters as the models tab.
- The secret is shown **once** — copy it immediately.
- **Edit** — same model list on an already issued key (secret unchanged).
- Table: prefix, access pills, request count, last used, revoke.

Empty allowlist and `*` mean all models. Otherwise the alias the client put in `model` is checked.

## Chat (playground)

Like LiteLLM Playground: pick a **type** (chat / image / video) or an endpoint, then a model.

- Chat: `/v1/chat/completions`, stream, Stop, temperature, max_tokens.
- Image: `/v1/images/generations` (OpenRouter native `POST /images`; OpenComfy native, same-host file URLs inlined as `b64_json` for the chat `<img>`). Attach reference photos (`+ фото` or the right-hand panel) — they are sent as `input_image` / `input_images` / `input_references`.
- Video: `/v1/videos`, status is polled. Same reference-photo attach as for images.
- Right-hand **model parameters** panel: `GET /admin/model-params` (OpenComfy `GET /v1/models/{id}` schema). Required fields such as `input_image` show up before send; the playground blocks submit if a required reference is missing.
- Image/video models are tagged in the catalog and selector. Picking a video-only (or image-only) model switches the type so chat is not sent to OpenComfy. No API key — admin session only.
- Left **history**: first request is the task, later messages are edits. The model gets the whole thread (for OpenRouter, the previous frame too), not only the last line.
- Enter sends, Shift+Enter is a new line.
- Thinking models show the reasoning block separately.

If the model is not in RAM, the first reply can take a minute (Ollama/LM Studio load weights). vLLM already holds the model in the process.

## Log

Last API requests: key prefix, model, backend, status, latency, bytes. Up to 500 rows, rotated automatically.

On the page: text search, **2xx / 4xx / 5xx**, model, backend, key, latency band. The selection is written to the query URL — you can copy the link. Wide table scrolls; filter header is sticky.

Admin-chat requests are logged with prefix `admin` and do not bump the key counter.

## Billing

`/admin/billing`. Large `$` for the selected period — **Hour / Day / Week / Month / Year** (links, no extra buttons). Bars are spend per bucket; empty hours still appear.

The number is the sum of cloud `usage.cost`. Local Ollama is $0. “Cache saved” is a prompt-cache estimate, not an invoice.

The log keeps 500 rows; billing is hourly totals in `billing_hour` (not trimmed with the log). Existing log rows are backfilled on migrate.
