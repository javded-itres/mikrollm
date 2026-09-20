# Документация MikroLLM

[English](../README.md) · **Русский**

Английский — язык по умолчанию. Эта папка — русские копии. Релизы GitHub пишутся по-английски ([releasing.md](releasing.md)).

| Документ | О чём |
|---|---|
| [install-desktop.md](install-desktop.md) | Одна команда: Ollama + MikroLLM на Linux/macOS |
| [install-local.md](install-local.md) | Запуск с исходников, флаги, данные |
| [install-docker.md](install-docker.md) | Docker и systemd на сервере |
| [install-mikrotik.md](install-mikrotik.md) | Контейнер RouterOS 7, veth, dst-nat, USB |
| [install-keenetic.md](install-keenetic.md) | KeeneticOS Entware (USB, без Docker) |
| [tls.md](tls.md) | HTTPS: свой PEM или self-signed в контейнере |
| [admin.md](admin.md) | Админка: серверы, очереди, модели, RAM, ключи, чат, лог, биллинг |
| [security.md](security.md) | Фильтры, системный промпт, prompt injection |
| [providers.md](providers.md) | Ollama, Ollama Cloud, OpenRouter, vLLM, LM Studio; prompt cache |
| [api.md](api.md) | OpenAI / Ollama API, chat / images / videos, ключи |
| [mcp.md](mcp.md) | MCP: агент настраивает модели, очереди, ключи, логи |
| [architecture.md](architecture.md) | Пакеты, порты, внедрение зависимостей |
| [hub.md](hub.md) | Клиент hub: вход в облачный каталог (сервис — отдельный репозиторий) |
| [load-test.md](load-test.md) | Полевые RPS: оверхед шлюза, локальная 35B, облако 32k–128k, RAM MikroTik |
| [releasing.md](releasing.md) | Английские GitHub Releases из changelog |

Заметки по дизайну: [design-prompt-cache.md](design-prompt-cache.md), [design-request-cache.md](design-request-cache.md).

Краткий обзор — в [корневом README](../../README.ru.md).
