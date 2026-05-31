#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
source "${ROOT_DIR}/scripts/lib/common.sh"

show_help() {
  cat <<'HELP'
Usage: scripts/git-export.sh [--target DIR] [--remote URL] [--branch NAME] [--commit MESSAGE] [--push] [--force] [--dry-run]

Creates a clean git-ready copy of PoDorel outside the working tree.

Defaults:
  target: ../PoDorel-git-ready
  branch: main
  commit: Initial PoDorel import
HELP
  podorel_print_common_help
}

TARGET="${PODOREL_GIT_EXPORT_DIR:-${ROOT_DIR}/../PoDorel-git-ready}"
REMOTE_URL="${PODOREL_GIT_REMOTE:-}"
BRANCH="${PODOREL_GIT_BRANCH:-main}"
COMMIT_MESSAGE="${PODOREL_GIT_COMMIT_MESSAGE:-Initial PoDorel import}"
PUSH=0
FORCE=0
DRY_RUN=0
VERBOSE=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --help)
      show_help
      exit 0
      ;;
    --target)
      TARGET="${2:?Missing value for --target}"
      shift
      ;;
    --remote)
      REMOTE_URL="${2:?Missing value for --remote}"
      shift
      ;;
    --branch)
      BRANCH="${2:?Missing value for --branch}"
      shift
      ;;
    --commit)
      COMMIT_MESSAGE="${2:?Missing value for --commit}"
      shift
      ;;
    --push)
      PUSH=1
      ;;
    --force)
      FORCE=1
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

podorel_setup_logging "git-export"
cd "$ROOT_DIR"

podorel_step "Checking export tools"
podorel_require_command git
podorel_require_command tar

TARGET_PARENT="$(dirname "$TARGET")"
mkdir -p "$TARGET_PARENT"
TARGET_PARENT_ABS="$(cd "$TARGET_PARENT" && pwd)"
TARGET_ABS="${TARGET_PARENT_ABS}/$(basename "$TARGET")"

case "${TARGET_ABS}/" in
  "${ROOT_DIR}/"*)
    echo "Refusing to export inside the source tree: ${TARGET_ABS}" >&2
    exit 1
    ;;
esac

podorel_step "Export plan"
echo "Source: ${ROOT_DIR}"
echo "Target: ${TARGET_ABS}"
echo "Branch: ${BRANCH}"
if [ "$REMOTE_URL" != "" ]; then
  echo "Remote: ${REMOTE_URL}"
fi
if [ "$DRY_RUN" = "1" ]; then
  exit 0
fi

if [ -e "$TARGET_ABS" ] && [ "$(find "$TARGET_ABS" -mindepth 1 -maxdepth 1 2>/dev/null | head -n 1)" != "" ]; then
  if [ "$FORCE" != "1" ]; then
    echo "Target exists and is not empty. Re-run with --force to replace it." >&2
    exit 1
  fi
  rm -rf "$TARGET_ABS"
fi
mkdir -p "$TARGET_ABS"

podorel_step "Copying clean source"
tar -C "$ROOT_DIR" \
  --exclude='./.git' \
  --exclude='./.podorel' \
  --exclude='./bin' \
  --exclude='./coverage' \
  --exclude='./dist' \
  --exclude='./ui/node_modules' \
  --exclude='./ui/dist' \
  --exclude='./ui/.angular' \
  --exclude='./reports' \
  --exclude='*.log' \
  --exclude='.env.*' \
  --exclude='.env' \
  --exclude='*.sock' \
  --exclude='*.sqlite-*' \
  --exclude='*.sqlite' \
  --exclude='*.db' \
  -cf - . | tar -C "$TARGET_ABS" -xf -

podorel_step "Initializing git repository"
cd "$TARGET_ABS"
if [ ! -d .git ]; then
  if ! git init -b "$BRANCH" >/dev/null 2>&1; then
    git init >/dev/null
    git checkout -B "$BRANCH" >/dev/null
  fi
else
  git checkout -B "$BRANCH" >/dev/null
fi

git add .
if git diff --cached --quiet; then
  echo "No file changes staged."
else
  if git config user.name >/dev/null && git config user.email >/dev/null; then
    git commit -m "$COMMIT_MESSAGE"
  else
    echo "Files are staged. Configure git user.name and user.email, then commit." >&2
  fi
fi

if [ "$REMOTE_URL" != "" ]; then
  if git remote get-url origin >/dev/null 2>&1; then
    git remote set-url origin "$REMOTE_URL"
  else
    git remote add origin "$REMOTE_URL"
  fi
fi

if [ "$PUSH" = "1" ]; then
  if [ "$REMOTE_URL" = "" ] && ! git remote get-url origin >/dev/null 2>&1; then
    echo "Cannot push without --remote or an existing origin remote." >&2
    exit 1
  fi
  git push -u origin "$BRANCH"
fi

podorel_step "Git export ready"
echo "$TARGET_ABS"
