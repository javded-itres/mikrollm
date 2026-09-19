# Безопасность

[English](../security.md) · **Русский**

Вкладка **Админка → Безопасность**. Фильтры живут в SQLite, считаются в процессе шлюза (без Lakera/Bedrock/Presidio — на MikroTik нет Python и лишних API).

По смыслу как guardrails LiteLLM: именованная политика, режим `pre`/`post`, действие block/mask, привязка к моделям. Prompt injection — эвристика LiteLLM `detect_prompt_injection` (фразы + глагол×инструкция), без второго вызова LLM.

## Типы

| Тип | Что делает |
|---|---|
| `system_prompt` | Вставляет (или дополняет) `role: system` в каждый chat-запрос. Multipart `content` не сплющивается: политика — отдельный text-блок **перед** кэшированными частями, `cache_control` на блоках сохраняется |
| `block_words` | Стоп-слова и фразы |
| `prompt_injection` | Jailbreak / «ignore previous instructions» (EN+RU) |
| `pii` | Email, телефон, карта, IP — block или mask |
| `nsfw` | Плагины **nsfw** + **adult** (explicit / 18+), как категория LiteLLM content_filter |
| `category` | Плагины: nsfw, adult, csam, violence, self_harm, hate, weapons, drugs |
| `regex` | Своё RE2-выражение |

Плагины — встроенные списки слов (без внешнего API). Тип `nsfw` включает оба 18+-плагина сразу; в типе `category` их можно включить по отдельности. Свои фразы — поле words. CSAM не смешивается с adult.

`post` смотрит ответ модели и **не** работает на `stream: true` (иначе пришлось бы буферизовать весь поток).

Ошибка клиенту: HTTP 400, `"type": "content_filter"`, заголовок `X-MikroLLM-Guardrail`.

## Куда вешать

- **alias** шлюза (`coder`)
- **очередь** (её alias, например `coder` у очереди `itres`)
- **модель на сервере** (upstream `ornith-1.5:35b`)

Один фильтр можно отметить на всех трёх. Шлюз собирает политики с очереди (если запрос пришёл в неё), alias шага и upstream — и **оставляет каждый id один раз**.

## MCP

`list_policies`, `list_plugins`, `save_policy`, `delete_policy`. NSFW на alias `coder`:

```json
{ "name": "no-nsfw", "kind": "nsfw", "aliases": ["coder"] }
```

Системный промпт:

```json
{
  "name": "itres-voice",
  "kind": "system_prompt",
  "prompt": "Ты ассистент ITRES. Не выдумывай внутренние URL.",
  "aliases": ["coder"]
}
```
