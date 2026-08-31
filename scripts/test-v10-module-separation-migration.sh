#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -f "$repo_root/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$repo_root/.env"
  set +a
fi

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
DB_USER="${DB_USER:-campusos}"
DB_PASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-campusos_dev}}"
drill_db="campusos_v10_migration_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..25}_*.sql; do
  ln -s "$migration" "$migration_dir/$(basename "$migration")"
done

(
  cd "$repo_root"
  PSQL_MODE=docker DB_NAME="$drill_db" MIGRATIONS_DIR="$migration_dir" ./scripts/migrate.sh up >/dev/null
)

run_sql() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 "$@"
}

run_file() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 <"$1"
}

scalar() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 -Atc "$1"
}

run_sql >/dev/null <<'SQL'
INSERT INTO plugins (id, name, display_name, version, runtime, status, config) VALUES
  (990001, 'category-moderation', 'Moderation', '0.9.0', 'builtin', 'running', '{"allow_pin":false}'::jsonb),
  (990002, 'personal-space', 'Personal Space', '0.9.0', 'builtin', 'running', '{"quota_mb":10}'::jsonb),
  (990003, 'controlled-richtext-article', 'RichText', '0.9.0', 'builtin', 'running', '{}'::jsonb),
  (990004, 'personal-schedule', 'Schedule', '0.9.0', 'builtin', 'stopped', '{}'::jsonb),
  (990005, 'homepage-customizer', 'Homepage', '0.9.0', 'builtin', 'running', '{"hero_title":"Legacy Campus"}'::jsonb),
  (990006, 'web-theme', 'Web Theme', '0.9.0', 'builtin', 'running', '{"default_style_pack":"legacy-theme"}'::jsonb);
INSERT INTO builtin_feature_states (feature_id, desired_enabled, effective_enabled, pending_restart, config)
VALUES ('web-theme', TRUE, TRUE, FALSE, '{"default_style_pack":"legacy-theme"}'::jsonb)
ON CONFLICT (feature_id) DO UPDATE SET config = EXCLUDED.config;
SQL

run_file "$repo_root/migrations/000026_v10_module_plugin_separation.up.sql" >/dev/null
test "$(scalar "SELECT count(*) FROM plugins WHERE runtime='builtin' AND deleted_at IS NULL")" = "0"
test "$(scalar "SELECT count(*) FROM permission_definitions WHERE code LIKE 'platform.feature.%'")" = "3"
test "$(scalar "SELECT config->'homepage'->>'hero_title' FROM builtin_feature_states WHERE feature_id='appearance'")" = "Legacy Campus"
test "$(scalar "SELECT desired_enabled FROM builtin_feature_states WHERE feature_id='personal-schedule'")" = "f"

run_file "$repo_root/migrations/000026_v10_module_plugin_separation.down.sql" >/dev/null
test "$(scalar "SELECT count(*) FROM plugins WHERE runtime='builtin' AND deleted_at IS NULL")" = "6"
test "$(scalar "SELECT count(*) FROM permission_definitions WHERE code LIKE 'platform.feature.%'")" = "0"
test "$(scalar "SELECT config->>'default_style_pack' FROM plugins WHERE name='web-theme' AND deleted_at IS NULL")" = "legacy-theme"

run_file "$repo_root/migrations/000026_v10_module_plugin_separation.up.sql" >/dev/null
test "$(scalar "SELECT count(*) FROM plugins WHERE runtime='builtin' AND deleted_at IS NULL")" = "0"
test "$(scalar "SELECT count(*) FROM permission_definitions WHERE code LIKE 'platform.feature.%'")" = "3"
test "$(scalar "SELECT count(*) FROM builtin_feature_states WHERE feature_id IN ('personal-space','controlled-richtext-article','personal-schedule','appearance')")" = "4"

echo "v0.10 module/plugin separation migration up/down/up drill passed"
