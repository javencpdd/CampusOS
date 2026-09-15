#!/usr/bin/env bash
set -euo pipefail

ACTION="${1:-all}"
if [[ "${CAMPUSOS_SKIP_DOTENV:-false}" != "true" && -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-${POSTGRES_PORT:-5432}}"
DB_USER="${DB_USER:-campusos}"
DB_NAME="${DB_NAME:-campusos}"
DB_PASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-campusos_dev}}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
PSQL_MODE="${PSQL_MODE:-auto}"

docker_container_available() {
  command -v docker >/dev/null 2>&1 || return 1
  docker inspect --format '{{.State.Running}}' "$POSTGRES_CONTAINER" 2>/dev/null | grep -qx true
}

select_psql_mode() {
  case "$PSQL_MODE" in
    host)
      command -v psql >/dev/null 2>&1 || {
        echo "psql not found on host. Install postgresql-client or use PSQL_MODE=docker." >&2
        exit 127
      }
      ;;
    docker)
      docker_container_available || {
        echo "docker postgres container '$POSTGRES_CONTAINER' is not running." >&2
        exit 127
      }
      ;;
    auto | "")
      if command -v psql >/dev/null 2>&1; then
        PSQL_MODE=host
      elif docker_container_available; then
        PSQL_MODE=docker
      else
        echo "psql is unavailable and PostgreSQL container '$POSTGRES_CONTAINER' is not running." >&2
        exit 127
      fi
      ;;
    *)
      echo "invalid PSQL_MODE '$PSQL_MODE' (expected auto, host, or docker)." >&2
      exit 2
      ;;
  esac
}

run_file() {
  local file="$1"
  if [[ "$PSQL_MODE" == host ]]; then
    PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -f "$file"
  else
    docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 <"$file"
  fi
}

select_psql_mode

case "$ACTION" in
  audit) run_file scripts/database-audit.sql ;;
  schema) run_file scripts/schema-contract.sql ;;
  hygiene) run_file scripts/migration-hygiene.sql ;;
  all)
    run_file scripts/database-audit.sql
    run_file scripts/schema-contract.sql
    run_file scripts/migration-hygiene.sql
    ;;
  *) echo "usage: $0 {audit|schema|hygiene|all}" >&2; exit 2 ;;
esac
