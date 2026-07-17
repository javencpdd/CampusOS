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
drill_db="campusos_v12_session_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..29}_*.sql; do
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
INSERT INTO users (id, username, nickname, email, status) VALUES
  (9930001, 'v12_session_user', 'Session User', 'v12-session@example.test', 'active');
INSERT INTO sessions (id, user_id, refresh_token, device_name, expires_at) VALUES
  (9930002, 9930001, 'legacy-raw-refresh-token-must-not-survive', 'legacy browser', NOW() + interval '30 days');
SQL

run_file "$repo_root/migrations/000030_v12_identity_sessions.up.sql" >/dev/null
test "$(scalar "SELECT refresh_token IS NULL AND refresh_token_digest IS NULL AND revoked_at IS NOT NULL AND revoke_reason='v12_legacy_token_invalidated' FROM sessions WHERE id=9930002")" = "t"
test "$(scalar "SELECT token_family_id FROM sessions WHERE id=9930002")" = "legacy-9930002"
test "$(scalar "SELECT to_regclass('public.uk_sessions_refresh_token_digest') IS NOT NULL")" = "t"

set +e
raw_output="$(run_sql 2>&1 <<'SQL'
INSERT INTO sessions (id, user_id, refresh_token, refresh_token_digest, token_family_id, expires_at)
VALUES (9930003, 9930001, 'must-fail', repeat('a', 64), 'family-a', NOW() + interval '30 days');
SQL
)"
raw_status=$?
set -e
if [[ $raw_status -eq 0 || "$raw_output" != *"ck_sessions_refresh_token_cleared"* ]]; then
  echo "session migration accepted a raw refresh token: $raw_output" >&2
  exit 1
fi

run_sql >/dev/null <<'SQL'
INSERT INTO sessions (id, user_id, refresh_token, refresh_token_digest, token_family_id, expires_at)
VALUES (9930004, 9930001, NULL, repeat('b', 64), 'family-b', NOW() + interval '30 days');
SQL
test "$(scalar "SELECT refresh_token IS NULL AND refresh_token_digest=repeat('b',64) FROM sessions WHERE id=9930004")" = "t"

run_file "$repo_root/migrations/000030_v12_identity_sessions.down.sql" >/dev/null
test "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='sessions' AND column_name='refresh_token_digest'")" = "0"
test "$(scalar "SELECT count(*) FROM sessions WHERE refresh_token IS NULL")" = "0"

run_file "$repo_root/migrations/000030_v12_identity_sessions.up.sql" >/dev/null
test "$(scalar "SELECT count(*) FROM sessions WHERE refresh_token IS NOT NULL")" = "0"
test "$(scalar "SELECT count(*) FROM sessions WHERE revoked_at IS NULL")" = "0"

echo "v12 identity session migration up/down/up drill passed"
