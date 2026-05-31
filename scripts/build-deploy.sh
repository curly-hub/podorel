#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
source "${ROOT_DIR}/scripts/lib/common.sh"

show_help() {
  cat <<'HELP'
Usage: scripts/build-deploy.sh [--target DIR] [--name NAME] [--dry-run] [--skip-tests] [--skip-ui-build] [--no-archive] [--force]

Builds a minimal copy-to-new-machine deployment folder for PoDorel.

The generated folder contains only the runtime files needed by the installer:
compiled Linux binaries, built UI assets, migrations, templates, systemd units,
a prebuilt-image Containerfile, and a self-contained install.sh.

Defaults:
  target: deploy
  name: podorel-<version>-<goos>-<goarch>
HELP
  podorel_print_common_help
}

TARGET_ROOT="${PODOREL_DEPLOY_DIR:-${ROOT_DIR}/deploy}"
NAME="${PODOREL_DEPLOY_NAME:-}"
DRY_RUN=0
SKIP_TESTS=0
SKIP_UI_BUILD=0
NO_ARCHIVE=0
FORCE=0
VERBOSE=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --help)
      show_help
      exit 0
      ;;
    --target)
      TARGET_ROOT="${2:?Missing value for --target}"
      shift
      ;;
    --name)
      NAME="${2:?Missing value for --name}"
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      ;;
    --skip-tests)
      SKIP_TESTS=1
      ;;
    --skip-ui-build)
      SKIP_UI_BUILD=1
      ;;
    --no-archive)
      NO_ARCHIVE=1
      ;;
    --force)
      FORCE=1
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

podorel_setup_logging "build-deploy"
cd "$ROOT_DIR"

podorel_step "Checking build tools"
podorel_require_command go
podorel_require_command npm
podorel_require_command tar
podorel_require_command install

GOOS_TARGET="${PODOREL_TARGET_GOOS:-$(go env GOOS)}"
GOARCH_TARGET="${PODOREL_TARGET_GOARCH:-$(go env GOARCH)}"
if [ "$GOOS_TARGET" != "linux" ]; then
  echo "PoDorel deploy bundles currently target Linux only. Set PODOREL_TARGET_GOOS=linux." >&2
  exit 1
fi

VERSION="${PODOREL_VERSION:-}"
if [ "$VERSION" = "" ]; then
  VERSION="$(git describe --tags --always --dirty 2>/dev/null || true)"
fi
if [ "$VERSION" = "" ]; then
  VERSION="dev-$(date -u +%Y%m%d%H%M%S)"
fi

if [ "$NAME" = "" ]; then
  NAME="podorel-${VERSION}-${GOOS_TARGET}-${GOARCH_TARGET}"
fi

TARGET_PARENT="$(dirname "$TARGET_ROOT")"
mkdir -p "$TARGET_PARENT"
TARGET_PARENT_ABS="$(cd "$TARGET_PARENT" && pwd)"
TARGET_ROOT_ABS="${TARGET_PARENT_ABS}/$(basename "$TARGET_ROOT")"
case "${TARGET_ROOT_ABS}/" in
  "${ROOT_DIR}/"*)
    if [ "$TARGET_ROOT_ABS" != "${ROOT_DIR}/deploy" ]; then
      echo "Refusing to create deploy output inside the source tree except ./deploy: ${TARGET_ROOT_ABS}" >&2
      exit 1
    fi
    ;;
esac

BUNDLE_DIR="${TARGET_ROOT_ABS}/${NAME}"
ARCHIVE_PATH="${TARGET_ROOT_ABS}/${NAME}.tar.gz"

podorel_step "Deploy artifact plan"
echo "Source: ${ROOT_DIR}"
echo "Bundle: ${BUNDLE_DIR}"
echo "Archive: ${ARCHIVE_PATH}"
echo "Target: ${GOOS_TARGET}/${GOARCH_TARGET}"
echo "Version: ${VERSION}"

if [ "$DRY_RUN" = "1" ]; then
  podorel_step "Dry run complete"
  exit 0
fi

if [ -e "$BUNDLE_DIR" ] || [ -e "$ARCHIVE_PATH" ]; then
  if [ "$FORCE" != "1" ]; then
    echo "Deploy artifact already exists. Re-run with --force to replace it." >&2
    exit 1
  fi
  rm -rf "$BUNDLE_DIR" "$ARCHIVE_PATH"
fi

mkdir -p "$BUNDLE_DIR/bin" "$BUNDLE_DIR/server" "$BUNDLE_DIR/ui" "$BUNDLE_DIR/packaging/systemd" "$BUNDLE_DIR/packaging/podman"

if [ "$SKIP_TESTS" != "1" ]; then
  podorel_step "Running release checks"
  go test ./...
  node ui/scripts/check-ui.mjs
fi

podorel_step "Building Go binaries"
CGO_ENABLED="${CGO_ENABLED:-1}" GOOS="$GOOS_TARGET" GOARCH="$GOARCH_TARGET" go build -trimpath -ldflags "-s -w" -o "$BUNDLE_DIR/bin/podorel" ./cmd/podorel
CGO_ENABLED="${CGO_ENABLED:-1}" GOOS="$GOOS_TARGET" GOARCH="$GOARCH_TARGET" go build -trimpath -ldflags "-s -w" -o "$BUNDLE_DIR/bin/podorel-agent" ./agent/cmd/podorel-agent
CGO_ENABLED="${CGO_ENABLED:-1}" GOOS="$GOOS_TARGET" GOARCH="$GOARCH_TARGET" go build -trimpath -ldflags "-s -w" -o "$BUNDLE_DIR/bin/podorel-web" ./server/cmd/podorel-web

if [ "$SKIP_UI_BUILD" != "1" ]; then
  podorel_step "Building Angular UI"
  if [ ! -d ui/node_modules ]; then
    (cd ui && npm ci)
  fi
  (cd ui && npm run build)
fi

UI_DIST="${ROOT_DIR}/ui/dist/podorel-ui/browser"
if [ ! -f "${UI_DIST}/index.html" ]; then
  echo "Built UI not found at ${UI_DIST}. Run without --skip-ui-build or build the UI first." >&2
  exit 1
fi

podorel_step "Copying runtime files"
cp -R server/migrations "$BUNDLE_DIR/server/migrations"
cp -R server/templates "$BUNDLE_DIR/server/templates"
cp -R "$UI_DIST"/. "$BUNDLE_DIR/ui/"
cp packaging/systemd/podorel-agent.service "$BUNDLE_DIR/packaging/systemd/podorel-agent.service"
cp packaging/systemd/podorel-web.service "$BUNDLE_DIR/packaging/systemd/podorel-web.service"
cp packaging/podman/Containerfile.web-prebuilt "$BUNDLE_DIR/packaging/podman/Containerfile.web-prebuilt"
cp LICENSE COMMERCIAL-LICENSE.md NOTICE "$BUNDLE_DIR/"

cat > "$BUNDLE_DIR/install.sh" <<'INSTALL_SH'
#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

show_help() {
  cat <<'HELP'
Usage: ./install.sh [--yes] [--admin-password PASS] [--public-url URL] [--listen-addr ADDR] [--target-user USER] [--dry-run]

Installs this prebuilt PoDorel deploy bundle on a supported Linux machine.

The bundle does not need Go, Node, npm, or the source tree on the target host.
It installs the agent binary, builds a small Podman image from the prebuilt web
binary and UI assets, and starts the user-level systemd services.
HELP
}

step() {
  echo
  echo "==> $*"
}

require_command() {
  local name="$1"
  if ! command -v "$name" >/dev/null 2>&1; then
    echo "Missing required command: $name" >&2
    return 1
  fi
}

detect_os_id() {
  if [ ! -r /etc/os-release ]; then
    echo "Unsupported Linux distribution for PoDorel v1. Supported: Debian, Ubuntu, Fedora." >&2
    return 1
  fi
  . /etc/os-release
  case "${ID:-}" in
    debian|ubuntu|fedora)
      echo "$ID"
      ;;
    *)
      echo "Unsupported Linux distribution for PoDorel v1. Supported: Debian, Ubuntu, Fedora." >&2
      return 1
      ;;
  esac
}

install_packages() {
  local os_id="$1"
  case "$os_id" in
    debian|ubuntu)
      apt-get update
      apt-get install -y podman uidmap slirp4netns fuse-overlayfs sqlite3
      ;;
    fedora)
      dnf install -y podman shadow-utils slirp4netns fuse-overlayfs sqlite
      ;;
  esac
}

listen_port() {
  local addr="$1"
  local port="${addr##*:}"
  if [[ ! "$port" =~ ^[0-9]+$ ]]; then
    echo "Could not determine listen port from ${addr}" >&2
    return 1
  fi
  echo "$port"
}

YES=0
DRY_RUN=0
ADMIN_PASSWORD="${PODOREL_ADMIN_PASSWORD:-}"
PUBLIC_URL="${PODOREL_PUBLIC_URL:-}"
LISTEN_ADDR="${PODOREL_LISTEN_ADDR:-0.0.0.0:8080}"
TARGET_USER="${PODOREL_INSTALL_TARGET_USER:-${SUDO_USER:-${USER}}}"
ORIGINAL_ARGS=("$@")

while [ "$#" -gt 0 ]; do
  case "$1" in
    --help)
      show_help
      exit 0
      ;;
    --yes|-y)
      YES=1
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
    *)
      echo "Unknown argument: $1" >&2
      show_help
      exit 2
      ;;
  esac
  shift
done

if [ "$DRY_RUN" != "1" ] && [ "$YES" != "1" ]; then
  if [ ! -t 0 ]; then
    echo "Refusing non-interactive install without --yes." >&2
    exit 2
  fi
  echo "PoDorel will install Podman prerequisites, install user services, and start the web pod."
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

OS_ID="$(detect_os_id)"
LISTEN_PORT="$(listen_port "$LISTEN_ADDR")"
step "Detected supported OS"
echo "$OS_ID"

if [ "$DRY_RUN" = "1" ]; then
  step "Dry run"
  echo "Bundle: ${SCRIPT_DIR}"
  echo "Target user: ${TARGET_USER}"
  echo "Listen address: ${LISTEN_ADDR}"
  echo "Published port: ${LISTEN_PORT}"
  echo "Public URL: ${PUBLIC_URL:-http://podorel.lan:8080}"
  require_command podman
  require_command install
  exit 0
fi

if [ "$EUID" -ne 0 ]; then
  require_command sudo
  export PODOREL_ADMIN_PASSWORD="$ADMIN_PASSWORD"
  export PODOREL_PUBLIC_URL="$PUBLIC_URL"
  export PODOREL_LISTEN_ADDR="$LISTEN_ADDR"
  export PODOREL_INSTALL_TARGET_USER="$TARGET_USER"
  exec sudo --preserve-env=PODOREL_ADMIN_PASSWORD,PODOREL_PUBLIC_URL,PODOREL_LISTEN_ADDR,PODOREL_INSTALL_TARGET_USER bash "$0" "${ORIGINAL_ARGS[@]}"
fi

for required in bin/podorel bin/podorel-agent bin/podorel-web ui/index.html server/migrations server/templates packaging/systemd/podorel-agent.service packaging/systemd/podorel-web.service packaging/podman/Containerfile.web-prebuilt; do
  if [ ! -e "$required" ]; then
    echo "Deploy bundle is incomplete; missing ${required}" >&2
    exit 1
  fi
done

GENERATED_PASSWORD=0
if [ "$ADMIN_PASSWORD" = "" ]; then
  ADMIN_PASSWORD="$(head -c 32 /dev/urandom | base64 | tr -d '\n')"
  GENERATED_PASSWORD=1
fi

TARGET_HOME="$(getent passwd "$TARGET_USER" | cut -d: -f6)"
if [ "$TARGET_HOME" = "" ]; then
  echo "Could not determine home directory for ${TARGET_USER}" >&2
  exit 1
fi

step "Installing OS packages"
install_packages "$OS_ID"

step "Checking required production tools"
require_command podman
require_command install
require_command loginctl
require_command sudo
require_command sed

step "Installing binaries"
install -d -m 0755 -o "$TARGET_USER" -g "$TARGET_USER" "${TARGET_HOME}/.local/bin"
install -m 0755 bin/podorel /usr/local/bin/podorel
install -m 0755 bin/podorel-agent "${TARGET_HOME}/.local/bin/podorel-agent"
chown "$TARGET_USER:$TARGET_USER" "${TARGET_HOME}/.local/bin/podorel-agent"

step "Creating persistent directories"
install -d -m 0700 -o "$TARGET_USER" -g "$TARGET_USER" "${TARGET_HOME}/.local/share/podorel" "${TARGET_HOME}/.local/share/podorel/logs" "${TARGET_HOME}/.config/podorel"
if [ ! -f "${TARGET_HOME}/.config/podorel/agent-token" ]; then
  umask 077
  head -c 32 /dev/urandom | base64 > "${TARGET_HOME}/.config/podorel/agent-token"
  chown "$TARGET_USER:$TARGET_USER" "${TARGET_HOME}/.config/podorel/agent-token"
fi
cat > "${TARGET_HOME}/.config/podorel/web.env" <<ENV
PODOREL_ADMIN_PASSWORD=${ADMIN_PASSWORD}
PODOREL_LISTEN_ADDR=${LISTEN_ADDR}
PODOREL_PUBLIC_URL=${PUBLIC_URL:-http://podorel.lan:8080}
PODOREL_MODE=production
PODOREL_AGENT_SOCKET=/run/podorel-agent/podorel-agent.sock
PODOREL_LOG_DIR=/app/data/logs
ENV
chmod 0600 "${TARGET_HOME}/.config/podorel/web.env"
chown "$TARGET_USER:$TARGET_USER" "${TARGET_HOME}/.config/podorel/web.env"

step "Enabling linger"
loginctl enable-linger "$TARGET_USER"

step "Installing systemd user units"
install -d -m 0755 -o "$TARGET_USER" -g "$TARGET_USER" "${TARGET_HOME}/.config/systemd/user"
install -m 0644 packaging/systemd/podorel-agent.service "${TARGET_HOME}/.config/systemd/user/podorel-agent.service"
WEB_UNIT_TMP="$(mktemp)"
sed "s/-p 8080:8080/-p ${LISTEN_PORT}:${LISTEN_PORT}/g" packaging/systemd/podorel-web.service > "$WEB_UNIT_TMP"
install -m 0644 "$WEB_UNIT_TMP" "${TARGET_HOME}/.config/systemd/user/podorel-web.service"
rm -f "$WEB_UNIT_TMP"
chown "$TARGET_USER:$TARGET_USER" "${TARGET_HOME}/.config/systemd/user/podorel-agent.service" "${TARGET_HOME}/.config/systemd/user/podorel-web.service"

step "Building web image from prebuilt runtime files"
sudo -u "$TARGET_USER" podman build -t podorel-web:latest -f packaging/podman/Containerfile.web-prebuilt .

step "Starting user services"
sudo -u "$TARGET_USER" XDG_RUNTIME_DIR="/run/user/$(id -u "$TARGET_USER")" systemctl --user daemon-reload
sudo -u "$TARGET_USER" XDG_RUNTIME_DIR="/run/user/$(id -u "$TARGET_USER")" systemctl --user enable --now podorel-agent.service
sudo -u "$TARGET_USER" XDG_RUNTIME_DIR="/run/user/$(id -u "$TARGET_USER")" systemctl --user enable --now podorel-web.service

if [ "$GENERATED_PASSWORD" = "1" ]; then
  printf '%s\n' "$ADMIN_PASSWORD" > "${TARGET_HOME}/.config/podorel/generated-admin-password"
  chmod 0600 "${TARGET_HOME}/.config/podorel/generated-admin-password"
  chown "$TARGET_USER:$TARGET_USER" "${TARGET_HOME}/.config/podorel/generated-admin-password"
  step "Generated admin password"
  echo "Saved to ${TARGET_HOME}/.config/podorel/generated-admin-password"
  echo "$ADMIN_PASSWORD"
fi

step "Install complete"
echo "PoDorel: ${PUBLIC_URL:-http://podorel.lan:8080}"
echo "Services: systemctl --user status podorel-web.service podorel-agent.service"
INSTALL_SH

chmod 0755 "$BUNDLE_DIR/install.sh"

cat > "$BUNDLE_DIR/README.txt" <<README_TXT
PoDorel deploy bundle
=====================

Version: ${VERSION}
Target: ${GOOS_TARGET}/${GOARCH_TARGET}
Built: $(date -u +%Y-%m-%dT%H:%M:%SZ)

Copy this folder to a supported Linux machine, then run:

  ./install.sh --yes --public-url http://podorel.lan:8080

To set the first admin password explicitly:

  ./install.sh --yes --admin-password 'change-me' --public-url http://podorel.lan:8080

The target machine needs Debian, Ubuntu, or Fedora with sudo access. The
installer installs Podman prerequisites and does not require Go, Node, npm, or
the PoDorel source tree.
README_TXT

cat > "$BUNDLE_DIR/MANIFEST.txt" <<MANIFEST_TXT
PoDorel deploy manifest
Version: ${VERSION}
Target: ${GOOS_TARGET}/${GOARCH_TARGET}

Runtime files:
  bin/podorel
  bin/podorel-agent
  bin/podorel-web
  ui/
  server/migrations/
  server/templates/
  packaging/systemd/
  packaging/podman/Containerfile.web-prebuilt
  install.sh
MANIFEST_TXT

podorel_step "Validating deploy bundle"
bash -n "$BUNDLE_DIR/install.sh"
test -x "$BUNDLE_DIR/bin/podorel"
test -x "$BUNDLE_DIR/bin/podorel-agent"
test -x "$BUNDLE_DIR/bin/podorel-web"
test -f "$BUNDLE_DIR/ui/index.html"

if [ "$NO_ARCHIVE" != "1" ]; then
  podorel_step "Creating archive"
  tar -C "$TARGET_ROOT_ABS" -czf "$ARCHIVE_PATH" "$NAME"
fi

podorel_step "Deploy bundle ready"
echo "$BUNDLE_DIR"
if [ "$NO_ARCHIVE" != "1" ]; then
  echo "$ARCHIVE_PATH"
fi
