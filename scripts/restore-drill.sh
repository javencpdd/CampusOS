#!/usr/bin/env bash
set -euo pipefail

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
if ! docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER" && \
  docker ps --format '{{.Names}}' | grep -qx 'campusos-dev-postgres-1'; then
  POSTGRES_CONTAINER='campusos-dev-postgres-1'
fi
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

archive="$(POSTGRES_CONTAINER="$POSTGRES_CONTAINER" DB_USER="$DB_USER" DB_PASSWORD="$DB_PASSWORD" ./scripts/backup.sh "$work_dir")"
./scripts/restore.sh verify "$archive"
./scripts/restore.sh extract "$archive" "$work_dir/extracted"

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 <"$work_dir/extracted/database.sql" >/dev/null

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 -Atc "
SELECT 'users=' || count(*) FROM users;
SELECT 'threads=' || count(*) FROM threads;
SELECT 'plugins=' || count(*) FROM plugins;
SELECT 'migrations=' || count(*) FROM schema_migrations;
SELECT 'v14_storage_tables=' || count(*) FROM pg_tables WHERE schemaname='public' AND tablename IN ('storage_objects','user_storage_accounts','user_storage_reservations','user_schedule_terms','user_schedule_preferences','personal_documents','personal_document_versions','personal_document_previews');
SELECT 'v14_schedule_binding_columns=' || count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='user_schedule_terms' AND column_name IN ('current_object_id','first_week_start','version');
" | tee "$work_dir/restored-counts.txt"

test -f "$work_dir/extracted/files/modules/features/personal-schedule/module.yaml"
test -f "$work_dir/extracted/files/data/plugins/hello-wasm/plugin.yaml"
test -d "$work_dir/extracted/files/data/plugin_data"
test -f "$work_dir/extracted/files/data/module_data/personal-space/styles/clean-blog.space-style.json"
test -f "$work_dir/extracted/files/data/resources/themes/campus-canvas/resource.json"
test -d "$work_dir/extracted/files/data/personal-space"
grep -qx 'v14_storage_tables=8' "$work_dir/restored-counts.txt"
grep -qx 'v14_schedule_binding_columns=3' "$work_dir/restored-counts.txt"
echo "single-node restore drill passed: database, modules, plugins, resources, user assets, v14 object metadata and schedule bindings restored in isolation"
