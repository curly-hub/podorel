#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
source "${ROOT_DIR}/scripts/lib/common.sh"
ORIGINAL_ARGS=("$@")

show_help() {
  cat <<'HELP'
Usage: scripts/install-new-machine.sh [--admin-password PASS] [--public-url URL] [--listen-addr ADDR] [--target-user USER] [--dry-run]

One-command install for a new supported Linux machine.

The script installs host prerequisites, builds the web container image, installs
the host-side agent as a user service, and starts the PoDorel web/API inside a
rootless Podman pod named "podorel".
HELP
  podorel_print_common_help
}

ADMIN_PASSWORD="${PODOREL_ADMIN_PASSWORD:-}"
PUBLIC_URL="${PODOREL_PUBLIC_URL:-}"
LISTEN_ADDR="${PODOREL_LISTEN_ADDR:-}"
TARGET_USER="${PODOREL_INSTALL_TARGET_USER:-${SUDO_USER:-${USER}}}"
DRY_RUN=0
VERBOSE=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --help)
      show_help
      exit 0
      ;;
    --admin-password)
      ADMIN_PASSWORD="${2:?Missing value for --admin-password}"
      shift
      ;;
    --public-url)
      PUBLIC_URL="${2:?Missing value for --public-url}"
      shift
      ;;
    --listen-addr)
      LISTEN_ADDR="${2:?Missing value for --listen-addr}"
      shift
      ;;
    --target-user)
      TARGET_USER="${2:?Missing value for --target-user}"
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      ;;
    --verbose)
      VERBOSE=1
      ;;
    *)
      echo "Unknown argument: $1" >&2
      show_help
      exit 2
      ;;
  esac
  shift
done

if [ "$DRY_RUN" = "1" ]; then
  podorel_setup_logging "install-new-machine"
  cd "$ROOT_DIR"
  export PODOREL_ADMIN_PASSWORD="${ADMIN_PASSWORD:-dry-run-admin-password}"
  export PODOREL_INSTALL_TARGET_USER="$TARGET_USER"
  if [ "$PUBLIC_URL" != "" ]; then
    export PODOREL_PUBLIC_URL="$PUBLIC_URL"
  fi
  if [ "$LISTEN_ADDR" != "" ]; then
    export PODOREL_LISTEN_ADDR="$LISTEN_ADDR"
  fi
  scripts/deploy-prod.sh --dry-run
  exit 0
fi

if [ "$EUID" -ne 0 ]; then
  podorel_require_command sudo
  export PODOREL_ADMIN_PASSWORD="$ADMIN_PASSWORD"
  export PODOREL_PUBLIC_URL="$PUBLIC_URL"
  export PODOREL_LISTEN_ADDR="$LISTEN_ADDR"
  export PODOREL_INSTALL_TARGET_USER="$TARGET_USER"
  exec sudo --preserve-env=PODOREL_ADMIN_PASSWORD,PODOREL_PUBLIC_URL,PODOREL_LISTEN_ADDR,PODOREL_INSTALL_TARGET_USER bash "$0" "${ORIGINAL_ARGS[@]}"
fi

podorel_setup_logging "install-new-machine"
cd "$ROOT_DIR"

GENERATED_PASSWORD=0
if [ "$ADMIN_PASSWORD" = "" ]; then
  ADMIN_PASSWORD="$(head -c 32 /dev/urandom | base64 | tr -d '\n')"
  GENERATED_PASSWORD=1
fi

export PODOREL_ADMIN_PASSWORD="$ADMIN_PASSWORD"
export PODOREL_INSTALL_TARGET_USER="$TARGET_USER"
if [ "$PUBLIC_URL" != "" ]; then
  export PODOREL_PUBLIC_URL="$PUBLIC_URL"
fi
if [ "$LISTEN_ADDR" != "" ]; then
  export PODOREL_LISTEN_ADDR="$LISTEN_ADDR"
fi

podorel_step "Installing PoDorel on new machine"
scripts/deploy-prod.sh

if [ "$GENERATED_PASSWORD" = "1" ]; then
  TARGET_HOME="$(getent passwd "$TARGET_USER" | cut -d: -f6)"
  TARGET_GROUP="$(id -gn "$TARGET_USER")"
  install -d -m 0755 -o "$TARGET_USER" -g "$TARGET_GROUP" "${TARGET_HOME}/.config"
  install -d -m 0700 -o "$TARGET_USER" -g "$TARGET_GROUP" "${TARGET_HOME}/.config/podorel"
  printf '%s\n' "$ADMIN_PASSWORD" > "${TARGET_HOME}/.config/podorel/generated-admin-password"
  chmod 0600 "${TARGET_HOME}/.config/podorel/generated-admin-password"
  chown "$TARGET_USER:$TARGET_GROUP" "${TARGET_HOME}/.config/podorel/generated-admin-password"
  podorel_step "Generated admin password"
  echo "Saved to ${TARGET_HOME}/.config/podorel/generated-admin-password"
  echo "$ADMIN_PASSWORD"
fi

FINAL_PUBLIC_URL="${PODOREL_PUBLIC_URL:-}"
FINAL_LISTEN_ADDR="${PODOREL_LISTEN_ADDR:-}"
podorel_resolve_public_url_and_listen_addr FINAL_PUBLIC_URL FINAL_LISTEN_ADDR
FINAL_LISTEN_PORT="$(podorel_listen_port "$FINAL_LISTEN_ADDR")"

podorel_step "Install complete"
echo "PoDorel: ${FINAL_PUBLIC_URL}"
echo "Firewall: Fedora firewalld opens TCP ${FINAL_LISTEN_PORT} automatically when running; otherwise allow it manually if blocked."
echo "Web pod: podman pod ps --filter name=podorel"
echo "Services: systemctl --user status podorel-web.service podorel-agent.service"
