#!/usr/bin/env bash
set -euo pipefail

OUTPUT_DIR="${1:-backups}"
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
PLUGINS_DIR="${PLUGINS_DIR:-data/plugins}"
PLUGIN_DATA_DIR="${PLUGIN_DATA_DIR:-data/plugin_data}"
MODULES_DIR="${MODULES_DIR:-modules}"
MODULE_DATA_DIR="${MODULE_DATA_DIR:-data/module_data}"
RESOURCE_DIR="${RESOURCE_DIR:-data/resources}"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$OUTPUT_DIR"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

dump_database() {
  if command -v pg_dump >/dev/null 2>&1; then
    PGPASSWORD="$DB_PASSWORD" pg_dump -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" --clean --if-exists --no-owner --no-privileges >"$work_dir/database.sql"
  elif command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER"; then
    docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" pg_dump -U "$DB_USER" -d "$DB_NAME" --clean --if-exists --no-owner --no-privileges >"$work_dir/database.sql"
  else
    echo "pg_dump is unavailable and PostgreSQL container '$POSTGRES_CONTAINER' is not running" >&2
    return 127
  fi
}

dump_database

paths=(migrations "$MODULES_DIR" "$PLUGINS_DIR" "$PLUGIN_DATA_DIR" "$MODULE_DATA_DIR" "$RESOURCE_DIR" data/personal-space data/images data/config data/skills)
[[ -f .env ]] && paths+=(.env)
existing=()
for item in "${paths[@]}"; do
  [[ -e "$item" ]] && existing+=("$item")
done
tar -czf "$work_dir/files.tar.gz" "${existing[@]}"

cat >"$work_dir/metadata.txt" <<EOF
format=campusos-single-node-v2
created_at=$timestamp
database=$DB_NAME
plugins_dir=$PLUGINS_DIR
plugin_data_dir=$PLUGIN_DATA_DIR
modules_dir=$MODULES_DIR
module_data_dir=$MODULE_DATA_DIR
resource_dir=$RESOURCE_DIR
EOF

(
  cd "$work_dir"
  sha256sum database.sql files.tar.gz metadata.txt >SHA256SUMS
)

archive="$OUTPUT_DIR/campusos-backup-$timestamp.tar.gz"
tar -C "$work_dir" -czf "$archive" database.sql files.tar.gz metadata.txt SHA256SUMS
echo "$archive"
