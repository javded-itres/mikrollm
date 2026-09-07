# Архитектура

Один процесс, один бинарь, SQLite. Корень композиции — `internal/app`: там создаются реализации и передаются в конструкторы.

```
cmd/mikrollm          флаги, http.Server
internal/app          сборка графа зависимостей
internal/domain       сущности (Backend, Model, APIKey, Job…)
internal/ports        интерфейсы Store, Health, Auth, Host, Jobs, ChatGateway
internal/store        SQLite (modernc.org/sqlite, без CGO)
internal/health       опрос Ollama
internal/auth         bcrypt, cookie, ключи SHA-256
internal/ollama       pull / delete / load / unload
internal/jobs         фон pull/load + прогресс
internal/proxy        OpenAI/Ollama API, LB
internal/admin        HTML-админка
internal/web          шаблоны и static (embed)
```

## Принципы

- **S** — прокси не знает HTML, Ollama-клиент не знает SQLite.
- **O / L** — новый адаптер вешается на порт, не меняя `proxy`/`admin`.
- **I** — health видит только `BackendQuery`, auth — `AuthStore`.
- **D** — HTTP-слои зависят от `ports`, не от конкретных пакетов. `*http.Client` тоже внедряется.

DI по-Go: конструкторы, без Wire/Fx.

```go
ui := admin.New(admin.Deps{
    Store: st, Health: checker, Auth: keys, Host: host, Jobs: tracker, Chat: px,
})
```

## Данные

Файл `<data>/mikrollm.db`, WAL. `MaxOpenConns=1` (ограничение modernc/sqlite). Список моделей **не** держит курсор во время второго запроса — иначе логин и API клинят.

Том `/data` на RouterOS переживает `container remove`.

## Фоновые задачи

`POST /admin/ollama/{id}/pull` сразу отвечает и качает в goroutine с `context.Background()`. Прогресс в памяти и в таблице `ollama_jobs`. UI опрашивает `GET /admin/ollama/jobs`. После рестарта процесса running-pull возобновляется.

Load в RAM — `POST /api/generate` с пустым prompt и `keep_alive: -1`.

## Сборка образа RouterOS

`docker buildx` → OCI tar → `scripts/oci_to_legacy_docker.py` → docker-save v1. Иначе RouterOS отвечает `no config found in manifest`.
