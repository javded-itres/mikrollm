#!/bin/sh
# MikroLLM on KeeneticOS via Entware (no Docker).
# On the router (SSH port 222, Entware already installed):
#   wget -qO- https://raw.githubusercontent.com/javded-itres/mikrollm/main/scripts/install-keenetic.sh | sh
set -e

REPO="${MIKROLLM_REPO:-javded-itres/mikrollm}"
PREFIX="${MIKROLLM_PREFIX:-/opt}"
BIN_DIR="$PREFIX/sbin"
DATA="${MIKROLLM_DATA:-$PREFIX/var/lib/mikrollm}"
ETC="${MIKROLLM_ETC:-$PREFIX/etc}"
LISTEN="${MIKROLLM_LISTEN:-:4000}"
INIT="$ETC/init.d/S95mikrollm"
ENVF="$ETC/mikrollm.env"
PIDF="$PREFIX/var/run/mikrollm.pid"
LOG="$PREFIX/var/log/mikrollm.log"

info() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need_entware() {
	if [ -x /opt/bin/opkg ] || [ -x /opt/sbin/opkg ] || command -v opkg >/dev/null 2>&1; then
		return 0
	fi
	die "Entware/OPKG not found. Enable the OPKG component, install Entware on USB (ext4), SSH as root (port 222). Docs: https://github.com/${REPO}/blob/main/docs/install-keenetic.md"
}

detect_asset() {
	u=$(uname -m 2>/dev/null || echo unknown)
	conf=""
	[ -f /opt/etc/opkg.conf ] && conf=$(cat /opt/etc/opkg.conf)
	case "$conf" in
	*aarch64*) echo mikrollm-linux-arm64; return ;;
	*mipselsf*|*mipsel-3*|*mipssf*|*mips-3.4*)
		die "Entware is MIPS; MikroLLM has no MIPS SQLite build yet. Use aarch64 Keenetic (Peak, Ultra KN-1811, Giga KN-1012, Hopper KN-3811)"
		;;
	esac
	case "$u" in
	aarch64 | arm64) echo mikrollm-linux-arm64 ;;
	mipsel | mips64el | mips)
		die "this Entware CPU is MIPS; MikroLLM's SQLite (no CGO) has no MIPS build yet. Use an aarch64 Keenetic (Peak, Ultra KN-1811, Giga KN-1012, Hopper KN-3811) or set MIKROLLM_BIN= to a binary you built yourself"
		;;
	*) die "unsupported arch: $u (need aarch64). Set MIKROLLM_ASSET=mikrollm-linux-arm64" ;;
	esac
}

fetch() {
	url=$1
	out=$2
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$out" "$url"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$out" "$url"
	else
		if command -v opkg >/dev/null 2>&1; then
			opkg update >/dev/null 2>&1 || true
			opkg install curl >/dev/null 2>&1 || opkg install wget >/dev/null 2>&1 || true
		fi
		if command -v curl >/dev/null 2>&1; then
			curl -fsSL -o "$out" "$url"
		elif command -v wget >/dev/null 2>&1; then
			wget -qO "$out" "$url"
		else
			die "need curl or wget"
		fi
	fi
}

rand_pass() {
	if [ -r /dev/urandom ]; then
		tr -dc 'A-Za-z0-9' </dev/urandom 2>/dev/null | head -c 16
		return
	fi
	date +%s
}

write_init() {
	mkdir -p "$ETC/init.d" "$PREFIX/var/run" "$PREFIX/var/log"
	cat >"$INIT" <<EOF
#!/bin/sh
# MikroLLM Entware init (Keenetic rc.unslung calls start)
PATH=/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin
BIN=$BIN_DIR/mikrollm
DATA=$DATA
ENVF=$ENVF
PIDF=$PIDF
LOG=$LOG
LISTEN=$LISTEN

start() {
	mkdir -p "\$DATA" /opt/var/run /opt/var/log
	if [ -f "\$PIDF" ] && kill -0 "\$(cat "\$PIDF")" 2>/dev/null; then
		echo "mikrollm already running"
		return 0
	fi
	if [ -f "\$ENVF" ]; then
		set -a
		# shellcheck disable=SC1090
		. "\$ENVF"
		set +a
	fi
	if [ ! -x "\$BIN" ]; then
		echo "missing \$BIN" >&2
		return 1
	fi
	\$BIN -data "\$DATA" -listen "\${MIKROLLM_LISTEN:-\$LISTEN}" >>"\$LOG" 2>&1 &
	echo \$! >"\$PIDF"
	echo "mikrollm started"
}

stop() {
	if [ -f "\$PIDF" ]; then
		kill "\$(cat "\$PIDF")" 2>/dev/null || true
		rm -f "\$PIDF"
	fi
	echo "mikrollm stopped"
}

status() {
	if [ -f "\$PIDF" ] && kill -0 "\$(cat "\$PIDF")" 2>/dev/null; then
		echo "running pid \$(cat "\$PIDF")"
		return 0
	fi
	echo "stopped"
	return 1
}

case "\$1" in
start) start ;;
stop) stop ;;
restart) stop; start ;;
check|status) status ;;
*) echo "Usage: \$0 {start|stop|restart|status}" ; exit 1 ;;
esac
EOF
	chmod 755 "$INIT"
}

need_entware
mkdir -p "$BIN_DIR" "$DATA" "$ETC" "$PREFIX/var/run" "$PREFIX/var/log"
chmod 700 "$DATA"

ASSET="${MIKROLLM_ASSET:-$(detect_asset)}"
info "arch asset: $ASSET"

if [ -n "${MIKROLLM_BIN:-}" ] && [ -f "$MIKROLLM_BIN" ]; then
	cp "$MIKROLLM_BIN" "$BIN_DIR/mikrollm"
	chmod 755 "$BIN_DIR/mikrollm"
	info "Installed local binary $MIKROLLM_BIN → $BIN_DIR/mikrollm"
else
	tmp="$DATA/mikrollm.download"
	base="https://github.com/${REPO}/releases/latest/download"
	info "Downloading $base/$ASSET"
	if ! fetch "$base/$ASSET" "$tmp"; then
		rm -f "$tmp"
		die "download failed. Build on a PC: make build-keenetic, then MIKROLLM_BIN=/tmp/mikrollm $0"
	fi
	chmod 755 "$tmp"
	mv "$tmp" "$BIN_DIR/mikrollm"
	info "Installed $BIN_DIR/mikrollm"
fi

if [ ! -f "$ENVF" ]; then
	pass="${ADMIN_PASSWORD:-$(rand_pass)}"
	[ -n "$pass" ] || die "could not generate ADMIN_PASSWORD"
	umask 077
	cat >"$ENVF" <<EOF
ADMIN_PASSWORD=$pass
MIKROLLM_LISTEN=$LISTEN
MIKROLLM_HUB_URL=${MIKROLLM_HUB_URL:-https://hub.mikrollm.ru}
EOF
	chmod 600 "$ENVF"
	printf '%s\n' "$pass" >"$DATA/admin.pass"
	chmod 600 "$DATA/admin.pass"
	info "Wrote $ENVF (admin password also in $DATA/admin.pass)"
else
	info "Keeping existing $ENVF"
	pass="(unchanged, see $ENVF or $DATA/admin.pass)"
fi

write_init
if [ -x "$INIT" ]; then
	"$INIT" stop >/dev/null 2>&1 || true
	"$INIT" start
fi

lan=$(ip -4 addr show 2>/dev/null | awk '/inet / && !/127.0.0.1/{print $2}' | head -1 | cut -d/ -f1)
[ -z "$lan" ] && lan="<router-lan-ip>"

info ""
info "MikroLLM is on ${LISTEN} (all interfaces)."
info "Admin:  http://${lan}:4000/admin"
info "Password: $pass"
info "Data: $DATA"
info "Do not open TCP 4000 on WAN. Models stay on a PC/NAS/cloud — this is the gateway only."
info "Hub: Status → Hub network member. Docs: https://github.com/${REPO}/blob/main/docs/install-keenetic.md"
