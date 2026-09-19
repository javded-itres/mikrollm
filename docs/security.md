# Security

**English** · [Русский](ru/security.md)

Tab **Admin → Security**. Filters live in SQLite and run in the gateway process (no Lakera/Bedrock/Presidio — no Python and no extra APIs on MikroTik).

Same idea as LiteLLM guardrails: a named policy, `pre`/`post` mode, block/mask action, attached to models. Prompt injection is the LiteLLM `detect_prompt_injection` heuristic (phrases + verb×instruction), no second LLM call.

## Kinds

| Kind | What it does |
|---|---|
| `system_prompt` | Inserts (or prepends) `role: system` on every chat request. Multipart `content` is not flattened: the policy is a separate text part **before** cached parts; `cache_control` on parts is kept |
| `block_words` | Stop words and phrases |
| `prompt_injection` | Jailbreak / “ignore previous instructions” (EN+RU) |
| `pii` | Email, phone, card, IP — block or mask |
| `nsfw` | Plugins **nsfw** + **adult** (explicit / 18+), like LiteLLM content_filter categories |
| `category` | Plugins: nsfw, adult, csam, violence, self_harm, hate, weapons, drugs |
| `regex` | Your RE2 expression |

Plugins are built-in word lists (no external API). Kind `nsfw` enables both 18+ plugins; `category` can enable them separately. Extra phrases go in `words`. CSAM is not mixed with adult.

`post` inspects the model reply and **does not** run on `stream: true` (that would require buffering the whole stream).

Client error: HTTP 400, `"type": "content_filter"`, header `X-MikroLLM-Guardrail`.

## Where to attach

- gateway **alias** (`coder`)
- **queue** (its alias, e.g. `coder` on queue `itres`)
- **upstream model** (`ornith-1.5:35b`)

One filter can be checked on all three. The gateway collects policies from the queue (if the request hit one), the step alias, and upstream — and **keeps each id once**.

## MCP

`list_policies`, `list_plugins`, `save_policy`, `delete_policy`. NSFW on alias `coder`:

```json
{ "name": "no-nsfw", "kind": "nsfw", "aliases": ["coder"] }
```

System prompt:

```json
{
  "name": "itres-voice",
  "kind": "system_prompt",
  "prompt": "You are the ITRES assistant. Do not invent internal URLs.",
  "aliases": ["coder"]
}
```
