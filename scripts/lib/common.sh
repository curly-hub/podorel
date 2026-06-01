#!/usr/bin/env bash

set -Eeuo pipefail

PODOREL_UNSUPPORTED_DISTRO_MESSAGE="Unsupported Linux distribution for PoDorel v1. Supported: Debian, Ubuntu, Fedora."
PODOREL_SUPPORTED_IDS="debian ubuntu fedora"

podorel_script_dir() {
  local source_path="${BASH_SOURCE[0]}"
  while [ -L "$source_path" ]; do
    source_path="$(readlink "$source_path")"
  done
  cd "$(dirname "$source_path")/../.." >/dev/null 2>&1
  pwd
}

podorel_setup_logging() {
  local script_name="$1"
  local root
  root="$(podorel_script_dir)"
  local log_dir="${root}/.podorel/script-logs"
  mkdir -p "$log_dir"
  PODOREL_SCRIPT_LOG="${log_dir}/${script_name}-$(date -u +%Y%m%d-%H%M%S).log"
  exec > >(tee -a "$PODOREL_SCRIPT_LOG") 2>&1
  trap 'podorel_on_error "$BASH_COMMAND" "$LINENO" "$?"' ERR
  echo "PoDorel script log: $PODOREL_SCRIPT_LOG"
}

podorel_on_error() {
  local command="$1"
  local line="$2"
  local status="$3"
  echo "PoDorel script failed at line ${line} with status ${status}: ${command}" >&2
}

podorel_step() {
  echo
  echo "==> $*"
}

podorel_require_command() {
  local name="$1"
  if ! command -v "$name" >/dev/null 2>&1; then
    echo "Missing required command: $name" >&2
    return 1
  fi
}

podorel_go_minor_value() {
  local version="${1#go}"
  local major="${version%%.*}"
  local rest="${version#*.}"
  local minor="${rest%%.*}"
  if [[ ! "$major" =~ ^[0-9]+$ || ! "$minor" =~ ^[0-9]+$ ]]; then
    echo "Could not parse Go version: $1" >&2
    return 1
  fi
  echo $((major * 1000 + minor))
}

podorel_require_go_version_for_module() {
  local mod_file="${1:-go.mod}"
  local required
  required="$(sed -n 's/^go //p' "$mod_file" | head -n 1)"
  if [ "$required" = "" ]; then
    echo "Could not determine required Go version from ${mod_file}" >&2
    return 1
  fi
  local active
  active="$(go env GOVERSION | sed 's/^go//')"
  local required_value
  local active_value
  required_value="$(podorel_go_minor_value "$required")"
  active_value="$(podorel_go_minor_value "$active")"
  if [ "$active_value" -lt "$required_value" ]; then
    echo "PoDorel requires Go ${required} or newer; active Go toolchain is go${active}." >&2
    echo "Install Go ${required}+ or allow the Go toolchain auto-download before running the installer." >&2
    return 1
  fi
  echo "Go toolchain: go${active} (module requires go ${required})"
}

podorel_detect_os_id() {
  if [ ! -r /etc/os-release ]; then
    echo "$PODOREL_UNSUPPORTED_DISTRO_MESSAGE" >&2
    return 1
  fi

  . /etc/os-release
  case "${ID:-}" in
    debian|ubuntu|fedora)
      echo "$ID"
      ;;
    *)
      echo "$PODOREL_UNSUPPORTED_DISTRO_MESSAGE" >&2
      return 1
      ;;
  esac
}

podorel_print_common_help() {
  cat <<'HELP'
Common flags:
  --help       Show help.
  --verbose    Print extra diagnostic details.
HELP
}

podorel_refuse_mode_conflict() {
  local development="$1"
  local production="$2"
  if [ "$development" = "1" ] && [ "$production" = "1" ]; then
    echo "Invalid mode combination: --development and --production are mutually exclusive." >&2
    return 1
  fi
}

