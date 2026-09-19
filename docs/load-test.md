# MikroLLM load report (matrix 1/2/4/8)

**English** · [Русский](ru/load-test.md)

Field stand, **2026-09-18**. Not MLPerf.

| | |
|---|---|
| Gateway | hAP ax³ container, RouterOS 7.22, `http://192.168.88.1:4000`, veth `192.168.254.5`, `memory-high=64M` |
| Local | `ornith-1.5:35b` in RAM Ollama on Mac `.80` |
| Cloud 128k–512k | `glm-5.3-flash` (Ollama Cloud, ISP / `ru-domains`) |
| Cloud 1M | `deepseek-v4.1-flash`, `glm-5.3-flash` (Ollama Cloud); `nvidia/nemotron-3-ultra-550b-a55b:free` (OpenRouter, AMS) |
| Chat | `stream:false`, `max_tokens:8`, `temperature:0`, unique nonce |
| Parallel | one wave N = c ∈ {1, 2, 4, 8} |
| Client timeout | 480 s local, 540–600 s cloud |

RAM/CPU — container `memory-current` / `cpu-usage` every ~0.35 s. Idle baseline **~26 MB**.

---

## 1. Local 35B — 32k / 64k / 128k

The model serializes about **two** slots: at c=4 and c=8 wall grows in steps ~67 s (32k) / ~190 s (64k).

| ctx | c | ok/N | prompt tok | body | wall | p50 | RPS | RAM after | RAM max |
|---|---:|---|---:|---:|---:|---:|---:|---:|---:|
| 32k | 1 | 1/1 | 32 236 | 156 KB | 67 s | 67 s | 0.015 | 27 | 27 |
| 32k | 2 | 2/2 | 32 236 | 156 KB | 69 s | 69 s | 0.029 | 28 | 28 |
| 32k | 4 | 4/4 | 32 237 | 156 KB | 134 s | 132 s | 0.030 | 32 | 32 |
| 32k | 8 | 8/8 | 32 236 | 156 KB | 322 s | 193 s | 0.025 | 30 | **36** |
| 64k | 1 | 1/1 | 64 443 | 312 KB | 189 s | 189 s | 0.005 | 29 | 36 |
| 64k | 2 | 2/2 | 64 442 | 312 KB | 379 s | 379 s | 0.005 | 30 | 36 |
| 64k | 4 | 4/4 | 64 444 | 312 KB | 380 s | 379 s | 0.011 | 31 | 36 |
| 64k | 8 | **4/8** | 64 443 | 312 KB | 480 s | 378 s | 0.008 | 37 | **39** |
| 128k | 1 | **0/1** | — | 623 KB | 480 s timeout | — | — | 35 | 39 |
| 128k | 2–8 | skip | | | | | | | |

128k on 35B did not fit in 480 s (extrapolation ~6–7 min per request). Bottleneck is Mac/MLX, not the router (RAM 27–39 MB).

---

## 2. Cloud — 128k / 256k / 512k (`glm-5.3-flash`)

Ollama Cloud via ISP. All bodies JSON.

| ctx | c | ok/N | prompt tok | body | wall | p50 | RPS | RAM after | RAM max run |
|---|---:|---|---:|---:|---:|---:|---:|---:|---:|
| 128k | 1 | 1/1 | 128 757 | 0.59 MB | 6.0 s | 6.0 s | 0.17 | 35 | 39 |
| 128k | 2 | 2/2 | 128 757 | 0.59 MB | 9.8 s | 9.8 s | 0.20 | 39 | 39 |
| 128k | 4 | 4/4 | 128 758 | 0.59 MB | 17 s | 14 s | 0.24 | 42 | 42 |
| 128k | 8 | 8/8 | 128 756 | 0.59 MB | 32 s | 21 s | **0.25** | 51 | 51 |
| 256k | 1 | 1/1 | 257 483 | 1.19 MB | 10 s | 10 s | 0.10 | 36 | 51 |
| 256k | 2 | 2/2 | 257 482 | 1.19 MB | 19 s | 19 s | 0.11 | 42 | 51 |
| 256k | 4 | 4/4 | 257 482 | 1.19 MB | 34 s | 27 s | 0.12 | 56 | 59 |
| 256k | 8 | **5/8** | 257 485 | 1.19 MB | 541 s | 450 s | 0.009 | **73** | **76** |
| 512k | 1 | 1/1 | 514 937 | 2.37 MB | 332 s | 332 s | 0.003 | 42 | 76 |
| 512k | 2 | 2/2 | 514 934 | 2.37 MB | 39 s | 39 s | 0.052 | 53 | 76 |
| 512k | 4 | **1/4** | 514 936 | 2.37 MB | 542 s | 410 s | 0.002 | **73** | **76** |
| 512k | 8 | skip | RAM / errors at c=4 | | | | | | |

c=8 at 256k and c=4 at 512k: container above `memory-high=64M`, some 540 s timeouts. c=2 at 512k after warmup — 39 s, all 200.

---

## 3. Cloud 1M

Body ~4.0–5.0 MB JSON. c≥2/4/8 the script cut when RAM ≥70 MB or after a fail at smaller c.

| Model | c | ok/N | prompt tok | body | wall | RAM after |
|---|---:|---|---:|---:|---:|---:|
| `deepseek-v4.1-flash` | 1 | **1/1** | **1 019 945** | 4.03 MB | **798 s** | 41 (peak 77) |
| `deepseek-v4.1-flash` | 2 | **0/2** | — | 4.03 MB | 602 s timeout | **75** |
| `deepseek-v4.1-flash` | 4–8 | skip | RAM / fail c=2 | | | |
| `glm-5.3-flash` | 1 | **0/1** | — | 4.75 MB | 600 s timeout | **77** |
| `glm-5.3-flash` | 2–8 | skip | | | | |
| `nvidia/nemotron-3-ultra-550b-a55b:free` | 1 | **0/1** | — | 4.73 MB | 977 s timeout | **84** |
| `nvidia/…:free` | 2–8 | skip | | | | |

The same glm 1M **c=1 in an earlier run** (cold container ~26 MB): 1 034 343 tok., 5.0 MB, **465 s**, 200. In this run the container was already “inflated” after 512k/1M deepseek — glm and NVIDIA missed 600 s.

---

## 4. Load on the router

Whole run: **17 694** samples (~2 h).

| Mode | Container RAM | CPU | Note |
|---|---|---|---|
| Idle | 26 MB | ~0 | |
| Local 32–64k, c≤8 | 27–39 MB | ≤1.1 | headroom to 64 |
| Cloud 128k c=8 | 51 MB | 1.9 | comfortable |
| Cloud 256k c=4 | 56–59 MB | 1.9 | at the 64 cap |
| Cloud 256k c=8 / 512k c=4 / 1M | **73–84 MB** | up to **25** | above `memory-high=64M` |
| End of run | **83.6 MB** | avg 7.7 | process alive, `/health` 200 |

CPU peak 25 is copying 2–5 MB bodies, not inference.

---

## 5. Gateway RAM forecast

Observation: **base ≈ 26 MB** + in-flight body buffers.

Rough peak (Go JSON, 2–3 body copies):

`RAM ≈ 26 + 8 × c × MB_body`

| Context | JSON body | c=1 | c=2 | c=4 | c=8 |
|---|---:|---|---|---|---|
| 32k | 0.15 MB | 28 MB | 28 | 32 | 36 — **64 is enough** |
| 64k | 0.30 MB | 30 | 30 | 32 | 39 — **64 is enough** |
| 128k | 0.59 MB | 35 | 39 | 42 | 51 — **64 is enough** |
| 256k | 1.2 MB | 36 | 42 | **56–59** | **76 — 64 is tight** |
| 512k | 2.4 MB | 42 | 53 | **73 — 64 is tight** | ~90 (forecast) |
| 1M | 4.0–5.0 MB | **75–84** | timeout + ~75 | 502 / OOM zone | do not batch |

`memory-high` on ax³:

| Scenario | memory-high |
|---|---|
| Local + cloud ≤128k, c≤8 | **64M** (current) |
| Cloud 256k, c≤4 | **96M** |
| Cloud 256k c=8 or 512k c=2 | **96–128M** |
| 1M, one request | **128M** |
| 1M, two or more at once | **192M+** or do not send as a batch |

35B/cloud inference does not live in router RAM — only HTTP bodies and SQLite.

---

## Summary

- The router holds **128k × 8** cloud, all 200, at ~51 MB.
- **256k × 4** is the working max on 64 MB; **256k × 8** and **512k × 4** already hit the limit and time out.
- **1M × 1** works on deepseek (~13 min, peak ~77 MB). glm/NVIDIA 1M in this run timed out on a “inflated” process; glm 1M on a fresh container was 465 s / 200 earlier.
- **1M × 2** — both timeout, RAM ~75 MB.
- Local 35B: 32k stable to c=8; 64k c=8 — half timeout; 128k does not finish in 480 s (GPU, not MikroTik).

Raw: `/tmp/mikrollm-bench-matrix.jsonl`.
