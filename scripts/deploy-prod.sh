#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
source "${ROOT_DIR}/scripts/lib/common.sh"

show_help() {
  cat <<'HELP'
Usage: sudo scripts/deploy-prod.sh [--help] [--verbose] [--dry-run]

Prepares the PoDorel production installation path with explicit production mode.
HELP
  podorel_print_common_help
}

VERBOSE=0
DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --help)
      show_help
      exit 0
      ;;
    --verbose)
      VERBOSE=1
      ;;
    --dry-run)
      DRY_RUN=1
      ;;
    *)
      echo "Unknown argument: $arg" >&2
      show_help
      exit 2
      ;;
  esac
done

podorel_setup_logging "deploy-prod"
cd "$ROOT_DIR"

podorel_step "Runtime mode"
echo "Active runtime mode: production"

if [ "${PODOREL_ADMIN_PASSWORD:-}" = "" ]; then
  echo "Missing required environment variable: PODOREL_ADMIN_PASSWORD" >&2
  exit 1
fi

podorel_step "Detecting supported OS"
OS_ID="$(podorel_detect_os_id)"
echo "Detected supported OS: ${OS_ID}"

echo "HTTPS: set PODOREL_TLS_CERT_FILE and PODOREL_TLS_KEY_FILE to make PoDorel serve native TLS."
echo "Passkey local CA download: set PODOREL_TLS_CA_FILE, or place podorel-local-ca.crt beside the server certificate."
echo "Reverse proxy mode: set PODOREL_TRUSTED_PROXY_MODE=true when TLS is terminated before PoDorel."

TARGET_USER="${PODOREL_INSTALL_TARGET_USER:-${SUDO_USER:-$USER}}"
TARGET_HOME="$(getent passwd "$TARGET_USER" | cut -d: -f6)"
if [ "$TARGET_HOME" = "" ]; then
  echo "Could not determine home directory for ${TARGET_USER}" >&2
  exit 1
fi
TARGET_UID="$(id -u "$TARGET_USER")"
TARGET_GROUP="$(id -gn "$TARGET_USER")"
TARGET_RUNTIME_DIR="/run/user/${TARGET_UID}"
PUBLIC_URL="${PODOREL_PUBLIC_URL:-}"
LISTEN_ADDR="${PODOREL_LISTEN_ADDR:-}"
TLS_CERT_FILE="${PODOREL_TLS_CERT_FILE:-}"
TLS_KEY_FILE="${PODOREL_TLS_KEY_FILE:-}"
TLS_CA_FILE="${PODOREL_TLS_CA_FILE:-}"
TRUSTED_PROXY_MODE="${PODOREL_TRUSTED_PROXY_MODE:-false}"
podorel_resolve_public_url_and_listen_addr PUBLIC_URL LISTEN_ADDR
LISTEN_PORT="$(podorel_listen_port "$LISTEN_ADDR")"
if [ "$TARGET_UID" = "0" ]; then
  echo "Production deployment target must be a non-root user. Re-run as the target user with sudo available, or pass --target-user USER." >&2
  exit 1
fi

run_as_target_user() {
  sudo -H -u "$TARGET_USER" env \
    HOME="$TARGET_HOME" \
    USER="$TARGET_USER" \
    LOGNAME="$TARGET_USER" \
    XDG_RUNTIME_DIR="$TARGET_RUNTIME_DIR" \
    "$@"
}

if [ "$DRY_RUN" = "1" ]; then
  podorel_step "Checking production commands for dry run"
  podorel_require_command podman
  podorel_require_command go
  podorel_require_go_version_for_module go.mod
  podorel_require_command install
  echo "Target user: ${TARGET_USER}"
  echo "Target home: ${TARGET_HOME}"
  echo "Target runtime: ${TARGET_RUNTIME_DIR}"
  echo "Listen address: ${LISTEN_ADDR}"
  echo "Published port: ${LISTEN_PORT}"
  echo "Public URL: ${PUBLIC_URL}"
  echo "TLS cert file: ${TLS_CERT_FILE:-not configured}"
  echo "TLS key file: ${TLS_KEY_FILE:-not configured}"
  echo "TLS CA file: ${TLS_CA_FILE:-auto-discover}"
  echo "Trusted proxy mode: ${TRUSTED_PROXY_MODE}"
  echo "Firewall: Fedora firewalld opens TCP ${LISTEN_PORT} automatically when running; otherwise allow it manually if blocked."
  podorel_step "Dry run complete"
  exit 0
fi

if [ "$EUID" -ne 0 ]; then
  echo "Production deployment must be run with sudo." >&2
  exit 1
fi

podorel_step "Installing OS packages"
case "$OS_ID" in
  debian|ubuntu)
    apt-get update
    apt-get install -y git golang-go podman uidmap slirp4netns fuse-overlayfs sqlite3
    if ! command -v trivy >/dev/null 2>&1; then
      echo "Trivy is not available from the current package metadata; install Trivy before production security scans." >&2
    fi
    ;;
  fedora)
    dnf install -y git golang podman shadow-utils slirp4netns fuse-overlayfs sqlite firewalld
    if ! command -v trivy >/dev/null 2>&1; then
      echo "Trivy is not available from the current package metadata; install Trivy before production security scans." >&2
    fi
    ;;
esac

podorel_step "Checking required production tools"
podorel_require_command podman
podorel_require_command go
podorel_require_go_version_for_module go.mod
podorel_require_command install
podorel_require_command loginctl


podorel_step "Building binaries"
go build -o ./bin/podorel ./cmd/podorel
go build -o ./bin/podorel-agent ./agent/cmd/podorel-agent

podorel_step "Preparing target user home"
install -d -m 0755 -o "$TARGET_USER" -g "$TARGET_GROUP" "${TARGET_HOME}/.config" "${TARGET_HOME}/.local" "${TARGET_HOME}/.local/share"

podorel_step "Installing binaries"
install -d -m 0755 -o "$TARGET_USER" -g "$TARGET_GROUP" "${TARGET_HOME}/.local/bin"
install -m 0755 ./bin/podorel /usr/local/bin/podorel
install -m 0755 ./bin/podorel-agent "${TARGET_HOME}/.local/bin/podorel-agent"
chown "$TARGET_USER:$TARGET_GROUP" "${TARGET_HOME}/.local/bin/podorel-agent"

podorel_step "Creating persistent directories"
install -d -m 0700 -o "$TARGET_USER" -g "$TARGET_GROUP" "${TARGET_HOME}/.local/share/podorel" "${TARGET_HOME}/.local/share/podorel/logs" "${TARGET_HOME}/.config/podorel"
if [ ! -f "${TARGET_HOME}/.config/podorel/agent-token" ]; then
  umask 077
  head -c 32 /dev/urandom | base64 > "${TARGET_HOME}/.config/podorel/agent-token"
  chown "$TARGET_USER:$TARGET_GROUP" "${TARGET_HOME}/.config/podorel/agent-token"
fi
cat > "${TARGET_HOME}/.config/podorel/web.env" <<ENV
PODOREL_ADMIN_PASSWORD=${PODOREL_ADMIN_PASSWORD}
PODOREL_LISTEN_ADDR=${LISTEN_ADDR}
PODOREL_PUBLIC_URL=${PUBLIC_URL}
PODOREL_TLS_CERT_FILE=${TLS_CERT_FILE}
PODOREL_TLS_KEY_FILE=${TLS_KEY_FILE}
PODOREL_TLS_CA_FILE=${TLS_CA_FILE}
PODOREL_TRUSTED_PROXY_MODE=${TRUSTED_PROXY_MODE}
PODOREL_MODE=production
PODOREL_AGENT_SOCKET=/run/podorel-agent/podorel-agent.sock
PODOREL_LOG_DIR=/app/data/logs
ENV
chmod 0600 "${TARGET_HOME}/.config/podorel/web.env"
chown "$TARGET_USER:$TARGET_GROUP" "${TARGET_HOME}/.config/podorel/web.env"

podorel_step "Enabling linger"
loginctl enable-linger "$TARGET_USER"

podorel_step "Installing systemd user units"
install -d -m 0755 -o "$TARGET_USER" -g "$TARGET_GROUP" "${TARGET_HOME}/.config/systemd/user"
install -m 0644 packaging/systemd/podorel-agent.service "${TARGET_HOME}/.config/systemd/user/podorel-agent.service"
WEB_UNIT_TMP="$(mktemp)"
sed "s/@PODOREL_WEB_PORT@/${LISTEN_PORT}/g" packaging/systemd/podorel-web.service > "$WEB_UNIT_TMP"
install -m 0644 "$WEB_UNIT_TMP" "${TARGET_HOME}/.config/systemd/user/podorel-web.service"
rm -f "$WEB_UNIT_TMP"
chown "$TARGET_USER:$TARGET_GROUP" "${TARGET_HOME}/.config/systemd/user/podorel-agent.service" "${TARGET_HOME}/.config/systemd/user/podorel-web.service"

podorel_step "Building web image"
run_as_target_user podman build -t podorel-web:latest -f packaging/podman/Containerfile.web .

podorel_step "Starting user services"
run_as_target_user systemctl --user daemon-reload
run_as_target_user systemctl --user enable podorel-agent.service
run_as_target_user systemctl --user enable podorel-web.service
run_as_target_user systemctl --user restart podorel-agent.service
run_as_target_user systemctl --user restart podorel-web.service

podorel_step "Configuring host firewall"
podorel_configure_fedora_firewall "$OS_ID" "$LISTEN_PORT"

podorel_step "Final URL"
echo "PoDorel: ${PUBLIC_URL}"
echo "Firewall: Fedora firewalld opens TCP ${LISTEN_PORT} automatically when running; otherwise allow it manually if blocked."
echo "Recovery: systemctl --user status podorel-web.service podorel-agent.service"
