#!/usr/bin/env bash
set -euo pipefail

ACTION="${1:-all}"
if [[ -f .env ]]; then
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

run_file() {
  local file="$1"
  if command -v psql >/dev/null 2>&1; then
    PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -f "$file"
  elif command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER"; then
    docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 <"$file"
  else
    echo "psql is unavailable and PostgreSQL container '$POSTGRES_CONTAINER' is not running" >&2
    return 127
  fi
}

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
