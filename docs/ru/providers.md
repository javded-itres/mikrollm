# Бэкенды: локальные и облачные

[English](../providers.md) · **Русский**

MikroLLM — шлюз. Бэкенд может быть **локальным процессом** в LAN или **облачным API** (OpenRouter, Ollama Cloud) — без своего GPU-сервера. В админке **Статус → Добавить сервер** укажите тип, URL и при необходимости API-ключ.

| Тип | Куда ходить | Чат | Список моделей | Скачать / RAM |
|---|---|---|---|---|
| **Ollama** (локальный) | `http://<хост>:11434` | `/api/chat`, `/v1/chat/completions` | `/api/tags`, `/api/ps` | да, из админки |
| **Ollama Cloud** | `https://ollama.com` | `/api/chat`, `/v1/chat/completions` | `/api/tags` | нет: модели уже в облаке |
| **OpenRouter** | `https://openrouter.ai/api/v1` | `/chat/completions` | `/models` | нет: модели уже в облаке |
| **vLLM** | `http://<хост>:8000` | `/v1/chat/completions` | `/health`, `/v1/models` | нет API: модель = процесс |
| **LM Studio** | `http://<хост>:1234` | `/v1/chat/completions` | `/api/v1/models` | да, из админки |
| **OpenComfy** | `http://<хост>:8788` | image chat shim | `/health`, `/v1/models` | нет: workflow на GPU-машине |

Клиенты всегда ходят в MikroLLM (`/v1/chat/completions`). Если клиент шлёт `/api/chat`, а бэкенд не Ollama / Ollama Cloud, шлюз переписывает путь на OpenAI-совместимый чат провайдера.

Облачные бэкенды требуют **HTTPS**. Образ RouterOS кладёт корневые CA в контейнер (`ca-certificates`). На обычном Linux/macOS используются системные сертификаты.

## Ollama

1. Поставьте [Ollama](https://ollama.com) на машину с моделью.
2. В админке: тип **Ollama**, URL `http://<хост>:11434`.
3. На странице **Модели** скачайте (`llama3.2`, `qwen2.5:32b`, …), загрузите в RAM, подключите к шлюзу.

Подробнее: [admin.md](admin.md).

## vLLM

vLLM поднимает **одну** модель на время жизни процесса. Сменить веса = остановить сервер и запустить с другим id. Через HTTP это сделать нельзя — поэтому в админке нет кнопок «скачать» / «в RAM».

### Установка

Нужны Python 3.9+, GPU NVIDIA (CUDA) или поддерживаемый бэкенд. Документация: [docs.vllm.ai](https://docs.vllm.ai/en/latest/getting_started/installation.html).

```bash
pip install vllm
# или
uv pip install vllm
```

Docker:

```bash
docker run --gpus all --ipc=host -p 8000:8000 \
  vllm/vllm-openai:latest \
  --model Qwen/Qwen2.5-7B-Instruct \
  --host 0.0.0.0 --port 8000
```

### Загрузить модель на сервер

Имя модели — id с Hugging Face (`org/name`).

```bash
vllm serve Qwen/Qwen2.5-7B-Instruct \
  --host 0.0.0.0 \
  --port 8000
```

С ключом (тогда тот же токен впишите в MikroLLM):

```bash
vllm serve Qwen/Qwen2.5-7B-Instruct \
  --host 0.0.0.0 --port 8000 \
  --api-key supersecret
```

Закрытая модель на Hugging Face:

```bash
export HF_TOKEN=hf_...
vllm serve meta-llama/Meta-Llama-3.1-8B-Instruct \
  --host 0.0.0.0 --port 8000
```

Первый запуск качает веса в кэш Hugging Face (`~/.cache/huggingface`). Это и есть «загрузка модели на сервер».

Проверка:

```bash
curl http://127.0.0.1:8000/health
curl http://127.0.0.1:8000/v1/models
```

### Сменить модель

```bash
# остановить процесс vllm / контейнер
vllm serve mistralai/Mistral-7B-Instruct-v0.3 --host 0.0.0.0 --port 8000
```

В MikroLLM после смены нажмите **Обновить статусы** — в каталоге появится новое имя.

### Подключить к шлюзу

Админка → **Статус** → тип **vLLM**, URL `http://<хост>:8000`, токен если задавали `--api-key`. Дальше **Модели → В шлюз**.

systemd-пример:

```ini
[Service]
ExecStart=/usr/bin/vllm serve Qwen/Qwen2.5-7B-Instruct --host 0.0.0.0 --port 8000
Restart=on-failure
```

## LM Studio

[LM Studio](https://lmstudio.ai) — десктоп (macOS / Windows / Linux) с локальным HTTP-сервером. С версии 0.4 есть REST `/api/v1/*`: список, download, load, unload.

### Включить сервер

1. Откройте LM Studio → **Developer**.
2. Start server, bind `0.0.0.0`, порт `1234` (чтобы шлюз в LAN видел хост).
3. Если включили API token — скопируйте его в поле «Токен» MikroLLM.

Headless (без окна): см. [Run as a service](https://lmstudio.ai/docs/developer/core/headless).

Проверка:

```bash
curl http://127.0.0.1:1234/v1/models
curl http://127.0.0.1:1234/api/v1/models \
  -H "Authorization: Bearer $LM_API_TOKEN"
```

### Скачать и загрузить из MikroLLM

В админке тип **LM Studio**, URL `http://<хост>:1234`.

- **Скачать**: id из каталога LM Studio (`ibm/granite-4-micro`) или ссылка Hugging Face. Прогресс как у Ollama pull.
- **В RAM / выгрузить**: кнопки на **Моделях** и карточке сервера. API: `POST /api/v1/models/load` и `/unload`.
- Файл с диска админка не удаляет — уберите модель в UI LM Studio.

Можно по-прежнему грузить модель руками в LM Studio (Chat / Developer → load). Шлюз увидит её после **Обновить**.

### Если сервер старый

До v1 REST LM Studio отдаёт только `/v1/models` (уже загруженные). Тогда MikroLLM покажет их как «в RAM», а кнопок download/load может не быть — обновите LM Studio.

## OpenRouter

Прямое облако: MikroLLM ходит на `https://openrouter.ai/api/v1`, локальный LLM-сервер не нужен.

1. Ключ: [openrouter.ai/keys](https://openrouter.ai/settings/keys) (`sk-or-v1-…`).
2. Админка → **Статус** → тип **OpenRouter**. URL подставится сам (`https://openrouter.ai/api/v1`). Вставьте ключ.
3. **Обновить статусы** — каталог с OpenRouter (`GET /models`).
4. **Модели → В шлюз** для нужных id (`openai/gpt-4o-mini`, `anthropic/claude-sonnet-4`, …).

Чат: `POST https://openrouter.ai/api/v1/chat/completions`, `Authorization: Bearer <ключ>`.

Скачивать и грузить в RAM нечего — веса у провайдера. На RouterOS нужен образ с CA-сертификатами (текущий Dockerfile их копирует).

**403 Forbidden.** OpenRouter (Cloudflare) часто отвечает 403 на API с IP РФ. Ключ при этом может быть верным: `GET /api/v1/key` с VPN даёт 200, с ISP — 403. Контейнер MikroLLM живёт в `192.168.254.0/24` и **не** попадает под правило «LAN 88 → AMS WG». Нужно отдельное mark-routing на `192.168.254.5` в таблицу `vpn` и src-nat на свободный адрес LAN (не `.1` роутера), иначе ответ VPN приходит на INPUT и сессия висит. `ru-domains` / `novpn` оставьте на ISP. Подробнее: [install-mikrotik.md](install-mikrotik.md#openrouter-403).

В поле токена вставляйте сам ключ `sk-or-v1-…`, без префикса `Bearer`.

## Prompt cache

Облака (через OpenRouter) скидывают **повтор префикса** промпта: тот же system + длинный контекст, новый вопрос в хвосте. Это не кэш готового ответа. Ollama / vLLM / LM Studio / **Ollama Cloud** — no-op.

На странице **Модели**: глобально `auto` | `off` | `on`, на alias — ещё `inherit`. Default **`auto`**: шлюз ставит top-level `"cache_control":{"type":"ephemeral"}` **только** если hop — Anthropic (id `anthropic/…` или провайдер каталога `Anthropic`) и клиент сам не прислал `cache_control` / breakpoint. Первый ход Claude — запись **1.25×** входной цены, чтение дальше ~0.10×. `"ttl":"1h"` шлюз не ставит.

`on` — то же поле на любой OpenRouter-запрос без hints. `off` — ничего не инжектить.

Клиентский `cache_control`, `session_id`, `prompt_cache_key`, `X-Session-Id` прокидываются. Агентам лучше слать стабильный `X-Session-Id` / `session_id` (≤256), чтобы OpenRouter держал sticky route.

**Qwen / `deepseek/deepseek-v3.2` / Gemini-explicit** сами не включаются от top-level поля Claude. Нужен per-block breakpoint:

```json
{
  "model": "qwen/qwen3-max",
  "messages": [
    {
      "role": "user",
      "content": [
        { "type": "text", "text": "Справка:" },
        { "type": "text", "text": "HUGE TEXT BODY", "cache_control": { "type": "ephemeral" } },
        { "type": "text", "text": "Новый вопрос" }
      ]
    }
  ]
}
```

В логах: `cached_tokens`, оценка `$` (`cache_discount` или мультипликатор). `usage.cost` — сколько списали, не экономия. Non-stream заголовки: `X-MikroLLM-Cache-Tokens`, при записи `X-MikroLLM-Cache-Write-Tokens`.

Проверка:

```bash
curl https://openrouter.ai/api/v1/key \
  -H "Authorization: Bearer $OPENROUTER_API_KEY"
```

## Ollama Cloud

Только **облачные** модели на [ollama.com](https://ollama.com) — не локальный демон `:11434` и не `:cloud` через ваш GPU.

1. Ключ: [ollama.com/settings/keys](https://ollama.com/settings/keys).
2. Админка → тип **Ollama Cloud**. URL `https://ollama.com`. Вставьте ключ.
3. Список: `GET https://ollama.com/api/tags` (имена **без** суффикса `-cloud`, например `gpt-oss:120b`).
4. Цены $/1M (вход / выход) — с [ollama.com/pricing](https://ollama.com/pricing); если модели нет в таблице — со страницы `/library/<модель>` (как у [glm-5.3](https://ollama.com/library/glm-5.3)). Имена в tags могут быть с тегом (`gemma4:31b`), в таблице — семейство (`gemma4`).
5. Подключите нужные в шлюз. Pull/load/delete в админке скрыты: качать некуда.

Чат с MikroLLM:

- клиент OpenAI → шлюз шлёт `POST https://ollama.com/v1/chat/completions`;
- клиент Ollama → `POST https://ollama.com/api/chat`.

Не путайте с локальным типом **Ollama**: тот ходит на ваш хост в LAN и умеет pull/RAM. Cloud — отдельная карточка.

```bash
curl https://ollama.com/api/tags \
  -H "Authorization: Bearer $OLLAMA_API_KEY"

curl https://ollama.com/api/chat \
  -H "Authorization: Bearer $OLLAMA_API_KEY" \
  -d '{"model":"gpt-oss:120b","messages":[{"role":"user","content":"hi"}],"stream":false}'
```

## OpenComfy

Локальный шлюз **картинок и видео** перед [ComfyUI](https://github.com/comfyanonymous/ComfyUI). Те же формы, что у LiteLLM/OpenAI: `POST /v1/images/generations`, `POST /v1/videos`, опрос `GET /v1/videos/{id}`. Это не чат-LLM.

1. OpenComfy рядом с ComfyUI (`opencomfy -config …`, порт `:8788`).
2. Админка → **Добавить сервер** → тип **OpenComfy**. URL `http://<gpu>:8788` (**без** `/v1`). Ключ из `keys.yaml` OpenComfy (`sk-…`).
3. **Обновить каталоги**. Модели с тегами image/video из `/v1/models`, `/v1/images/models`, `/v1/videos/models`.
4. **Модели** → подключить нужные имена (`flux-dev`, `minimax-hailuo-02`, …) как alias — как Flux/Sora с OpenRouter.

Клиенты по-прежнему ходят в MikroLLM:

```bash
curl http://<mikrollm>:4000/v1/images/generations \
  -H "Authorization: Bearer sk-…" \
  -d '{"model":"toy-image","prompt":"красный куб"}'

curl http://<mikrollm>:4000/v1/videos \
  -H "Authorization: Bearer sk-…" \
  -d '{"model":"minimax-hailuo-02","prompt":"волны","seconds":"6"}'
```

OpenComfy отдаёт URL файла на `:8788`. MikroLLM подтягивает same-host URL в `b64_json` (до 4 МиБ), иначе CSP админки (`img-src 'self' data: blob:`) прячет картинку в чате.

В playground выберите модель и тип **image** / **video**, не chat. Справа панель параметров (схема OpenComfy). Агент может ходить в OpenComfy `POST /mcp` (tool `generate_<id>` на каждый workflow). Видео-модели не чат-бэкенды.

Pull/load/delete скрыты: веса и workflow остаются на хосте ComfyUI.

## Смешанный пул

Один alias может указывать на несколько серверов разного типа. Балансировка та же (`least_conn` / `round_robin` / `failover`). Имя модели для клиента — alias; на апстрим уходит `upstream_name`.

Убедитесь, что **одно и то же имя** есть на всех хостах пула, либо заведите отдельные alias (`qwen-ollama`, `qwen-vllm`).
