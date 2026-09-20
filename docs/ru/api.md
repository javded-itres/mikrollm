# HTTP API

[English](../api.md) · **Русский**

База: `http://<хост>:4000` или `https://<хост>:4000`, если включён TLS ([tls.md](tls.md)).

Ключ: заголовок `Authorization: Bearer sk-…` или `X-Api-Key: sk-…`.

Без ключа (кроме health/ready и админки) — `401`. Модель не из allowlist ключа — `403`. Превышен RPM — `429`.

## Эндпоинты

| Метод | Путь | Авторизация | Назначение |
|---|---|---|---|
| GET | `/health` | нет | процесс жив |
| GET | `/ready` | нет | есть хотя бы один живой бэкенд |
| GET | `/v1/models` | ключ | список alias; `context_length` / `max_input_tokens` |
| GET | `/v1/model/info` | ключ | как LiteLLM: `model_info.max_input_tokens` |
| GET | `/model/info` | ключ | то же, без префикса `/v1` |
| POST | `/v1/chat/completions` | ключ | OpenAI Chat Completions, в т.ч. `stream: true`. Optional `X-Session-Id` (на OpenRouter). Non-stream: `X-MikroLLM-Cache-Tokens` |
| POST | `/v1/images/generations` | ключ | OpenAI Images API. OpenRouter: нативный `POST /images`. OpenComfy / OpenAI-бэкенды: нативный `/v1/images/generations`; same-host `data[].url` встраивается как `b64_json` |
| POST | `/v1/videos` | ключ | OpenAI Videos API (Sora-style). `GET /v1/videos/{id}` и `/content` — статус и файл (`?model=` если id неизвестен шлюзу) |
| POST | `/api/chat` | ключ | Ollama `/api/chat` |
| GET | `/api/tags` | ключ | имена моделей |
| GET | `/admin` | cookie | HTML-админка |
| POST | `/mcp` | MCP-токен | MCP JSON-RPC (модели, очереди, ключи, логи). [mcp.md](mcp.md) |

`/` редиректит на `/admin`.

## Chat Completions

Тело как у OpenAI. Поле `model` — **alias шлюза** или имя модели на бэкенде, если alias нет, но health её видит.

```bash
curl http://192.168.88.1:4000/v1/chat/completions \
  -H "Authorization: Bearer sk-…" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.8:27b-mlx",
    "messages": [{"role": "user", "content": "привет"}],
    "stream": false,
    "temperature": 0.7,
    "max_tokens": 512
  }'
```

Стрим — SSE (`data: {…}` / `data: [DONE]`), как у OpenAI. Шлюз проксирует байты апстрима, не буферизуя ответ целиком.

Шлюз подменяет `model` на `upstream_name` alias, если они различаются.

**Prompt cache (OpenRouter).** Клиент может слать `X-Session-Id` и поля `session_id` / `cache_control` / `prompt_cache_key` — они доходят до апстрима. При `prompt_cache=auto` шлюз сам ставит top-level `cache_control` на Claude. Non-stream ответ: `X-MikroLLM-Cache-Tokens` (и `X-MikroLLM-Cache-Write-Tokens`, если запись > 0). Стрим — токены только в `usage` последнего SSE и в логе. Fallback на другую модель не переносит `cache_control`. Подробнее: [providers.md](providers.md#prompt-cache).

Если `model` — **очередь** (её alias, имя, `имя-alias`, `имя/alias` или extra alias), запрос идёт по шагам. Пока слоты заняты, соединение ждёт; ответ всегда в это же соединение. Переполнение (ждущих ≥ N) уводит на запасной alias; если запасного нет — `503`. Заголовок `X-MikroLLM-Queue` не обязателен — смотрите ленту на дашборде. Тело апстриму переписывается: в `"model"` подставляется имя модели выбранного шага, не клиентский alias.

Если у alias задана **запасная модель** и апстрим ответил 402 или текстом про кредиты/квоту/подписку, запрос повторяется на запасной alias. В ответе будет заголовок `X-MikroLLM-Fallback: исходная -> запасная`.

`GET /v1/models` дополняет каждую модель полями `context_length`, `max_model_len`, `max_tokens`, `max_input_tokens` (если известен контекст), `provider`, `owned_by`, при наличии прайса OpenRouter — `input_cost_per_token` / `output_cost_per_token`.

`GET /v1/model/info` (и `/model/info`) — формат как у LiteLLM:

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

Проксируется в `POST <ollama>/api/chat` для локального Ollama и Ollama Cloud. Иначе путь меняется на OpenAI-чат провайдера (`/v1/chat/completions` или `/chat/completions` у OpenRouter).

## Выбор бэкенда

1. Ищется включённый alias с таким именем.
2. Берутся его серверы, из них — **healthy**.
3. Политика LB (см. [admin.md](admin.md)).
4. Если alias нет — любой healthy бэкенд, у которого имя есть в каталоге (или список моделей ещё не подтянулся).
5. Если у бэкенда задан токен, шлюз шлёт `Authorization: Bearer …` апстриму.

## Ошибки

JSON в духе OpenAI:

```json
{"error": {"message": "no healthy backend for model llama3.2"}}
```

Типичные коды: `400` нет `model`, `401` ключ, `403` allowlist, `429` RPM, `502` апстрим недоступен, `503` очередь переполнена / сброс ждущих.

## Клиенты

Любой OpenAI SDK: `base_url=http://<хост>:4000/v1`, `api_key=sk-…`.

Open WebUI, Holix, Cursor и т.д. — тот же base URL и ключ из админки.
