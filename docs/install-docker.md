# Server install (Docker and systemd)

**English** · [Русский](ru/install-docker.md)

The image is scratch + one static binary. Build the binary for the target arch first, then the Docker layer.

## Docker

On the build machine (needs Docker Buildx):

```bash
# linux/amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/mikrollm ./cmd/mikrollm
docker buildx build --platform linux/amd64 -t mikrollm:amd64 --load .

docker run --name mikrollm --restart unless-stopped \
  -p 4000:4000 \
  -v mikrollm-data:/data \
  -e ADMIN_PASSWORD='change-me' \
  mikrollm:amd64
```

ARM64 (Raspberry Pi, Ampere, etc.):

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/mikrollm ./cmd/mikrollm
docker buildx build --platform linux/arm64 -t mikrollm:arm64 --load .
```

The container listens on `:4000`. Data is the `/data` volume. Container DNS must resolve Ollama / vLLM / LM Studio hosts (often `--network host` on a home LAN, or raw IPs in admin).

Compose example:

```yaml
services:
  mikrollm:
    image: mikrollm:amd64
    restart: unless-stopped
    ports:
      - "4000:4000"
    volumes:
      - mikrollm-data:/data
    environment:
      ADMIN_PASSWORD: "change-me"
      MIKROLLM_LISTEN: ":4000"
      MIKROLLM_DATA: "/data"
      # MIKROLLM_MCP_TOKEN: "mcp-…"   # prefer issuing from admin, do not commit
      # MIKROLLM_TLS_CERT: /data/tls/cert.pem
      # MIKROLLM_TLS_KEY: /data/tls/key.pem
volumes:
  mikrollm-data:
```

Reset the password in an existing volume:

```bash
docker run --rm -v mikrollm-data:/data \
  -e ADMIN_PASSWORD='newpass' -e ADMIN_PASSWORD_RESET=1 \
  mikrollm:amd64
```

Then start the normal container **without** `ADMIN_PASSWORD_RESET`.

## systemd (no Docker)

Copy the binary, e.g. to `/usr/local/bin/mikrollm`. Data dir: `/var/lib/mikrollm`.

`/etc/systemd/system/mikrollm.service`:

```ini
[Unit]
Description=MikroLLM Ollama gateway
After=network.target

[Service]
Type=simple
User=mikrollm
Group=mikrollm
Environment=ADMIN_PASSWORD=change-me
ExecStart=/usr/local/bin/mikrollm -listen :4000 -data /var/lib/mikrollm
Restart=on-failure
RestartSec=3
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

```bash
sudo useradd --system --home /var/lib/mikrollm --shell /usr/sbin/nologin mikrollm
sudo mkdir -p /var/lib/mikrollm
sudo chown mikrollm:mikrollm /var/lib/mikrollm
sudo systemctl daemon-reload
sudo systemctl enable --now mikrollm
```

Admin: `http://<server>:4000/admin`. MCP: `POST /mcp` — [mcp.md](mcp.md). Prefer a firewall on `:4000` and LAN or a reverse proxy only.

## Check

```bash
curl -sS http://127.0.0.1:4000/health
# {"ok":true}
```
