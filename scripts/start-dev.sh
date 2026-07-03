#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_DIR="${CAMPUSOS_LOG_DIR:-$ROOT_DIR/.campusos/logs}"
SKIP_INFRA="${SKIP_INFRA:-false}"
SKIP_MIGRATE="${SKIP_MIGRATE:-false}"
STOP_EXISTING="${STOP_EXISTING:-false}"

cd "$ROOT_DIR"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

SERVER_PORT="${SERVER_PORT:-8080}"
WEB_PORT="${WEB_PORT:-3000}"
ADMIN_PORT="${ADMIN_PORT:-3001}"

mkdir -p "$LOG_DIR"

pids=()

require_cmd() {
  local name="$1"
  if ! command -v "$name" >/dev/null 2>&1; then
    echo "missing required command: $name" >&2
    exit 127
  fi
}

port_pids() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true
    return
  fi
  if command -v ss >/dev/null 2>&1; then
    ss -ltnp 2>/dev/null |
      awk -v port=":$port" '$4 ~ port"$" { print $0 }' |
      sed -nE 's/.*pid=([0-9]+).*/\1/p' |
      sort -u
  fi
}

ensure_port_free() {
  local port="$1"
  local label="$2"
  mapfile -t occupied < <(port_pids "$port")
  if [[ "${#occupied[@]}" -eq 0 ]]; then
    return
  fi
  if [[ "$STOP_EXISTING" == "true" ]]; then
    echo "==> stopping existing $label process(es) on port $port: ${occupied[*]}"
    kill "${occupied[@]}" 2>/dev/null || true
    sleep 1
    return
  fi
  echo "port $port is already in use for $label: ${occupied[*]}" >&2
  echo "set STOP_EXISTING=true to stop existing listeners automatically." >&2
  exit 1
}

cleanup() {
  if [[ "${#pids[@]}" -gt 0 ]]; then
    echo
    echo "==> stopping CampusOS dev services"
    kill "${pids[@]}" 2>/dev/null || true
    wait "${pids[@]}" 2>/dev/null || true
  fi
}
trap cleanup INT TERM EXIT

start_service() {
  local name="$1"
  local workdir="$2"
  shift 2
  local log_file="$LOG_DIR/$name.log"
  echo "==> starting $name; log: $log_file"
  (
    cd "$workdir"
    "$@"
  ) >"$log_file" 2>&1 &
  pids+=("$!")
}

wait_for_url() {
  local label="$1"
  local url="$2"
  local max_attempts="${3:-30}"
  for ((i = 1; i <= max_attempts; i++)); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      echo "==> $label ready: $url"
      return
    fi
    sleep 1
  done
  echo "warning: $label did not become ready in ${max_attempts}s: $url" >&2
}

require_cmd go
require_cmd pnpm
require_cmd curl

ensure_port_free "$SERVER_PORT" "api"
ensure_port_free "$WEB_PORT" "web"
ensure_port_free "$ADMIN_PORT" "admin"

if [[ "$SKIP_INFRA" != "true" ]]; then
  echo "==> starting Docker infrastructure"
  ./scripts/docker-up.sh
fi

if [[ "$SKIP_MIGRATE" != "true" ]]; then
  echo "==> applying database migrations"
  ./scripts/migrate.sh up
fi

start_service api "$ROOT_DIR" go run ./cmd/server/main.go
start_service web "$ROOT_DIR/web" pnpm dev --host 0.0.0.0 --port "$WEB_PORT"
start_service admin "$ROOT_DIR/admin" pnpm dev --host 0.0.0.0 --port "$ADMIN_PORT"

wait_for_url "api" "http://localhost:$SERVER_PORT/api/v1/health" 40
wait_for_url "web" "http://localhost:$WEB_PORT/" 30
wait_for_url "admin" "http://localhost:$ADMIN_PORT/" 30

cat <<EOF

CampusOS dev services are running:
  web:   http://localhost:$WEB_PORT
  admin: http://localhost:$ADMIN_PORT
  api:   http://localhost:$SERVER_PORT/api/v1

Logs:
  api:   $LOG_DIR/api.log
  web:   $LOG_DIR/web.log
  admin: $LOG_DIR/admin.log

Press Ctrl+C to stop all services.
EOF

wait -n "${pids[@]}"
