# Desktop install (Ollama + MikroLLM)

**English** · [Русский](ru/install-desktop.md)

One command on **Linux** or **macOS**. It installs [Ollama](https://ollama.com) if needed, downloads the MikroLLM binary (or builds it with Go), starts a user service, and on first run connects every local Ollama model as a gateway alias.

```bash
curl -fsSL https://raw.githubusercontent.com/javded-itres/mikrollm/main/scripts/install.sh | sh
```

From a clone:

```bash
sh scripts/install.sh
```

Then open **http://127.0.0.1:4000/admin**. The password is printed at the end of the script and stored in `~/.mikrollm/admin.pass`.

## What it does

1. Detects OS/arch (`linux`/`darwin`, `amd64`/`arm64`).
2. Installs Ollama (`brew install ollama` on macOS if Homebrew is present; official installer on Linux) and starts it on `:11434`.
3. Installs `mikrollm` into `~/.local/bin` from GitHub Releases, or `go build` if there is no matching asset.
4. Writes `~/.mikrollm/env` and starts:
   - macOS: LaunchAgent `app.mikrollm`
   - Linux: systemd user unit `mikrollm.service` when available, otherwise `nohup`
5. Starts MikroLLM with `-seed local`: backend `ollama` → `http://127.0.0.1:11434`, and connects whatever `ollama list` already has.

Optional pull before connect:

```bash
MIKROLLM_PULL=llama3.2 curl -fsSL https://raw.githubusercontent.com/javded-itres/mikrollm/main/scripts/install.sh | sh
```

## Paths

| Path | Role |
|---|---|
| `~/.local/bin/mikrollm` | binary |
| `~/.mikrollm/` | SQLite, logs, env |
| `~/.mikrollm/admin.pass` | admin password (mode 600) |
| `~/.mikrollm/run.sh` | service wrapper |

Override with `MIKROLLM_PREFIX`, `MIKROLLM_DATA`, `MIKROLLM_LISTEN`, `MIKROLLM_REPO`.

Add `~/.local/bin` to `PATH` if the script says so.

## After install

- **Models**: if you `ollama pull` later, Admin → Models → Refresh catalogs → connect, or restart MikroLLM (`-seed local` only auto-connects when the alias table is still empty).
- **Keys**: Admin → Keys → `sk-…` for `/v1/chat/completions`.
- **Chat**: http://127.0.0.1:4000/admin/chat (no key).

Stop:

```bash
# macOS
launchctl unload ~/Library/LaunchAgents/app.mikrollm.plist

# Linux (systemd user)
systemctl --user disable --now mikrollm.service
```

## Windows

No first-class installer. Use [WSL](https://learn.microsoft.com/windows/wsl) and the same `curl … | sh` line inside the Linux distro. Building a native Windows binary is `GOOS=windows go build ./cmd/mikrollm`.

## From source instead

[install-local.md](install-local.md) (`go run` / `make run`). That path still seeds the LAN demo hosts `mac-80` / `mac-82` unless you pass `-seed local`.
