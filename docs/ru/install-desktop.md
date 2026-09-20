# Установка на компьютер (Ollama + MikroLLM)

[English](../install-desktop.md) · **Русский**

Одна команда на **Linux** или **macOS**. Ставит [Ollama](https://ollama.com) при необходимости, скачивает бинарь MikroLLM (или собирает через Go), поднимает пользовательский сервис и при первом запуске подключает все локальные модели Ollama как alias шлюза.

```bash
curl -fsSL https://raw.githubusercontent.com/javded-itres/mikrollm/main/scripts/install.sh | sh
```

Из клона репозитория:

```bash
sh scripts/install.sh
```

Дальше **http://127.0.0.1:4000/admin**. Пароль печатается в конце скрипта и лежит в `~/.mikrollm/admin.pass`.

## Что происходит

1. Определяет ОС/архитектуру (`linux`/`darwin`, `amd64`/`arm64`).
2. Ставит Ollama (`brew install ollama` на macOS, если есть Homebrew; официальный инсталлятор на Linux) и запускает `:11434`.
3. Кладёт `mikrollm` в `~/.local/bin` с GitHub Releases или `go build`, если ассета нет.
4. Пишет `~/.mikrollm/env` и стартует:
   - macOS: LaunchAgent `app.mikrollm`
   - Linux: systemd user `mikrollm.service`, иначе `nohup`
5. Запускает MikroLLM с `-seed local`: бэкенд `ollama` → `http://127.0.0.1:11434`, alias на то, что уже есть в `ollama list`.

Скачать модель до подключения:

```bash
MIKROLLM_PULL=llama3.2 curl -fsSL https://raw.githubusercontent.com/javded-itres/mikrollm/main/scripts/install.sh | sh
```

## Пути

| Путь | Назначение |
|---|---|
| `~/.local/bin/mikrollm` | бинарь |
| `~/.mikrollm/` | SQLite, логи, env |
| `~/.mikrollm/admin.pass` | пароль админки (режим 600) |
| `~/.mikrollm/run.sh` | обёртка сервиса |

Переопределение: `MIKROLLM_PREFIX`, `MIKROLLM_DATA`, `MIKROLLM_LISTEN`, `MIKROLLM_REPO`.

Если скрипт попросил — добавьте `~/.local/bin` в `PATH`.

## После установки

- **Модели**: новый `ollama pull` — Админка → Модели → Обновить каталоги → подключить (автоподключение только пока таблица alias пустая).
- **Ключи**: Админка → Ключи → `sk-…` для `/v1/chat/completions`.
- **Чат**: http://127.0.0.1:4000/admin/chat (без ключа).

Остановка:

```bash
# macOS
launchctl unload ~/Library/LaunchAgents/app.mikrollm.plist

# Linux (systemd user)
systemctl --user disable --now mikrollm.service
```

Роутеры Keenetic — другой путь (Entware, не этот скрипт): [install-keenetic.md](install-keenetic.md).

## Windows

Отдельного инсталлятора нет. [WSL](https://learn.microsoft.com/windows/wsl) и та же строка `curl … | sh` внутри дистрибутива. Нативный бинарь: `GOOS=windows go build ./cmd/mikrollm`.

## Вместо этого — из исходников

[install-local.md](install-local.md) (`go run` / `make run`). Там по умолчанию сеются LAN-хосты `mac-80` / `mac-82`, если не указать `-seed local`.
