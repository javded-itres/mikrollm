# Docker Compose: gateway and vLLM

**English** · [Русский](ru/install-compose.md)

[`docker-compose.yml`](../docker-compose.yml) starts two containers:

- **mikrollm** — the published image `javded/mikrollm` (linux/amd64 and linux/arm64). Admin and the OpenAI API listen on port **4000**.
- **vllm** — `vllm/vllm-openai:v0.18.0` serving **Qwen3.6** as the client model name `qwen3.6`. The process port **8000** is bound only to `127.0.0.1` on the host. The gateway reaches it as `http://vllm:8000`.

The gateway image does not contain the model weights. vLLM downloads them on first start into the `hf-cache` volume. The default checkpoint is GPTQ Int4 of Qwen3.6-27B, sized for one Tesla V100S 32 GB.

## Host

- Linux with Docker Engine and the Compose plugin (`docker compose version`).
- NVIDIA driver and [nvidia-container-toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html). The vLLM service requests `driver: nvidia` with `capabilities: [gpu]`. That does not work without the toolkit.
- One **Tesla V100S-PCIE-32GB** (Volta, compute capability 7.0). The default image is `vllm/vllm-openai:v0.18.0` (linux/amd64). Its build still compiles CUDA arch 7.0. `vllm/vllm-openai:latest` does not, and it will not start on this card.
- V100 has no BF16 or FP8 tensor cores. The default checkpoint is `btbtyler09/Qwen3.6-27B-GPTQ-4bit` (GPTQ Int4 of `Qwen/Qwen3.6-27B`, about 16–20 GB) served as `float16`, context **16384**, text only. FP8 weights and an FP8 KV cache do not run here. The native 262144 window does not fit in 32 GB.

## Start

From a checkout of this repo:

```bash
cp .env.example .env
# set ADMIN_PASSWORD in .env — do not commit .env
docker compose up -d
```

`ADMIN_PASSWORD` is required. Compose refuses to start the gateway if it is empty.

The gateway container starts as soon as the vLLM container has been created. Weight download and model load continue after that. Watch them with:

```bash
docker compose logs -f vllm
```

When vLLM answers, check both sides:

```bash
curl -fsS http://127.0.0.1:8000/health
curl -fsS http://127.0.0.1:4000/health
```

Admin: `http://127.0.0.1:4000/admin`. The password is `ADMIN_PASSWORD` from `.env` on the first start only. Later edits of `.env` do not replace a password already stored in the `mikrollm-data` volume. To overwrite it once:

```bash
docker compose stop mikrollm
docker run --rm -v mikrollm_mikrollm-data:/data \
  -e ADMIN_PASSWORD='new-password' -e ADMIN_PASSWORD_RESET=1 \
  javded/mikrollm:v0.0.13
```

Write the same password into `.env`, then `docker compose up -d`. Do not leave `ADMIN_PASSWORD_RESET` set.

The volume name is `<project>_mikrollm-data`. The project name defaults to the directory (`mikrollm` next to this file). `docker volume ls` shows the real name.

## Connect the model

`MIKROLLM_SEED=none` in the compose file. This image would otherwise insert the LAN demo hosts `mac-80` and `mac-82`. Nothing is seeded.

In admin:

1. **Status** → add a server, kind **vLLM**, URL `http://vllm:8000`. No API key. Save.
2. **Models** → find `qwen3.6` → **To gateway**.

Clients then call the gateway, not vLLM:

```bash
curl -fsS http://127.0.0.1:4000/v1/chat/completions \
  -H "Authorization: Bearer sk-…" \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3.6","messages":[{"role":"user","content":"hello"}]}'
```

Issue `sk-…` under **Keys**. vLLM on the host loopback is not the public API.

## Settings (`.env`)

| Variable | Default | What it changes |
|---|---|---|
| `ADMIN_PASSWORD` | none, required | Admin password on first create. |
| `MIKROLLM_IMAGE` | `javded/mikrollm:v0.0.13` | Gateway image. The tag matches a GitHub release. |
| `HF_TOKEN` | empty | Hugging Face token. The default Qwen weights are public; a token raises the download rate limit. |
| `VLLM_IMAGE` | `vllm/vllm-openai:v0.18.0` | vLLM image with CUDA arch 7.0. `latest` has no V100 kernels. |
| `VLLM_MODEL` | `btbtyler09/Qwen3.6-27B-GPTQ-4bit` | GPTQ Int4 checkpoint. The name clients send stays `qwen3.6`. |
| `VLLM_MAX_MODEL_LEN` | `16384` | Context that fits in 32 GB next to the Int4 weights. |
| `VLLM_GPU_MEMORY_UTILIZATION` | `0.90` | Fraction of the 32 GB vLLM may use. |
| `VLLM_MAX_NUM_SEQS` | `4` | Parallel sequences. |
| `VLLM_TENSOR_PARALLEL_SIZE` | `1` | One V100S. |

Qwen does not publish a GPTQ Int4 of Qwen3.6. `btbtyler09/Qwen3.6-27B-GPTQ-4bit` is a GPTQ of the same `Qwen/Qwen3.6-27B` weights (`qwen3_5`), which vLLM 0.18 can load. BF16 (`Qwen/Qwen3.6-27B`, about 55 GB) and FP8 (`Qwen/Qwen3.6-27B-FP8`, and the 35B-A3B FP8 checkpoint) do not fit this card and do not have Volta kernels.

After a checkpoint or context change, recreate vLLM:

```bash
docker compose up -d --force-recreate vllm
```

The old weights stay in `hf-cache` until you delete that volume.

## Flags that are not in `.env`

These are arguments in `docker-compose.yml` under `vllm.command`. Edit the file, then recreate the vLLM container.

- `--dtype float16` and `--quantization gptq`: V100 has no BF16 or FP8. GPTQ is the Volta path; Marlin is not.
- `--enforce-eager`: Gated DeltaNet CUDA graphs fail to capture on sm_70.
- `--language-model-only`: skips the vision tower so the 32 GB card has room for KV.
- No `--kv-cache-dtype fp8`, no prefix caching, no chunked prefill. vLLM does not enable those on Volta.
- `VLLM_ENABLE_CUDA_COMPATIBILITY=1`: the image is CUDA 12.9; this lets an older datacenter driver drive the V100.
- Thinking off on the server: `--default-chat-template-kwargs` `{"enable_thinking": false}`.

## Upgrade the gateway

Put the new tag in `.env`:

```bash
MIKROLLM_IMAGE=javded/mikrollm:v0.0.14
```

```bash
docker compose pull mikrollm
docker compose up -d mikrollm
```

The `mikrollm-data` volume keeps the admin password, keys, and the vLLM server row. A new tag is published when a `v*` git tag is pushed: [Docker Hub](https://hub.docker.com/r/javded/mikrollm).

Building a local image instead of pulling is [install-docker.md](install-docker.md). Providers in general: [providers.md](providers.md#vllm).
