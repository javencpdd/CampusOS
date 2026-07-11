#!/usr/bin/env bash
set -euo pipefail

ACTION="${1:-}"
ARCHIVE="${2:-}"
DESTINATION="${3:-}"
if [[ -z "$ACTION" || -z "$ARCHIVE" ]]; then
  echo "usage: $0 verify <backup.tar.gz> | extract <backup.tar.gz> <dir> | apply <backup.tar.gz> --confirm" >&2
  exit 2
fi

reject_unsafe_archive() {
  local archive="$1"
  if tar -tzf "$archive" | awk 'BEGIN{bad=0} /^\//{bad=1} /(^|\/)\.\.($|\/)/{bad=1} END{exit bad ? 0 : 1}'; then
    echo "unsafe path found in archive: $archive" >&2
    return 1
  fi
}

prepare() {
  local archive="$1" target="$2"
  [[ -f "$archive" ]] || { echo "backup not found: $archive" >&2; return 1; }
  reject_unsafe_archive "$archive"
  mkdir -p "$target"
  tar -C "$target" -xzf "$archive"
  for required in database.sql files.tar.gz metadata.txt SHA256SUMS; do
    [[ -f "$target/$required" ]] || { echo "backup is missing $required" >&2; return 1; }
  done
  (cd "$target" && sha256sum -c SHA256SUMS)
  reject_unsafe_archive "$target/files.tar.gz"
  grep -qx 'format=campusos-single-node-v1' "$target/metadata.txt"
}

case "$ACTION" in
  verify)
    work_dir="$(mktemp -d)"
    trap 'rm -rf "$work_dir"' EXIT
    prepare "$ARCHIVE" "$work_dir"
    echo "backup verified: $ARCHIVE"
    ;;
  extract)
    [[ -n "$DESTINATION" ]] || { echo "extract destination is required" >&2; exit 2; }
    prepare "$ARCHIVE" "$DESTINATION"
    mkdir -p "$DESTINATION/files"
    tar -C "$DESTINATION/files" -xzf "$DESTINATION/files.tar.gz"
    echo "backup extracted for inspection: $DESTINATION"
    ;;
  apply)
    [[ "$DESTINATION" == "--confirm" ]] || { echo "apply requires an explicit --confirm argument" >&2; exit 2; }
    if [[ -f .env ]]; then
      set -a
      # shellcheck disable=SC1091
      source .env
      set +a
    fi
    work_dir="$(mktemp -d)"
    trap 'rm -rf "$work_dir"' EXIT
    prepare "$ARCHIVE" "$work_dir"
    DB_HOST="${DB_HOST:-localhost}"
    DB_PORT="${DB_PORT:-${POSTGRES_PORT:-5432}}"
    DB_USER="${DB_USER:-campusos}"
    DB_NAME="${DB_NAME:-campusos}"
    DB_PASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-campusos_dev}}"
    POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
    tar -xzf "$work_dir/files.tar.gz"
    if command -v psql >/dev/null 2>&1; then
      PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -f "$work_dir/database.sql"
    elif command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER"; then
      docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 <"$work_dir/database.sql"
    else
      echo "psql is unavailable and PostgreSQL container '$POSTGRES_CONTAINER' is not running" >&2
      exit 127
    fi
    echo "restore applied; run migrations, schema checks, tests and service smoke before reopening traffic"
    ;;
  *)
    echo "unknown action: $ACTION" >&2
    exit 2
    ;;
esac
