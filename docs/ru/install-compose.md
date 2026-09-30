# Docker Compose: шлюз и vLLM

[English](../install-compose.md) · **Русский**

[`docker-compose.yml`](../../docker-compose.yml) поднимает два контейнера:

- **mikrollm** — образ `javded/mikrollm` с Docker Hub (linux/amd64 и linux/arm64). Админка и OpenAI API слушают порт **4000**.
- **vllm** — `vllm/vllm-openai` с моделью **Qwen3.6**. Имя для клиентов — `qwen3.6`. Порт **8000** на хосте открыт только на `127.0.0.1`. Шлюз ходит на `http://vllm:8000`.

Весов модели в образе шлюза нет. vLLM качает их при первом старте (около 18 ГБ у чекпоинта FP8 по умолчанию) в том `hf-cache`.

## Хост

- Linux, Docker Engine и плагин Compose (`docker compose version`).
- Драйвер NVIDIA и [nvidia-container-toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html). Сервис vLLM запрашивает `driver: nvidia` и `capabilities: [gpu]`. Без toolkit это не работает.
- Образ vLLM по умолчанию — **linux/amd64**. Для контекста 32768 хватает одной карты на 24–48 ГБ. Родное окно модели — 262144, вместе с KV это около 80 ГБ.
- Blackwell (SM100 / SM120): `VLLM_IMAGE=vllm/vllm-openai:cu130-nightly`. Qwen3.6 нужен vLLM 0.17 или новее (на остальных картах достаточно `latest`).

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
| `VLLM_IMAGE` | `vllm/vllm-openai:latest` | Образ vLLM. На Blackwell — `cu130-nightly`. |
| `VLLM_MODEL` | `Qwen/Qwen3.6-35B-A3B-FP8` | Чекпоинт. Имя, которое шлют клиенты, остаётся `qwen3.6`. |
| `VLLM_MAX_MODEL_LEN` | `32768` | Контекст. `262144` — родное окно, нужна карта больше. |
| `VLLM_GPU_MEMORY_UTILIZATION` | `0.90` | Доля памяти GPU, которую может занять vLLM. |
| `VLLM_MAX_NUM_SEQS` | `8` | Число параллельных последовательностей. |
| `VLLM_TENSOR_PARALLEL_SIZE` | `1` | На сколько GPU режутся веса. |

Другие чекпоинты (меняется только `VLLM_MODEL`):

| Чекпоинт | Железо |
|---|---|
| `Qwen/Qwen3.6-35B-A3B` | BF16, 2×H100 или 1×H200 |
| `Qwen/Qwen3.6-27B-FP8` | плотная 27B, от ~40 ГБ |
| `Qwen/Qwen3.6-27B-GPTQ-Int4` | плотная 27B, одна карта 24 ГБ |

После смены чекпоинта или контекста пересоздайте vLLM:

```bash
docker compose up -d --force-recreate vllm
```

Старые веса остаются в `hf-cache`, пока том не удалить.

## Флаги не из `.env`

Это аргументы `vllm.command` в `docker-compose.yml`. Правьте файл и пересоздайте контейнер vLLM.

- CUDA graph больше mamba-кэша: добавьте `--max-cudagraph-capture-size` `64`.
- Только текст (больше места под KV, без картинок): вместо `--mm-encoder-tp-mode` / `--mm-processor-cache-type` поставьте `--language-model-only`.
- Ниже задержка, один клиент: `--speculative-config` `{"method":"mtp","num_speculative_tokens":2}`.
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
