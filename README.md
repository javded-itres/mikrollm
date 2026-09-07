# MikroLLM

Лёгкий шлюз к [Ollama](https://ollama.com) в духе LiteLLM: OpenAI-совместимый API, виртуальные ключи `sk-…`, HTML-админка, playground-чат, pull/load моделей.

Пишется на Go, без Python и без CGO. Бинарь ~12 МБ, в работе обычно 8–20 МБ RAM. Удобно ставить:

- в **контейнер RouterOS 7** на MikroTik (hAP ax³ и другие ARM64);
- на **обычный сервер** (Linux amd64/arm64, Docker или systemd);
- локально для разработки.

Документация: [docs/](docs/README.md).

## Что умеет

- Несколько серверов Ollama, health-check каждые 10 с (`/api/version`, `/api/tags`, `/api/ps`).
- Alias для клиентов, балансировка `least_conn` / `round_robin` / `failover`.
- Виртуальные ключи с ограничением моделей и RPM.
- Админка: серверы, модели, ключи, лог, чат.
- Скачивание модели (`pull`) с полосой прогресса; F5 не обрывает задачу.
- Загрузка / выгрузка весов в RAM (`keep_alive: -1` / `0`).
- Playground: выбрать alias или имя Ollama и писать в чат без ключа (нужна сессия админки).

## Быстрый старт (локально)

Нужны Go 1.23+ и хотя бы один Ollama на `localhost:11434` или в LAN.

```bash
git clone https://github.com/javded-itres/mikrollm.git
cd mikrollm
go test ./...
make run
```

`make run` слушает `:4000`, пароль админки `admin`, каталог данных `./data`.

Откройте http://127.0.0.1:4000/admin → **Статус** → поправьте URL Ollama → **Модели** → подключите нужные → **Ключи** → выпустите `sk-…`.

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
| HTTP API | [docs/api.md](docs/api.md) |
| Устройство кода | [docs/architecture.md](docs/architecture.md) |

## Требования

- Ollama 0.3+ на хостах с моделями (сеть должна быть доступна из процесса MikroLLM).
- Для образа RouterOS: Docker Buildx, Python 3, USB-диск на роутере желателен.
- Порт **4000/tcp** (меняется флагом `-listen` / `MIKROLLM_LISTEN`).

## Конфигурация

| Флаг | Переменная | По умолчанию |
|---|---|---|
| `-listen` | `MIKROLLM_LISTEN` | `:4000` |
| `-data` | `MIKROLLM_DATA` | `./data` |
| `-admin-password` | `ADMIN_PASSWORD` | пусто: пароль генерируется и пишется в лог при **первом** старте |
| `-admin-password-reset` | `ADMIN_PASSWORD_RESET=1` | не сбрасывать |

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
