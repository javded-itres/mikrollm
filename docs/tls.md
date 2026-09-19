# TLS / HTTPS

**English** · [Русский](ru/tls.md)

TLS listens in the **same MikroLLM process**, no nginx and no second container. That matters on RouterOS: `memory-high=64M`.

With no flags — plain HTTP on `-listen` (default `:4000`). Enable TLS — that port is HTTPS only. dst-nat `4000→192.168.254.5:4000` does not change.

## Modes

| How | When |
|---|---|
| `-acme-hosts` | Let's Encrypt **issues and renews**. Need a public FQDN and port 80 from the internet |
| `-tls-cert` + `-tls-key` | Ready PEM (certbot, MikroTik CA). Files are re-read without restart |
| `-tls-auto` / `MIKROLLM_TLS_AUTO=1` | LAN, self-signed ECDSA in `<data>/tls/` |
| nothing | HTTP, as in 0.0.3 |

`-acme-hosts` cannot mix with `-tls-cert` / `-tls-auto`.

Paths are relative to `-data` unless absolute. cert/key must both be set.

| Flag | Env | Meaning |
|---|---|---|
| `-tls-cert` | `MIKROLLM_TLS_CERT` | certificate PEM (chain allowed) |
| `-tls-key` | `MIKROLLM_TLS_KEY` | key PEM |
| `-tls-auto` | `MIKROLLM_TLS_AUTO=1` | create `<data>/tls/cert.pem` and `key.pem` if missing |
| `-tls-hosts` | `MIKROLLM_TLS_HOSTS` | SANs for auto: `192.168.88.1,192.168.254.5,mikrollm.lan` |
| `-acme-hosts` | `MIKROLLM_ACME_HOSTS` | Let's Encrypt FQDNs, comma-separated |
| `-acme-email` | `MIKROLLM_ACME_EMAIL` | LE account email (expiry mail) |
| `-acme-http` | `MIKROLLM_ACME_HTTP` | HTTP-01, default `:80`; `off` — TLS-ALPN-01 only |
| `-acme-dir` | `MIKROLLM_ACME_DIR` | account and cert cache, default `<data>/acme` |
| `-acme-staging` | `MIKROLLM_ACME_STAGING=1` | Let's Encrypt staging CA (not trusted by browsers) |

TLS 1.2 minimum. Admin cookie on HTTPS gets `Secure`. No HSTS — otherwise LAN is easy to “brick” after falling back to HTTP.

Self-signed: the browser will warn. MCP/Grok/curl need a trusted chain (import CA) or your own cert. `curl -k` is only for a smoke test.

## MikroTik: auto

In envlist (shorter than a long `cmd`):

```routeros
/container envs add name=mikrollm key=MIKROLLM_TLS_AUTO value=1
/container envs add name=mikrollm key=MIKROLLM_TLS_HOSTS value="192.168.88.1,192.168.254.5,localhost"
```

Restart the container. Files appear on USB: `/usb1/docker/mikrollm-data/tls/`. Log: `listening https :4000`.

```bash
curl -k -sS https://192.168.88.1:4000/health
# {"ok":true}
```

Admin: `https://192.168.88.1:4000/admin`  
MCP: `https://192.168.88.1:4000/mcp`

Grok with self-signed likely will not verify the cert. Import `cert.pem` into the system keychain as trusted, or use a cert signed by a CA the Mac already trusts.

## MikroTik: RouterOS certificate

On the router (substitute names and IPs). One command at a time — CLI breaks long lines.

```routeros
/certificate add name=lan-ca common-name="ITRES LAN CA" key-usage=key-cert-sign,crl-sign days-valid=3650
/certificate sign lan-ca
/certificate add name=mikrollm common-name="192.168.88.1" subject-alt-name="IP:192.168.88.1,IP:192.168.254.5,DNS:localhost" days-valid=825
/certificate sign mikrollm ca=lan-ca
/certificate export-certificate mikrollm type=pem
/certificate export-certificate lan-ca type=pem
```

Files under `/file` (`cert_export_mikrollm.crt`, `.key`, CA `.crt`). Copy onto the data volume:

```
/usb1/docker/mikrollm-data/tls/cert.pem   — server cert (optionally + CA in the same PEM)
/usb1/docker/mikrollm-data/tls/key.pem    — key, permissions as the container
```

```routeros
/container envs add name=mikrollm key=MIKROLLM_TLS_CERT value=/data/tls/cert.pem
/container envs add name=mikrollm key=MIKROLLM_TLS_KEY value=/data/tls/key.pem
```

Then `MIKROLLM_TLS_AUTO` is not needed. Put CA (`lan-ca`) on the Mac keychain — Safari/Chrome warnings go away, MCP works without `-k`.

## Let's Encrypt (automatic)

LE **does not** issue for `192.168.88.1`. You need a public DNS name, e.g. `llm.example.com`.

MikroLLM does HTTP-01 itself, stores keys in `<data>/acme`, and renews (~30 days before expiry). No separate certbot.

Recommended MikroTik layout — **open only TCP 80 to the internet** (challenge). Admin and API stay on LAN `:4000`. Prefer not to forward WAN 443: that would put the whole gateway on the internet.

1. DNS: `llm.example.com` **A** to the router’s public WAN IP. From LAN — static on the router so the name resolves to `192.168.88.1` (no hairpin):

```routeros
/ip dns static add name=llm.example.com address=192.168.88.1
```

2. Forward only 80 to the container:

```routeros
/ip firewall nat add chain=dstnat dst-address-type=local protocol=tcp dst-port=80 \
  action=dst-nat to-addresses=192.168.254.5 to-ports=80 comment="mikrollm ACME"
/ip firewall filter add chain=input protocol=tcp dst-port=80 action=drop comment="ACME not to ROS www"
```

`dst-address-type=local` catches WAN; set `dst-address` to your public IP if the rule is too wide. Router input port 80 must not steal ROS www — NAT into the container must come first.

3. Container env (one command at a time):

```routeros
/container envs add name=mikrollm key=MIKROLLM_ACME_HOSTS value=llm.example.com
/container envs add name=mikrollm key=MIKROLLM_ACME_EMAIL value=you@example.com
```

The container listens on `:80` (HTTP-01) and HTTPS on `-listen` (`:4000`). First issue can take up to a minute. Log: `listening https :4000 (Let's Encrypt …)`.

```bash
curl -sS https://llm.example.com:4000/health
```

Grok MCP:

```toml
[mcp_servers.mikrollm]
url = "https://llm.example.com:4000/mcp"
headers = { "Authorization" = "Bearer mcp-…" }
```

Try staging first (`MIKROLLM_ACME_STAGING=1`) so you do not hit the LE limit. Staging certs are not trusted by browsers — that is expected. Then drop staging and restart (do not mix staging and prod caches in `/data/acme`: change `MIKROLLM_ACME_DIR` or wipe `acme`).

If WAN has no public IP (CGNAT), HTTP-01 will not arrive. Issue on another machine (DNS-01) and put files in `/data/tls`:

```bash
# on a Mac, DNS-01 via your DNS (acme.sh example)
acme.sh --issue --dns dns_cf -d llm.example.com
cp ~/.acme.sh/llm.example.com_ecc/fullchain.cer /path/to/mikrollm-data/tls/cert.pem
cp ~/.acme.sh/llm.example.com_ecc/llm.example.com.key /path/to/mikrollm-data/tls/key.pem
```

```routeros
/container envs add name=mikrollm key=MIKROLLM_TLS_CERT value=/data/tls/cert.pem
/container envs add name=mikrollm key=MIKROLLM_TLS_KEY value=/data/tls/key.pem
```

MikroLLM re-reads PEM on the next TLS handshake; restart is optional. In-process ACME is not used in this mode.

TLS-ALPN-01 (no port 80) works only if Let's Encrypt can reach **443** on the name. That needs dst-nat WAN 443 → container (same port as `-listen`, or listen `:443`). That publishes HTTPS to the internet — filter the source.

## Docker / local

```bash
# auto
MIKROLLM_TLS_AUTO=1 MIKROLLM_TLS_HOSTS=127.0.0.1,localhost \
  ./mikrollm -listen :4000 -data ./data -admin-password admin

curl -k https://127.0.0.1:4000/health
```

```yaml
environment:
  ADMIN_PASSWORD: "change-me"
  MIKROLLM_TLS_CERT: /data/tls/cert.pem
  MIKROLLM_TLS_KEY: /data/tls/key.pem
```
