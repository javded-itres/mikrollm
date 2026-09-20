# Архитектура

[English](../architecture.md) · **Русский**

Один процесс, один бинарь, SQLite. Корень композиции — `internal/app`: там создаются реализации и передаются в конструкторы.

```
cmd/mikrollm          флаги, http.Server (опционально TLS)
internal/app          сборка графа зависимостей
internal/domain       сущности (Backend, Model, APIKey, Job…)
internal/ports        интерфейсы Store, Health, Auth, Host, Jobs, ChatGateway
internal/store        SQLite (modernc.org/sqlite, без CGO)
internal/health       опрос Ollama / Ollama Cloud / OpenRouter / vLLM / LM Studio
internal/auth         bcrypt, cookie+CSRF, ключи SHA-256
internal/host         pull / delete / load / unload по типу бэкенда
internal/ollama       Ollama HTTP (pull NDJSON, generate keep_alive)
internal/jobs         фон pull/load + прогресс
internal/proxy        OpenAI/Ollama API, LB
internal/queue        дисковая очередь запросов (SQLite WAL), слоты шагов, sticky HTTP
internal/admin        HTML-админка
internal/mcp          MCP Streamable HTTP (`/mcp`), JSON-RPC, без SDK
internal/guard        фильтры запроса/ответа, системный промпт, prompt injection
internal/promptcache  prefix cache OpenRouter: cache_control, usage, SSE tail
internal/tlsconf      PEM / self-signed / Let's Encrypt ACME (autocert)
internal/web          шаблоны и static (embed)
internal/hubclient    исходящий клиент hub (register / announce / long-poll)
```

## Принципы

- **S** — прокси не знает HTML, host-адаптер не знает SQLite.
- **O / L** — новый адаптер вешается на порт, не меняя `proxy`/`admin`.
- **I** — health видит только `BackendQuery`, auth — `AuthStore`.
- **D** — HTTP-слои зависят от `ports`, не от конкретных пакетов. `*http.Client` тоже внедряется.

DI по-Go: конструкторы, без Wire/Fx.

```go
ui := admin.New(admin.Deps{
    Store: st, Health: checker, Auth: keys, Host: host, Jobs: tracker, Chat: px, Queues: queues,
})
mcp.New(mcp.Deps{Store: st, Health: checker, Auth: keys, Host: host, Jobs: tracker, Queues: queues}).Mount(mux)
```

MCP-токен — SHA-256 в `admin_meta` (`mcp_token_hash` / prefix). Сравнение constant-time; plaintext не хранится.

## Данные

Файл `<data>/mikrollm.db`, WAL. `MaxOpenConns=1` (ограничение modernc/sqlite). Список моделей **не** держит курсор во время второго запроса — иначе логин и API клинят.

Том `/data` на RouterOS переживает `container remove`. Очереди — таблицы `queues` / `queue_steps` / `queue_aliases` / `queue_jobs`; тела ждущих запросов на диске, `ResponseWriter` живого HTTP — в памяти.

## Фоновые задачи

`POST /admin/ollama/{id}/pull` сразу отвечает и качает в goroutine с `context.Background()`. Прогресс в памяти и в таблице `ollama_jobs`. UI опрашивает `GET /admin/ollama/jobs`. После рестарта процесса running-pull возобновляется.

Load в RAM: Ollama — `POST /api/generate` с пустым prompt и `keep_alive: -1`; LM Studio — `POST /api/v1/models/load`. vLLM не грузит модель через API (см. [providers.md](providers.md)).

## Сборка образа RouterOS

`docker buildx` → OCI tar → `scripts/oci_to_legacy_docker.py` → docker-save v1. Иначе RouterOS отвечает `no config found in manifest`.
