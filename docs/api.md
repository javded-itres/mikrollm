# HTTP API

База: `http://<хост>:4000`.

Ключ: заголовок `Authorization: Bearer sk-…` или `X-Api-Key: sk-…`.

Без ключа (кроме health/ready и админки) — `401`. Модель не из allowlist ключа — `403`. Превышен RPM — `429`.

## Эндпоинты

| Метод | Путь | Авторизация | Назначение |
|---|---|---|---|
| GET | `/health` | нет | процесс жив |
| GET | `/ready` | нет | есть хотя бы один живой Ollama |
| GET | `/v1/models` | ключ | список alias (и fallback на tags) |
| POST | `/v1/chat/completions` | ключ | OpenAI Chat Completions, в т.ч. `stream: true` |
| POST | `/api/chat` | ключ | Ollama `/api/chat` |
| GET | `/api/tags` | ключ | имена моделей |
| GET | `/admin` | cookie | HTML-админка |

`/` редиректит на `/admin`.

## Chat Completions

Тело как у OpenAI. Поле `model` — **alias шлюза** или имя модели Ollama, если alias нет, но теги бэкенда её содержат.

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

## Ollama Chat

```bash
curl http://192.168.88.1:4000/api/chat \
  -H "Authorization: Bearer sk-…" \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3.2","messages":[{"role":"user","content":"hi"}],"stream":false}'
```

Проксируется в `POST <ollama>/api/chat`.

## Выбор бэкенда

1. Ищется включённый alias с таким именем.
2. Берутся его серверы, из них — **healthy**.
3. Политика LB (см. [admin.md](admin.md)).
4. Если alias нет — любой healthy Ollama, у которого имя есть в `/api/tags` (или tags ещё не подтянулись).

## Ошибки

JSON в духе OpenAI:

```json
{"error": {"message": "no healthy backend for model llama3.2"}}
```

Типичные коды: `400` нет `model`, `401` ключ, `403` allowlist, `429` RPM, `502` апстрим недоступен.

## Клиенты

Любой OpenAI SDK: `base_url=http://<хост>:4000/v1`, `api_key=sk-…`.

Open WebUI, Holix, Cursor и т.д. — тот же base URL и ключ из админки.
