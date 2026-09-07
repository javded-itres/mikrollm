# Установка на сервер (Docker и systemd)

Образ — scratch + один статический бинарь. Сначала соберите бинарь под целевую архитектуру, затем Docker-слой.

## Docker

На машине сборки (нужен Docker Buildx):

```bash
# linux/amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/mikrollm ./cmd/mikrollm
docker buildx build --platform linux/amd64 -t mikrollm:amd64 --load .

docker run --name mikrollm --restart unless-stopped \
  -p 4000:4000 \
  -v mikrollm-data:/data \
  -e ADMIN_PASSWORD='смените-на-свой' \
  mikrollm:amd64
```

ARM64 (Raspberry Pi, Ampere и т.п.):

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/mikrollm ./cmd/mikrollm
docker buildx build --platform linux/arm64 -t mikrollm:arm64 --load .
```

Контейнер слушает `:4000`. Данные — том `/data`. DNS контейнера должен резолвить хосты Ollama (часто достаточно `--network host` в домашней сети или явные IP в админке).

Пример compose:

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
      ADMIN_PASSWORD: "смените-на-свой"
      MIKROLLM_LISTEN: ":4000"
      MIKROLLM_DATA: "/data"
volumes:
  mikrollm-data:
```

Сброс пароля в уже существующем томе:

```bash
docker run --rm -v mikrollm-data:/data \
  -e ADMIN_PASSWORD='новый' -e ADMIN_PASSWORD_RESET=1 \
  mikrollm:amd64
```

Затем запустите обычный контейнер **без** `ADMIN_PASSWORD_RESET`.

## systemd (без Docker)

Скопируйте бинарь, например в `/usr/local/bin/mikrollm`. Каталог данных — `/var/lib/mikrollm`.

`/etc/systemd/system/mikrollm.service`:

```ini
[Unit]
Description=MikroLLM Ollama gateway
After=network.target

[Service]
Type=simple
User=mikrollm
Group=mikrollm
Environment=ADMIN_PASSWORD=смените-на-свой
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

Админка: `http://<сервер>:4000/admin`. Снаружи лучше закрыть `:4000` файрволом и пускать только LAN или reverse-proxy.

## Проверка

```bash
curl -sS http://127.0.0.1:4000/health
# {"ok":true}
```
