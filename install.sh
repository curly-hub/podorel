#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

show_help() {
  cat <<'HELP'
Usage: ./install.sh [--yes] [--admin-password PASS] [--public-url URL] [--listen-addr ADDR] [--target-user USER] [--dry-run]

Single-command PoDorel install for a supported Linux machine.

Examples:
  ./install.sh --yes
  ./install.sh --yes --public-url http://podorel.lan:8080
  ./install.sh --dry-run --yes

The installer may ask for sudo, installs prerequisites, builds the web image,
installs the user agent service, starts the PoDorel web pod, and writes a
generated admin password to ~/.config/podorel/generated-admin-password when no
--admin-password is supplied.
HELP
}

YES=0
DRY_RUN=0
PASS_ARGS=()

while [ "$#" -gt 0 ]; do
  case "$1" in
    --help)
      show_help
      exit 0
      ;;
    --yes|-y)
      YES=1
      ;;
    --dry-run)
      DRY_RUN=1
      PASS_ARGS+=("$1")
      ;;
    *)
      PASS_ARGS+=("$1")
      ;;
  esac
  shift
done

if [ "$DRY_RUN" != "1" ] && [ "$YES" != "1" ]; then
  if [ ! -t 0 ]; then
    echo "Refusing non-interactive install without --yes." >&2
    exit 2
  fi
  echo "PoDorel will install prerequisites, create user services, and start the web pod."
  read -r -p "Continue? [y/N] " answer
  case "$answer" in
    y|Y|yes|YES)
      ;;
    *)
      echo "Install cancelled."
      exit 0
      ;;
  esac
fi

exec scripts/install-new-machine.sh "${PASS_ARGS[@]}"
