#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${ROOT_DIR}"

log() {
  printf '[run-dev] %s\n' "$*" >&2
}

die() {
  printf '[run-dev] error: %s\n' "$*" >&2
  exit 1
}

load_env_file() {
  local file="$1"
  if [[ -f "${file}" ]]; then
    log "loading ${file#${ROOT_DIR}/}"
    set -a
    # shellcheck disable=SC1090
    source "${file}"
    set +a
  fi
}

require_command() {
  local name="$1"
  command -v "${name}" >/dev/null 2>&1 || die "${name} is required"
}

is_port_value() {
  local value="$1"
  [[ "${value}" =~ ^[0-9]+$ ]] && ((10#${value} >= 1)) && ((10#${value} <= 65535))
}

can_listen_on_port() {
  local port="$1"
  local host="$2"
  node - "${port}" "${host}" <<'NODE'
const net = require('node:net');

const port = Number(process.argv[2]);
const host = process.argv[3];

const server = net.createServer();
server.once('error', () => {
  process.exit(1);
});
server.once('listening', () => {
  server.close(() => {
    process.exit(0);
  });
});
server.listen(port, host);
NODE
}

list_port_pids() {
  local port="$1"
  lsof -nP -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null | sort -u || true
}

format_pids() {
  local pids="$1"
  printf '%s' "${pids//$'\n'/, }"
}

wait_for_port_free() {
  local port="$1"
  local host="$2"

  for _ in 1 2 3 4 5 6 7 8 9 10; do
    if can_listen_on_port "${port}" "${host}"; then
      return 0
    fi
    sleep 0.2
  done

  return 1
}

find_available_port() {
  local start="$1"
  local host="$2"
  local candidate

  for offset in $(seq 1 100); do
    candidate=$((10#${start} + offset))
    if is_port_value "${candidate}" && can_listen_on_port "${candidate}" "${host}"; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done

  return 1
}

use_alternate_port() {
  local name="$1"
  local port="$2"
  local host="$3"
  local candidate

  candidate="$(find_available_port "${port}" "${host}")" || die "${name}=${port} is not available and no nearby free port was found"
  log "${name}=${port} is not available; using ${name}=${candidate}"
  printf -v "${name}" '%s' "${candidate}"
}

ensure_port_available() {
  local name="$1"
  local port="$2"
  local host="$3"
  local pids

  is_port_value "${port}" || die "${name} must be a TCP port between 1 and 65535"
  if can_listen_on_port "${port}" "${host}"; then
    return
  fi

  log "${name}=${port} is not available yet; waiting briefly"
  if wait_for_port_free "${port}" "${host}"; then
    return
  fi

  require_command lsof
  pids="$(list_port_pids "${port}")"
  if [[ -z "${pids}" ]]; then
    use_alternate_port "${name}" "${port}" "${host}"
    return
  fi

  log "${name}=${port} is in use by PID(s): $(format_pids "${pids}"); stopping them"
  kill -TERM ${pids} 2>/dev/null || true

  if wait_for_port_free "${port}" "${host}"; then
    return
  fi

  pids="$(list_port_pids "${port}")"
  if [[ -z "${pids}" ]]; then
    use_alternate_port "${name}" "${port}" "${host}"
    return
  fi
  log "${name}=${port} is still in use; force killing PID(s): $(format_pids "${pids}")"
  kill -KILL ${pids} 2>/dev/null || true
  wait_for_port_free "${port}" "${host}" || use_alternate_port "${name}" "${port}" "${host}"
}

load_env_file "${ROOT_DIR}/.env"
load_env_file "${ROOT_DIR}/.env.local"

require_command node
require_command pnpm

if ! command -v git >/dev/null 2>&1; then
  log "warning: git was not found; repository sync tasks will fail until git is installed"
fi

NODE_MAJOR="$(node -p "Number(process.versions.node.split('.')[0])")"
if [[ "${NODE_MAJOR}" -lt 22 ]]; then
  die "Node.js >=22 is required; current version is $(node -v)"
fi

if [[ ! -d "${ROOT_DIR}/node_modules/.pnpm" ]]; then
  log "node_modules not found; installing dependencies"
  pnpm install
fi

FRONTEND_PORT="${FRONTEND_PORT:-1390}"
BACKEND_PORT="${PORT:-8000}"
GIT_PLUS_DATA_DIR="${GIT_PLUS_DATA_DIR:-${ROOT_DIR}/tmpdata}"
ensure_port_available FRONTEND_PORT "${FRONTEND_PORT}" 127.0.0.1
ensure_port_available BACKEND_PORT "${BACKEND_PORT}" 0.0.0.0
FRONTEND_DEV_SERVER="http://127.0.0.1:${FRONTEND_PORT}"
BACKEND_DEV_SERVER="http://127.0.0.1:${BACKEND_PORT}"
export FRONTEND_DEV_SERVER
export PORT="${BACKEND_PORT}"
export GIT_PLUS_DATA_DIR

log "starting frontend on ${FRONTEND_DEV_SERVER}"
log "starting backend on http://localhost:${BACKEND_PORT}"
log "runtime data: ${GIT_PLUS_DATA_DIR}"

exec pnpm dlx concurrently@9.2.1 \
  --kill-others-on-fail \
  --names frontend,backend \
  "VITE_API_PROXY_TARGET=${BACKEND_DEV_SERVER} pnpm --filter ./frontend exec vite dev --host 127.0.0.1 --port ${FRONTEND_PORT} --strictPort" \
  "FRONTEND_DEV_SERVER=${FRONTEND_DEV_SERVER} GIT_PLUS_DATA_DIR=${GIT_PLUS_DATA_DIR} pnpm --filter ./backend dev -- --listen 0.0.0.0:${BACKEND_PORT}"
