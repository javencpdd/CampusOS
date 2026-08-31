#!/usr/bin/env bash
# 用法：在 Git Bash、WSL2 或 Linux 中执行
#   bash ./scripts/test-v14-historical-fixture-migration.sh
# 本脚本只创建/删除 v13 历史 fixture 的隔离数据库和临时个人空间目录；不会读取、写入或采用真实用户数据。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -f "$repo_root/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$repo_root/.env"
  set +a
fi

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
if ! docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER" && \
  docker ps --format '{{.Names}}' | grep -qx 'campusos-dev-postgres-1'; then
  POSTGRES_CONTAINER='campusos-dev-postgres-1'
fi
DB_USER="${DB_USER:-campusos}"
DB_PASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-campusos_dev}}"
DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-${CAMPUSOS_DEV_POSTGRES_PORT:-55432}}"
drill_db="campusos_v14_historical_fixture_$(date -u +%Y%m%d%H%M%S)_$$"
fixture_root="$(mktemp -d)"
baseline_migrations="$(mktemp -d)"
log_file="$(mktemp)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$fixture_root" "$baseline_migrations"
  rm -f "$log_file"
}
trap cleanup EXIT

docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER" || {
  echo "PostgreSQL container '$POSTGRES_CONTAINER' is not running." >&2
  exit 1
}

scalar() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 -Atc "$1"
}

for migration in "$repo_root"/migrations/*.up.sql; do
  version="$(basename "$migration" | sed -E 's/^0*([0-9]+)_.*/\1/')"
  if (( version <= 43 )); then
    cp "$migration" "$baseline_migrations/"
  fi
done

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
if ! (
  cd "$repo_root"
  PSQL_MODE=docker POSTGRES_CONTAINER="$POSTGRES_CONTAINER" DB_NAME="$drill_db" MIGRATIONS_DIR="$baseline_migrations" ./scripts/migrate.sh up
) >"$log_file" 2>&1; then
  tail -n 80 "$log_file" >&2
  exit 1
fi
[[ "$(scalar "SELECT count(*) FROM schema_migrations")" == "43" ]] || { echo "v0.13 fixture did not stop at migration 43" >&2; exit 1; }

# This is deliberately a pre-v0.14 database state. The account already exists
# in the historical schema; no v0.14 table or record is inserted before the
# append-only migrations are applied.
fixture_user="$(scalar "SELECT id FROM users ORDER BY id LIMIT 1")"
[[ -n "$fixture_user" ]] || { echo "v0.13 fixture has no user" >&2; exit 1; }

if ! (
  cd "$repo_root"
  PSQL_MODE=docker POSTGRES_CONTAINER="$POSTGRES_CONTAINER" DB_NAME="$drill_db" ./scripts/migrate.sh up
) >>"$log_file" 2>&1; then
  tail -n 80 "$log_file" >&2
  exit 1
fi
[[ "$(scalar "SELECT count(*) FROM schema_migrations")" == "49" ]] || { echo "v0.14 migrations did not reach 49" >&2; exit 1; }

schedule_dir="$fixture_root/$fixture_user/file/schedule/terms"
mkdir -p "$schedule_dir"
schedule_file="$schedule_dir/2026-fall.json"
cat >"$schedule_file" <<JSON
{"user_id":"$fixture_user","academic_term_id":"","term_year":2026,"semester":"fall","first_week_start":"2026-08-31","settings":{"periods_per_day":12,"morning_start":"08:00","afternoon_start":"14:00","evening_start":"19:00"},"courses":[{"id":"legacy-course","name":"历史迁移课程","weekday":1,"start_period":1,"end_period":2,"weeks":[1,2],"location":"教学楼 A"}],"updated_at":"2026-08-01T00:00:00Z"}
JSON
cat >"$fixture_root/$fixture_user/file/schedule/index.json" <<JSON
{"user_id":"$fixture_user","term_year":2026,"semester":"fall","updated_at":"2026-08-01T00:00:00Z"}
JSON
source_hash="$(sha256sum "$schedule_file" | awk '{print $1}')"
fixture_dsn="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${drill_db}?sslmode=disable"

adoption_output="$(cd "$repo_root" && GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" DATABASE_DSN="$fixture_dsn" go run ./cmd/campusosctl schedule adopt --root "$fixture_root" --dsn "$fixture_dsn" --apply --actor "$fixture_user" --reason "v0.14 historical fixture adoption")"
[[ "$adoption_output" == *'"objects_created": 1'* ]] || { echo "historical schedule object was not created: $adoption_output" >&2; exit 1; }
[[ "$(scalar "SELECT count(*) FROM user_schedule_terms WHERE user_id=$fixture_user")" == "1" ]] || { echo "historical schedule binding is missing" >&2; exit 1; }
[[ "$(scalar "SELECT count(*) FROM user_schedule_preferences WHERE user_id=$fixture_user")" == "1" ]] || { echo "historical schedule preference is missing" >&2; exit 1; }
[[ "$(scalar "SELECT status FROM academic_terms WHERE year=2026 AND semester='fall'")" == "closed" ]] || { echo "historical term was not registered as closed" >&2; exit 1; }
[[ "$(scalar "SELECT count(*) FROM storage_objects WHERE owner_user_id=$fixture_user AND namespace='schedule' AND purpose='historical-term-json' AND status='ready'")" == "1" ]] || { echo "historical object metadata is missing" >&2; exit 1; }
[[ "$(sha256sum "$schedule_file" | awk '{print $1}')" == "$source_hash" ]] || { echo "legacy schedule JSON changed during adoption" >&2; exit 1; }

reconcile_output="$(cd "$repo_root" && GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" DATABASE_DSN="$fixture_dsn" go run ./cmd/campusosctl storage reconcile --root "$fixture_root" --dsn "$fixture_dsn" --dry-run --batch-size 10 --max-differences 100)"
for difference in metadata_missing_physical physical_without_metadata payload_hash_or_size_mismatch ledger_mismatch; do
  if grep -Eq "\"${difference}\"[[:space:]]*:[[:space:]]*[1-9][0-9]*" <<<"$reconcile_output"; then
    echo "historical fixture reconciliation reported ${difference}: $reconcile_output" >&2
    exit 1
  fi
done

echo "v0.14 historical v0.13-db + legacy-schedule fixture migration/adoption/reconcile drill passed (PostgreSQL container: $POSTGRES_CONTAINER)"
