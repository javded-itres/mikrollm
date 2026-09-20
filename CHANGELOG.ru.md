# Changelog

[English](CHANGELOG.md) · **Русский**

## Unreleased

## 0.0.5 — 2026-09-20

- Зашитый адрес hub — `https://hub.mikrollm.ru` (`MIKROLLM_HUB_URL` по-прежнему переопределяет). Инструкция: [docs/ru/hub.md](docs/ru/hub.md).
- Чат админки: справа панель **параметры модели** (`GET /admin/model-params`). У OpenComfy видны обязательные поля (например `input_image`); без референса запрос не отправляется. Фото уходят как `input_image` / `input_images` и `input_references`.
- Playground: выбор video-only модели (Hailuo и т.п.) переключает тип на **Видео**, чтобы чат не ловил ответ OpenComfy `video models use POST /v1/videos`. Chat на video-only alias — 400 с этой подсказкой.
- Playground: в режиме **изображение** / **видео** можно прикрепить несколько фото-референсов (`+ фото`, вставка, drag-and-drop). Уходят как `input_references` (JPEG, до 6, с уменьшением). Тело media-relay в hub — до 8 МиБ.
- **Картинки OpenRouter:** `POST /v1/images/generations` идёт в OpenRouter `POST /images`, не в chat с `modalities: ["image","text"]`. Image-only модели (Flux, Seedream, …) больше не отвечают 404 «No endpoints found that support the requested output modalities». Правки кадра — `input_references`.
- **Hub и медиа:** image/video alias анонсируются и релеятся (`POST /v1/relay/{node}/images` и `/videos`, плюс статус/файл). Бинарь клипа — `b64` (лимит 6 МиБ). Chat на video-only alias по-прежнему 400. [docs/ru/hub.md](docs/ru/hub.md)
- **Картинки OpenComfy в чате админки:** `POST /v1/images/generations` скачивает same-host `data[].url` (файлы OpenComfy `/v1/files/…`) и отдаёт `b64_json`. CSP playground — `img-src 'self' data: blob:`, браузер не грузит `http://gpu:8788/…`. Чужие хосты не запрашиваются.
- Одна команда на компьютер: `curl -fsSL https://raw.githubusercontent.com/javded-itres/mikrollm/main/scripts/install.sh | sh` ставит Ollama (если нужно) и MikroLLM на Linux/macOS, поднимает сервис и с `-seed local` подключает локальные модели Ollama. [docs/ru/install-desktop.md](docs/ru/install-desktop.md)

- Бэкенд **OpenComfy**: локальный шлюз ComfyUI для картинок и видео (`:8788`). Статус → Добавить сервер → OpenComfy, URL без `/v1`, ключ `sk-…`. Каталог `/v1/models` + image/video models; `POST /v1/images/generations` и `POST /v1/videos` как у OpenAI (не chat+modalities OpenRouter). [docs/ru/providers.md](docs/ru/providers.md#opencomfy)
- **Сеть hub** (не P2P): на Статусе **Участник hub сети** — шлюз сам регистрируется на зашитом `https://hub.mikrollm.ru` (или `MIKROLLM_HUB_URL`). Помеченные alias анонсируются; хаб ходит long-poll и релеить chat. В этом репозитории только клиент (`internal/hubclient`). **Сервис** хаба — [`mikrollm_hub`](https://github.com/javded-itres/mikrollm_hub). [docs/ru/hub.md](docs/ru/hub.md)

## 0.0.4 — 2026-09-19

- Документация на двух языках; **по умолчанию английский** (`README.md`, `docs/`, GitHub Releases). Русский: `README.ru.md`, `docs/ru/`. [docs/ru/releasing.md](docs/ru/releasing.md)
- Вкладка **Биллинг**: сумма `$` за час / день / неделю / месяц / год, столбцы без лишних кнопок. История в `billing_hour`, не обрезается вместе с логом.
- Prompt cache OpenRouter: `system_prompt` не сплющивает multipart/`cache_control`; `X-Session-Id` на OpenRouter; default `auto` ставит top-level `cache_control` только на Claude (первый ход 1.25× write — выключатель на **Модели**). Логи/дашборд/MCP: `cached_tokens`, оценка $, `usage.cost` отдельно. [docs/providers.md](docs/ru/providers.md#prompt-cache)
- HTTPS в том же процессе: `-tls-cert`/`-tls-key` (PEM перечитывается без рестарта) или `-tls-auto` (self-signed в `<data>/tls`). Let's Encrypt: `-acme-hosts` (HTTP-01 на `:80`, автопродление). [docs/tls.md](docs/ru/tls.md)
- Провайдеры можно выключить без удаления: кнопка на **Статус**, MCP `upsert_provider` с `enabled`. Health и маршруты пропускают выключенный бэкенд; `enabled`/`weight` без поля не сбрасываются.
- Вкладка **Безопасность**: системный промпт, стоп-слова, PII, prompt injection (эвристика как LiteLLM), категории, regex. Фильтр вешается на alias, очередь и/или upstream; один id применяется один раз. [docs/security.md](docs/ru/security.md)
- NSFW / 18+ как плагины категорий (LiteLLM `content_filter`): `nsfw`, `adult`, отдельно `csam`. Тип политики `nsfw` включает оба сразу; MCP `list_plugins`.
- Медиа: `POST /v1/images/generations` и `POST /v1/videos` как у LiteLLM/OpenAI. OpenRouter-картинки идут через chat + `modalities`. В каталоге и playground модели с image/video помечены; в чате можно выбрать тип контента или эндпоинт.
- Playground: история генерации, правки к исходному запросу (тред + прошлый кадр для OpenRouter).
- Принудительное обновление каталога провайдера (кнопка на карточке и «Обновить каталоги»). OpenRouter тянет chat+image+video (`/videos/models`). Список моделей: пагинация 10 / 20 / 50 / 100 / все.
- Цены image/video из OpenRouter: `image_output` / `image_token` и `pricing_skus` (за кадр, за 1M img, за секунду ролика), не только prompt/completion за токены.

## 0.0.3 — 2026-09-08

Очереди запросов, MCP для агента, цены Ollama Cloud, фильтры лога, ужесточение админки.

- **Очереди**: шаги (локальные → облако), слоты, ожидание на диске (SQLite WAL, без Kafka/Redis), sticky HTTP — ответ всегда в то же соединение. Overflow-порог, лимиты `MIKROLLM_QUEUE_MAX_BYTES` / `MIKROLLM_QUEUE_MAX_JOBS` / `MIKROLLM_QUEUE_MAX_WAIT`. Вкладка `/admin/queues` и живая лента на дашборде
- Клиент может слать в `model` alias очереди, её **имя**, `имя-alias` или `имя/alias` (например `coder`, `itres`, `itres-coder`). Дополнительные alias вешаются на ту же очередь. В апстрим уходит настоящее имя модели, не клиентский alias
- **MCP** в том же процессе: `POST /mcp` (Streamable HTTP, JSON-RPC). Агент настраивает провайдеры, модели, очереди и ключи, читает логи и статус. Токен `mcp-…` (или пароль админки). [docs/mcp.md](docs/ru/mcp.md)
- Ollama Cloud: цены $/1M (вход/выход) с [ollama.com/pricing](https://ollama.com/pricing) и страниц `/library/<модель>` в каталоге, ключах и чате, как у OpenRouter
- Лог: поиск, фильтры 2xx/4xx/5xx, модель, бэкенд, ключ, задержка; до 500 строк
- Ключи: список моделей с именами и прокруткой, не плотная сетка без подписей
- Безопасность: CSRF, лимит логина по IP (без порта), cookie только на `/admin`, ключ не в URL, URL бэкенда только http(s), прокси не копирует Set-Cookie/CORS/Location, CSP/nosniff, без следования 3xx на апстрим, смена пароля крутит session secret
- Логотип в шапке, на логине и как favicon

## 0.0.2 — 2026-09-08

Бэкенды vLLM, LM Studio, OpenRouter и Ollama Cloud; админка, ключи, каталог и запасные модели.

- Бэкенды **vLLM** и **LM Studio** рядом с Ollama (тип + необязательный Bearer). vLLM поднимает модель через `vllm serve`; LM Studio — download / load / unload по REST v1. Инструкции: [docs/providers.md](docs/ru/providers.md)
- **OpenRouter** и **Ollama Cloud** напрямую по HTTPS, без своего GPU-сервера. Образ RouterOS с корневыми CA
- OpenRouter: health через `GET /key` (каталог кэшируется), разбор тела 403; ключ сохраняется без лишнего `Bearer`
- 403 с IP РФ: контейнер `192.168.254.5` нужно вести через тот же VPN, что LAN — [docs/install-mikrotik.md](docs/ru/install-mikrotik.md#openrouter-403)
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
