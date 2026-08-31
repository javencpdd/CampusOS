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
DB_PORT="${POSTGRES_PORT:-${DB_PORT:-5432}}"
drill_db="campusos_v12_worker_convergence_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..36}_*.sql; do
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

run_file "$repo_root/migrations/000037_v12_outbox_worker_convergence.up.sql" >/dev/null
test "$(scalar "SELECT pg_get_constraintdef(oid) LIKE '%failed%' FROM pg_constraint WHERE conrelid='platform_outbox_attempts'::regclass AND conname='chk_platform_outbox_attempt_status'")" = "t"

(
  cd "$repo_root"
  CAMPUSOS_RELIABILITY_TEST_DB_HOST=127.0.0.1 \
  CAMPUSOS_RELIABILITY_TEST_DB_PORT="$DB_PORT" \
  CAMPUSOS_RELIABILITY_TEST_DB_USER="$DB_USER" \
  CAMPUSOS_RELIABILITY_TEST_DB_PASSWORD="$DB_PASSWORD" \
  CAMPUSOS_RELIABILITY_TEST_DB_NAME="$drill_db" \
  GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" \
    go test ./internal/platform/reliability -run '^TestPostgreSQLClaimNeverExceedsMaxAttempts$' -count=1
)

docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
INSERT INTO platform_outbox (id, event_type, schema_version, payload, headers, status, max_attempts)
VALUES ('migration-worker-event', 'test.migration.worker', 'v1', '{}', '{}', 'published', 8);
INSERT INTO platform_outbox_attempts (
  id, event_id, consumer_name, worker_id, lease_generation, attempt, status, error_message
) VALUES (
  'migration-finalize-attempt', 'migration-worker-event', 'system:outbox-finalize',
  'migration-worker', 1, 1, 'failed', 'outbox state transition failed'
);
SQL

run_file "$repo_root/migrations/000037_v12_outbox_worker_convergence.down.sql" >/dev/null
test "$(scalar "SELECT status FROM platform_outbox_attempts WHERE id='migration-finalize-attempt'")" = "retry"

run_file "$repo_root/migrations/000037_v12_outbox_worker_convergence.up.sql" >/dev/null
docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
UPDATE platform_outbox_attempts SET status='failed' WHERE id='migration-finalize-attempt';
SQL
test "$(scalar "SELECT status FROM platform_outbox_attempts WHERE id='migration-finalize-attempt'")" = "failed"

echo "v0.12 reliability worker convergence migration and PostgreSQL Claim drill passed"
