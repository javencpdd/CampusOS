#!/usr/bin/env bash
set -euo pipefail

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

POSTGRES_PORT="${POSTGRES_PORT:-5432}"
REDIS_PORT="${REDIS_PORT:-6379}"
NATS_CLIENT_PORT="${NATS_CLIENT_PORT:-4222}"
PGADMIN_PORT="${PGADMIN_PORT:-5050}"

is_running() {
  local service="$1"
  docker compose ps --services --filter status=running | grep -qx "$service"
}

port_in_use() {
  local port="$1"
  if command -v ss >/dev/null 2>&1; then
    ss -ltn | awk '{print $4}' | grep -Eq "[:.]${port}$"
    return $?
  fi
  if command -v lsof >/dev/null 2>&1; then
    lsof -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1
    return $?
  fi
  return 1
}

queue_service() {
  local service="$1"
  local port="$2"
  local env_name="$3"

  if is_running "$service"; then
    echo "==> $service already running"
    return
  fi

  if port_in_use "$port"; then
    echo "==> skip $service: localhost:$port is already in use. Stop the local service or set $env_name to another port."
    return
  fi

  SERVICES+=("$service")
}

SERVICES=()
queue_service postgres "$POSTGRES_PORT" POSTGRES_PORT
queue_service redis "$REDIS_PORT" REDIS_PORT
queue_service nats "$NATS_CLIENT_PORT" NATS_CLIENT_PORT
queue_service pgadmin "$PGADMIN_PORT" PGADMIN_PORT

if [[ ${#SERVICES[@]} -eq 0 ]]; then
  echo "==> no Docker infrastructure service needs to be started"
  exit 0
fi

docker compose up -d "${SERVICES[@]}"
