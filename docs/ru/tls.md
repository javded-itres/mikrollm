# TLS / HTTPS

[English](../tls.md) · **Русский**

TLS слушает **тот же процесс** MikroLLM, без nginx и без второго контейнера. На RouterOS это важно: `memory-high=64M`.

Пока флаги не заданы — как раньше, обычный HTTP на `-listen` (по умолчанию `:4000`). Включили TLS — на этом порту только HTTPS. dst-nat `4000→192.168.254.5:4000` не меняется.

## Варианты

| Как | Когда |
|---|---|
| `-acme-hosts` | Let's Encrypt **сам** выпускает и продлевает. Нужен публичный FQDN и порт 80 с интернета |
| `-tls-cert` + `-tls-key` | Готовый PEM (certbot, CA MikroTik). Файлы перечитываются без рестарта |
| `-tls-auto` / `MIKROLLM_TLS_AUTO=1` | LAN, self-signed ECDSA в `<data>/tls/` |
| ничего | HTTP, как в 0.0.3 |

`-acme-hosts` нельзя смешивать с `-tls-cert` / `-tls-auto`.

Пути относительно `-data`, если не абсолютные. Пара cert/key обязательна целиком.

| Флаг | Переменная | Смысл |
|---|---|---|
| `-tls-cert` | `MIKROLLM_TLS_CERT` | PEM сертификата (можно цепочка) |
| `-tls-key` | `MIKROLLM_TLS_KEY` | PEM ключа |
| `-tls-auto` | `MIKROLLM_TLS_AUTO=1` | создать `<data>/tls/cert.pem` и `key.pem`, если их нет |
| `-tls-hosts` | `MIKROLLM_TLS_HOSTS` | SAN для auto: `192.168.88.1,192.168.254.5,mikrollm.lan` |
| `-acme-hosts` | `MIKROLLM_ACME_HOSTS` | FQDN для Let's Encrypt, через запятую |
| `-acme-email` | `MIKROLLM_ACME_EMAIL` | почта аккаунта LE (напоминания об истечении) |
| `-acme-http` | `MIKROLLM_ACME_HTTP` | HTTP-01, по умолчанию `:80`; `off` — только TLS-ALPN-01 |
| `-acme-dir` | `MIKROLLM_ACME_DIR` | кэш аккаунта и сертификатов, по умолчанию `<data>/acme` |
| `-acme-staging` | `MIKROLLM_ACME_STAGING=1` | тестовый CA Let's Encrypt (не доверен браузерами) |

Минимум TLS 1.2. Cookie админки на HTTPS ставится с `Secure`. HSTS нет — иначе LAN легко «прибить» после отката на HTTP.

Self-signed браузер покажет предупреждение. Для MCP/Grok/curl нужна доверенная цепочка (импорт CA) или свой сертификат. `curl -k` только для проверки.

## MikroTik: auto

В envlist (короче, чем длинный `cmd`):

```routeros
/container envs add name=mikrollm key=MIKROLLM_TLS_AUTO value=1
/container envs add name=mikrollm key=MIKROLLM_TLS_HOSTS value="192.168.88.1,192.168.254.5,localhost"
```

Перезапустите контейнер. Файлы появятся на USB: `/usb1/docker/mikrollm-data/tls/`. Лог: `listening https :4000`.

```bash
curl -k -sS https://192.168.88.1:4000/health
# {"ok":true}
```

Админка: `https://192.168.88.1:4000/admin`  
MCP: `https://192.168.88.1:4000/mcp`

Grok с self-signed, скорее всего, не проверит сертификат. Импортируйте `cert.pem` в связку «система» как доверенный, либо поставьте сертификат, подписанный CA, которой Mac уже верит.

## MikroTik: сертификат RouterOS

На роутере (имена и IP подставьте свои). Команды лучше по одной — CLI ломает длинные строки.

```routeros
/certificate add name=lan-ca common-name="ITRES LAN CA" key-usage=key-cert-sign,crl-sign days-valid=3650
/certificate sign lan-ca
/certificate add name=mikrollm common-name="192.168.88.1" subject-alt-name="IP:192.168.88.1,IP:192.168.254.5,DNS:localhost" days-valid=825
/certificate sign mikrollm ca=lan-ca
/certificate export-certificate mikrollm type=pem
/certificate export-certificate lan-ca type=pem
```

Файлы в `/file` (`cert_export_mikrollm.crt`, `.key`, CA `.crt`). Скопируйте на том данных:

```
/usb1/docker/mikrollm-data/tls/cert.pem   — сертификат сервера (при желании + CA в тот же PEM)
/usb1/docker/mikrollm-data/tls/key.pem    — ключ, права как у контейнера
```

```routeros
/container envs add name=mikrollm key=MIKROLLM_TLS_CERT value=/data/tls/cert.pem
/container envs add name=mikrollm key=MIKROLLM_TLS_KEY value=/data/tls/key.pem
```

`MIKROLLM_TLS_AUTO` тогда не нужен. CA (`lan-ca`) поставьте на Mac в связку ключей — предупреждения в Safari/Chrome пропадут, MCP начнёт ходить без `-k`.

## Let's Encrypt (автовыпуск)

LE **не выдаёт** сертификат на `192.168.88.1`. Нужно имя в публичном DNS, например `llm.example.com`.

MikroLLM сам проходит HTTP-01, кладёт ключи в `<data>/acme` и продлевает (~за 30 дней до конца). Отдельный certbot не нужен.

Рекомендуемая схема на MikroTik — **открыть в интернет только TCP 80** (челлендж). Админка и API остаются на LAN `:4000`. WAN 443 лучше не пробрасывать: иначе весь шлюз окажется в интернете.

1. DNS: `llm.example.com` **A** на белый WAN-IP роутера. С LAN — статика на роутере, чтобы имя резолвилось в `192.168.88.1` (без hairpin):

```routeros
/ip dns static add name=llm.example.com address=192.168.88.1
```

2. Проброс только 80 на контейнер:

```routeros
/ip firewall nat add chain=dstnat dst-address-type=local protocol=tcp dst-port=80 \
  action=dst-nat to-addresses=192.168.254.5 to-ports=80 comment="mikrollm ACME"
/ip firewall filter add chain=input protocol=tcp dst-port=80 action=drop comment="ACME not to ROS www"
```

`dst-address-type=local` ловит WAN; поправьте `dst-address` на ваш белый IP, если правило слишком широкое. На input роутера порт 80 не должен перехватывать www ROS — NAT в контейнер должен быть раньше.

3. Env контейнера (команды по одной):

```routeros
/container envs add name=mikrollm key=MIKROLLM_ACME_HOSTS value=llm.example.com
/container envs add name=mikrollm key=MIKROLLM_ACME_EMAIL value=you@example.com
```

Контейнер слушает `:80` (HTTP-01) и HTTPS на `-listen` (`:4000`). Первый выпуск может занять до минуты. Лог: `listening https :4000 (Let's Encrypt …)`.

```bash
curl -sS https://llm.example.com:4000/health
```

Grok MCP:

```toml
[mcp_servers.mikrollm]
url = "https://llm.example.com:4000/mcp"
headers = { "Authorization" = "Bearer mcp-…" }
```

Сначала проверьте на staging (`MIKROLLM_ACME_STAGING=1`), чтобы не упереться в лимит LE. Staging-сертификат браузер не примет — это нормально. Потом уберите staging и перезапустите (кэш в `/data/acme` для staging и prod лучше не смешивать: смените `MIKROLLM_ACME_DIR` или сотрите каталог `acme`).

Если WAN без белого IP (CGNAT) — HTTP-01 не дойдёт. Тогда выпуск с другой машины (DNS-01) и файлы в `/data/tls`:

```bash
# на Mac, DNS-01 через ваш DNS (пример acme.sh)
acme.sh --issue --dns dns_cf -d llm.example.com
cp ~/.acme.sh/llm.example.com_ecc/fullchain.cer /path/to/mikrollm-data/tls/cert.pem
cp ~/.acme.sh/llm.example.com_ecc/llm.example.com.key /path/to/mikrollm-data/tls/key.pem
```

```routeros
/container envs add name=mikrollm key=MIKROLLM_TLS_CERT value=/data/tls/cert.pem
/container envs add name=mikrollm key=MIKROLLM_TLS_KEY value=/data/tls/key.pem
```

MikroLLM перечитывает PEM при следующем TLS-хендшейке, рестарт не обязателен. ACME внутри процесса в этом режиме не используется.

TLS-ALPN-01 (без порта 80) сработает, только если Let's Encrypt достучится до **443** на имени. Для этого нужен dst-nat WAN 443 → контейнер (тот же порт, что `-listen`, либо слушайте `:443`). Это уже публикация HTTPS в интернет — фильтруйте source.

## Docker / локально

```bash
# auto
MIKROLLM_TLS_AUTO=1 MIKROLLM_TLS_HOSTS=127.0.0.1,localhost \
  ./mikrollm -listen :4000 -data ./data -admin-password admin

curl -k https://127.0.0.1:4000/health
```

```yaml
environment:
  ADMIN_PASSWORD: "смените-на-свой"
  MIKROLLM_TLS_CERT: /data/tls/cert.pem
  MIKROLLM_TLS_KEY: /data/tls/key.pem
```
