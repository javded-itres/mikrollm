# Docker Compose: шлюз и vLLM

[English](../install-compose.md) · **Русский**

[`docker-compose.yml`](../../docker-compose.yml) поднимает два контейнера:

- **mikrollm** — образ `javded/mikrollm` с Docker Hub (linux/amd64 и linux/arm64). Админка и OpenAI API слушают порт **4000**.
- **vllm** — `vllm/vllm-openai:v0.18.0` с моделью **Qwen3.6**. Имя для клиентов — `qwen3.6`. Порт **8000** на хосте открыт только на `127.0.0.1`. Шлюз ходит на `http://vllm:8000`.

Весов модели в образе шлюза нет. vLLM качает их при первом старте в том `hf-cache`. Чекпоинт по умолчанию — GPTQ Int4 Qwen3.6-27B под одну Tesla V100S 32 ГБ.

## Хост

- Linux, Docker Engine и плагин Compose (`docker compose version`).
- Драйвер NVIDIA и [nvidia-container-toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html). Сервис vLLM запрашивает `driver: nvidia` и `capabilities: [gpu]`. Без toolkit это не работает.
- Одна **Tesla V100S-PCIE-32GB** (Volta, compute capability 7.0). Образ по умолчанию — `vllm/vllm-openai:v0.18.0` (linux/amd64): при сборке arch list ещё содержит 7.0. `vllm/vllm-openai:latest` ядер sm_70 не содержит и на этой карте не стартует.
- У V100 нет тензорных ядер BF16 и FP8. Чекпоинт по умолчанию — `btbtyler09/Qwen3.6-27B-GPTQ-4bit` (GPTQ Int4 от `Qwen/Qwen3.6-27B`, около 16–20 ГБ), счёт в `float16`, контекст **16384**, только текст. FP8-веса и FP8 KV на этой карте не работают. Родные 262144 токена в 32 ГБ не помещаются.

## Запуск

Из каталога этого репозитория:

```bash
cp .env.example .env
# задайте ADMIN_PASSWORD в .env — файл .env не коммитьте
docker compose up -d
```

`ADMIN_PASSWORD` обязателен. Без него Compose не запустит шлюз.

Контейнер шлюза стартует, как только создан контейнер vLLM. Загрузка весов идёт дальше. Смотреть её так:

```bash
docker compose logs -f vllm
```

Когда vLLM ответил, проверьте обе стороны:

```bash
curl -fsS http://127.0.0.1:8000/health
curl -fsS http://127.0.0.1:4000/health
```

Админка: `http://127.0.0.1:4000/admin`. Пароль — `ADMIN_PASSWORD` из `.env`, и только при первом создании базы. Поздняя правка `.env` не меняет пароль, уже записанный в том `mikrollm-data`. Перезаписать один раз:

```bash
docker compose stop mikrollm
docker run --rm -v mikrollm_mikrollm-data:/data \
  -e ADMIN_PASSWORD='новый-пароль' -e ADMIN_PASSWORD_RESET=1 \
  javded/mikrollm:v0.0.13
```

Тот же пароль запишите в `.env` и выполните `docker compose up -d`. Не оставляйте `ADMIN_PASSWORD_RESET` включённым.

Имя тома — `<проект>_mikrollm-data`. Проект по умолчанию совпадает с каталогом (`mikrollm` рядом с этим файлом). Точное имя видно в `docker volume ls`.

## Подключить модель

В compose стоит `MIKROLLM_SEED=none`. Иначе этот бинарь добавил бы демо-хосты LAN `mac-80` и `mac-82`. Seed не выполняется.

В админке:

1. **Статус** → добавить сервер, тип **vLLM**, URL `http://vllm:8000`. Ключ API не нужен. Сохранить.
2. **Модели** → `qwen3.6` → **В шлюз**.

Клиенты ходят в шлюз, не в vLLM:

```bash
curl -fsS http://127.0.0.1:4000/v1/chat/completions \
  -H "Authorization: Bearer sk-…" \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3.6","messages":[{"role":"user","content":"hello"}]}'
```

Ключ `sk-…` выпускается на вкладке **Ключи**. vLLM на loopback хоста — не публичный API.

## Настройки (`.env`)

| Переменная | По умолчанию | Что меняет |
|---|---|---|
| `ADMIN_PASSWORD` | нет, обязателен | Пароль админки при первом создании. |
| `MIKROLLM_IMAGE` | `javded/mikrollm:v0.0.13` | Образ шлюза. Тег совпадает с релизом GitHub. |
| `HF_TOKEN` | пусто | Токен Hugging Face. Веса Qwen открытые; токен поднимает лимит скачивания. |
| `VLLM_IMAGE` | `vllm/vllm-openai:v0.18.0` | Образ vLLM с CUDA arch 7.0. В `latest` ядер V100 нет. |
| `VLLM_MODEL` | `btbtyler09/Qwen3.6-27B-GPTQ-4bit` | Чекпоинт GPTQ Int4. Имя для клиентов остаётся `qwen3.6`. |
| `VLLM_MAX_MODEL_LEN` | `16384` | Контекст, который влезает в 32 ГБ рядом с весами Int4. |
| `VLLM_GPU_MEMORY_UTILIZATION` | `0.90` | Доля 32 ГБ, которую может занять vLLM. |
| `VLLM_MAX_NUM_SEQS` | `4` | Число параллельных последовательностей. |
| `VLLM_TENSOR_PARALLEL_SIZE` | `1` | Одна V100S. |

У Qwen нет официального GPTQ Int4 для Qwen3.6. `btbtyler09/Qwen3.6-27B-GPTQ-4bit` — GPTQ тех же весов `Qwen/Qwen3.6-27B` (архитектура `qwen3_5`), его поднимает vLLM 0.18. BF16 (`Qwen/Qwen3.6-27B`, около 55 ГБ) и FP8 (`Qwen/Qwen3.6-27B-FP8` и FP8 35B-A3B) на эту карту не встают и ядер под Volta не имеют.

После смены чекпоинта или контекста пересоздайте vLLM:

```bash
docker compose up -d --force-recreate vllm
```

Старые веса остаются в `hf-cache`, пока том не удалить.

## Флаги не из `.env`

Это аргументы `vllm.command` в `docker-compose.yml`. Правьте файл и пересоздайте контейнер vLLM.

- `--dtype float16` и `--quantization gptq`: у V100 нет BF16 и FP8. На Volta работает GPTQ, Marlin — нет.
- `--enforce-eager`: CUDA graph у Gated DeltaNet на sm_70 не снимается и роняет старт.
- `--language-model-only`: без vision tower, чтобы на 32 ГБ осталось место под KV.
- Нет `--kv-cache-dtype fp8`, prefix cache и chunked prefill: на Volta vLLM их не включает.
- `VLLM_ENABLE_CUDA_COMPATIBILITY=1`: образ на CUDA 12.9, так старый датацентровый драйвер всё ещё видит V100.
- Выключить thinking на сервере: `--default-chat-template-kwargs` `{"enable_thinking": false}`.

## Обновить шлюз

Новый тег в `.env`:

```bash
MIKROLLM_IMAGE=javded/mikrollm:v0.0.14
```

```bash
docker compose pull mikrollm
docker compose up -d mikrollm
```

Том `mikrollm-data` хранит пароль админки, ключи и строку сервера vLLM. Новый тег появляется на Docker Hub после пуша git-тега `v*`: [Docker Hub](https://hub.docker.com/r/javded/mikrollm).

Свой образ вместо скачивания — [install-docker.md](install-docker.md). Остальные провайдеры — [providers.md](providers.md#vllm).
