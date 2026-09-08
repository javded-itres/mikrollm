# Установка локально

## Зависимости

- Go 1.23 или новее
- (по желанию) Ollama, vLLM или LM Studio на этой же машине или в LAN

```bash
git clone https://github.com/javded-itres/mikrollm.git
cd mikrollm
go test ./...
```

## Запуск

```bash
go run ./cmd/mikrollm -listen :4000 -data ./data -admin-password admin
```

или `make run` (то же самое).

Админка: http://127.0.0.1:4000/admin  
Пароль: то, что передали в `-admin-password`. Если флаг и `ADMIN_PASSWORD` пустые **и** база ещё не создана, пароль генерируется и печатается в stdout:

```
generated admin password: …………
```

Каталог `-data` (по умолчанию `./data`) содержит `mikrollm.db` (SQLite, WAL). Его нельзя отдавать в git — см. `.gitignore`.

## Пустая база

При первом старте, если таблица `backends` пустая, добавляются:

| Имя | URL |
|---|---|
| mac-82 | `http://192.168.88.82:11434` |
| mac-80 | `http://192.168.88.80:11434` |

Это удобно для типовой LAN MikroTik. Иначе зайдите в **Статус**, удалите лишнее и добавьте свой бэкенд (`http://127.0.0.1:11434` Ollama, `:8000` vLLM, `:1234` LM Studio). Как загрузить модель на GPU-сервер — [providers.md](providers.md).

## Сброс пароля

```bash
ADMIN_PASSWORD=новыйсекрет ADMIN_PASSWORD_RESET=1 \
  go run ./cmd/mikrollm -data ./data
```

После успешного входа уберите `ADMIN_PASSWORD_RESET`, иначе пароль будет перезаписываться на каждом старте.

MCP-токен при первом старте генерируется в лог (`generated MCP token:`). Задать свой: `-mcp-token` / `MIKROLLM_MCP_TOKEN`. Сброс: `-mcp-token-reset` / `MIKROLLM_MCP_TOKEN_RESET=1`. Как подключить агента — [mcp.md](mcp.md).

Очереди (необязательно): `MIKROLLM_QUEUE_MAX_BYTES` (по умолчанию 16 МиБ тела на диске), `MIKROLLM_QUEUE_MAX_JOBS` (200), `MIKROLLM_QUEUE_MAX_WAIT` (`3m`).

## Сборка бинаря

```bash
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o mikrollm ./cmd/mikrollm
./mikrollm -listen :4000 -data ./data -admin-password admin
```

Кросс-сборка:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/mikrollm-linux-amd64 ./cmd/mikrollm
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o dist/mikrollm-linux-arm64 ./cmd/mikrollm
```
