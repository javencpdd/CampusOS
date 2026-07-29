#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_DIR="${CAMPUSOS_LOG_DIR:-$ROOT_DIR/.campusos/logs}"
RUN_DIR="${CAMPUSOS_RUN_DIR:-$ROOT_DIR/.campusos/run}"
PID_FILE="$RUN_DIR/native-dev.pid"
ENV_FILE="${CAMPUSOS_DEV_ENV_FILE:-$ROOT_DIR/.env}"
SKIP_INFRA="${SKIP_INFRA:-false}"
SKIP_MIGRATE="${SKIP_MIGRATE:-false}"
STOP_EXISTING="${STOP_EXISTING:-false}"

cd "$ROOT_DIR"

EXPLICIT_SERVER_PORT="${SERVER_PORT:-}"
EXPLICIT_WEB_PORT="${WEB_PORT:-}"
EXPLICIT_ADMIN_PORT="${ADMIN_PORT:-}"
EXPLICIT_DOCS_PORT="${DOCS_PORT:-}"

if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$ENV_FILE"
  set +a
fi

[[ -n "$EXPLICIT_SERVER_PORT" ]] && SERVER_PORT="$EXPLICIT_SERVER_PORT"
[[ -n "$EXPLICIT_WEB_PORT" ]] && WEB_PORT="$EXPLICIT_WEB_PORT"
[[ -n "$EXPLICIT_ADMIN_PORT" ]] && ADMIN_PORT="$EXPLICIT_ADMIN_PORT"
[[ -n "$EXPLICIT_DOCS_PORT" ]] && DOCS_PORT="$EXPLICIT_DOCS_PORT"

SERVER_PORT="${SERVER_PORT:-8080}"
WEB_PORT="${WEB_PORT:-3000}"
ADMIN_PORT="${ADMIN_PORT:-3001}"
DOCS_PORT="${DOCS_PORT:-3002}"

DOCKER_DEV_ENV_FILE="${CAMPUSOS_DOCKER_DEV_ENV:-$ROOT_DIR/deploy/docker/.env.dev.local}"
INFRA_MODE="${CAMPUSOS_DEV_INFRA_MODE:-auto}"
if [[ "$INFRA_MODE" == "docker-dev" && ! -f "$DOCKER_DEV_ENV_FILE" && -z "${CAMPUSOS_DOCKER_DEV_ENV:-}" ]]; then
  DOCKER_DEV_ENV_FILE="$ROOT_DIR/deploy/docker/.env.dev.example"
fi
docker_dev_project_exists() {
  command -v docker >/dev/null 2>&1 || return 1
  docker info >/dev/null 2>&1 || return 1
  local args=(docker compose --env-file "$ROOT_DIR/deploy/docker/.env.dev.example")
  if [[ -n "${CAMPUSOS_DOCKER_PROJECT:-}" ]]; then
    args+=(--project-name "$CAMPUSOS_DOCKER_PROJECT")
  fi
  args+=(-f "$ROOT_DIR/compose.dev.yml" ps --all --quiet postgres)
  [[ -n "$("${args[@]}" 2>/dev/null)" ]]
}
if [[ "$INFRA_MODE" == "auto" ]]; then
  if [[ -f "$DOCKER_DEV_ENV_FILE" ]] && command -v docker >/dev/null 2>&1; then
    INFRA_MODE="docker-dev"
  elif docker_dev_project_exists; then
    DOCKER_DEV_ENV_FILE="$ROOT_DIR/deploy/docker/.env.dev.example"
    INFRA_MODE="docker-dev"
  else
    INFRA_MODE="legacy"
  fi
fi
case "$INFRA_MODE" in
  docker-dev | legacy) ;;
  *)
    echo "CAMPUSOS_DEV_INFRA_MODE must be auto, docker-dev, or legacy." >&2
    exit 2
    ;;
esac

mkdir -p "$LOG_DIR" "$RUN_DIR"

pids=()
run_claimed=false
cleanup_started=false

read_env_setting_from() {
  local file="$1"
  local key="$2"
  local fallback="${3:-}"
  local value=""
  if [[ -f "$file" ]]; then
    value="$(awk -F= -v wanted="$key" '
      $1 == wanted {
        value = substr($0, index($0, "=") + 1)
      }
      END { print value }
    ' "$file")"
  fi
  value="${value%$'\r'}"
  value="${value#\"}"
  value="${value%\"}"
  printf '%s\n' "${value:-$fallback}"
}

export_docker_setting() {
  local key="$1"
  local fallback="${2:-}"
  local value
  value="$(read_env_setting_from "$DOCKER_DEV_ENV_FILE" "$key" "$fallback")"
  printf -v "$key" '%s' "$value"
  export "$key"
}

pg_dsn_value() {
  local value="${1//\\/\\\\}"
  value="${value//\'/\\\'}"
  printf "'%s'" "$value"
}

configure_docker_dev_runtime() {
  if [[ ! -f "$DOCKER_DEV_ENV_FILE" ]]; then
    echo "Docker development configuration does not exist: $DOCKER_DEV_ENV_FILE" >&2
    echo "Run './scripts/docker-dev.sh setup' first or set CAMPUSOS_DEV_INFRA_MODE=legacy." >&2
    exit 1
  fi

  export CAMPUSOS_DOCKER_DEV_ENV="$DOCKER_DEV_ENV_FILE"

  local postgres_port postgres_user postgres_db postgres_password redis_port nats_port
  postgres_port="$(read_env_setting_from "$DOCKER_DEV_ENV_FILE" CAMPUSOS_DEV_POSTGRES_PORT 55432)"
  postgres_user="$(read_env_setting_from "$DOCKER_DEV_ENV_FILE" POSTGRES_USER campusos)"
  postgres_db="$(read_env_setting_from "$DOCKER_DEV_ENV_FILE" POSTGRES_DB campusos)"
  postgres_password="$(read_env_setting_from "$DOCKER_DEV_ENV_FILE" POSTGRES_PASSWORD campusos_dev)"
  redis_port="$(read_env_setting_from "$DOCKER_DEV_ENV_FILE" CAMPUSOS_DEV_REDIS_PORT 56379)"
  nats_port="$(read_env_setting_from "$DOCKER_DEV_ENV_FILE" CAMPUSOS_DEV_NATS_PORT 54222)"

  export DB_HOST=127.0.0.1
  export DB_PORT="$postgres_port"
  export DB_USER="$postgres_user"
  export DB_NAME="$postgres_db"
  export DB_PASSWORD="$postgres_password"
  export POSTGRES_PORT="$postgres_port"
  export DATABASE_DSN="host=127.0.0.1 port=$postgres_port user=$(pg_dsn_value "$postgres_user") password=$(pg_dsn_value "$postgres_password") dbname=$(pg_dsn_value "$postgres_db") sslmode=disable"
  export REDIS_ENABLED=true
  export REDIS_ADDR="127.0.0.1:$redis_port"
  export REDIS_PASSWORD
  REDIS_PASSWORD="$(read_env_setting_from "$DOCKER_DEV_ENV_FILE" REDIS_PASSWORD)"
  export NATS_URL="nats://127.0.0.1:$nats_port"

  export_docker_setting CAMPUSOS_ENV development
  export_docker_setting CAMPUSOS_INSTANCE_MODE single
  export_docker_setting JWT_SECRET campusos-dev-secret-key-change-in-production
  export_docker_setting JWT_ACCESS_TTL 15m
  export_docker_setting JWT_REFRESH_TTL 720h
  export_docker_setting JWT_ISSUER campusos
  export_docker_setting AUTH_PASSWORD_HASH_ENABLED true
  export_docker_setting AUTH_BOOTSTRAP_ADMIN_SECRET
  export_docker_setting AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN true
  export_docker_setting AUTH_CHALLENGE_ACTIVE_KEY_ID development-v1
  export_docker_setting AUTH_CHALLENGE_HMAC_KEYS development-v1:campusos-development-challenge-key-change-before-production
  export_docker_setting AUTH_CHALLENGE_IP_HASH_SECRET campusos-development-ip-hash-key-change-before-production
  export_docker_setting AUTH_SESSION_IP_HASH_SECRET campusos-development-session-ip-hash-key-change-before-production
  export_docker_setting AUTH_REFRESH_BODY_COMPAT false
  export_docker_setting AUTH_MFA_ACTIVE_KEY_ID development-v1
  export_docker_setting AUTH_MFA_ENCRYPTION_KEYS development-v1:campusos-development-mfa-encryption-key-change-before-production
  export_docker_setting AUTH_MFA_ISSUER CampusOS
  export_docker_setting EMAIL_PROVIDER fake
  export_docker_setting EMAIL_SMTP_HOST
  export_docker_setting EMAIL_SMTP_PORT 587
  export_docker_setting EMAIL_SMTP_USERNAME
  export_docker_setting EMAIL_SMTP_PASSWORD
  export_docker_setting EMAIL_SMTP_FROM
  export_docker_setting EMAIL_SMTP_TIMEOUT 10s
  export_docker_setting EMAIL_SMTP_STARTTLS true
}

if [[ "$INFRA_MODE" == "docker-dev" ]]; then
  configure_docker_dev_runtime
fi

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

port_is_listening() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1
    return $?
  fi
  if command -v ss >/dev/null 2>&1; then
    ss -ltn 2>/dev/null | awk '{print $4}' | grep -Eq "[:.]${port}$"
    return $?
  fi
  return 1
}

ensure_port_free() {
  local port="$1"
  local label="$2"
  if ! port_is_listening "$port"; then
    return
  fi
  mapfile -t occupied < <(port_pids "$port")
  if [[ "$STOP_EXISTING" == "true" ]]; then
    if [[ "${#occupied[@]}" -eq 0 ]]; then
      echo "port $port is still occupied for $label, but no host process can be stopped." >&2
      echo "If Docker owns it, run './scripts/docker-dev.sh stop-apps' first." >&2
      exit 1
    fi
    echo "==> stopping existing $label process(es) on port $port: ${occupied[*]}"
    kill "${occupied[@]}" 2>/dev/null || true
    for _ in {1..10}; do
      if ! port_is_listening "$port"; then
        return
      fi
      sleep 0.2
    done
    echo "port $port remains in use after stopping existing $label process(es)." >&2
    exit 1
  fi
  echo "port $port is already in use for $label${occupied[*]:+ by PID(s): ${occupied[*]}}" >&2
  echo "set STOP_EXISTING=true to stop existing listeners automatically." >&2
  exit 1
}

cleanup() {
  if [[ "$cleanup_started" == "true" ]]; then
    return
  fi
  cleanup_started=true
  if [[ "${#pids[@]}" -gt 0 ]]; then
    echo
    echo "==> stopping CampusOS dev services"
    kill "${pids[@]}" 2>/dev/null || true
    wait "${pids[@]}" 2>/dev/null || true
    pids=()
  fi
  if [[ "$run_claimed" == "true" && -f "$PID_FILE" ]]; then
    local recorded_pid=""
    IFS= read -r recorded_pid <"$PID_FILE" || true
    if [[ "$recorded_pid" == "$$" ]]; then
      rm -f "$PID_FILE"
    fi
  fi
  run_claimed=false
}

handle_shutdown() {
  cleanup
  exit 0
}
trap handle_shutdown INT TERM
trap cleanup EXIT

claim_native_run() {
  if [[ -f "$PID_FILE" ]]; then
    local existing_pid=""
    IFS= read -r existing_pid <"$PID_FILE" || true
    if [[ "$existing_pid" =~ ^[0-9]+$ ]] && kill -0 "$existing_pid" 2>/dev/null; then
      echo "CampusOS native development services are already managed by PID $existing_pid." >&2
      exit 1
    fi
    rm -f "$PID_FILE"
  fi
  printf '%s\n' "$$" >"$PID_FILE"
  run_claimed=true
}

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

if [[ "$STOP_EXISTING" == "true" ]] \
  && command -v docker >/dev/null 2>&1 \
  && docker info >/dev/null 2>&1; then
  echo "==> checking Docker development application services"
  ./scripts/docker-dev.sh stop-apps
  ./scripts/docker-dev.sh stop-native
fi

ensure_port_free "$SERVER_PORT" "api"
ensure_port_free "$WEB_PORT" "web"
ensure_port_free "$ADMIN_PORT" "admin"
ensure_port_free "$DOCS_PORT" "docs"
claim_native_run

if [[ "$SKIP_INFRA" != "true" ]]; then
  if [[ "$INFRA_MODE" == "docker-dev" ]]; then
    echo "==> starting shared Docker development infrastructure"
    ./scripts/docker-dev.sh infra-up
  else
    echo "==> starting legacy Docker infrastructure"
    ./scripts/docker-up.sh
  fi
fi

if [[ "$SKIP_MIGRATE" != "true" ]]; then
  echo "==> applying database migrations"
  if [[ "$INFRA_MODE" == "docker-dev" ]]; then
    ./scripts/docker-dev.sh migrate up
  else
    ./scripts/migrate.sh up
  fi
fi

start_service api "$ROOT_DIR" go run ./cmd/server/main.go
start_service web "$ROOT_DIR/web" env \
  CAMPUSOS_API_PROXY_TARGET="http://localhost:$SERVER_PORT" \
  pnpm dev --host 0.0.0.0 --port "$WEB_PORT"
start_service admin "$ROOT_DIR/admin" env \
  CAMPUSOS_API_PROXY_TARGET="http://localhost:$SERVER_PORT" \
  VITE_WEB_URL="http://localhost:$WEB_PORT" \
  VITE_DOCS_URL="http://localhost:$DOCS_PORT" \
  pnpm dev --host 0.0.0.0 --port "$ADMIN_PORT"
start_service docs "$ROOT_DIR/docs-site" pnpm exec vitepress dev --host 0.0.0.0 --port "$DOCS_PORT"

wait_for_url "api" "http://localhost:$SERVER_PORT/api/v1/health" 40
wait_for_url "web" "http://localhost:$WEB_PORT/" 30
wait_for_url "admin" "http://localhost:$ADMIN_PORT/" 30
wait_for_url "docs" "http://localhost:$DOCS_PORT/" 30

cat <<EOF

CampusOS dev services are running:
  web:   http://localhost:$WEB_PORT
  admin: http://localhost:$ADMIN_PORT
  docs:  http://localhost:$DOCS_PORT
  api:   http://localhost:$SERVER_PORT/api/v1

Data mode:
  $INFRA_MODE

Logs:
  api:   $LOG_DIR/api.log
  web:   $LOG_DIR/web.log
  admin: $LOG_DIR/admin.log
  docs:  $LOG_DIR/docs.log

Press Ctrl+C to stop all services.
EOF

wait -n "${pids[@]}"
