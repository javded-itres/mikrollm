# Установка в контейнер MikroTik RouterOS 7

Проверено на **RouterOS 7.22**, hAP ax³, **linux/arm64**. Python в контейнере нет: только статический Go в scratch. RAM контейнера держите скромной (`memory-high=64M` достаточно).

Нужны: пакет **container** в RouterOS, Docker Buildx и Python 3 **на машине сборки**, место на USB (рекомендуется).

## 1. Собрать tar для RouterOS

RouterOS понимает **docker-save v1** (`manifest.json` + `Config` + `Layers`). `docker buildx` по умолчанию отдаёт OCI — его надо конвертировать.

```bash
make tar-ros
# dist/mikrollm          — бинарь linux/arm64
# dist/mikrollm-ros-legacy.tar — то, что грузить на роутер
```

Скрипт конвертации: [`scripts/oci_to_legacy_docker.py`](../scripts/oci_to_legacy_docker.py).

Готовый tar также лежит в [GitHub Releases](https://github.com/javded-itres/mikrollm/releases).

## 2. Сеть контейнера

Типовая схема: bridge для docker-сетей, veth MikroLLM в этой подсети, бэкенды (Ollama / vLLM / LM Studio) — хосты LAN (`192.168.88.0/24` в примере).

```routeros
/interface veth add name=LLM address=192.168.254.5/24 gateway=192.168.254.1
/interface bridge port add bridge=Bridge-Docker interface=LLM
```

Подставьте свой docker-bridge, если он называется иначе. Шлюз `192.168.254.1` должен быть адресом роутера на этом bridge (чтобы контейнер ходил в LAN к Ollama и наружу за слоями pull).

DNS контейнеру задайте явно: `192.168.88.1` или публичный резолвер.

## 3. Тома и переменные

```routeros
/container mounts add name=mikrollm-data src=/usb1/docker/mikrollm-data dst=/data
/container envs add name=mikrollm key=ADMIN_PASSWORD value="смените-на-свой"
```

Каталог `src` создайте заранее (`/file make-dir` или с компьютера по SMB/FTP). База SQLite живёт в этом томе и **переживает** удаление контейнера.

## 4. Загрузить tar на роутер

Скопируйте `mikrollm-ros-legacy.tar` в `/usb1/docker/mikrollm-ros.tar` (WinBox, SMB, `/tool fetch` с HTTP в LAN).

Пример с HTTP на машине в LAN:

```bash
# на ПК
python3 -m http.server 8766
```

```routeros
/tool fetch url="http://192.168.88.10:8766/mikrollm-ros-legacy.tar" dst-path=usb1/docker/mikrollm-ros.tar
```

## 5. Создать контейнер

Команды лучше разбить: длинная строка в CLI RouterOS переносится и ломается.

```routeros
/container add name=mikrollm file=usb1/docker/mikrollm-ros.tar interface=LLM
/container set [find name=mikrollm] logging=yes start-on-boot=yes
/container set [find name=mikrollm] mountlists=mikrollm-data
/container set [find name=mikrollm] envlists=mikrollm
/container set [find name=mikrollm] auto-restart-interval=5s memory-high=64M
/container set [find name=mikrollm] dns=192.168.88.1,1.1.1.1
/container set [find name=mikrollm] workdir=/ entrypoint=/mikrollm
/container set [find name=mikrollm] cmd="-data /data -listen :4000"
/container start [find name=mikrollm]
```

`root-dir` можно указать на USB (`/usb1/docker/mikrollm`), чтобы слои не занимали внутреннюю NAND.

## 6. Проброс порта на LAN

Чтобы открывать `http://192.168.88.1:4000` с компьютеров LAN:

```routeros
/ip firewall nat add chain=dstnat dst-address=192.168.88.1 dst-port=4000 \
  protocol=tcp action=dst-nat to-addresses=192.168.254.5 to-ports=4000 \
  comment="mikrollm"
```

Прямой заход на `http://192.168.254.5:4000` тоже работает, если маршрут до docker-сети есть.

Не выставляйте `:4000` в интернет без нужды. Админка защищена паролем, API — ключами, MCP — отдельным токеном, но это не замена VPN. MCP-токен лучше выпустить в админке (**Статус → MCP для агента**), а не класть в envlist.

## 7. Проверка

```bash
curl -sS http://192.168.88.1:4000/health
```

Админка: http://192.168.88.1:4000/admin  
MCP: `POST http://192.168.88.1:4000/mcp` с Bearer — [mcp.md](mcp.md).

Лог контейнера (если `logging=yes`) попадает в `/log` RouterOS. При первом старте после обновления там будет `generated MCP token:`, если токена ещё не было.

## Обновление версии

1. Собрать новый `mikrollm-ros-legacy.tar` и залить поверх файла.
2. `/container stop [find name=mikrollm]`
3. `/container remove [find name=mikrollm]`
4. Снова `/container add` + `set` из шага 5 (mount/env те же).
5. `/container start`

Том `/data` не трогайте — ключи, серверы и прогресс pull сохранятся. После обновления **снимите** `ADMIN_PASSWORD_RESET`, если включали его для отладки.

## Типичные ошибки

| Симптом | Что проверить |
|---|---|
| `no config found in manifest` | tar не сконвертирован из OCI, нужен `oci_to_legacy_docker.py` |
| контейнер Start, сразу Stop | `cmd`/`entrypoint`, смотрите log; часто обрезанная длинная команда |
| админка висит на POST login | устаревшие сборки: вложенный SQLite-запрос; нужна версия ≥ 0.0.1 |
| Ollama «недоступен» | с контейнера должен пинговаться `192.168.88.x`; gateway veth, firewall |
| pull с админки не идёт в интернет | DNS контейнера, маршруты, не помечать src контейнера в `main` целиком, если это ломает VPN |
| OpenRouter **403 Forbidden** | API с IP РФ режется. Контейнер `192.168.254.5` не в правиле LAN 88 → VPN, см. [ниже](#openrouter-403) |

## OpenRouter 403

OpenRouter отвечает 403 на `GET /api/v1/key` и `/models` с адреса ISP РФ; тот же ключ с LAN через AMS WG проходит. Ollama Cloud с ISP при этом может быть жив.

Контейнер в `192.168.254.5` **не** совпадает с `src-address=192.168.88.0/24`, поэтому его HTTPS уходит в `main` (ISP). Пометьте только этот адрес в таблицу `vpn` (после `ru-domains` / `novpn`), и сделайте src-nat на **свободный** адрес LAN, который AMS уже маршрутизирует — не `.1` роутера и не адрес WG (`10.88.97.2`), иначе ответ попадает в INPUT и health зависает до timeout.

```routeros
/ip firewall mangle add chain=prerouting action=mark-routing new-routing-mark=vpn \
  passthrough=yes src-address=192.168.254.5 dst-address=!192.168.0.0/16 routing-mark=!main \
  comment="mikrollm via AMS WG"

/ip firewall nat add place-before=[find comment="do not masq AMS WG"] chain=srcnat \
  action=src-nat to-addresses=192.168.88.9 src-address=192.168.254.5 \
  out-interface=wireguard-ams comment="mikrollm via AMS WG"
```

`192.168.88.9` должен быть вне DHCP pool и не назначен на интерфейс. Локальный Ollama (`192.168.88.x`) правилом не трогается (`dst-address=!192.168.0.0/16`). Не помечайте весь `192.168.254.0/24`: там mihomo / wstunnel, их увод в VPN зациклит туннель.

## Память и CPU

MikroLLM сам лёгкий. Тяжёлые локальные модели живут на Mac/PC с Ollama, vLLM или LM Studio, не на роутере. OpenRouter и Ollama Cloud ходят из контейнера в интернет по HTTPS — в образе есть `ca-certificates`. Не поднимайте `memory-high` «на всякий случай» до сотен мегабайт — ax³ и так тесный. Как подключить облако или загрузить модель на GPU — [providers.md](providers.md).
