# Hub network

**English** · [Русский](ru/hub.md)

MikroLLM can join a **cloud hub** so a node behind NAT (MikroTik, home LAN, laptop) **shares selected aliases** and **uses aliases from other members** without opening inbound ports or running a VPN.

This is **not** peer-to-peer. Each gateway only makes **outbound HTTPS**. The hub is a **catalog + job relay**. Secrets of backends (`sk-or-…`, LAN Ollama) never go to the hub.

| Piece | Where | Role |
|---|---|---|
| **Client** | this repo (`internal/hubclient`) | Join, announce aliases, pull jobs, run them locally |
| **Service** | [`mikrollm_hub`](https://github.com/javded-itres/mikrollm_hub) (Go API + Next.js UI) | Catalog, long-poll, relay |
| **Public hub** | `https://hub.mikrollm.ru` | Default URL compiled into the MikroLLM binary |

Override the URL with `MIKROLLM_HUB_URL` (private/staging hub). The admin UI does not edit this env.

```
Client A (admin / sk- key)
    │  POST /v1/chat|images|videos  (model = hub alias)
    ▼
MikroLLM on node A
    │  POST https://hub.mikrollm.ru/v1/relay/{nodeB}/chat|images|videos
    ▼
Hub (catalog + wait for B)
    │  job on GET /v1/pull  (B already long-polling)
    ▼
MikroLLM on node B  →  local Ollama / OpenComfy / …
    │  POST /v1/result
    ▼
Hub returns the JSON (or video bytes) to A
```

Online in the catalog = the node is pulling (last seen **&lt; 45 s**). Announce repeats about every **15 s**; toggling share **Kicks** immediately.

---

## 1. Join the hub (your node)

1. Open admin **Status**.
2. Section **Hub network member** (`Участник hub сети`).
3. Set a short **name** (e.g. `hap-ax3`, `ams-1`, `home-mac`). This is what others see in the catalog.
4. Check **Hub network member** → Save.

The client:

- `POST /v1/register` once (node id + token stored in SQLite);
- `PUT /v1/announce` with aliases marked **in hub**;
- `GET /v1/pull` in a loop (~20 s long-poll).

Status LED: **online** / **off** / **error** (message next to it).

Uncheck the box to leave. The **node id is kept**; enable again to reuse the same registration.

Compiled URL: `https://hub.mikrollm.ru`. Private hub:

```bash
MIKROLLM_HUB_URL=http://127.0.0.1:4090
```

On RouterOS put that in the container envlist (`MIKROLLM_HUB_URL`) and restart the container. A public Caddy in front of `:4090` is enough (TLS + reverse_proxy).

Public catalog UI of the hosted hub: open `https://hub.mikrollm.ru` in a browser if the operator proxied the Next UI; the **API** is always `/v1/catalog`, `/v1/relay/…`.

---

## 2. Models vs aliases (local, then hub)

MikroLLM has two layers. Hub only sees **aliases you publish**.

### Catalog (disks / clouds)

**Models** page, table of what backends reported (Ollama tags, OpenRouter slugs, OpenComfy image/video ids).

1. **Refresh catalogs** (no cache). Filter by server, **Все с hub**, provider, image/video/chat, price.
2. Tick rows → **To gateway** / **В шлюз**. That **creates an alias** with the same name (or you pick a custom client name).
3. OpenRouter / Ollama Cloud are not downloaded; they are already remote. OpenComfy is a local ComfyUI gateway (`:8788`).

### Alias table («Alias в шлюзе»)

This is what **clients** put in `model:` (`sk-` keys, playground, queues).

| Column / control | Meaning |
|---|---|
| Client name | Alias (`coder`, `toy-image`, `minimax-hailuo-02`) |
| On server | Upstream id (`ornith-1.5:35b`, OpenRouter slug) |
| Servers | Which backends may run it (LB) |
| **Hub** pill **в hub** | This alias is **published** to the cloud catalog |
| Pill **с hap-ax3** | Alias **imported from another node** (not re-shared) |

**Custom alias:** e.g. `fast` → `qwen3.8:27b-mlx` so apps keep one name.

LB: `least_conn` (default), `round_robin`, `failover`. Fallback alias on 402/quota. Context override optional.

Details of RAM / pull / prices: [admin.md](admin.md#models).

---

## 3. Share your aliases («в hub»)

Only **local** aliases (your Ollama, OpenComfy, …) can be shared. Remote hub aliases cannot be re-shared (no nested relay).

1. **Hub network member** must be on (step 1).
2. **Models** → **Alias в шлюзе**.
3. Per row: Hub control **locally** → **in hub** (`локально` → `в hub`).
4. Or tick several rows → **В hub** / **Убрать из hub**.
5. Filter the table: all / shared / local-only / from another MikroLLM.

Within ~15 s (often sooner after Kick) the alias appears on `GET /v1/catalog` and on other members’ **Models** catalog (filter **Все с hub** or `hub · <name>`).

Chat, **image**, and **video** aliases can all be shared. The catalog `media` tag comes from the model (and name heuristics: Hailuo → video, Flux → image).

Do **not** share a huge-context chat job or a long video through a **64 MB RouterOS** container if you can run that node on a PC/VPS instead.

---

## 4. Use someone else’s model

On **your** MikroLLM (the one your app talks to):

1. **Models** → filter **Все с hub** or `hub · hap-ax3`.
2. Tick the remote row (provider = node name, host pill «онлайн»).
3. **В шлюз**. That creates a **local alias** with `HubNodeID` set. Clients see a normal name.
4. Playground, `sk-` keys, and queues treat it like any other alias.

Routing: your gateway `POST`s to the hub `/v1/relay/{node}/chat` (or `/images`, `/videos`). The **owner** node pulls the job and runs it on **its** backend.

If the peer is offline (no pull for 45 s): **502** / «peer offline». Refresh catalogs; the pill shows offline.

You can also call the hub **without** connecting an alias, using a playground catalog value `hub|<nodeId>|<alias>` — connecting is what you want for apps and keys.

---

## 5. Playground (Chat)

Type **Чат / Изображение / Видео** must match the model:

| Type | Endpoint | Typical models |
|---|---|---|
| Chat | `POST /v1/chat/completions` | Ollama, OpenRouter LLMs |
| Image | `POST /v1/images/generations` | OpenComfy `toy-image`, Flux, Gemini image |
| Video | `POST /v1/videos` then poll | Hailuo, Seedance |

Picking a video-only model **switches** the type to Video (same for image-only). Chat on Hailuo is 400: use Video.

**Reference photos** (image/video): **+ фото**, paste, or drop. Up to 6 JPEGs (downscaled). Sent as `input_references` (and `input_image` / `input_images` when the schema requires it). OpenComfy workflows that require `input_image` block send until a file is attached.

A hub image/video alias works the same way: your playground hits **your** `/admin/images`, which relays through the hub to the owner.

---

## 6. Apps, keys, queues

Issue a key on **Keys**. Allow the hub alias the same as a local one.

```bash
curl http://127.0.0.1:4000/v1/chat/completions \
  -H "Authorization: Bearer sk-…" \
  -H "Content-Type: application/json" \
  -d '{"model":"ornith-1.5:35b","messages":[{"role":"user","content":"hi"}],"stream":false}'
```

Images: `POST /v1/images/generations` with `"model":"<hub-alias>"`. Videos: `POST /v1/videos`.

**Queues:** a step may be a hub alias (after **В шлюз**). Example: local 7B → hub `hap-ax3` 35B → OpenRouter. The client still sends one alias/queue name. [admin.md](admin.md#queues).

---

## 7. Limits and timing

| Limit | Value |
|---|---|
| Chat relay body | 1 MiB, `stream=false` |
| Image/video relay body (prompt + refs) | 8 MiB |
| Binary result (clip) | 6 MiB (else **413** on a small node) |
| Relay wait | 120 s |
| Catalog online | last pull &lt; 45 s |
| Announce | every ~15 s (Kick on share/join) |
| Catalog refresh on a member | ~15 s, or **Refresh catalogs** |

RouterOS `memory-high 64M`: keep shared work small (chat, modest images). Heavy video belongs on a PC/VPS member.

---

## 8. What not to do

- Do not share **OpenRouter** keys through the hub by default (you would pay for other members). Share **local** Ollama / OpenComfy.
- Do not expect nested share (a hub alias cannot be published again).
- Do not send a **video** model as chat.
- Do not point MikroTik at a WireGuard inner IP of the hub (`10.88.97.1:4090` times out from LAN). Use `https://hub.mikrollm.ru` or the public `host:4090`.

---

## 9. Run your own hub

Repo: [`mikrollm_hub`](https://github.com/javded-itres/mikrollm_hub).

```bash
# API
go run ./cmd/hub -listen :4090 -data ./hub-data
# UI
cd web && HUB_INTERNAL_URL=http://127.0.0.1:4090 npm run dev   # :3000
```

TLS (example Caddy):

```
hub.example.com {
	encode gzip
	request_body { max_size 50MB }
	reverse_proxy 127.0.0.1:4090 {
		flush_interval -1
		transport http {
			read_timeout 180s
			write_timeout 180s
		}
	}
}
```

Members: `MIKROLLM_HUB_URL=https://hub.example.com`. Operator UI `/operator` uses `HUB_ADMIN_PASSWORD`.

---

## 10. Protocol (node → hub)

All node calls except register use `Authorization: Bearer <node token>`.

| Method | Path | Notes |
|---|---|---|
| POST | `/v1/register` | `{name}` → `{node_id, token}` |
| PUT | `/v1/announce` | `{name, aliases:[{alias, media, context}]}` |
| GET | `/v1/pull` | Long-poll ~20 s; `204` idle; `200` + `{job_id, alias, kind, ref, body}` |
| POST | `/v1/result` | `{job_id, status, body}` or `{b64, content_type}` |
| GET | `/v1/catalog` | Public. Online = seen in 45 s, not banned |
| POST | `/v1/relay/{node}/chat` | Chat JSON (`model` in body), wait ≤ 120 s |
| POST | `/v1/relay/{node}/images` | Images JSON |
| POST | `/v1/relay/{node}/videos` | Videos create |
| POST | `/v1/relay/{node}/videos/{id}` | Video status |
| POST | `/v1/relay/{node}/videos/{id}/content` | Video bytes |

The owner injects `X-MikroLLM-Hub-Relay` on the in-process `/v1/chat/completions`, `/v1/images/generations`, or `/v1/videos` call so no `sk-` is required. Only aliases with **in hub** are executed.

`kind` on a job: `chat` (default), `images`, `videos`, `videos_status`, `videos_content`.

---

## 11. Troubleshooting

| Symptom | What to check |
|---|---|
| Status **off** | Checkbox, save, `MIKROLLM_HUB_URL` reachable (HTTPS, DNS, firewall) |
| Status **error** | Message next to the LED; 401 → token cleared, re-register |
| Alias missing in catalog | **в hub** pill, wait 15 s, **Refresh catalogs** on the other node |
| 502 peer offline | Owner must stay pulling; 45 s window |
| `video models use POST /v1/videos` | Playground type **Video**, not Chat |
| Empty image in chat | OpenComfy file URL vs CSP — current gateway inlines `b64_json` |
| OpenRouter “output modalities: image, text” | Images go to `POST /images`, not chat; update the gateway |
| 413 on video | Clip &gt; 6 MiB through a small node |
| Hub image 400 on old builds | Need a gateway that relays `/images` and `/videos`, not chat-only |

Admin **Log**: backend name is the hub node; key `hub` is a relayed job on the owner.
