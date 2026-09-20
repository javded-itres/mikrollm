# Install on Keenetic (Entware)

**English** · [Русский](ru/install-keenetic.md)

KeeneticOS has **no Docker containers** like RouterOS. MikroLLM runs as a **static binary under Entware** (`/opt`). Same gateway as on MikroTik: no Python, SQLite on USB, modest RAM. **Do not run Ollama on the router** — point it at a PC, NAS, OpenComfy, OpenRouter, or the [hub](hub.md).

Need: a Keenetic with a **USB storage** port (not modem-only 4G), **OPKG** component, **Entware** on an **ext4** volume (or NAND `storage` on models that allow it), SSH (port **222**).

## Which models

| CPU | Examples | Release asset |
|---|---|---|
| **aarch64** (supported now) | Peak KN-2710, Ultra KN-1811, Giga KN-1012, Hopper KN-3811 / SE KN-3812 | `mikrollm-linux-arm64` |
| mipsel / mips | Giga KN-1010, Ultra KN-1810, Viva, Hopper KN-3810, SE/DSL | *not in this release* — `modernc.org/sqlite` has no MIPS build; same Entware layout later |

256 MB RAM or more is comfortable. Leave headroom for routing.

## 1. Entware

1. Install the **OPKG** component (Management → Updates and components).
2. USB stick formatted **ext4**, plugged in. (Or built-in `storage` on supported units.)
3. Install Entware for your arch ([Keenetic help](https://help.keenetic.com/) / OPKG page), e.g. CLI:

```text
opkg disk storage:/ https://bin.entware.net/aarch64-k3.10/installer/aarch64-installer.tar.gz
```

Use `mipselsf-k3.4` or `mipssf-k3.4` installers on MIPS units. Web UI: **OPKG** → disk → init script `/opt/etc/init.d/rc.unslung`.

4. SSH: `ssh -p 222 root@192.168.1.1` (BusyBox + `/opt`).

## 2. One-liner (on the router)

```sh
wget -qO- https://raw.githubusercontent.com/javded-itres/mikrollm/main/scripts/install-keenetic.sh | sh
```

Or `curl -fsSL … | sh` if `curl` is already in Entware.

The script downloads the matching GitHub Release binary, writes `/opt/etc/mikrollm.env`, installs `/opt/etc/init.d/S95mikrollm`, and starts the process on **`:4000`**.

From a PC (after `make build-keenetic`):

```sh
scp -P 222 dist/mikrollm-linux-arm64 root@192.168.1.1:/tmp/mikrollm
ssh -p 222 root@192.168.1.1 'MIKROLLM_BIN=/tmp/mikrollm sh -s' < scripts/install-keenetic.sh
```

Override: `MIKROLLM_ASSET`, `MIKROLLM_LISTEN`, `ADMIN_PASSWORD`, `MIKROLLM_HUB_URL`.

## 3. Open the admin UI

The process binds all interfaces. From LAN:

**http://192.168.1.1:4000/admin** (use your LAN IP). Password is printed by the installer and stored in `/opt/var/lib/mikrollm/admin.pass` and `/opt/etc/mikrollm.env`.

Do **not** publish TCP 4000 on WAN. Keenetic firewall: no extra NAT is required for LAN if the daemon listens on `:4000`.

## 4. Paths

| Path | Role |
|---|---|
| `/opt/sbin/mikrollm` | binary |
| `/opt/var/lib/mikrollm/` | SQLite (survives binary replace) |
| `/opt/etc/mikrollm.env` | `ADMIN_PASSWORD`, listen, hub URL |
| `/opt/etc/init.d/S95mikrollm` | start/stop (`start` `stop` `restart` `status`) |
| `/opt/var/log/mikrollm.log` | stdout/stderr |

Reboot: Entware `rc.unslung` runs `S95mikrollm start`.

## 5. After install

- **Models**: add a backend (LAN Ollama / OpenRouter / OpenComfy). Nothing is pulled onto the Keenetic.
- **Hub**: Status → **Hub network member**. Default URL `https://hub.mikrollm.ru`.
- Update: re-run the one-liner (keeps `mikrollm.env` and the database).

Stop:

```sh
/opt/etc/init.d/S95mikrollm stop
```

## Build assets

```bash
make build-keenetic
# dist/mikrollm-linux-arm64
```

`CGO_ENABLED=0`. MIPS binaries are blocked on SQLite until `modernc.org/libc` grows those GOARCH tags.

## Limits

Same idea as the MikroTik 64 MB box: chat and small images are fine; do not relay large video through the router. SQLite lives on USB — prefer a decent flash/SSD stick, not the NAND if you have USB.
