# Local install

**English** · [Русский](ru/install-local.md)

## Dependencies

- Go 1.23 or newer
- (optional) Ollama, vLLM, or LM Studio on this machine or on the LAN

```bash
git clone https://github.com/javded-itres/mikrollm.git
cd mikrollm
go test ./...
```

## Run

```bash
go run ./cmd/mikrollm -listen :4000 -data ./data -admin-password admin
```

or `make run` (same thing).

Admin: http://127.0.0.1:4000/admin  
Password: whatever you passed to `-admin-password`. If the flag and `ADMIN_PASSWORD` are empty **and** the database does not exist yet, a password is generated and printed:

```
generated admin password: …………
```

The `-data` directory (default `./data`) holds `mikrollm.db` (SQLite, WAL). Do not commit it — see `.gitignore`.

## Empty database

On first start, if the `backends` table is empty, these are added:

| Name | URL |
|---|---|
| mac-82 | `http://192.168.88.82:11434` |
| mac-80 | `http://192.168.88.80:11434` |

Handy for a typical MikroTik LAN. Desktop one-liner (`-seed local`) seeds `ollama` at `http://127.0.0.1:11434` instead and connects local models: [install-desktop.md](install-desktop.md). Otherwise open **Status**, delete extras, and add your backend (`http://127.0.0.1:11434` Ollama, `:8000` vLLM, `:1234` LM Studio). Loading a model on a GPU host: [providers.md](providers.md).

## Reset the password

```bash
ADMIN_PASSWORD=newsecret ADMIN_PASSWORD_RESET=1 \
  go run ./cmd/mikrollm -data ./data
```

After a successful login, drop `ADMIN_PASSWORD_RESET`, or the password will be rewritten on every start.

The MCP token is generated into the log on first start (`generated MCP token:`). Set your own with `-mcp-token` / `MIKROLLM_MCP_TOKEN`. Reset: `-mcp-token-reset` / `MIKROLLM_MCP_TOKEN_RESET=1`. Connecting an agent: [mcp.md](mcp.md).

Queues (optional): `MIKROLLM_QUEUE_MAX_BYTES` (default 16 MiB body on disk), `MIKROLLM_QUEUE_MAX_JOBS` (200), `MIKROLLM_QUEUE_MAX_WAIT` (`3m`).

HTTPS: `MIKROLLM_TLS_AUTO=1`, a `-tls-cert` / `-tls-key` pair, or Let's Encrypt `-acme-hosts llm.example.com` (the machine needs port 80 from the internet). Details: [tls.md](tls.md).

Hub (optional): `MIKROLLM_HUB_URL` (default `https://hub.mikrollm.ru`). Enable **Hub network member** on Status. The hub **service** is a separate repo (`mikrollm_hub`). Client notes: [hub.md](hub.md).

## Build a binary

```bash
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o mikrollm ./cmd/mikrollm
./mikrollm -listen :4000 -data ./data -admin-password admin
```

Cross-compile:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/mikrollm-linux-amd64 ./cmd/mikrollm
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o dist/mikrollm-linux-arm64 ./cmd/mikrollm
```
