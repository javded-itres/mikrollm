#!/bin/sh
# MikroLLM desktop installer: Ollama + local gateway, Linux and macOS.
# curl -fsSL https://raw.githubusercontent.com/javded-itres/mikrollm/main/scripts/install.sh | sh
set -e

REPO="${MIKROLLM_REPO:-javded-itres/mikrollm}"
PREFIX="${MIKROLLM_PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
DATA="${MIKROLLM_DATA:-$HOME/.mikrollm}"
LISTEN="${MIKROLLM_LISTEN:-:4000}"
PULL="${MIKROLLM_PULL:-}"

info() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || die "need $1"
}

os=$(uname -s | tr 'A-Z' 'a-z')
arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported arch: $arch" ;;
esac
case "$os" in
linux | darwin) ;;
*) die "unsupported OS: $os (Linux and macOS only)" ;;
esac

need_cmd curl
need_cmd uname
mkdir -p "$BIN_DIR" "$DATA"
chmod 700 "$DATA"

# --- Ollama ---
install_ollama() {
	if command -v ollama >/dev/null 2>&1; then
		info "Ollama already installed"
		return
	fi
	info "Installing Ollama…"
	if [ "$os" = darwin ]; then
		if command -v brew >/dev/null 2>&1; then
			brew install ollama
		elif [ -d /Applications/Ollama.app ]; then
			:
		else
			die "install Ollama from https://ollama.com/download then re-run (or: brew install ollama)"
		fi
	else
		if [ "$(id -u)" -eq 0 ]; then
			curl -fsSL https://ollama.com/install.sh | sh
		else
			curl -fsSL https://ollama.com/install.sh | sh
		fi
	fi
	command -v ollama >/dev/null 2>&1 || die "ollama not on PATH after install"
}

start_ollama() {
	if curl -sf --max-time 2 http://127.0.0.1:11434/api/version >/dev/null 2>&1; then
		info "Ollama already running on :11434"
		return
	fi
	info "Starting Ollama…"
	if [ "$os" = darwin ]; then
		open -a Ollama >/dev/null 2>&1 || true
	fi
	if command -v systemctl >/dev/null 2>&1; then
		systemctl start ollama >/dev/null 2>&1 || systemctl --user start ollama >/dev/null 2>&1 || true
	fi
	if ! curl -sf --max-time 2 http://127.0.0.1:11434/api/version >/dev/null 2>&1; then
		nohup ollama serve >/dev/null 2>&1 &
	fi
	i=0
	while [ "$i" -lt 40 ]; do
		if curl -sf --max-time 1 http://127.0.0.1:11434/api/version >/dev/null 2>&1; then
			info "Ollama is up"
			return
		fi
		i=$((i + 1))
		sleep 1
	done
	die "Ollama did not start on 127.0.0.1:11434"
}

# --- MikroLLM binary ---
asset_candidates() {
	printf '%s\n' "mikrollm-${os}-${arch}"
	if [ "$os" = linux ] && [ "$arch" = amd64 ]; then
		printf '%s\n' "mikrollm-linux-amd64"
	fi
	if [ "$os" = linux ] && [ "$arch" = arm64 ]; then
		printf '%s\n' "mikrollm-linux-arm64" "mikrollm"
	fi
	if [ "$os" = darwin ] && [ "$arch" = arm64 ]; then
		printf '%s\n' "mikrollm-darwin-arm64"
	fi
	if [ "$os" = darwin ] && [ "$arch" = amd64 ]; then
		printf '%s\n' "mikrollm-darwin-amd64"
	fi
}

download_mikrollm() {
	base="https://github.com/${REPO}/releases/latest/download"
	tmp="$DATA/mikrollm.download"
	for name in $(asset_candidates); do
		url="$base/$name"
		info "Trying $url"
		if curl -fsSL -o "$tmp" "$url"; then
			chmod 755 "$tmp"
			mv "$tmp" "$BIN_DIR/mikrollm"
			info "Installed $BIN_DIR/mikrollm"
			return 0
		fi
		rm -f "$tmp"
	done
	if command -v go >/dev/null 2>&1; then
		info "No release binary for ${os}/${arch}; building from source…"
		src="$DATA/src"
		rm -rf "$src"
		git clone --depth 1 "https://github.com/${REPO}.git" "$src"
		(cd "$src" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$BIN_DIR/mikrollm" ./cmd/mikrollm)
		rm -rf "$src"
		info "Built $BIN_DIR/mikrollm"
		return 0
	fi
	die "no GitHub release asset for ${os}/${arch} and Go is not installed"
}

write_env() {
	if [ ! -f "$DATA/admin.pass" ]; then
		if command -v openssl >/dev/null 2>&1; then
			openssl rand -hex 8 >"$DATA/admin.pass"
		else
			# fallback: not crypto-strong, first-run only
			date | cksum | awk '{print $1}' >"$DATA/admin.pass"
		fi
		chmod 600 "$DATA/admin.pass"
	fi
	pass=$(tr -d '\n' <"$DATA/admin.pass")
	umask 077
	cat >"$DATA/env" <<EOF
ADMIN_PASSWORD=$pass
MIKROLLM_SEED=local
MIKROLLM_LISTEN=$LISTEN
MIKROLLM_DATA=$DATA
EOF
	chmod 600 "$DATA/env"
	cat >"$DATA/run.sh" <<EOF
#!/bin/sh
set -a
. "$DATA/env"
set +a
exec "$BIN_DIR/mikrollm" -listen "\$MIKROLLM_LISTEN" -data "\$MIKROLLM_DATA" -seed local
EOF
	chmod 700 "$DATA/run.sh"
}

install_service() {
	if [ "$os" = darwin ]; then
		plist="$HOME/Library/LaunchAgents/app.mikrollm.plist"
		mkdir -p "$HOME/Library/LaunchAgents"
		cat >"$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>app.mikrollm</string>
  <key>ProgramArguments</key>
  <array>
    <string>$DATA/run.sh</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>$DATA/mikrollm.log</string>
  <key>StandardErrorPath</key><string>$DATA/mikrollm.log</string>
</dict>
</plist>
EOF
		launchctl unload "$plist" >/dev/null 2>&1 || true
		launchctl load "$plist"
		info "launchd: $plist"
		return
	fi
	if command -v systemctl >/dev/null 2>&1 && [ -d "$HOME/.config" ]; then
		mkdir -p "$HOME/.config/systemd/user"
		unit="$HOME/.config/systemd/user/mikrollm.service"
		cat >"$unit" <<EOF
[Unit]
Description=MikroLLM local gateway
After=network.target

[Service]
Type=simple
EnvironmentFile=$DATA/env
ExecStart=$BIN_DIR/mikrollm -listen ${LISTEN} -data ${DATA} -seed local
Restart=on-failure

[Install]
WantedBy=default.target
EOF
		systemctl --user daemon-reload >/dev/null 2>&1 || true
		systemctl --user enable --now mikrollm.service >/dev/null 2>&1 || true
		if systemctl --user is-active mikrollm.service >/dev/null 2>&1; then
			info "systemd user service: mikrollm.service"
			return
		fi
	fi
	info "Starting MikroLLM in the background (no systemd user session)"
	nohup "$DATA/run.sh" >>"$DATA/mikrollm.log" 2>&1 &
}

wait_gateway() {
	i=0
	while [ "$i" -lt 25 ]; do
		if curl -sf --max-time 1 http://127.0.0.1:4000/health >/dev/null 2>&1; then
			return 0
		fi
		i=$((i + 1))
		sleep 1
	done
	return 1
}

install_ollama
start_ollama
if [ -n "$PULL" ]; then
	info "ollama pull $PULL"
	ollama pull "$PULL"
fi
download_mikrollm
write_env
# stop a leftover process on :4000 from a previous install
if command -v launchctl >/dev/null 2>&1 && [ -f "$HOME/Library/LaunchAgents/app.mikrollm.plist" ]; then
	launchctl unload "$HOME/Library/LaunchAgents/app.mikrollm.plist" >/dev/null 2>&1 || true
fi
install_service
if wait_gateway; then
	info "MikroLLM is up"
else
	info "gateway not answering yet; log: $DATA/mikrollm.log"
fi

pass=$(tr -d '\n' <"$DATA/admin.pass")
info ""
info "Admin:    http://127.0.0.1:4000/admin"
info "Password: $pass  (also $DATA/admin.pass)"
info "API:      http://127.0.0.1:4000/v1"
info "Data:     $DATA"
info "Binary:   $BIN_DIR/mikrollm"
case ":$PATH:" in
*":$BIN_DIR:"*) ;;
*) info "Add to PATH:  export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac
info ""
info "Ollama models on this machine are connected as gateway aliases (refresh Models if you pull more)."
info "Issue an sk- key in admin → Keys, or chat at http://127.0.0.1:4000/admin/chat"
