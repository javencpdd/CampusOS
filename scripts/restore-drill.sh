#!/usr/bin/env bash
set -euo pipefail

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
DB_USER="${DB_USER:-campusos}"
DB_PASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-campusos_dev}}"
drill_db="campusos_restore_drill_$(date -u +%Y%m%d%H%M%S)"
work_dir="$(mktemp -d)"
archive=""

cleanup() {
  if [[ -n "$drill_db" ]] && command -v docker >/dev/null 2>&1; then
    docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists "$drill_db" >/dev/null 2>&1 || true
  fi
  rm -rf "$work_dir"
  [[ -n "$archive" ]] && rm -f "$archive"
}
trap cleanup EXIT

archive="$(./scripts/backup.sh "$work_dir")"
./scripts/restore.sh verify "$archive"
./scripts/restore.sh extract "$archive" "$work_dir/extracted"

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 <"$work_dir/extracted/database.sql" >/dev/null

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 -Atc "
SELECT 'users=' || count(*) FROM users;
SELECT 'threads=' || count(*) FROM threads;
SELECT 'plugins=' || count(*) FROM plugins;
SELECT 'migrations=' || count(*) FROM schema_migrations;
" | tee "$work_dir/restored-counts.txt"

test -f "$work_dir/extracted/files/modules/features/personal-schedule/module.yaml"
test -f "$work_dir/extracted/files/data/plugins/hello-wasm/plugin.yaml"
test -d "$work_dir/extracted/files/data/plugin_data"
test -f "$work_dir/extracted/files/data/module_data/personal-space/styles/clean-blog.space-style.json"
test -f "$work_dir/extracted/files/data/resources/themes/campus-canvas/resource.json"
test -d "$work_dir/extracted/files/data/personal-space"
echo "single-node restore drill passed: database, modules, plugins, resources, and user assets restored in isolation"
