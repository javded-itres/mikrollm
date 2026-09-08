<p align="center">
  <img src="docs/assets/banner.jpg" alt="banner" width="920">
</p>

# MikroLLM

Лёгкий шлюз к [Ollama](https://ollama.com) (локальный и Cloud), [OpenRouter](https://openrouter.ai), [vLLM](https://docs.vllm.ai) и [LM Studio](https://lmstudio.ai) в духе LiteLLM: OpenAI-совместимый API, виртуальные ключи `sk-…`, HTML-админка, playground-чат, pull/load моделей там, где API это умеет.

Пишется на Go, без Python и без CGO. Бинарь ~12 МБ, в работе обычно 8–20 МБ RAM. Удобно ставить:

- в **контейнер RouterOS 7** на MikroTik (hAP ax³ и другие ARM64);
- на **обычный сервер** (Linux amd64/arm64, Docker или systemd);
- локально для разработки.

Документация: [docs/](docs/README.md).

## Что умеет

- Несколько бэкендов: **Ollama**, **Ollama Cloud**, **OpenRouter**, **vLLM**, **LM Studio**; health-check каждые 10 с.
- Alias для клиентов, балансировка `least_conn` / `round_robin` / `failover`, запасная модель при конце кредитов.
- Очереди: шаги (локальные → бесплатное облако → платное), визуализация на дашборде, ожидание на диске без Kafka/Redis. Клиент может слать alias, имя очереди или `имя-alias`.
- Виртуальные ключи с ограничением моделей и RPM.
- Админка: серверы, модели (фильтр по провайдеру и цене), ключи, очереди, лог с фильтрами, чат.
- **MCP** на `POST /mcp`: агент настраивает провайдеры, модели, очереди и ключи, читает логи и статус. [docs/mcp.md](docs/mcp.md)
- Скачивание модели (`pull`) с полосой прогресса на Ollama и LM Studio; F5 не обрывает задачу.
- Загрузка / выгрузка весов в RAM (Ollama `keep_alive`, LM Studio `/api/v1/models/load|unload`).
- vLLM: модель задаётся на сервере (`vllm serve <HuggingFace-id>`) — [инструкция](docs/providers.md#vllm).
- OpenRouter и Ollama Cloud — напрямую по HTTPS, без промежуточного GPU-сервера; нужен API-ключ. Цены $/1M в каталоге: OpenRouter из `/models`, Ollama Cloud с [ollama.com/pricing](https://ollama.com/pricing).
- Playground: выбрать alias или имя модели и писать в чат без ключа (нужна сессия админки).

## Быстрый старт (локально)

Нужны Go 1.23+ и хотя бы один Ollama на `localhost:11434` или в LAN.

```bash
git clone https://github.com/javded-itres/mikrollm.git
cd mikrollm
go test ./...
make run
```

`make run` слушает `:4000`, пароль админки `admin`, каталог данных `./data`.

Откройте http://127.0.0.1:4000/admin → **Статус** → добавьте Ollama / vLLM / LM Studio → **Модели** → подключите нужные → **Ключи** → выпустите `sk-…`.

```bash
curl http://127.0.0.1:4000/v1/chat/completions \
  -H "Authorization: Bearer sk-…" \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3.2","messages":[{"role":"user","content":"привет"}],"stream":false}'
```

Пустой каталог данных при первом запуске **сеет** два бэкенда `mac-82` / `mac-80` на `192.168.88.80/82`. Если у вас другие адреса — удалите их в админке и добавьте свои. На уже существующей базе seed не выполняется.

## Куда ставить

| Сценарий | Документ |
|---|---|
| Разработка на машине с Go | [docs/install-local.md](docs/install-local.md) |
| Linux-сервер, Docker или systemd | [docs/install-docker.md](docs/install-docker.md) |
| Контейнер MikroTik RouterOS 7 | [docs/install-mikrotik.md](docs/install-mikrotik.md) |
| Админка: модели, RAM, ключи, чат | [docs/admin.md](docs/admin.md) |
| Ollama / Cloud / OpenRouter / vLLM / LM Studio | [docs/providers.md](docs/providers.md) |
| HTTP API | [docs/api.md](docs/api.md) |
| MCP для агента | [docs/mcp.md](docs/mcp.md) |
| Устройство кода | [docs/architecture.md](docs/architecture.md) |

## Требования

- Хотя бы один бэкенд: локальный Ollama / vLLM / LM Studio **или** облако OpenRouter / Ollama Cloud (HTTPS + API-ключ). Сеть должна быть доступна из процесса MikroLLM.
- Для образа RouterOS: Docker Buildx, Python 3, USB-диск на роутере желателен.
- Порт **4000/tcp** (меняется флагом `-listen` / `MIKROLLM_LISTEN`).

## Конфигурация

| Флаг | Переменная | По умолчанию |
|---|---|---|
| `-listen` | `MIKROLLM_LISTEN` | `:4000` |
| `-data` | `MIKROLLM_DATA` | `./data` |
| `-admin-password` | `ADMIN_PASSWORD` | пусто: пароль генерируется и пишется в лог при **первом** старте |
| `-admin-password-reset` | `ADMIN_PASSWORD_RESET=1` | не сбрасывать |
| `-mcp-token` | `MIKROLLM_MCP_TOKEN` | Bearer для `/mcp`; если пусто — генерируется при первом старте |
| `-mcp-token-reset` | `MIKROLLM_MCP_TOKEN_RESET=1` | выпустить новый MCP-токен |
|  | `MIKROLLM_QUEUE_MAX_BYTES` | `16777216` — потолок тела очереди на диске, старше ждущие сбрасываются |
|  | `MIKROLLM_QUEUE_MAX_JOBS` | `200` — максимум ждущих+идущих |
|  | `MIKROLLM_QUEUE_MAX_WAIT` | `3m` — сколько HTTP-соединение может ждать слот |

Пароль хранится в SQLite (`data/mikrollm.db`) как bcrypt. Сброс — только с `ADMIN_PASSWORD_RESET=1`.

## Сборка

```bash
make test
make build-arm64          # dist/mikrollm (linux/arm64)
make tar-ros              # dist/mikrollm-ros-legacy.tar для RouterOS
```

Релизы GitHub содержат готовые бинарники и tar для MikroTik.

## Лицензия

[MIT](LICENSE)
