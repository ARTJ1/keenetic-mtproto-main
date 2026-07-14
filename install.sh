#!/bin/sh
# keenetic-mtproto Entware install / upgrade script
# Usage on router:
#   curl -fsSL https://raw.githubusercontent.com/ARTJ1/keenetic-mtproto-main/main/install.sh | sh
# Pin a release (recommended):
#   curl -fsSL https://raw.githubusercontent.com/ARTJ1/keenetic-mtproto-main/main/install.sh | VERSION=v1.0.6 sh

set -eu

REPO="${REPO:-ARTJ1/keenetic-mtproto-main}"
VERSION="${VERSION:-latest}"
BIN_DIR="/opt/sbin"
DATA_DIR="/opt/etc/keenetic-mtproto"
CONFIG_FILE="${DATA_DIR}/config.json"
SERVICE_DIR="/opt/etc/init.d"
SERVICE_NAME="S95keenetic-mtproto"
BINARY_NAME="keenetic-mtproto"
PIDFILE="/opt/var/run/keenetic-mtproto.pid"
DEST="${BIN_DIR}/${BINARY_NAME}"

log() { printf '%s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

need_entware() {
	[ -d /opt/sbin ] && [ -d /opt/etc ] || die "Entware (/opt) not found. Enable OPKG / install Entware first."
}

detect_arch() {
	m=$(uname -m 2>/dev/null || echo unknown)
	case "$m" in
	aarch64|arm64) echo "linux-arm64" ;;
	armv7l|armv7|arm) echo "linux-armv7" ;;
	# uname -m:
	#   mips   = big-endian  (EcoNet EN75xx etc.)
	#   mipsel = little-endian (MT7621 etc.)
	mips) echo "linux-mips_softfloat" ;;
	mipsel) echo "linux-mipsle_softfloat" ;;
	x86_64|amd64) echo "linux-amd64" ;;
	*)
		die "Unsupported arch: $m"
		;;
	esac
}

download() {
	url="$1"
	out="$2"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --connect-timeout 30 --max-time 300 -o "$out" "$url"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$out" "$url"
	else
		die "Need curl or wget"
	fi
	# Archives are several MB; reject truncated/HTML error pages.
	sz=$(wc -c <"$out" | tr -d ' ')
	[ "$sz" -gt 1000000 ] || die "download too small (${sz} bytes) from $url"
}

asset_url() {
	arch="$1"
	if [ "$VERSION" = "latest" ]; then
		printf 'https://github.com/%s/releases/latest/download/keenetic-mtproto-%s.tar.gz' "$REPO" "$arch"
	else
		printf 'https://github.com/%s/releases/download/%s/keenetic-mtproto-%s.tar.gz' "$REPO" "$VERSION" "$arch"
	fi
}

stop_service() {
	if [ -x "${SERVICE_DIR}/${SERVICE_NAME}" ]; then
		"${SERVICE_DIR}/${SERVICE_NAME}" stop 2>/dev/null || true
	fi
	if [ -f "$PIDFILE" ]; then
		kill "$(cat "$PIDFILE")" 2>/dev/null || true
		rm -f "$PIDFILE"
	fi
	killall "$BINARY_NAME" 2>/dev/null || true
	killall -9 "$BINARY_NAME" 2>/dev/null || true
	sleep 1
}

write_service() {
	mkdir -p "$SERVICE_DIR" /opt/var/run
	if [ -f "$SERVICE_DIR/rc.func" ]; then
		cat >"${SERVICE_DIR}/${SERVICE_NAME}" <<EOF
#!/bin/sh
ENABLED=yes
PROCS=${BINARY_NAME}
ARGS="--config=${CONFIG_FILE}"
PREARGS=""
which nohup >/dev/null 2>&1 && PREARGS="nohup"
DESC="\$PROCS"
PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
. /opt/etc/init.d/rc.func
EOF
	else
		cat >"${SERVICE_DIR}/${SERVICE_NAME}" <<'EOF'
#!/bin/sh
PROG="/opt/sbin/keenetic-mtproto"
CONFIG="/opt/etc/keenetic-mtproto/config.json"
PIDFILE="/opt/var/run/keenetic-mtproto.pid"
PATH=/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin

start() {
	echo "Starting keenetic-mtproto..."
	[ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null && echo "Already running" && return 0
	mkdir -p "$(dirname "$PIDFILE")"
	if which nohup >/dev/null 2>&1; then
		nohup "$PROG" --config "$CONFIG" >/dev/null 2>&1 &
	else
		("$PROG" --config "$CONFIG" >/dev/null 2>&1 &)
	fi
	echo $! >"$PIDFILE"
	echo "Started"
}

stop() {
	echo "Stopping keenetic-mtproto..."
	if [ -f "$PIDFILE" ]; then
		kill "$(cat "$PIDFILE")" 2>/dev/null || true
		rm -f "$PIDFILE"
	fi
	killall keenetic-mtproto 2>/dev/null || true
	echo "Stopped"
}

status() {
	if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
		echo "Running (pid $(cat "$PIDFILE"))"
	else
		echo "Stopped"
		return 1
	fi
}

case "$1" in
	start) start ;;
	stop) stop ;;
	restart) stop; start ;;
	status) status ;;
	*) echo "Usage: $0 {start|stop|restart|status}"; exit 1 ;;
esac
EOF
	fi
	chmod +x "${SERVICE_DIR}/${SERVICE_NAME}"
}

write_default_config() {
	[ -f "$CONFIG_FILE" ] && return 0
	mkdir -p "$DATA_DIR"
	cat >"$CONFIG_FILE" <<'EOF'
{
  "web": {
    "port": 7788,
    "bind": "0.0.0.0",
    "username": "admin",
    "password": "admin"
  },
  "proxy": {
    "enabled": true,
    "port": 8443,
    "bind_address": "0.0.0.0",
    "fake_sni": "storage.googleapis.com",
    "upstream_mode": "auto",
    "secrets": [],
    "cfproxy_enabled": true,
    "dc_fallback_enabled": true
  }
}
EOF
	chmod 600 "$CONFIG_FILE"
	log "Wrote default config: $CONFIG_FILE (login admin/admin — change it)"
}

main() {
	need_entware
	arch=$(detect_arch)
	url=$(asset_url "$arch")
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT

	log "arch=$(uname -m) → $arch"
	log "VERSION=$VERSION"
	log "Downloading $url"
	download "$url" "$tmp/pkg.tar.gz"
	tar -xzf "$tmp/pkg.tar.gz" -C "$tmp"

	bin="$tmp/$BINARY_NAME"
	[ -f "$bin" ] || bin=$(find "$tmp" -type f -name "$BINARY_NAME" | head -n 1)
	[ -n "$bin" ] && [ -f "$bin" ] || die "binary not found in archive"
	chmod +x "$bin"

	# Sanity: ELF binary (reject HTML error pages / wrong downloads).
	magic=$(od -An -tx1 -N4 "$bin" 2>/dev/null | tr -d ' \n')
	case "$magic" in
	7f454c46*) ;;
	*) die "Downloaded file is not an ELF binary (got magic=$magic). Wrong arch URL or truncated download." ;;
	esac

	# -version exists from v1.0.7+; older packages just print help/error — ignore.
	if "$bin" -version >/tmp/kmt-ver.txt 2>/tmp/kmt-ver.err; then
		ver=$(tr -d '\r\n' </tmp/kmt-ver.txt)
		log "Package reports version: $ver"
	elif command -v strings >/dev/null 2>&1; then
		ver=$(strings "$bin" 2>/dev/null | grep -E '^v1\.[0-9]+\.[0-9]+$' | head -n 1 || true)
		[ -n "$ver" ] && log "Package embeds version string: $ver"
	fi

	log "Stopping old service..."
	stop_service

	mkdir -p "$BIN_DIR" "$DATA_DIR"
	cp -f "$bin" "$DEST"
	chmod 755 "$DEST"
	# Keep previous only as .bak after successful copy.
	[ -f "${DEST}.bak" ] || true

	write_default_config
	write_service

	log "Starting service..."
	"${SERVICE_DIR}/${SERVICE_NAME}" start || die "failed to start service"
	sleep 2

	if [ -x "$DEST" ] && "$DEST" -version >/tmp/kmt-ver2.txt 2>/dev/null; then
		ver2=$(tr -d '\r\n' </tmp/kmt-ver2.txt)
		log "Installed binary version: $ver2"
	fi

	if command -v curl >/dev/null 2>&1; then
		info=$(curl -fsS --max-time 5 http://127.0.0.1:7788/api/system/info 2>/dev/null || true)
		[ -n "$info" ] && log "API: $info"
	fi

	log ""
	log "OK: ${DEST}"
	log "Config:  ${CONFIG_FILE}"
	log "Service: ${SERVICE_DIR}/${SERVICE_NAME}"
	log "Web UI:  http://<router-ip>:7788"
	log "If browser still shows old UI: hard refresh (Ctrl+F5) or clear cache."
}

main "$@"
