# MCP

MikroLLM поднимает [MCP](https://modelcontextprotocol.io) в том же процессе, что и шлюз: `POST /mcp`, без отдельного бинаря и без второго SQLite.

Агент (Grok, Cursor, Claude) может смотреть статус, настраивать провайдеры, модели, очереди и ключи, читать логи.

## Транспорт

Streamable HTTP, JSON-RPC 2.0.

| Метод | Путь | Назначение |
|---|---|---|
| POST | `/mcp` | initialize, tools/list, tools/call, ping |
| GET | `/mcp` | `405` (серверных SSE-уведомлений нет, RAM на MikroTik) |
| DELETE | `/mcp` | закрыть сессию (no-op, 200) |

`Accept: application/json, text/event-stream`. Ответ обычно `application/json`. Если клиент просит только SSE — одно событие `message`.

Сессии **stateless**: заголовок `Mcp-Session-Id` на initialize выдаётся и зеркалится, но сервер его не хранит. После рестарта контейнера reconnect без потери состояния.

Тело запроса не больше 1 МБ.

## Авторизация

Только Bearer. Cookie админки на `/mcp` не уходит (`Path=/admin`).

```
Authorization: Bearer mcp-…
```

Принимается:

1. MCP-токен (SHA-256 в `admin_meta`, как у ключей `sk-`).
2. Пароль админки — чтобы поднять доступ, если токен потеряли. bcrypt, лимит попыток с IP как на логине.

Обычные ключи `sk-` **не** открывают MCP: у них права клиента чата, не админки.

Неверный токен — `401` и `WWW-Authenticate: Bearer`.

## Как получить токен

При **первом** старте, если токена ещё нет, он генерируется и пишется в лог:

```
generated MCP token: mcp-…
```

Дальше — админка **Статус → MCP для агента**: выпустить / сменить. Секрет показывается один раз.

Или флаг / env (перезаписывает сохранённый, если задан):

| Флаг | Переменная | Смысл |
|---|---|---|
| `-mcp-token` | `MIKROLLM_MCP_TOKEN` | задать токен (хранится хеш) |
| `-mcp-token-reset` | `MIKROLLM_MCP_TOKEN_RESET=1` | сгенерировать новый, даже если уже есть |

Не коммитьте токен в git. В envlist RouterOS он попадёт в конфиг роутера — лучше выпустить из админки.

## Grok

`~/.grok/config.toml` или `.grok/config.toml` в репозитории:

```toml
[mcp_servers.mikrollm]
url = "http://192.168.88.1:4000/mcp"
enabled = true
headers = { "Authorization" = "Bearer ${MIKROLLM_MCP_TOKEN}" }
```

Подставьте токен или экспортните `MIKROLLM_MCP_TOKEN`. LAN, без hairpin WSS: агент должен ходить на `192.168.88.1:4000` с хоста в LAN / split-tunnel, не через публичный hairpin.

Проверка:

```bash
curl -sS http://192.168.88.1:4000/mcp \
  -H "Authorization: Bearer mcp-…" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1"}}}'
```

Затем `"method":"tools/list"` и `"method":"tools/call","params":{"name":"get_status","arguments":{}}`.

CLI:

```bash
grok mcp add --transport http mikrollm http://192.168.88.1:4000/mcp \
  --header "Authorization: Bearer mcp-…"
```

## Инструменты

Сначала `get_status`. Имена стабильные, описания на русском.

| Tool | Действие |
|---|---|
| `get_status` | версия, RAM процесса, бэкенды, alias, живые очереди, jobs |
| `refresh_health` | внеочередной опрос бэкендов + сводка |
| `list_providers` / `upsert_provider` / `delete_provider` | Ollama, vLLM, LM Studio, OpenRouter, Ollama Cloud. Токен в ответах маскируется; пустой `token` не затирает ключ |
| `list_models` / `list_catalog` / `connect_model` / `save_model` / `delete_model` | alias шлюза и каталог health |
| `host_action` | `pull` / `load` (фон, смотрите `list_jobs`) или `unload` / `delete` |
| `list_jobs` | прогресс pull/load |
| `list_queues` / `save_queue` / `delete_queue` | очередь: `steps` (`model_alias`, `max_concurrent`), `extra_aliases`, overflow |
| `list_keys` / `create_key` / `update_key` / `delete_key` | виртуальные `sk-`. Секрет только в ответе `create_key` |
| `list_logs` / `log_stats` | фильтры q / model / backend / key / 2xx·4xx·5xx / latency; p50/p95 |
| `rotate_mcp_token` | новый Bearer, старый сразу мёртв |

`save_queue` с `steps` и `extra_aliases` заменяет соответствующие списки. Если поля нет — старое не трогается.

Сводка:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": { "name": "get_status", "arguments": {} }
}
```

Очередь «локальная → облако»:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "save_queue",
    "arguments": {
      "name": "itres",
      "alias": "coder",
      "overflow_after": 2,
      "steps": [
        {"model_alias": "ornith-1.5:35b", "max_concurrent": 2},
        {"model_alias": "smart-kimi-k2.7", "max_concurrent": 3}
      ],
      "extra_aliases": ["itres-coder"]
    }
  }
}
```

Клиент чата тогда может слать `model: "coder"`, `itres` или `itres-coder`.

## Безопасность

MCP = полная админка. Кто знает токен, может выпустить ключи и сменить провайдеров.

- Не публикуйте `/mcp` в интернет без TLS и фильтра.
- На RouterOS оставляйте dst-nat :4000 в LAN.
- Токены бэкендов и `sk-` в ответах инструментов не повторяются, кроме одноразового `create_key` / `rotate_mcp_token`.
