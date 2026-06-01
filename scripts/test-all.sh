#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
source "${ROOT_DIR}/scripts/lib/common.sh"

show_help() {
  cat <<'HELP'
Usage: scripts/test-all.sh [--help] [--verbose]

Runs the PoDorel test harness.
HELP
  podorel_print_common_help
}

VERBOSE=0
for arg in "$@"; do
  case "$arg" in
    --help)
      show_help
      exit 0
      ;;
    --verbose)
      VERBOSE=1
      ;;
    *)
      echo "Unknown argument: $arg" >&2
      show_help
      exit 2
      ;;
  esac
done

podorel_setup_logging "test-all"
cd "$ROOT_DIR"

podorel_step "Checking required local tools"
podorel_require_command bash
podorel_require_command go
podorel_require_go_version_for_module go.mod
podorel_require_command node

podorel_step "Checking Go version alignment"
GO_MOD_VERSION="$(sed -n 's/^go //p' go.mod | head -n 1)"
GO_WORK_VERSION="$(sed -n 's/^go //p' go.work | head -n 1)"
GO_MOD_MINOR="$(printf '%s\n' "$GO_MOD_VERSION" | sed -E 's/^([0-9]+\.[0-9]+).*/\1/')"
GO_WORK_MINOR="$(printf '%s\n' "$GO_WORK_VERSION" | sed -E 's/^([0-9]+\.[0-9]+).*/\1/')"
CONTAINER_GO_VERSION="$(sed -n 's/^FROM .*golang:\([0-9][0-9]*\.[0-9][0-9]*\).*/\1/p' packaging/podman/Containerfile.web | head -n 1)"
if [ "$GO_MOD_MINOR" != "$GO_WORK_MINOR" ]; then
  echo "go.mod and go.work must use the same Go minor version: ${GO_MOD_VERSION} != ${GO_WORK_VERSION}" >&2
  exit 1
fi
if [ "$GO_MOD_MINOR" != "$CONTAINER_GO_VERSION" ]; then
  echo "Containerfile.web builder Go minor version must match go.mod: ${CONTAINER_GO_VERSION} != ${GO_MOD_MINOR}" >&2
  exit 1
fi

podorel_step "Checking Go formatting"
mapfile -t GO_FILES < <(gofmt -l $(find . -path ./ui/node_modules -prune -o -name '*.go' -print))
if [ "${#GO_FILES[@]}" -gt 0 ]; then
  printf 'Go files need gofmt:\n' >&2
  printf '  %s\n' "${GO_FILES[@]}" >&2
  exit 1
fi

podorel_step "Running go vet"
go vet ./...

podorel_step "Running Go tests"
go test ./...

if [ "${PODOREL_RUN_REAL_PODMAN_TESTS:-}" = "1" ]; then
  podorel_step "Real Podman integration tests are enabled"
else
  podorel_step "Real Podman integration tests are skipped"
  echo "Set PODOREL_RUN_REAL_PODMAN_TESTS=1 to create and clean podorel-test-* pods."
fi

podorel_step "Running UI architecture checks"
node ui/scripts/check-ui.mjs
if [ -d ui/node_modules ]; then
  (cd ui && npm run build)
else
  echo "Skipping Angular CLI build because ui/node_modules is not installed."
fi

podorel_step "Checking deployment script syntax"
bash -n install.sh
bash -n scripts/deploy-dev.sh
bash -n scripts/deploy-prod.sh
bash -n scripts/build-deploy.sh
bash -n scripts/git-export.sh
bash -n scripts/install-new-machine.sh
bash -n scripts/test-all.sh

podorel_step "Checking systemd readiness packaging"
AGENT_UNIT="$(<packaging/systemd/podorel-agent.service)"
WEB_UNIT="$(<packaging/systemd/podorel-web.service)"
if [[ "$AGENT_UNIT" != *"Type=notify"* || "$AGENT_UNIT" != *"WatchdogSec=30s"* ]]; then
  echo "Agent systemd unit must use Type=notify with WatchdogSec=30s." >&2
  exit 1
fi
if [[ "$WEB_UNIT" != *"Type=notify"* || "$WEB_UNIT" != *"--sdnotify=container"* || "$WEB_UNIT" != *"pod create --name podorel"* || "$WEB_UNIT" != *"--pod podorel"* || "$WEB_UNIT" != *"PODOREL_AGENT_SOCKET=/run/podorel-agent/podorel-agent.sock"* || "$WEB_UNIT" != *"%t/podorel:/run/podorel-agent:ro"* ]]; then
  echo "Web systemd unit must use Type=notify, --sdnotify=container, run inside the podorel Podman pod, and mount the host agent socket." >&2
  exit 1
fi
if [[ "$WEB_UNIT" == *"WatchdogSec="* ]]; then
  echo "Web systemd unit must not use WatchdogSec because systemd can abort Podman/conmon instead of the containerized web process." >&2
  exit 1
fi
if [[ "$WEB_UNIT" != *"-p 0.0.0.0:@PODOREL_WEB_PORT@:@PODOREL_WEB_PORT@"* ]]; then
  echo "Web systemd unit must publish the installer-substituted port on all host interfaces." >&2
  exit 1
fi

podorel_step "Checking Fedora firewall installer support"
COMMON_LIB="$(<scripts/lib/common.sh)"
DEPLOY_PROD_SCRIPT="$(<scripts/deploy-prod.sh)"
BUILD_DEPLOY_SCRIPT="$(<scripts/build-deploy.sh)"
if [[ "$COMMON_LIB" != *"podorel_configure_fedora_firewall"* || "$COMMON_LIB" != *"firewall-cmd --permanent --add-port"* || "$COMMON_LIB" != *"PODOREL_SKIP_FIREWALL"* ]]; then
  echo "Shared installer helpers must support opt-out Fedora firewalld port configuration." >&2
  exit 1
fi
if [[ "$DEPLOY_PROD_SCRIPT" != *"sqlite firewalld"* || "$DEPLOY_PROD_SCRIPT" != *'podorel_configure_fedora_firewall "$OS_ID" "$LISTEN_PORT"'* ]]; then
  echo "Production installer must install firewalld on Fedora and configure the selected TCP port." >&2
  exit 1
fi
if [[ "$BUILD_DEPLOY_SCRIPT" != *"sqlite firewalld"* || "$BUILD_DEPLOY_SCRIPT" != *"configure_fedora_firewall"* || "$BUILD_DEPLOY_SCRIPT" != *"PODOREL_SKIP_FIREWALL"* ]]; then
  echo "Generated deploy bundle installer must include Fedora firewalld port configuration." >&2
  exit 1
fi

podorel_step "Running installer dry-run checks"
PODOREL_ADMIN_PASSWORD="test-password-for-dry-run" scripts/deploy-prod.sh --dry-run
PROD_PORT_DRY_RUN_OUTPUT="$(PODOREL_ADMIN_PASSWORD="test-password-for-dry-run" PODOREL_PUBLIC_URL="http://curly-hub.local:9095" scripts/deploy-prod.sh --dry-run)"
if [[ "$PROD_PORT_DRY_RUN_OUTPUT" != *"Listen address: 0.0.0.0:9095"* || "$PROD_PORT_DRY_RUN_OUTPUT" != *"Published port: 9095"* || "$PROD_PORT_DRY_RUN_OUTPUT" != *"Public URL: http://curly-hub.local:9095"* ]]; then
  echo "Production dry-run must derive the listen/published port from an explicit public URL port." >&2
  exit 1
fi
PODOREL_ADMIN_PASSWORD="test-password-for-dry-run" scripts/install-new-machine.sh --dry-run
NEW_MACHINE_PORT_DRY_RUN_OUTPUT="$(PODOREL_ADMIN_PASSWORD="test-password-for-dry-run" PODOREL_PUBLIC_URL="http://curly-hub.local:9095" scripts/install-new-machine.sh --dry-run)"
if [[ "$NEW_MACHINE_PORT_DRY_RUN_OUTPUT" != *"Listen address: 0.0.0.0:9095"* || "$NEW_MACHINE_PORT_DRY_RUN_OUTPUT" != *"Published port: 9095"* ]]; then
  echo "New-machine dry-run must pass explicit public URL ports through to deploy-prod." >&2
  exit 1
fi
./install.sh --dry-run --yes
scripts/build-deploy.sh --dry-run --target /tmp/podorel-deploy-dry-run --name podorel-dry-run
scripts/git-export.sh --dry-run
scripts/deploy-dev.sh --dry-run
DEV_DRY_RUN_OUTPUT="$(scripts/deploy-dev.sh --dry-run)"
if [[ "$DEV_DRY_RUN_OUTPUT" != *"http://localhost:8080"* || "$DEV_DRY_RUN_OUTPUT" == *"127.0.0.1"* ]]; then
  echo "Development dry-run must advertise localhost URLs and avoid 127.0.0.1." >&2
  exit 1
fi
CUSTOM_DEV_DRY_RUN_OUTPUT="$(PODOREL_LISTEN_ADDR=localhost:18080 scripts/deploy-dev.sh --dry-run)"
if [[ "$CUSTOM_DEV_DRY_RUN_OUTPUT" != *"Generated UI proxy target: http://localhost:18080"* ]]; then
  echo "Development dry-run must generate a proxy target for the configured backend port." >&2
  exit 1
fi
if PODOREL_LISTEN_ADDR=127.0.0.1:8080 scripts/deploy-dev.sh --dry-run >/tmp/podorel-dev-dry-run-invalid.log 2>&1; then
  echo "Development dry-run must reject 127.0.0.1 listen addresses." >&2
  exit 1
fi

if [ "$VERBOSE" = "1" ]; then
  podorel_step "Optional tool visibility"
  command -v podman || true
  command -v trivy || true
fi

podorel_step "All currently implemented checks passed"
