# Install in a MikroTik RouterOS 7 container

**English** · [Русский](ru/install-mikrotik.md)

Verified on **RouterOS 7.22**, hAP ax³, **linux/arm64**. No Python in the container: static Go on scratch. Keep container RAM modest (`memory-high=64M` is enough).

Need: the **container** package on RouterOS, Docker Buildx and Python 3 **on the build machine**, USB space (recommended).

## 1. Build the RouterOS tar

RouterOS wants **docker-save v1** (`manifest.json` + `Config` + `Layers`). `docker buildx` defaults to OCI — convert it.

```bash
make tar-ros
# dist/mikrollm          — linux/arm64 binary
# dist/mikrollm-ros-legacy.tar — what you upload to the router
```

Conversion script: [`scripts/oci_to_legacy_docker.py`](../scripts/oci_to_legacy_docker.py).

A ready tar is also on [GitHub Releases](https://github.com/javded-itres/mikrollm/releases).

## 2. Container network

Typical layout: a bridge for docker nets, MikroLLM veth on that subnet, backends (Ollama / vLLM / LM Studio) as LAN hosts (`192.168.88.0/24` in the example).

```routeros
/interface veth add name=LLM address=192.168.254.5/24 gateway=192.168.254.1
/interface bridge port add bridge=Bridge-Docker interface=LLM
```

Use your docker-bridge name if it differs. Gateway `192.168.254.1` must be the router’s address on that bridge (so the container can reach LAN Ollama and the internet for pull layers).

Set DNS explicitly: `192.168.88.1` or a public resolver.

## 3. Volumes and env

```routeros
/container mounts add name=mikrollm-data src=/usb1/docker/mikrollm-data dst=/data
/container envs add name=mikrollm key=ADMIN_PASSWORD value="change-me"
```

Create `src` first (`/file make-dir` or from a PC over SMB/FTP). SQLite lives on this volume and **survives** container remove.

## 4. Upload the tar

Copy `mikrollm-ros-legacy.tar` to `/usb1/docker/mikrollm-ros.tar` (WinBox, SMB, `/tool fetch` from HTTP on the LAN).

HTTP from a LAN machine:

```bash
# on the PC
python3 -m http.server 8766
```

```routeros
/tool fetch url="http://192.168.88.10:8766/mikrollm-ros-legacy.tar" dst-path=usb1/docker/mikrollm-ros.tar
```

## 5. Create the container

Split commands: a long RouterOS CLI line wraps and breaks.

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

You can put `root-dir` on USB (`/usb1/docker/mikrollm`) so layers do not eat internal NAND.

## 6. LAN port forward

To open `http://192.168.88.1:4000` from LAN PCs:

```routeros
/ip firewall nat add chain=dstnat dst-address=192.168.88.1 dst-port=4000 \
  protocol=tcp action=dst-nat to-addresses=192.168.254.5 to-ports=4000 \
  comment="mikrollm"
```

Direct `http://192.168.254.5:4000` also works if you have a route to the docker net.

Do not publish `:4000` to the internet unless you must. Admin is password-protected, API uses keys, MCP uses a separate token — that is not a VPN. Issue the MCP token in admin (**Status → MCP for agents**), not in envlist.

## 7. Check

```bash
curl -sS http://192.168.88.1:4000/health
```

Admin: http://192.168.88.1:4000/admin  
MCP: `POST http://192.168.88.1:4000/mcp` with Bearer — [mcp.md](mcp.md).

HTTPS on the same `:4000` (no second container): [tls.md](tls.md). Self-signed — `MIKROLLM_TLS_AUTO=1`. Let's Encrypt — `MIKROLLM_ACME_HOSTS` and dst-nat WAN **80 only** to the container; DNS name, from LAN a static DNS to `192.168.88.1`.

Container logs (if `logging=yes`) go to RouterOS `/log`. On first start after an upgrade you will see `generated MCP token:` if none existed yet.

## Upgrade

1. Build a new `mikrollm-ros-legacy.tar` and overwrite the file.
2. `/container stop [find name=mikrollm]`
3. `/container remove [find name=mikrollm]`
4. `/container add` + `set` from step 5 again (same mount/env).
5. `/container start`

Leave the `/data` volume alone — keys, servers, and pull progress survive. After an upgrade **remove** `ADMIN_PASSWORD_RESET` if you used it for debugging.

## Typical failures

| Symptom | Check |
|---|---|
| `no config found in manifest` | tar was not converted from OCI; need `oci_to_legacy_docker.py` |
| Start then immediate Stop | `cmd`/`entrypoint`, read the log; often a truncated long command |
| Admin hangs on POST login | old builds: nested SQLite query; need ≥ 0.0.1 |
| Ollama “unreachable” | container must ping `192.168.88.x`; veth gateway, firewall |
| Admin pull has no internet | container DNS, routes; do not mark the whole container src into `main` if that breaks VPN |
| OpenRouter **403 Forbidden** | API from a Russian IP is blocked. Container `192.168.254.5` is not in the LAN 88 → VPN rule, see [below](#openrouter-403) |

## OpenRouter 403

OpenRouter returns 403 on `GET /api/v1/key` and `/models` from a Russian ISP address; the same key via LAN through AMS WG works. Ollama Cloud on the ISP can still be fine.

The container at `192.168.254.5` **does not** match `src-address=192.168.88.0/24`, so its HTTPS uses `main` (ISP). Mark only this address into table `vpn` (after `ru-domains` / `novpn`), and src-nat to a **free** LAN address that AMS already routes — not the router `.1` and not the WG address (`10.88.97.2`), or the reply hits INPUT and health hangs until timeout.

```routeros
/ip firewall mangle add chain=prerouting action=mark-routing new-routing-mark=vpn \
  passthrough=yes src-address=192.168.254.5 dst-address=!192.168.0.0/16 routing-mark=!main \
  comment="mikrollm via AMS WG"

/ip firewall nat add place-before=[find comment="do not masq AMS WG"] chain=srcnat \
  action=src-nat to-addresses=192.168.88.9 src-address=192.168.254.5 \
  out-interface=wireguard-ams comment="mikrollm via AMS WG"
```

`192.168.88.9` must be outside the DHCP pool and not assigned to an interface. Local Ollama (`192.168.88.x`) is untouched (`dst-address=!192.168.0.0/16`). Do not mark all of `192.168.254.0/24`: mihomo / wstunnel live there; sending them into VPN loops the tunnel.

## Memory and CPU

MikroLLM itself is light. Heavy local models live on a Mac/PC with Ollama, vLLM, or LM Studio, not on the router. OpenRouter and Ollama Cloud leave the container over HTTPS — the image has `ca-certificates`. Do not raise `memory-high` “just in case” to hundreds of megabytes — ax³ is already tight. Cloud connect or GPU load: [providers.md](providers.md).
