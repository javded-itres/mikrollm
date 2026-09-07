# Changelog

## Unreleased

## 0.0.2 — 2026-09-08

Бэкенды vLLM, LM Studio, OpenRouter и Ollama Cloud; админка, ключи, каталог и запасные модели.

- Бэкенды **vLLM** и **LM Studio** рядом с Ollama (тип + необязательный Bearer). vLLM поднимает модель через `vllm serve`; LM Studio — download / load / unload по REST v1. Инструкции: [docs/providers.md](docs/providers.md)
- **OpenRouter** и **Ollama Cloud** напрямую по HTTPS, без своего GPU-сервера. Образ RouterOS с корневыми CA
- OpenRouter: health через `GET /key` (каталог кэшируется), разбор тела 403; ключ сохраняется без лишнего `Bearer`
- 403 с IP РФ: контейнер `192.168.254.5` нужно вести через тот же VPN, что LAN — [docs/install-mikrotik.md](docs/install-mikrotik.md#openrouter-403)
- Ключи: полный список alias и моделей при создании, правка allowlist после выпуска
- Контекст модели (`max_input_tokens`) в админке и в `GET /v1/models`, `GET /v1/model/info`
- Каталог и ключи: фильтр по провайдеру и цене, имя провайдера и $/1M (вход/выход)
- Alias: запасная модель при 402 / конце кредитов или подписки (`X-MikroLLM-Fallback`)
- `/api/chat` на не-Ollama бэкенд переписывается в OpenAI chat completions
- Админка: список серверов, чат с прокруткой и закреплением ввода, баннер pull скрывается через 5 с

## 0.0.1 — 2026-09-07

Первый публичный релиз.

- OpenAI `/v1/chat/completions` и Ollama `/api/chat` / `/api/tags`
- Несколько бэкендов Ollama, health-check, LB
- Виртуальные ключи, RPM, allowlist
- Админка: модели, RAM, pull с прогрессом, ключи, playground-чат, лог
- Образ scratch linux/arm64 для RouterOS 7
- SQLite без CGO
