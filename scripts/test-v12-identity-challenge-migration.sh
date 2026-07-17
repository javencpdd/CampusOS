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
drill_db="campusos_v12_challenge_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..28}_*.sql; do
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

run_file "$repo_root/migrations/000029_v12_identity_challenges.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.identity_email_challenges') IS NOT NULL")" = "t"
test "$(scalar "SELECT to_regclass('public.identity_challenge_rate_limits') IS NOT NULL")" = "t"
test "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='identity_email_challenges' AND column_name IN ('public_id','key_id','nonce','ticket_digest','requested_ip_hash')")" = "5"
test "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='identity_email_challenges' AND column_name LIKE '%code%'")" = "0"
test "$(scalar "SELECT to_regclass('public.uk_identity_email_challenge_public_id') IS NOT NULL")" = "t"

set +e
invalid_output="$(docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 2>&1 <<'SQL'
INSERT INTO identity_email_challenges (
  id, public_id, purpose, email_normalized, key_id, nonce, expires_at, requested_ip_hash
) VALUES (9929001, 'invalid-purpose', 'wrong', 'learner@example.test', 'v1', 'nonce', NOW() + interval '10 minutes', 'digest');
SQL
)"
invalid_status=$?
set -e
if [[ $invalid_status -eq 0 || "$invalid_output" != *"chk_identity_email_challenge_purpose"* ]]; then
  echo "challenge migration did not enforce purpose constraint: $invalid_output" >&2
  exit 1
fi

run_file "$repo_root/migrations/000029_v12_identity_challenges.down.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.identity_email_challenges') IS NULL")" = "t"
test "$(scalar "SELECT to_regclass('public.identity_challenge_rate_limits') IS NULL")" = "t"

run_file "$repo_root/migrations/000029_v12_identity_challenges.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.uk_identity_email_challenge_public_id') IS NOT NULL")" = "t"

echo "v12 identity challenge migration up/down/up drill passed"
