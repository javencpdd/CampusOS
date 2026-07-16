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
drill_db="campusos_v11_migration_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..26}_*.sql; do
  ln -s "$migration" "$migration_dir/$(basename "$migration")"
done

(
  cd "$repo_root"
  PSQL_MODE=docker DB_NAME="$drill_db" MIGRATIONS_DIR="$migration_dir" ./scripts/migrate.sh up >/dev/null
)

run_file() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 <"$1"
}

scalar() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 -Atc "$1"
}

docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
INSERT INTO webhook_endpoints
  (id, name, url, secret, events, enabled, max_retries, timeout_ms, created_by)
VALUES
  (9911001, 'v11-migration-drill', 'https://example.com/webhook', '', ARRAY['thread.created'], TRUE, 2, 5000, 'migration-drill');
SQL

run_file "$repo_root/migrations/000027_v11_reliable_commands_and_outbox.up.sql" >/dev/null
test "$(scalar "SELECT count(*) FROM pg_tables WHERE schemaname='public' AND tablename IN ('platform_outbox','outbox_consumer_receipts','platform_outbox_attempts','platform_command_audits','platform_worker_leases','platform_operation_runs','platform_compatibility_usage','platform_retention_runs')")" = "8"
test "$(scalar "SELECT count(*) FROM permission_definitions WHERE code IN ('platform.reliability.read','platform.reliability.replay','platform.retention.preview')")" = "3"
test "$(scalar "SELECT max_concurrent || ':' || rate_limit_per_minute FROM webhook_endpoints WHERE id=9911001")" = "2:60"
test "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='authorization_audits' AND column_name IN ('command_id','trace_id','resource_version')")" = "3"

docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
INSERT INTO platform_outbox (id, event_type, idempotency_key) VALUES ('drill-event', 'drill.created', 'drill-event-key');
INSERT INTO platform_command_audits (id, command_id, command_code, event_id)
VALUES ('drill-audit', 'drill-command', 'drill.command', 'drill-event');
SQL

run_file "$repo_root/migrations/000027_v11_reliable_commands_and_outbox.down.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.platform_outbox') IS NULL")" = "t"
test "$(scalar "SELECT count(*) FROM permission_definitions WHERE code IN ('platform.reliability.read','platform.reliability.replay','platform.retention.preview')")" = "0"
test "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='webhook_endpoints' AND column_name IN ('max_concurrent','rate_limit_per_minute')")" = "0"
test "$(scalar "SELECT count(*) FROM webhook_endpoints WHERE id=9911001")" = "1"

run_file "$repo_root/migrations/000027_v11_reliable_commands_and_outbox.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.platform_outbox') IS NOT NULL")" = "t"
test "$(scalar "SELECT count(*) FROM permission_definitions WHERE code IN ('platform.reliability.read','platform.reliability.replay','platform.retention.preview')")" = "3"
test "$(scalar "SELECT max_concurrent || ':' || rate_limit_per_minute FROM webhook_endpoints WHERE id=9911001")" = "2:60"
test "$(scalar "SELECT count(*) FROM webhook_endpoints WHERE id=9911001")" = "1"

echo "v11 reliability migration up/down/up drill passed"
