# Контекст проекта (Holix)

> Создано командой `/init`. Обновляйте при существенных изменениях кодовой базы.

## Обзор
- **Назначение:** MikroLLM — компактный шлюз LLM на Go (в духе LiteLLM) к бэкендам **Ollama / Ollama Cloud / OpenRouter / vLLM / LM Studio**: OpenAI-совместимый API, виртуальные ключи `sk-…`, HTML-админка с playground-чатом, pull/load/unload моделей, очереди, кэш промптов, MCP- и A2A-прокси, облачный hub-релей.
- **Основные пользователи:** владельцы MikroTik (RouterOS 7 container), Keenetic (Entware), домашних/серверных установок (Docker/systemd), iOS-приложения; клиенты — OpenAI SDK, MCP-клиенты, A2A-агенты.
- **Тип репозитория:** монолитный Go-сервис (один статический бинарник ~12 МБ, SQLite, без CGO/Python) + субпроекты: iOS-приложение (`ios/`, gomobile + Swift) и gomobile-обвязка (`mobile/`). Документация двуязычная EN/RU (`docs/`, `docs/ru/`).
- Ветка документации по умолчанию — английская (`docs/README.md`), русские копии в `docs/ru/`.

## Структура каталогов
```
./
├── cmd/mikrollm/          # точка входа: флаги CLI, env, http.Server (+ TLS)
├── internal/
│   ├── app/               # композиционный корень: сборка зависимостей, маршруты
│   ├── domain/            # сущности (Backend, Model, APIKey, Job…) и их логика
│   ├── ports/             # интерфейсы: Store, Health, Auth, Host, Jobs, ChatGateway
│   ├── store/             # SQLite (modernc.org/sqlite, без CGO): миграции, биллинг, очереди
│   ├── health/            # опрос бэкендов + парсинг прайса Ollama Cloud
│   ├── auth/              # bcrypt-пароль админа, cookie+CSRF, SHA-256 ключей sk-…
│   ├── host/              # pull/delete/load/unload по типу бэкенда
│   ├── ollama/            # HTTP-клиент Ollama (pull NDJSON, keep_alive)
│   ├── jobs/              # фоновые pull/load + прогресс
│   ├── proxy/             # OpenAI/Ollama-совместимый API, LB (least_conn/round_robin/failover)
│   ├── queue/             # дисковая очередь (SQLite WAL), степы local→free→paid, sticky HTTP
│   ├── promptcache/       # кэш промптов (prepare/price/tail/usage)
│   ├── guard/             # фильтры запросов/ответов, system prompt, prompt injection (NSFW-плагин)
│   ├── admin/             # HTML-админка (шаблоны internal/web/templates, i18n)
│   ├── mcp/               # MCP Streamable HTTP сервер (/mcp), JSON-RPC без SDK
│   ├── mcpproxy/          # прокси внешних MCP-серверов
│   ├── a2aproxy/          # A2A-агенты: JSON-RPC, группы, сессии, deliver
│   ├── hubclient/         # клиент облачного hub (join, announce, релей задач)
│   ├── codex/, grok/      # адаптеры специфичных провайдеров
│   ├── params/            # разбор/мерж генпараметров
│   ├── tlsconf/           # TLS: PEM-файлы, self-signed, ACME
│   └── web/               # static/ (app.js, css, openapi.json, PWA), templates/ (HTML админки)
├── mobile/                # gomobile-обвязка (mobile.go) для iOS
├── ios/                   # Xcode-проект MikroLLM (SwiftUI, WKWebView) + Mobile.xcframework
├── scripts/               # install.sh, install-keenetic.sh, oci_to_legacy_docker.py
├── docs/                  # EN-доки (README, api, architecture, hub, install-*, tls, admin…)
├── docs/ru/               # русские копии документации
└── docker-compose.yml     # MikroLLM + vLLM Qwen3.6 на одной GPU-машине
```

## Технологический стек
- **Языки:** Go 1.23.0 (`go.mod`, модуль `github.com/javded-itres/mikrollm`); Swift/SwiftUI для `ios/`; JavaScript (vanilla) для админки (`internal/web/static/app.js`).
- **Фреймворки:** стандартная библиотека Go (`net/http`, `flag`), `html/template` для админки; gomobile (`golang.org/x/mobile`) для iOS-обвязки. Внешних web-фреймворков нет.
- **БД / очереди / кэши:** SQLite через `modernc.org/sqlite` (чистый Go, без CGO), WAL-режим, `MaxOpenConns(1)`; дисковая очередь задач на SQLite (`internal/queue`); кэш промптов (`internal/promptcache`).
- **Менеджеры пакетов и сборка:** `go mod` + `Makefile` (`make test`, `make run`, `make build-arm64`, `make build-keenetic`, `make ios`, `make tidy`); Docker/Docker Buildx (`docker-compose.yml`, Dockerfile scratch + статический бинарник); XcodeGen для `ios/`.
- **Ключевые зависимости (прямые):** `golang.org/x/crypto` (bcrypt), `golang.org/x/mobile`, `modernc.org/sqlite`. SDK для MCP/A2A не используются — JSON-RPC реализован вручную.

## Архитектура
- **Схема верхнего уровня:** один процесс = HTTP-сервер + SQLite. Клиенты (OpenAI SDK / MCP / A2A / hub) → `cmd/mikrollm` → `internal/app` (композиция) → `internal/proxy` (LB и маршрутизация) → бэкенды (Ollama, Ollama Cloud, OpenRouter, vLLM, LM Studio). Параллельно: `health` (опрос каждые 10 с), `jobs` (pull/load в фоне), `queue` (степы local→free→paid), `guard` (фильтры), `promptcache`.
- **Слои:** `domain` (сущности) → `ports` (интерфейсы Store/Health/Auth/Host/Jobs/ChatGateway) → реализации (`store`, `health`, `auth`, `host`, `jobs`) → транспорт (`proxy`, `admin`, `mcp`, `mcpproxy`, `a2aproxy`, `hubclient`). Композиционный корень — `internal/app/app.go`: все реализации конструируются и передаются в конструкторы (проверка соответствия портам через `var _ ports.X = (*impl)(nil)`).
- **Точки входа:** CLI `cmd/mikrollm/main.go` (флаги + env, `http.Server` с опциональным TLS из `internal/tlsconf`); HTTP: `/v1/*` (proxy), `/admin` (HTML), `/mcp` (MCP Streamable), `/a2a/u/{name}` (A2A), hub-релей; фоновые воркеры: health-чекер, jobs (pull/load), queue engine, hub client.

## REST / HTTP API (если есть)
- **Базовый URL / версионирование:** `http://<host>:4000` (или HTTPS при TLS), OpenAI-совместимые пути под `/v1/`; полный список — `docs/api.md`, OpenAPI-спецификация — `internal/web/static/openapi.json`.
- **Аутентификация:** `Authorization: Bearer sk-…` или `X-Api-Key: sk-…` (виртуальные ключи, SHA-256 в БД, allowlist моделей на ключ); без ключа — `401`; модель не в allowlist — `403`; превышение RPM — `429`. `/health` и `/ready` — без ключа.
- **Карта маршрутов (основное):**
  - `GET /health`, `GET /ready` — живость и наличие здорового бэкенда (`proxy.Health/Ready`).
  - `GET /v1/models`, `GET /v1/model/info`, `GET /model/info` — каталог алиасов, LiteLLM-style `model_info` (`proxy.ServeModelParams`).
  - `POST /v1/chat/completions` — Chat Completions, `stream: true`, `X-Session-Id`, `X-MikroLLM-Cache-Tokens` (`proxy.ChatCompletions`).
  - `POST /v1/images/generations`, `POST /v1/videos` — Images API и Sora-style видео (`proxy/images.go`).
  - `POST /v1/embeddings` — эмбеддинги (`proxy/embeddings.go`).
  - `/mcp` — MCP Streamable HTTP (JSON-RPC, `internal/mcp`), bearer-токен MCP.
  - `/a2a/u/{name}` — A2A JSON-RPC карточка/`message/send`, `GET /a2a/connect` для локальных агентов (`internal/a2aproxy`).
  - `/admin/*` — HTML-админка (cookie `mikrollm_session` + CSRF, только POST для изменений).
- **Паттерны запросов/ответов:** OpenAI-совместимые JSON; стриминг — SSE; ошибочные ответы в формате `{"error": …}`; sticky HTTP для очередей.

## Ключевые модули и зоны ответственности
| Модуль / пакет | Роль |
|----------------|------|
| `cmd/mikrollm` | main: флаги (`-listen`, `-data`, `-admin-password`, `-mcp-token`, `-tls-*`), env-дублёры `MIKROLLM_*` |
| `internal/app` | композиционный корень: Store/Health/Auth/Host/Jobs/Proxy/Admin/MCP/A2A/Hub, маршрутизация mux |
| `internal/domain` | сущности и правила: Backend, Model, APIKey, Job, генпараметры, hub schedule, guard ctx |
| `internal/ports` | интерфейсы Store, Health, Auth, Host, Jobs, ChatGateway (инверсия зависимостей) |
| `internal/store` | SQLite: миграции, CRUD бэкендов/моделей/ключей, биллинг, очередь, MCP-апстримы, A2A |
| `internal/proxy` | OpenAI/Ollama-совместимый API: chat/images/videos/embeddings, LB (least_conn/round_robin/failover), hub-релей |
| `internal/queue` | дисковая очередь задач (SQLite WAL), степы local→free→paid, sticky HTTP |
| `internal/promptcache` | кэш промптов: prepare/price/tail/usage, режимы через admin |
| `internal/guard` | фильтры запросов/ответов, system prompt, prompt injection, NSFW-плагин |
| `internal/health` | опрос бэкендов (10 с), парсинг прайса Ollama Cloud (`ollama_pricing.go`) |
| `internal/host` + `internal/ollama` | pull/delete/load/unload моделей по типу бэкенда; NDJSON-прогресс pull |
| `internal/jobs` | фоновые задачи pull/load, прогресс в админку |
| `internal/auth` | bcrypt-пароль админа, cookie+CSRF, SHA-256 ключей `sk-…`, rate limit логинов |
| `internal/admin` | HTML-админка (i18n EN/RU): Status/Queues/Models/Security/Keys/Chat/Log/Billing/Agents/MCP |
| `internal/mcp`, `internal/mcpproxy` | MCP Streamable HTTP сервер и прокси внешних MCP-серверов |
| `internal/a2aproxy` | A2A-агенты: карточка, `message/send`, группы, сессии, deliver на `/a2a/connect` |
| `internal/hubclient` | клиент облачного hub (`hub.mikrollm.ru`): join, announce алиасов, релей задач |
| `internal/tlsconf` | TLS: PEM-файлы, self-signed, ACME |
| `internal/codex`, `internal/grok` | адаптеры специфичных провайдеров (Codex, Grok) |
| `internal/web` | static (app.js, openapi.json, PWA) и шаблоны HTML-админки |
| `mobile/`, `ios/` | gomobile-обвязка и Xcode-приложение (SwiftUI + WKWebView) |

## Конфигурация и окружение
- **Файлы конфигурации:** конфигов-файлов нет — всё в SQLite (`<data>/mikrollm.db`): бэкенды, модели, ключи, настройки guard/очередей. `docker-compose.yml` — пример стека с vLLM Qwen3.6 (`.env.example` → `.env` с `ADMIN_PASSWORD`).
- **Обязательные переменные окружения:** `ADMIN_PASSWORD` (первичный/сброс пароля админа), `MIKROLLM_LISTEN` (`:4000`), `MIKROLLM_DATA` (`./data`); опционально: `MIKROLLM_MCP_TOKEN` (+`_RESET`), `MIKROLLM_TLS_CERT/_KEY`, `MIKROLLM_TLS_AUTO=1`, `MIKROLLM_HUB_URL` (переопределение `hub.mikrollm.ru`).
- **Работа с секретами:** ключи бэкендов (`sk-or-…` и пр.) хранятся в SQLite и **не** передаются в hub; пароль админа — bcrypt, виртуальные ключи `sk-…` — SHA-256; MCP bearer хранится хэшированным; секреты в env только при старте (`docker-compose.yml` не содержит секретов, берёт их из `.env`).

## Индекс документации
| Путь | Что описывает |
|------|---------------|
| `docs/README.md` (+ `docs/ru/README.md`) | индекс документации; EN — основная ветка |
| `docs/api.md` | HTTP API: маршруты, авторизация, коды ошибок |
| `docs/architecture.md` | слои, композиционный корень, зависимости |
| `docs/admin.md` | админка: Status/Queues/Models/Security/Keys/Chat/Log/Billing, PWA |
| `docs/security.md` | guard: фильтры, system prompt, prompt injection |
| `docs/providers.md` | особенности бэкендов: Ollama, Ollama Cloud, OpenRouter, vLLM, LM Studio |
| `docs/hub.md` | облачный hub: join, announce, релей задач; только outbound HTTPS |
| `docs/a2a.md` | A2A-агенты: карточка, `message/send`, локальные агенты через `/a2a/connect` |
| `docs/install-desktop.md` | one-liner: Ollama + MikroLLM на Linux/macOS |
| `docs/install-local.md` | запуск из исходников, флаги, каталог данных |
| `docs/install-docker.md` | Docker и systemd на сервере |
| `docs/install-compose.md` | compose-стек `javded/mikrollm` + vLLM Qwen3.6 |
| `docs/install-mikrotik.md` | RouterOS 7 container: veth, dst-nat, USB |
| `docs/install-keenetic.md` | KeeneticOS Entware (USB, без Docker) |
| `docs/install-ios.md` | iOS: gomobile + WKWebView |
| `docs/tls.md` | HTTPS: свои PEM или in-process self-signed |
| `docs/design-prompt-cache.md`, `docs/design-request-cache.md` | дизайн кэша промптов/запросов |
| `internal/web/static/openapi.json` | OpenAPI-спецификация HTTP API |

## Рабочий процесс разработки
- **Установка:** из исходников: `go mod tidy` (или `make tidy`); Docker: `docker buildx build` (см. `docs/install-docker.md`); one-liner для Linux/macOS — `docs/install-desktop.md` / `scripts/install.sh`; Keenetic — `docs/install-keenetic.md` / `scripts/install-keenetic.sh`.
- **Локальный запуск:** `make run` = `go run ./cmd/mikrollm -listen :4000 -data ./data -admin-password admin`; админка `http://127.0.0.1:4000/admin`.
- **Тесты:** `make test` = `go test ./...` (unit-тесты лежат рядом с кодом, `*_test.go`, включая `internal/app/app_test.go`).
- **Сборка:** `make build-arm64` (linux/arm64, CGO_ENABLED=0, `-trimpath -ldflags "-s -w -X main.version=…"`), `make build-keenetic`, `make ios` (gomobile, версия x/mobile зафиксирована в `MOBILE_VER`).
- **Линт / форматирование:** отдельного линтера в Makefile нет — стандартные `go vet ./...` и `gofmt`; уточнить при появлении CI.

## Соглашения по коду
- **Стиль / правила линтера:** стандартный `gofmt`; отдельного линтера нет, `go vet ./...` перед коммитом. Файл `RULES.md` в репо отсутствует.
- **Именование:** Go-стандарт; доменные типы в `internal/domain` (`Backend`, `Model`, `APIKey`, `Job`), реализации портов (`store.Store`, `health.Checker`, `host.Manager`, `jobs.Tracker`); таблицы SQLite во множественном числе (`backends`, `models`…).
- **Обработка ошибок / логирование:** стандартный `log` (без slog/zerolog); ошибки возвращяются наверх, HTTP-ошибки — JSON `{"error": …}` с корректным статусом (401/403/429).
- **Подход к тестированию:** unit-тесты рядом с кодом (`*_test.go`), без внешних assertion-библиотек; тесты есть почти в каждом пакете (`proxy_test.go`, `store_test.go`, `app_test.go` и т.д.); фикстуры — `internal/health/testdata/`.

## Важные файлы
| Файл | Почему важен |
|------|--------------|
| `cmd/mikrollm/main.go` | все флаги/env-дублёры, порядок инициализации, TLS |
| `internal/app/app.go` | композиционный корень: что от чего зависит, карта маршрутов mux |
| `internal/ports/ports.go` | контракты слоёв (Store, Health, Auth, Host, Jobs, ChatGateway) |
| `internal/store/store.go` | миграции SQLite, DSN-прагмы (WAL, busy_timeout), MaxOpenConns(1) |
| `internal/proxy/proxy.go` | LB-стратегии, маршрутизация чата, sticky-очереди |
| `internal/queue/engine.go` | степы local→free→paid, слоты, дисковая очередь |
| `internal/health/ollama_pricing.go` | парсинг прайса Ollama Cloud (фикстура `testdata/ollama_pricing.html`) |
| `internal/web/templates/layout.html` | базовый шаблон админки (i18n, меню) |
| `internal/web/static/openapi.json` | OpenAPI-контракт API |
| `Makefile` | цели test/run/build-arm64/build-keenetic/ios; фикс версии x/mobile (`MOBILE_VER`) |
| `docker-compose.yml` | рабочий GPU-стек MikroLLM + vLLM Qwen3.6 (V100, GPTQ Int4, fp16) |

## Заметки и подводные камни
- **Go 1.23 жёстко:** позже `go.mod` поднять нельзя, пока не обновлён `golang.org/x/mobile` — версия зафиксирована в `MOBILE_VER` (Makefile), `go get` может незаметно сдвинуть модуль за 1.24+.
- **SQLite без CGO:** `modernc.org/sqlite` не имеет сборок linux/mips(le) — Keenetic MIPS не поддерживается, только arm64.
- **MaxOpenConns(1) + WAL:** вся запись идёт через одно соединение; долгие транзакции блокируют очереди/биллинг.
- **Док-ветка EN по умолчанию:** правки документации делать в `docs/`, затем зеркалировать в `docs/ru/`.
- **docker-compose.yml заточен под V100 (sm_70):** нет BF16/FP8, `--enforce-eager`, контекст 16384, без prefix cache; не переносить настройки на другие GPU без проверки.
- **Секреты не уходят в hub:** ключи бэкендов живут только в локальном SQLite; hub получает лишь имена алиасов.
- **Изменение состояния админки — только POST с CSRF-токеном;** новые `sk-`-ключи показываются один раз.
- **Субпроекты `ios/` и `mobile/`:** для глубины использовать `/init ios` / `/init mobile` — здесь только обзор.
- **Уточнить:** наличие CI (GitHub Actions в репо не видно в предсканировании).

## Предзаполнено из скана
- Примерно 223 исходных файлов в области
- Каталог области: `.`

### Дерево каталогов
```
./
├── cmd/
│   └── mikrollm/
│       └── main.go
├── docs/
│   ├── assets/
│   │   ├── banner.jpg
│   │   └── logo.jpg
│   ├── ru/
│   │   ├── a2a.md
│   │   ├── admin.md
│   │   ├── api.md
│   │   ├── architecture.md
│   │   ├── design-prompt-cache.md
│   │   ├── design-request-cache.md
│   │   ├── hub.md
│   │   ├── install-compose.md
│   │   ├── install-desktop.md
│   │   ├── install-docker.md
│   │   ├── install-ios.md
│   │   └── install-keenetic.md
│   ├── a2a.md
│   ├── admin.md
│   ├── api.md
│   ├── architecture.md
│   ├── design-prompt-cache.md
│   ├── design-request-cache.md
│   ├── hub.md
│   ├── install-compose.md
│   ├── install-desktop.md
│   ├── install-docker.md
│   ├── install-ios.md
│   └── install-keenetic.md
├── internal/
│   ├── a2aproxy/
│   │   ├── deliver.go
│   │   ├── groups.go
│   │   ├── groups_test.go
│   │   ├── proxy.go
│   │   ├── proxy_test.go
│   │   ├── session.go
│   │   └── session_test.go
│   ├── admin/
│   │   ├── a2a.go
│   │   ├── a2a_groups.go
│   │   ├── admin.go
│   │   ├── agents_template_test.go
│   │   ├── chat.go
│   │   ├── i18n.go
│   │   ├── mcpup.go
│   │   ├── ollama.go
│   │   ├── ollama_test.go
│   │   ├── policies.go
│   │   ├── queues.go
│   │   └── security.go
│   ├── app/
│   │   ├── app.go
│   │   └── app_test.go
│   ├── auth/
│   │   ├── auth.go
│   │   └── auth_test.go
│   ├── codex/
│   │   ├── codex.go
│   │   └── codex_test.go
│   ├── domain/
│   │   ├── a2a.go
│   │   ├── context.go
│   │   ├── genparams.go
│   │   ├── genparams_test.go
│   │   ├── guardctx.go
│   │   ├── guardctx_test.go
│   │   ├── http.go
│   │   ├── hub_schedule.go
│   │   ├── hub_schedule_test.go
│   │   ├── kind.go
│   │   ├── kind_test.go
│   │   └── mcpup.go
│   ├── grok/
│   │   ├── grok.go
│   │   └── grok_test.go
│   ├── guard/
│   │   ├── guard.go
│   │   ├── guard_test.go
│   │   ├── patterns.go
│   │   ├── plugin.go
│   │   └── plugins_nsfw.go
│   ├── health/
│   │   ├── testdata/
│   │   │   └── ollama_pricing.html
│   │   ├── health.go
│   │   ├── health_test.go
│   │   ├── ollama_pricing.go
│   │   └── ollama_pricing_test.go
│   ├── host/
│   │   ├── manager.go
│   │   └── manager_test.go
│   ├── hubclient/
│   │   ├── client.go
│   │   ├── client_test.go
│   │   ├── proto.go
│   │   └── proto_test.go
│   ├── jobs/
│   │   ├── jobs.go
│   │   └── jobs_test.go
│   ├── mcp/
│   │   ├── args.go
│   │   ├── server.go
│   │   ├── server_test.go
│   │   └── tools.go
│   ├── mcpproxy/
│   │   ├── proxy.go
│   │   └── proxy_test.go
│   ├── ollama/
│   │   ├── client.go
│   │   └── client_test.go
│   ├── params/
│   │   ├── params.go
│   │   └── params_test.go
│   ├── ports/
│   │   └── ports.go
│   ├── promptcache/
│   │   ├── mode.go
│   │   ├── prepare.go
│   │   ├── prepare_test.go
│   │   ├── price.go
│   │   ├── price_test.go
│   │   ├── tail.go
│   │   ├── tail_test.go
│   │   ├── usage.go
│   │   └── usage_test.go
│   ├── proxy/
│   │   ├── decision.go
│   │   ├── decision_test.go
│   │   ├── embeddings.go
│   │   ├── embeddings_test.go
│   │   ├── images.go
│   │   ├── images_test.go
│   │   ├── params.go
│   │   ├── proxy.go
│   │   └── proxy_test.go
│   ├── queue/
│   │   ├── engine.go
│   │   └── engine_test.go
│   ├── store/
│   │   ├── billing.go
│   │   ├── billing_test.go
│   │   ├── queue.go
│   │   ├── security.go
│   │   ├── store.go
│   │   ├── store_a2a.go
│   │   ├── store_a2a_group.go
│   │   ├── store_mcp.go
│   │   └── store_test.go
│   ├── tlsconf/
│   │   ├── acme.go
│   │   ├── acme_test.go
│   │   ├── file.go
│   │   ├── tlsconf.go
│   │   └── tlsconf_test.go
│   └── web/
│       ├── static/
│       │   ├── swagger/
│       │   ├── app.css
│       │   ├── app.js
│       │   ├── apple-touch-icon.png
│       │   ├── icon-192.png
│       │   ├── icon-512.png
│       │   ├── logo.svg
│       │   ├── manifest.webmanifest
│       │   ├── openapi.json
│       │   ├── pwa.js
│       │   └── sw.js
│       ├── templates/
│       │   ├── agents.html
│       │   ├── billing.html
│       │   ├── chat.html
│       │   ├── dash.html
│       │   ├── docs.html
│       │   ├── keys.html
│       │   ├── layout.html
│       │   …
│       …
…
… (tree truncated)
```
