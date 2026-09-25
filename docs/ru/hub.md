# Сеть hub

[English](../hub.md) · **Русский**

MikroLLM может войти в **облачный hub**: узел за NAT (MikroTik, домашняя сеть, ноутбук) **отдаёт выбранные alias** и **берёт alias других участников**, без входящих портов и VPN.

Это **не** P2P. Каждый шлюз только сам ходит наружу по **HTTPS**. Хаб — **каталог + relay задач**. Секреты бэкендов (`sk-or-…`, LAN Ollama) на хаб не уходят.

| Часть | Где | Роль |
|---|---|---|
| **Клиент** | этот репозиторий (`internal/hubclient`) | Вход, анонс alias, pull задач, локальный запуск |
| **Сервис** | [`mikrollm_hub`](https://github.com/javded-itres/mikrollm_hub) (Go API + Next.js UI) | Каталог, long-poll, relay |
| **Публичный hub** | `https://hub.mikrollm.ru` | Адрес по умолчанию, зашит в бинарь MikroLLM |

Переопределение: `MIKROLLM_HUB_URL` (свой/staging хаб). В админке это env не редактируется.

```
Клиент A (админка / ключ sk-)
    │  POST /v1/chat|images|videos  (model = hub-alias)
    ▼
MikroLLM на узле A
    │  POST https://hub.mikrollm.ru/v1/relay/{nodeB}/chat|images|videos
    ▼
Hub (каталог + ждёт B)
    │  задача на GET /v1/pull  (B уже в long-poll)
    ▼
MikroLLM на узле B  →  локальный Ollama / OpenComfy / …
    │  POST /v1/result
    ▼
Hub отдаёт JSON (или байты видео) узлу A
```

Онлайн в каталоге = узел ходит в pull (last seen **&lt; 45 с**). Анонс примерно каждые **15 с**; переключение «в hub» будит цикл сразу (**Kick**).

---

## 1. Войти в hub (свой узел)

1. Админка → **Статус**.
2. Блок **Участник hub сети**.
3. Короткое **имя** (например `hap-ax3`, `ams-1`, `home-mac`). Так вас видят другие.
4. Галочка **Участник hub сети** → сохранить.
5. По желанию **Шарить по расписанию** — окно времени и дни недели (пустые дни = каждый день). Пояс IANA (по умолчанию `Europe/Moscow`). Вне окна узел остаётся в каталоге, alias «не сейчас», relay отвечает 503.

Клиент:

- один раз `POST /v1/register` (id узла и токен в SQLite);
- `PUT /v1/announce` со списком alias с пометкой **в hub**;
- цикл `GET /v1/pull` (long-poll ~20 с).

Индикатор: **online** / **off** / **error** (текст рядом).

Пока галочка включена, в шлюзе живёт зарезервированный alias **`auto`** — только чат. Оператор хаба (**Оператор → Настройки**) отмечает несколько чат-alias и каждому задаёт уровень: **быстрая**, **средняя**, **сильная**. На каждый запрос хаб выбирает уровень по тексту (короткий вопрос, обычный диалог, код / tools / длинный текст) и ставит задачу первой живой модели этого уровня. Классификатор — фиксированные правила, его не обучают и текст промпта не сохраняют. В ответе заголовок `X-MikroLLM-Routed-Model` показывает `узел/alias`. Картинки и видео вызываются своим alias, не `auto`. Старый шлюз без пула по-прежнему ходит в одну пару из каталога (`defaults.node_id` + `defaults.alias`). Вышли из сети (или оператор очистил список) — `auto` пропадает.

Снять галочку — выйти из сети. **id узла сохраняется**; повторное включение использует ту же регистрацию.

Зашитый URL: `https://hub.mikrollm.ru`. Свой хаб:

```bash
MIKROLLM_HUB_URL=http://127.0.0.1:4090
```

На RouterOS — в envlist контейнера (`MIKROLLM_HUB_URL`) и перезапуск. Снаружи достаточно Caddy на `:4090` (TLS + reverse_proxy).

Каталог hosted-хаба в браузере: `https://hub.mikrollm.ru`, если оператор проксирует Next UI; **API** всегда `/v1/catalog`, `/v1/relay/…`.

---

## 2. Модели и alias (сначала локально, потом hub)

У MikroLLM два слоя. В hub попадают только **опубликованные alias**.

### Каталог (диски / облака)

Вкладка **Модели** — то, что отдали бэкенды (теги Ollama, slug OpenRouter, id OpenComfy).

1. **Обновить каталоги** (без кэша). Фильтры: сервер, **Все с hub**, провайдер, image/video/chat, цена.
2. Отметить строки → **В шлюз**. Так создаётся **alias** с тем же именем (или своим именем для клиентов).
3. OpenRouter / Ollama Cloud не скачиваются — они уже в облаке. OpenComfy — локальный шлюз ComfyUI (`:8788`).

### Таблица «Alias в шлюзе»

Это то, что клиенты пишут в `model:` (ключи `sk-`, playground, очереди).

| Колонка / элемент | Смысл |
|---|---|
| Клиентам | Alias (`coder`, `toy-image`, `minimax-hailuo-02`) |
| На сервере | Upstream id (`ornith-1.5:35b`, slug OpenRouter) |
| Серверы | На каких бэкендах крутить (LB) |
| Пилюля **в hub** | Alias **опубликован** в облачный каталог |
| Пилюля **с hap-ax3** | Alias **взят с другого узла** (повторно не шарится) |

**Свой alias:** например `fast` → `qwen3.8:27b-mlx`, чтобы приложения не меняли имя.

LB: `least_conn` (по умолчанию), `round_robin`, `failover`. Запасная модель на 402/квоту. Контекст можно переопределить.

RAM / pull / цены: [admin.md](admin.md#модели).

---

## 3. Отдать свои alias («в hub»)

Шарится только **локальный** alias (ваш Ollama, OpenComfy, …). Чужой hub-alias повторно опубликовать нельзя (нет вложенного relay).

1. Включён **Участник hub сети** (шаг 1).
2. **Модели** → **Alias в шлюзе**.
3. В строке: Hub **локально** → **в hub**.
4. Или отметить несколько строк → **В hub** / **Убрать из hub**.
5. Фильтр таблицы: все / шарятся / только локально / с другого MikroLLM.

За ~15 с (часто быстрее после Kick) alias появляется в `GET /v1/catalog` и в каталоге **Моделей** у других (фильтр **Все с hub** или `hub · <имя>`).

Можно шарить chat, **картинки** и **видео**. Поле `media` берётся с модели (и из имени: Hailuo → video, Flux → image).

Через контейнер RouterOS на **64 МБ** не гоняйте огромный контекст и длинное видео — такой узел лучше на ПК/VPS.

---

## 4. Взять чужую модель

На **своём** MikroLLM (к нему ходят ваши приложения):

1. **Модели** → фильтр **Все с hub** или `hub · hap-ax3`.
2. Отметить чужую строку (провайдер = имя узла, пилюля «онлайн»).
3. **В шлюз**. Появится **локальный alias** с `HubNodeID`. Для клиентов это обычное имя.
4. Playground, ключи `sk-` и очереди работают как с любым alias.

Маршрут: ваш шлюз делает `POST` на хаб `/v1/relay/{node}/chat` (или `/images`, `/videos`). **Хозяин** забирает задачу pull-ом и считает её на **своём** бэкенде.

Если пир офлайн (нет pull 45 с): **502** / «peer offline». Обновите каталоги; пилюля покажет офлайн.

В playground можно выбрать строку каталога `hub|<nodeId>|<alias>` без «В шлюз» — для приложений и ключей удобнее подключить alias.

---

## 5. Playground (Чат)

Тип **Чат / Изображение / Видео** должен совпадать с моделью:

| Тип | Эндпоинт | Типичные модели |
|---|---|---|
| Чат | `POST /v1/chat/completions` | Ollama, LLM на OpenRouter |
| Изображение | `POST /v1/images/generations` | OpenComfy `toy-image`, Flux, Gemini image |
| Видео | `POST /v1/videos`, затем опрос | Hailuo, Seedance |

Выбор video-only модели **сам** ставит тип «Видео» (для image-only — «Изображение»). Chat на Hailuo даёт 400: нужен тип Видео.

**Фото-референсы** (изображение/видео): **+ фото**, вставка, drag-and-drop. До 6 JPEG (уменьшаются). Уходят как `input_references` (и `input_image` / `input_images`, если схема требует). Если workflow OpenComfy требует `input_image`, без файла отправка блокируется.

Hub-alias картинки/видео работает так же: playground бьёт в **ваш** `/admin/images`, а шлюз релеить на хозяина.

---

## 6. Приложения, ключи, очереди

Ключ на вкладке **Ключи**. Разрешите hub-alias так же, как локальный.

```bash
curl http://127.0.0.1:4000/v1/chat/completions \
  -H "Authorization: Bearer sk-…" \
  -H "Content-Type: application/json" \
  -d '{"model":"ornith-1.5:35b","messages":[{"role":"user","content":"hi"}],"stream":false}'
```

Картинки: `POST /v1/images/generations` с `"model":"<hub-alias>"`. Видео: `POST /v1/videos`.

**Очереди:** шаг может быть hub-alias (после **В шлюз**). Пример: локальная 7B → hub `hap-ax3` 35B → OpenRouter. Клиент по-прежнему шлёт одно имя alias/очереди. [admin.md](admin.md#очереди).

---

## 7. Лимиты и тайминги

| Лимит | Значение |
|---|---|
| Тело chat-relay | 1 МиБ, `stream=false` |
| Тело image/video (промпт + референсы) | 8 МиБ |
| Бинарь ответа (клип) | 6 МиБ (иначе **413** на маленьком узле) |
| Ожидание relay | 120 с |
| Онлайн в каталоге | last pull &lt; 45 с |
| Анонс | ~каждые 15 с (Kick при шаринге/входе) |
| Обновление каталога у участника | ~15 с или кнопка **Обновить каталоги** |

RouterOS `memory-high 64M`: шарьте лёгкое (chat, небольшие картинки). Тяжёлое видео — на ПК/VPS.

---

## 8. Чего не делать

- Не шарьте по умолчанию ключ **OpenRouter** (платить будете вы). Шарьте **локальный** Ollama / OpenComfy.
- Не ждите вложенного шаринга (hub-alias нельзя опубликовать снова).
- Не отправляйте **видео**-модель как чат.
- Не цельтесь с MikroTik во внутренний WG-IP хаба (`10.88.97.1:4090` с LAN не отвечает). Нужен `https://hub.mikrollm.ru` или публичный `host:4090`.

---

## 9. Свой хаб

Репозиторий: [`mikrollm_hub`](https://github.com/javded-itres/mikrollm_hub).

```bash
# API
go run ./cmd/hub -listen :4090 -data ./hub-data
# UI
cd web && HUB_INTERNAL_URL=http://127.0.0.1:4090 npm run dev   # :3000
```

TLS (пример Caddy):

```
hub.example.com {
	encode gzip
	request_body { max_size 50MB }
	reverse_proxy 127.0.0.1:4090 {
		flush_interval -1
		transport http {
			read_timeout 180s
			write_timeout 180s
		}
	}
}
```

Участники: `MIKROLLM_HUB_URL=https://hub.example.com`. Оператор `/operator` — пароль `HUB_ADMIN_PASSWORD`.

---

## 10. Протокол (узел → хаб)

Все вызовы, кроме register, с `Authorization: Bearer <токен узла>`.

| Метод | Путь | Заметка |
|---|---|---|
| POST | `/v1/register` | `{name}` → `{node_id, token}` |
| PUT | `/v1/announce` | `{name, aliases:[{alias, media, context}]}` |
| GET | `/v1/pull` | Long-poll ~20 с; `204` тихо; `200` + `{job_id, alias, kind, ref, body}` |
| POST | `/v1/result` | `{job_id, status, body}` или `{b64, content_type}` |
| GET | `/v1/catalog` | Публично. Онлайн = видели за 45 с, не в бане |
| POST | `/v1/relay/{node}/chat` | Chat JSON (`model` в теле), ждать ≤ 120 с |
| POST | `/v1/relay/{node}/images` | Images JSON |
| POST | `/v1/relay/{node}/videos` | Создание видео |
| POST | `/v1/relay/{node}/videos/{id}` | Статус видео |
| POST | `/v1/relay/{node}/videos/{id}/content` | Байты видео |

Хозяин ставит `X-MikroLLM-Hub-Relay` на внутренний `/v1/chat/completions`, `/v1/images/generations` или `/v1/videos`, `sk-` не нужен. Исполняются только alias с пометкой **в hub**.

`kind` задачи: `chat` (по умолчанию), `images`, `videos`, `videos_status`, `videos_content`.

---

## 11. Если не работает

| Симптом | Что проверить |
|---|---|
| Статус **off** | Галочка, сохранение, доступен ли `MIKROLLM_HUB_URL` (HTTPS, DNS, фаервол) |
| Статус **error** | Текст у индикатора; 401 → токен сброшен, регистрация заново |
| Alias нет в каталоге | Пилюля **в hub**, подождать 15 с, **Обновить каталоги** у соседа |
| 502 peer offline | Хозяин должен ходить в pull; окно 45 с |
| `video models use POST /v1/videos` | Тип playground **Видео**, не Чат |
| Пустая картинка в чате | URL файла OpenComfy и CSP — текущий шлюз встраивает `b64_json` |
| OpenRouter «output modalities: image, text» | Картинки идут в `POST /images`, не в chat; обновите шлюз |
| 413 на видео | Клип &gt; 6 МиБ через маленький узел |
| 400 на картинку через hub на старых сборках | Нужен шлюз, который релеить `/images` и `/videos`, не только chat |

**Лог** админки: backend — имя узла hub; ключ `hub` — входящая relay-задача на хозяине.
