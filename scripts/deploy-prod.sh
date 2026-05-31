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

echo "WARNING: PoDorel v1 serves HTTP only. Traffic is not encrypted unless an external trusted reverse proxy is used."
echo "Use Caddy, Traefik, Nginx, Tailscale, Cloudflare Tunnel, or another external reverse proxy for HTTPS."

TARGET_USER="${PODOREL_INSTALL_TARGET_USER:-${SUDO_USER:-$USER}}"
TARGET_HOME="$(getent passwd "$TARGET_USER" | cut -d: -f6)"
if [ "$TARGET_HOME" = "" ]; then
  echo "Could not determine home directory for ${TARGET_USER}" >&2
  exit 1
fi

if [ "$DRY_RUN" = "1" ]; then
  podorel_step "Checking production commands for dry run"
  podorel_require_command podman
  podorel_require_command go
  podorel_require_command install
  echo "Target user: ${TARGET_USER}"
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
    dnf install -y git golang podman shadow-utils slirp4netns fuse-overlayfs sqlite
    if ! command -v trivy >/dev/null 2>&1; then
      echo "Trivy is not available from the current package metadata; install Trivy before production security scans." >&2
    fi
    ;;
esac

podorel_step "Checking required production tools"
podorel_require_command podman
podorel_require_command go
podorel_require_command install
podorel_require_command loginctl

podorel_step "Building binaries"
go build -o ./bin/podorel ./cmd/podorel
go build -o ./bin/podorel-agent ./agent/cmd/podorel-agent

podorel_step "Installing binaries"
install -d -m 0755 -o "$TARGET_USER" -g "$TARGET_USER" "${TARGET_HOME}/.local/bin"
install -m 0755 ./bin/podorel /usr/local/bin/podorel
install -m 0755 ./bin/podorel-agent "${TARGET_HOME}/.local/bin/podorel-agent"
chown "$TARGET_USER:$TARGET_USER" "${TARGET_HOME}/.local/bin/podorel-agent"

podorel_step "Creating persistent directories"
install -d -m 0700 -o "$TARGET_USER" -g "$TARGET_USER" "${TARGET_HOME}/.local/share/podorel" "${TARGET_HOME}/.local/share/podorel/logs" "${TARGET_HOME}/.config/podorel"
if [ ! -f "${TARGET_HOME}/.config/podorel/agent-token" ]; then
  umask 077
  head -c 32 /dev/urandom | base64 > "${TARGET_HOME}/.config/podorel/agent-token"
  chown "$TARGET_USER:$TARGET_USER" "${TARGET_HOME}/.config/podorel/agent-token"
fi
cat > "${TARGET_HOME}/.config/podorel/web.env" <<ENV
PODOREL_ADMIN_PASSWORD=${PODOREL_ADMIN_PASSWORD}
PODOREL_LISTEN_ADDR=${PODOREL_LISTEN_ADDR:-0.0.0.0:8080}
PODOREL_PUBLIC_URL=${PODOREL_PUBLIC_URL:-http://podorel.lan:8080}
PODOREL_MODE=production
PODOREL_AGENT_SOCKET=/run/podorel-agent/podorel-agent.sock
PODOREL_LOG_DIR=/app/data/logs
ENV
chmod 0600 "${TARGET_HOME}/.config/podorel/web.env"
chown "$TARGET_USER:$TARGET_USER" "${TARGET_HOME}/.config/podorel/web.env"

podorel_step "Enabling linger"
loginctl enable-linger "$TARGET_USER"

podorel_step "Installing systemd user units"
install -d -m 0755 -o "$TARGET_USER" -g "$TARGET_USER" "${TARGET_HOME}/.config/systemd/user"
install -m 0644 packaging/systemd/podorel-agent.service "${TARGET_HOME}/.config/systemd/user/podorel-agent.service"
install -m 0644 packaging/systemd/podorel-web.service "${TARGET_HOME}/.config/systemd/user/podorel-web.service"
chown "$TARGET_USER:$TARGET_USER" "${TARGET_HOME}/.config/systemd/user/podorel-agent.service" "${TARGET_HOME}/.config/systemd/user/podorel-web.service"

podorel_step "Building web image"
sudo -u "$TARGET_USER" podman build -t podorel-web:latest -f packaging/podman/Containerfile.web .

podorel_step "Starting user services"
sudo -u "$TARGET_USER" XDG_RUNTIME_DIR="/run/user/$(id -u "$TARGET_USER")" systemctl --user daemon-reload
sudo -u "$TARGET_USER" XDG_RUNTIME_DIR="/run/user/$(id -u "$TARGET_USER")" systemctl --user enable --now podorel-agent.service
sudo -u "$TARGET_USER" XDG_RUNTIME_DIR="/run/user/$(id -u "$TARGET_USER")" systemctl --user enable --now podorel-web.service

podorel_step "Final URL"
echo "PoDorel: ${PODOREL_PUBLIC_URL:-http://podorel.lan:8080}"
echo "Recovery: systemctl --user status podorel-web.service podorel-agent.service"
