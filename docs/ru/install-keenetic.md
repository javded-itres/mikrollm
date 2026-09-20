# Установка на Keenetic (Entware)

[English](../install-keenetic.md) · **Русский**

У KeeneticOS **нет контейнеров** как у RouterOS. MikroLLM — **статический бинарь в Entware** (`/opt`). Тот же шлюз, что на MikroTik: без Python, SQLite на USB, скромная RAM. **Ollama на роутер не ставим** — бэкенд на ПК, NAS, OpenComfy, OpenRouter или [hub](hub.md).

Нужно: Keenetic с **USB под накопитель** (не только модем 4G), компонент **OPKG**, **Entware** на томе **ext4** (или NAND `storage` там, где это разрешено), SSH (порт **222**).

## Какие модели

| CPU | Примеры | Файл в релизе |
|---|---|---|
| **aarch64** (сейчас поддерживается) | Peak KN-2710, Ultra KN-1811, Giga KN-1012, Hopper KN-3811 / SE KN-3812 | `mikrollm-linux-arm64` |
| mipsel / mips | Giga KN-1010, Ultra KN-1810, Viva, Hopper KN-3810, SE/DSL | *в этом релизе нет* — у `modernc.org/sqlite` нет сборки MIPS; тот же Entware позже |

Комфортно от **256 МБ RAM**. Запас оставьте маршрутизации.

## 1. Entware

1. Компонент **OPKG** (Управление → Обновления и компоненты).
2. Флешка **ext4**, вставлена. (Или встроенный `storage` на подходящих моделях.)
3. Поставить Entware под свою arch ([справка Keenetic](https://help.keenetic.com/) / страница OPKG), например в CLI:

```text
opkg disk storage:/ https://bin.entware.net/aarch64-k3.10/installer/aarch64-installer.tar.gz
```

На MIPS — инсталляторы `mipselsf-k3.4` или `mipssf-k3.4`. Веб: **OPKG** → накопитель → скрипт `/opt/etc/init.d/rc.unslung`.

4. SSH: `ssh -p 222 root@192.168.1.1` (BusyBox + `/opt`).

## 2. Одна команда (на роутере)

```sh
wget -qO- https://raw.githubusercontent.com/javded-itres/mikrollm/main/scripts/install-keenetic.sh | sh
```

Или `curl -fsSL … | sh`, если curl уже есть в Entware.

Скрипт качает нужный бинарь с GitHub Releases, пишет `/opt/etc/mikrollm.env`, ставит `/opt/etc/init.d/S95mikrollm` и запускает процесс на **`:4000`**.

С ПК (после `make build-keenetic`):

```sh
scp -P 222 dist/mikrollm-linux-arm64 root@192.168.1.1:/tmp/mikrollm
ssh -p 222 root@192.168.1.1 'MIKROLLM_BIN=/tmp/mikrollm sh -s' < scripts/install-keenetic.sh
```

Переопределения: `MIKROLLM_ASSET`, `MIKROLLM_LISTEN`, `ADMIN_PASSWORD`, `MIKROLLM_HUB_URL`.

## 3. Админка

Процесс слушает все интерфейсы. Из LAN:

**http://192.168.1.1:4000/admin** (свой LAN-IP). Пароль печатает установщик и лежит в `/opt/var/lib/mikrollm/admin.pass` и `/opt/etc/mikrollm.env`.

**Не** открывайте TCP 4000 в WAN. Для LAN отдельный NAT обычно не нужен, если демон на `:4000`.

## 4. Пути

| Путь | Назначение |
|---|---|
| `/opt/sbin/mikrollm` | бинарь |
| `/opt/var/lib/mikrollm/` | SQLite (переживает замену бинаря) |
| `/opt/etc/mikrollm.env` | `ADMIN_PASSWORD`, listen, URL hub |
| `/opt/etc/init.d/S95mikrollm` | start/stop (`start` `stop` `restart` `status`) |
| `/opt/var/log/mikrollm.log` | stdout/stderr |

Ребут: Entware `rc.unslung` вызывает `S95mikrollm start`.

## 5. После установки

- **Модели**: добавьте бэкенд (LAN Ollama / OpenRouter / OpenComfy). На Keenetic ничего не скачивается.
- **Hub**: Статус → **Участник hub сети**. Адрес по умолчанию `https://hub.mikrollm.ru`.
- Обновление: снова одна команда (env и база не затираются).

Стоп:

```sh
/opt/etc/init.d/S95mikrollm stop
```

## Сборка

```bash
make build-keenetic
# dist/mikrollm-linux-arm64
```

`CGO_ENABLED=0`. Бинари MIPS упираются в SQLite, пока в `modernc.org/libc` нет этих GOARCH.

## Лимиты

Как контейнер MikroTik на 64 МБ: chat и небольшие картинки нормально; большое видео через роутер не гоняйте. SQLite на USB — лучше нормальная флешка/SSD, не NAND, если есть USB.
