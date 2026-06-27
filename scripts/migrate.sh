#!/usr/bin/env bash
set -euo pipefail

ACTION="${1:-up}"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

MIGRATIONS_DIR="${MIGRATIONS_DIR:-migrations}"
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-${POSTGRES_PORT:-5432}}"
DB_USER="${DB_USER:-campusos}"
DB_NAME="${DB_NAME:-campusos}"
DB_PASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-campusos_dev}}"

run_psql() {
  PGPASSWORD="$DB_PASSWORD" psql \
    -h "$DB_HOST" \
    -p "$DB_PORT" \
    -U "$DB_USER" \
    -d "$DB_NAME" \
    -v ON_ERROR_STOP=1 \
    "$@"
}

ensure_schema_migrations() {
  run_psql -q -c "
    CREATE TABLE IF NOT EXISTS schema_migrations (
      version    VARCHAR(32) PRIMARY KEY,
      name       VARCHAR(255) NOT NULL,
      applied_at TIMESTAMP NOT NULL DEFAULT NOW()
    );
  " >/dev/null
}

version_for() {
  basename "$1" | sed -E 's/^([0-9]+)_.*/\1/'
}

name_for() {
  basename "$1" | sed -E 's/\.(up|down)\.sql$//'
}

is_applied() {
  local version="$1"
  local result
  result="$(run_psql -tAc "SELECT 1 FROM schema_migrations WHERE version = '$version'")"
  [[ "$result" == "1" ]]
}

mark_applied() {
  local version="$1"
  local name="$2"
  run_psql -q -c "
    INSERT INTO schema_migrations (version, name, applied_at)
    VALUES ('$version', '$name', NOW())
    ON CONFLICT (version) DO UPDATE
      SET name = EXCLUDED.name,
          applied_at = EXCLUDED.applied_at;
  " >/dev/null
}

unmark_applied() {
  local version="$1"
  run_psql -q -c "DELETE FROM schema_migrations WHERE version = '$version';" >/dev/null
}

run_up() {
  ensure_schema_migrations
  shopt -s nullglob
  local files=("$MIGRATIONS_DIR"/*.up.sql)
  shopt -u nullglob

  if [[ ${#files[@]} -eq 0 ]]; then
    echo "No up migrations found in $MIGRATIONS_DIR"
    return
  fi

  for f in "${files[@]}"; do
    local version name
    version="$(version_for "$f")"
    name="$(name_for "$f")"
    if is_applied "$version"; then
      echo "==> skip $name"
      continue
    fi
    echo "==> apply $name"
    run_psql -f "$f"
    mark_applied "$version" "$name"
  done
}

run_down() {
  ensure_schema_migrations
  mapfile -t files < <(find "$MIGRATIONS_DIR" -maxdepth 1 -name '*.down.sql' -type f | sort -r)

  if [[ ${#files[@]} -eq 0 ]]; then
    echo "No down migrations found in $MIGRATIONS_DIR"
    return
  fi

  for f in "${files[@]}"; do
    local version name
    version="$(version_for "$f")"
    name="$(name_for "$f")"
    echo "==> rollback $name"
    run_psql -f "$f"
    unmark_applied "$version"
  done
}

run_status() {
  ensure_schema_migrations
  echo "==> schema_migrations"
  run_psql -c "SELECT version, name, applied_at FROM schema_migrations ORDER BY version;"
}

case "$ACTION" in
  up)
    run_up
    ;;
  down)
    run_down
    ;;
  reset)
    run_down
    run_up
    ;;
  status)
    run_status
    ;;
  *)
    echo "Usage: $0 {up|down|reset|status}" >&2
    exit 2
    ;;
esac
