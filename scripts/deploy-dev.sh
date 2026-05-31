#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
source "${ROOT_DIR}/scripts/lib/common.sh"

show_help() {
  cat <<'HELP'
Usage: scripts/deploy-dev.sh [--help] [--verbose] [--dry-run] [--detach]

Starts PoDorel development services with explicit development mode.

Development binds to HTTP localhost only. The Angular proxy is generated for the
active backend port so /api and websockets cannot accidentally point at an old
backend.

By default this script is a foreground supervisor: if the script process exits,
it stops the agent, web API, and UI. Use --detach when you want the dev stack to
survive the launching shell/session.
HELP
  podorel_print_common_help
}

VERBOSE=0
DRY_RUN=0
DETACH=0
FORWARD_ARGS=()
for arg in "$@"; do
  case "$arg" in
    --help)
      show_help
      exit 0
      ;;
    --verbose)
      VERBOSE=1
      FORWARD_ARGS+=("$arg")
      ;;
    --dry-run)
      DRY_RUN=1
      FORWARD_ARGS+=("$arg")
      ;;
    --detach)
      DETACH=1
      ;;
    *)
      echo "Unknown argument: $arg" >&2
      show_help
      exit 2
      ;;
  esac
done

if [ "$DETACH" = "1" ] && [ "${PODOREL_DEV_DETACHED:-}" != "1" ]; then
  mkdir -p "${ROOT_DIR}/.podorel"
  supervisor_log="${ROOT_DIR}/.podorel/dev-supervisor.log"
  echo "Starting PoDorel dev supervisor detached. Log: ${supervisor_log}"
  setsid -f env PODOREL_DEV_DETACHED=1 "$0" "${FORWARD_ARGS[@]}" >"$supervisor_log" 2>&1
  echo "PoDorel will report detailed supervisor status in ${ROOT_DIR}/.podorel/dev-status.json."
  exit 0
fi

podorel_setup_logging "deploy-dev"
cd "$ROOT_DIR"

podorel_step "Runtime mode"
echo "Active runtime mode: development"

podorel_step "Detecting supported OS"
podorel_detect_os_id >/dev/null

podorel_step "Checking development tools"
podorel_require_command curl
podorel_require_command go
podorel_require_command npm
podorel_require_command podman
podorel_require_command setsid

podorel_step "Checking UI dependencies"
if [ ! -d ui/node_modules ]; then
  echo "Angular dependencies are not installed yet. Run npm install in ui/ before using the Angular dev server." >&2
fi

PODOREL_DEV_LISTEN_ADDR="${PODOREL_LISTEN_ADDR:-localhost:8080}"
PODOREL_DEV_PUBLIC_URL="${PODOREL_PUBLIC_URL:-http://${PODOREL_DEV_LISTEN_ADDR}}"
PODOREL_DEV_UI_URL="${PODOREL_UI_URL:-http://localhost:4200}"
PODOREL_DEV_DATA_DIR="${PODOREL_DEV_DATA_DIR:-${HOME}/.local/share/podorel}"
PODOREL_DEV_CONFIG_DIR="${PODOREL_DEV_CONFIG_DIR:-${HOME}/.config/podorel}"
PODOREL_DEV_TOKEN_FILE="${PODOREL_AGENT_TOKEN_FILE:-${PODOREL_DEV_CONFIG_DIR}/agent-token}"
PODOREL_DEV_SOCKET_PATH="${PODOREL_AGENT_SOCKET:-${TMPDIR:-/tmp}/podorel-${UID}/podorel-agent.sock}"
PODOREL_DEV_LOG_DIR="${ROOT_DIR}/.podorel/dev-logs"
PODOREL_DEV_PID_DIR="${ROOT_DIR}/.podorel/dev-pids"
PODOREL_DEV_PROXY_CONFIG="${ROOT_DIR}/.podorel/dev-proxy.conf.json"
PODOREL_DEV_PROXY_ARG="../.podorel/dev-proxy.conf.json"
PODOREL_DEV_STATUS_FILE="${PODOREL_DEV_STATUS_FILE:-${ROOT_DIR}/.podorel/dev-status.json}"
PIDS=()
COMPOSE_STARTED=0
COMPOSE_WARNING=""
STOP_REASON=""


json_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\n'/\\n}"
  printf '%s' "$value"
}

write_dev_status() {
  local status="$1"
  local message="$2"
  local now mode
  now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  mode="foreground"
  if [ "${PODOREL_DEV_DETACHED:-}" = "1" ]; then
    mode="detached"
  fi
  mkdir -p "$(dirname "$PODOREL_DEV_STATUS_FILE")"
  cat >"$PODOREL_DEV_STATUS_FILE" <<JSON
{
  "status": "$(json_escape "$status")",
  "message": "$(json_escape "$message")",
  "updated_at": "${now}",
  "supervisor_pid": $$,
  "supervisor_mode": "${mode}",
  "web_url": "$(json_escape "$PODOREL_DEV_PUBLIC_URL")",
  "ui_url": "$(json_escape "$PODOREL_DEV_UI_URL")",
  "agent_pid": "${PIDS[0]:-}",
  "web_pid": "${PIDS[1]:-}",
  "ui_pid": "${PIDS[2]:-}",
  "log_dir": "$(json_escape "$PODOREL_DEV_LOG_DIR")",
  "script_log": "$(json_escape "${PODOREL_SCRIPT_LOG:-}")",
  "compose_warning": "$(json_escape "$COMPOSE_WARNING")"
}
JSON
}

url_host_port() {
  local url="$1"
  url="${url#http://}"
  url="${url#https://}"
  echo "${url%%/*}"
}

url_port() {
  local url="$1"
  local host_port
  host_port="$(url_host_port "$url")"
  if [[ "$host_port" == *:* ]]; then
    echo "${host_port##*:}"
  elif [[ "$url" == https://* ]]; then
    echo "443"
  else
    echo "80"
  fi
}

listen_port() {
  local listen_addr="$1"
  echo "${listen_addr##*:}"
}

validate_localhost_endpoint() {
  local label="$1"
  local value="$2"
  if [[ "$value" == 127.0.0.1:* || "$value" == http://127.0.0.1:* || "$value" == https://127.0.0.1:* ]]; then
    echo "${label} must use localhost, not 127.0.0.1." >&2
    return 1
  fi
  if [[ "$value" != localhost:* && "$value" != http://localhost:* && "$value" != https://localhost:* ]]; then
    echo "${label} must bind to localhost in development: ${value}" >&2
    return 1
  fi
}

port_listener_pid() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -nP -iTCP:"${port}" -sTCP:LISTEN -t 2>/dev/null | sort -u | head -n 1
    return 0
  fi
  if command -v ss >/dev/null 2>&1; then
    ss -H -ltnp "sport = :${port}" 2>/dev/null | sed -n 's/.*pid=\([0-9][0-9]*\).*/\1/p' | head -n 1
    return 0
  fi
  echo "Cannot inspect port ${port}; install lsof or ss." >&2
  return 1
}

pid_command() {
  local pid="$1"
  ps -p "$pid" -o command= 2>/dev/null || true
}

is_managed_command() {
  local command="$1"
  case "$command" in
    *podorel-agent*|*podorel-web*|*"agent/cmd/podorel-agent"*|*"server/cmd/podorel-web"*|*"ng serve --host localhost --port "*|*"npm --prefix ui start"*|*"npm start"*)
      return 0
      ;;
  esac
  return 1
}

process_group_id() {
  local pid="$1"
  ps -p "$pid" -o pgid= 2>/dev/null | tr -d ' ' || true
}

terminate_process_tree() {
  local pid="$1"
  local child
  while read -r child; do
    if [ -n "$child" ]; then
      terminate_process_tree "$child"
    fi
  done < <(ps -o pid= --ppid "$pid" 2>/dev/null || true)
  kill "$pid" >/dev/null 2>&1 || true
}

terminate_tracked_pid() {
  local pid="$1"
  local pgid
  pgid="$(process_group_id "$pid")"
  if [ -n "$pgid" ] && [ "$pgid" = "$pid" ]; then
    kill -- "-${pid}" >/dev/null 2>&1 || true
  else
    terminate_process_tree "$pid"
  fi
}

wait_for_exit() {
  local pid="$1"
  for _ in {1..20}; do
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  return 1
}

cleanup_managed_pid() {
  local name="$1"
  local pid_file="${PODOREL_DEV_PID_DIR}/${name}.pid"
  local pid command
  if [ ! -f "$pid_file" ]; then
    return 0
  fi
  pid="$(<"$pid_file")"
  if [[ ! "$pid" =~ ^[0-9]+$ ]]; then
    rm -f "$pid_file"
    return 0
  fi
  if ! kill -0 "$pid" >/dev/null 2>&1; then
    rm -f "$pid_file"
    return 0
  fi
  command="$(pid_command "$pid")"
  if ! is_managed_command "$command"; then
    echo "Refusing to stop ${name} pid ${pid}; it no longer looks like a PoDorel dev child." >&2
    echo "Command: ${command}" >&2
    return 1
  fi
  echo "Stopping stale managed ${name} process ${pid}"
  terminate_tracked_pid "$pid"
  if wait_for_exit "$pid"; then
    rm -f "$pid_file"
    return 0
  fi
  echo "Managed ${name} process ${pid} did not exit after SIGTERM." >&2
  return 1
}

preflight_port() {
  local label="$1"
  local port="$2"
  local pid command
  pid="$(port_listener_pid "$port")"
  if [ -z "$pid" ]; then
    return 0
  fi
  command="$(pid_command "$pid")"
  echo "${label} port ${port} is already in use by pid ${pid}." >&2
  echo "Command: ${command}" >&2
  echo "Stop that process or choose an explicit custom port before starting PoDorel dev." >&2
  return 1
}

generate_proxy_config() {
  mkdir -p "$(dirname "$PODOREL_DEV_PROXY_CONFIG")"
  cat >"$PODOREL_DEV_PROXY_CONFIG" <<JSON
{
  "/api": {
    "target": "${PODOREL_DEV_PUBLIC_URL}",
    "secure": false,
    "changeOrigin": true,
    "ws": true
  }
}
JSON
}

if [ "$DRY_RUN" = "1" ]; then
  podorel_step "Development endpoints"
  echo "Web API: ${PODOREL_DEV_PUBLIC_URL}"
  echo "Angular UI: ${PODOREL_DEV_UI_URL}"
  echo "Go listen address: ${PODOREL_DEV_LISTEN_ADDR}"
  echo "Generated UI proxy target: ${PODOREL_DEV_PUBLIC_URL}"
  validate_localhost_endpoint "Development listen address" "$PODOREL_DEV_LISTEN_ADDR"
  validate_localhost_endpoint "Development public URL" "$PODOREL_DEV_PUBLIC_URL"
  validate_localhost_endpoint "Development UI URL" "$PODOREL_DEV_UI_URL"
  podorel_step "Dry run complete"
  exit 0
fi

podorel_step "Preparing development data"
mkdir -p "${PODOREL_DEV_DATA_DIR}/logs" "${PODOREL_DEV_CONFIG_DIR}" "$(dirname "$PODOREL_DEV_SOCKET_PATH")" "$PODOREL_DEV_LOG_DIR" "$PODOREL_DEV_PID_DIR" "$(dirname "$PODOREL_DEV_STATUS_FILE")"
write_dev_status "starting" "Development supervisor is starting."

podorel_step "Development supervisor"
if [ "${PODOREL_DEV_DETACHED:-}" = "1" ]; then
  echo "Supervisor mode: detached. PoDorel should remain up after this shell/session exits."
else
  echo "Warning: supervisor mode is foreground. If this terminal/session closes, PoDorel dev services stop."
  echo "Use scripts/deploy-dev.sh --detach to keep PoDorel running independently."
fi
if [ ! -f "$PODOREL_DEV_TOKEN_FILE" ]; then
  umask 077
  head -c 32 /dev/urandom | base64 > "$PODOREL_DEV_TOKEN_FILE"
fi

podorel_step "Preflight development endpoints"
validate_localhost_endpoint "Development listen address" "$PODOREL_DEV_LISTEN_ADDR"
validate_localhost_endpoint "Development public URL" "$PODOREL_DEV_PUBLIC_URL"
validate_localhost_endpoint "Development UI URL" "$PODOREL_DEV_UI_URL"
for managed_name in agent web ui; do
  if ! cleanup_managed_pid "$managed_name"; then
    exit 1
  fi
done
if ! preflight_port "Web API" "$(listen_port "$PODOREL_DEV_LISTEN_ADDR")"; then
  exit 1
fi
if ! preflight_port "Angular UI" "$(url_port "$PODOREL_DEV_UI_URL")"; then
  exit 1
fi
generate_proxy_config
echo "UI proxy: /api -> ${PODOREL_DEV_PUBLIC_URL} (${PODOREL_DEV_PROXY_CONFIG})"

cleanup() {
  local status="$?"
  trap - EXIT INT TERM
  if [ "$status" = "0" ]; then
    write_dev_status "stopped" "Development supervisor exited cleanly; child services were stopped."
  else
    write_dev_status "failed" "${STOP_REASON:-Development supervisor exited with status ${status}; child services were stopped.}"
  fi
  if [ "${#PIDS[@]}" -gt 0 ]; then
    local pid
    for pid in "${PIDS[@]}"; do
      terminate_tracked_pid "$pid"
    done
  fi
  if [ "$COMPOSE_STARTED" = "1" ]; then
    if command -v podman-compose >/dev/null 2>&1; then
      podman-compose -f packaging/podman/podman-compose.dev.yml down >/dev/null 2>&1 || true
    elif podman compose version >/dev/null 2>&1; then
      podman compose -f packaging/podman/podman-compose.dev.yml down >/dev/null 2>&1 || true
    fi
  fi
  exit "$status"
}
trap cleanup EXIT INT TERM

start_process() {
  local name="$1"
  shift
  local log_file="${PODOREL_DEV_LOG_DIR}/${name}.log"
  setsid "$@" >"$log_file" 2>&1 &
  local pid="$!"
  PIDS+=("$pid")
  echo "$pid" >"${PODOREL_DEV_PID_DIR}/${name}.pid"
  echo "${name}: pid ${pid}, log ${log_file}"
}

ensure_process_running() {
  local name="$1"
  local pid="$2"
  if ! kill -0 "$pid" >/dev/null 2>&1; then
    wait "$pid" || true
    echo "${name} exited before it became healthy. Check ${PODOREL_DEV_LOG_DIR}/${name}.log" >&2
    return 1
  fi
}

wait_for_agent() {
  local pid="$1"
  local token
  token="$(tr -d '\n' < "$PODOREL_DEV_TOKEN_FILE")"
  for _ in {1..80}; do
    ensure_process_running agent "$pid"
    if curl -fsS --max-time 2 --unix-socket "$PODOREL_DEV_SOCKET_PATH" -H "Authorization: Bearer ${token}" http://podorel-agent/health >/dev/null 2>&1; then
      echo "agent: healthy"
      return 0
    fi
    sleep 0.25
  done
  echo "agent did not become healthy at ${PODOREL_DEV_SOCKET_PATH}." >&2
  return 1
}

wait_for_http() {
  local name="$1"
  local pid="$2"
  local url="$3"
  for _ in {1..120}; do
    ensure_process_running "$name" "$pid"
    if curl -fsS --max-time 2 "$url" >/dev/null 2>&1; then
      echo "${name}: healthy"
      return 0
    fi
    sleep 0.5
  done
  echo "${name} did not become healthy at ${url}." >&2
  return 1
}

supervise_children() {
  while true; do
    local pid
    for pid in "${PIDS[@]}"; do
      if ! kill -0 "$pid" >/dev/null 2>&1; then
        wait "$pid" || true
        STOP_REASON="Required dev service pid ${pid} exited; stopping the remaining services."
        echo "A required PoDorel dev service exited; stopping the remaining services." >&2
        return 1
      fi
    done
    sleep 1
  done
}

podorel_step "Starting development dependencies"
if command -v podman-compose >/dev/null 2>&1; then
  if podman-compose -f packaging/podman/podman-compose.dev.yml up -d; then
    COMPOSE_STARTED=1
  else
    COMPOSE_WARNING="Podman Compose dependencies did not start; continuing because no external PoDorel dev dependency is required yet."
    echo "$COMPOSE_WARNING" >&2
  fi
elif podman compose version >/dev/null 2>&1; then
  if podman compose -f packaging/podman/podman-compose.dev.yml up -d; then
    COMPOSE_STARTED=1
  else
    COMPOSE_WARNING="Podman Compose dependencies did not start; continuing because no external PoDorel dev dependency is required yet."
    echo "$COMPOSE_WARNING" >&2
  fi
else
  COMPOSE_WARNING="No Podman Compose provider found; continuing because PoDorel currently has no external dev dependency services."
  echo "$COMPOSE_WARNING"
fi

podorel_step "Starting PoDorel development services"
start_process agent go run ./agent/cmd/podorel-agent --development --socket-path "$PODOREL_DEV_SOCKET_PATH" --token-file "$PODOREL_DEV_TOKEN_FILE"
wait_for_agent "${PIDS[-1]}"
start_process web env PODOREL_AGENT_SOCKET="$PODOREL_DEV_SOCKET_PATH" PODOREL_AGENT_TOKEN_FILE="$PODOREL_DEV_TOKEN_FILE" PODOREL_LOG_DIR="$PODOREL_DEV_LOG_DIR" PODOREL_DEV_STATUS_FILE="$PODOREL_DEV_STATUS_FILE" go run ./server/cmd/podorel-web --development --listen-addr "$PODOREL_DEV_LISTEN_ADDR" --public-url "$PODOREL_DEV_PUBLIC_URL" --db-path "${PODOREL_DEV_DATA_DIR}/podorel.db" --ui-dist-path ui/dist/podorel-ui/browser
wait_for_http web "${PIDS[-1]}" "${PODOREL_DEV_PUBLIC_URL}/api/health"
start_process ui npm --prefix ui start -- --proxy-config "$PODOREL_DEV_PROXY_ARG"
wait_for_http ui "${PIDS[-1]}" "$PODOREL_DEV_UI_URL"

podorel_step "Development URLs"
echo "Angular UI: ${PODOREL_DEV_UI_URL}"
echo "Web API: ${PODOREL_DEV_PUBLIC_URL}/api/health"
echo "Logs: ${PODOREL_DEV_LOG_DIR}"
echo "Supervisor status: ${PODOREL_DEV_STATUS_FILE}"
if [ "${PODOREL_DEV_DETACHED:-}" = "1" ]; then
  echo "Detached supervisor is running. Stop it by terminating pid $$ or running a new dev start that cleans managed PIDs."
else
  echo "Press Ctrl-C to stop PoDorel development services. Closing this shell/session also stops them."
fi
write_dev_status "running" "Development supervisor is running."

supervise_children
