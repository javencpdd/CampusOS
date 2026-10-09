#!/usr/bin/env bash
# V12-01a authorization audit actor migration acceptance on an owned PostgreSQL 16 tmpfs container.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
for executable in docker go curl python3 make; do
  command -v "$executable" >/dev/null || { echo "missing $executable" >&2; exit 127; }
done
if (( $# != 0 && $# != 2 )); then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi
if (( $# == 2 )) && [[ "$1" != "--report" ]]; then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi

mkdir -p .cache/v12-01a/tmp .cache/v12-01a/go-cache
export TMPDIR="$repo_root/.cache/v12-01a/tmp"
export GOCACHE="$repo_root/.cache/v12-01a/go-cache"
report="$repo_root/.cache/v12-01a/authorization-audit-drill.json"
if (( $# == 2 )); then report="$2"; fi
work_dir="$(mktemp -d "$repo_root/.cache/v12-01a/audit.XXXXXX")"
container="campusos-v12-01a-audit-$$"
database="campusos_v12_01a_audit"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
bootstrap_secret="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
jwt_secret="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
challenge_secret="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
mfa_secret="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
api_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
plugin_ui_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
container_started=false
api_pid=""

cleanup() {
  local status=$?
  if [[ -n "$api_pid" ]]; then
    kill "$api_pid" 2>/dev/null || true
    wait "$api_pid" 2>/dev/null || true
  fi
  if [[ "$container_started" == true ]]; then
    if (( status != 0 )); then
      docker logs "$container" >"$repo_root/.cache/v12-01a/postgres-last-failure.log" 2>&1 || true
      echo "isolated PostgreSQL failure log: .cache/v12-01a/postgres-last-failure.log" >&2
    fi
    docker stop "$container" >/dev/null 2>&1 || true
  fi
  if (( status != 0 )) && [[ -f "$work_dir/api.log" ]]; then
    python3 - "$work_dir/api.log" <<'PYERROR'
import re
import sys
from pathlib import Path
for line in Path(sys.argv[1]).read_text(errors='replace').splitlines():
    match = re.search(r'\(SQLSTATE ([0-9A-Z]{5})\)', line)
    if match:
        print('isolated API SQLSTATE ' + match.group(1), file=sys.stderr)
PYERROR
  fi
  rm -rf -- "$work_dir"
}
trap cleanup EXIT

psql_scalar() {
  docker exec -e PGPASSWORD="$pg_password" "$container" \
    psql -U campusos -d "$database" -qAtv ON_ERROR_STOP=1 -c "$1"
}
run_migrate() {
  local migration_dir="$1"
  shift
  CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
    DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database" \
    MIGRATIONS_DIR="$migration_dir" bash scripts/migrate.sh "$@"
}
require_equals() {
  local label="$1" actual="$2" expected="$3"
  if [[ "$actual" != "$expected" ]]; then
    echo "$label: expected '$expected', got '$actual'" >&2
    exit 1
  fi
}
reject_sql() {
  local label="$1" statement="$2"
  if psql_scalar "$statement" >"$work_dir/rejected-$label.log" 2>&1; then
    echo "$label: invalid audit actor was accepted" >&2
    exit 1
  fi
}

docker run --rm -d --name "$container" \
  --tmpfs /var/lib/postgresql/data:rw,size=512m \
  -e POSTGRES_DB="$database" -e POSTGRES_USER=campusos \
  -e POSTGRES_PASSWORD="$pg_password" \
  -p 127.0.0.1::5432 postgres:16-alpine >/dev/null
container_started=true
for attempt in $(seq 1 80); do
  if docker exec "$container" pg_isready -U campusos -d "$database" >/dev/null 2>&1; then break; fi
  if (( attempt == 80 )); then echo "isolated PostgreSQL did not become ready" >&2; exit 1; fi
  sleep 0.25
done
pg_version="$(psql_scalar 'SHOW server_version_num;')"
if (( pg_version < 160000 || pg_version >= 170000 )); then
  echo "expected PostgreSQL 16, got server_version_num=$pg_version" >&2
  exit 1
fi
tmpfs_options="$(docker inspect --format '{{index .HostConfig.Tmpfs "/var/lib/postgresql/data"}}' "$container")"
[[ "$tmpfs_options" == *size=512m* ]] || { echo "PostgreSQL data directory is not tmpfs" >&2; exit 1; }
pg_port="$(docker port "$container" 5432/tcp | awk -F: '/127\.0\.0\.1/ {print $NF; exit}')"
[[ "$pg_port" =~ ^[0-9]+$ ]] || { echo "PostgreSQL port is not loopback-bound" >&2; exit 1; }
database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/$database?sslmode=disable"

baseline_dir="$work_dir/v1.1-migrations"
mkdir -p "$baseline_dir"
cp migrations/00000[1-3]_v1_1_*.sql "$baseline_dir/"
run_migrate "$baseline_dir" up >"$work_dir/migrations.log"
run_migrate "$baseline_dir" check >>"$work_dir/migrations.log"
require_equals "v1.1 migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "3"
require_equals "v1.1 audit actor ID type" "$(psql_scalar "SELECT data_type FROM information_schema.columns WHERE table_schema='public' AND table_name='authorization_audits' AND column_name='actor_id';")" "bigint"
psql_scalar "INSERT INTO authorization_audits(id,actor_id,outcome) VALUES(910001,123,'allow'),(910002,NULL,'deny');" >/dev/null

run_migrate migrations up >>"$work_dir/migrations.log"
run_migrate migrations check >>"$work_dir/migrations.log"
require_equals "v1.2 migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "4"
require_equals "actor ID type" "$(psql_scalar "SELECT data_type || ':' || character_maximum_length FROM information_schema.columns WHERE table_schema='public' AND table_name='authorization_audits' AND column_name='actor_id';")" "character varying:128"
require_equals "actor kind required" "$(psql_scalar "SELECT is_nullable FROM information_schema.columns WHERE table_schema='public' AND table_name='authorization_audits' AND column_name='actor_kind';")" "NO"
require_equals "historical user backfill" "$(psql_scalar "SELECT actor_kind || ':' || actor_id FROM authorization_audits WHERE id=910001;")" "user:123"
require_equals "historical unknown backfill" "$(psql_scalar "SELECT actor_kind FROM authorization_audits WHERE id=910002 AND actor_id IS NULL;")" "legacy_unknown"

psql_scalar "INSERT INTO authorization_audits(id,actor_kind,actor_id,outcome) VALUES
  (910010,'anonymous',NULL,'allow'),
  (910011,'user','456','allow'),
  (910012,'user','77','allow'),
  (910013,'admin','77','allow'),
  (910014,'integration','integration:a','allow'),
  (910015,'plugin_instance','plugin:a','allow'),
  (910016,'worker','worker:a','allow'),
  (910017,'system','system:a','allow'),
  (910018,'user','uid:opaque','allow'),
  (910019,'user','001','allow');" >/dev/null
require_equals "same ID separate principal domains" "$(psql_scalar "SELECT count(DISTINCT actor_kind) FROM authorization_audits WHERE actor_id='77' AND id IN (910012,910013);")" "2"
reject_sql "anonymous-with-id" "INSERT INTO authorization_audits(id,actor_kind,actor_id,outcome) VALUES(910030,'anonymous','1','allow');"
reject_sql "admin-without-id" "INSERT INTO authorization_audits(id,actor_kind,actor_id,outcome) VALUES(910031,'admin',NULL,'allow');"
reject_sql "blank-id" "INSERT INTO authorization_audits(id,actor_kind,actor_id,outcome) VALUES(910032,'user','','allow');"
reject_sql "invalid-kind" "INSERT INTO authorization_audits(id,actor_kind,actor_id,outcome) VALUES(910033,'moderator','1','allow');"
reject_sql "id-with-space" "INSERT INTO authorization_audits(id,actor_kind,actor_id,outcome) VALUES(910034,'user','a b','allow');"
reject_sql "id-too-long" "INSERT INTO authorization_audits(id,actor_kind,actor_id,outcome) VALUES(910035,'user',repeat('x',129),'allow');"

if run_migrate migrations down >"$work_dir/down-guard-admin.log" 2>&1; then
  echo "down accepted an admin audit actor" >&2
  exit 1
fi
require_equals "failed down preserves migration" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "4"
require_equals "failed down preserves audit rows" "$(psql_scalar 'SELECT count(*) FROM authorization_audits;')" "12"
psql_scalar "DELETE FROM authorization_audits WHERE id BETWEEN 910013 AND 910017;" >/dev/null
if run_migrate migrations down >"$work_dir/down-guard-opaque.log" 2>&1; then
  echo "down accepted an opaque user audit ID" >&2
  exit 1
fi
require_equals "opaque down guard preserves migration" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "4"
psql_scalar "DELETE FROM authorization_audits WHERE id=910018;" >/dev/null
if run_migrate migrations down >"$work_dir/down-guard-leading-zero.log" 2>&1; then
  echo "down accepted a noncanonical numeric user audit ID" >&2
  exit 1
fi
require_equals "noncanonical ID down guard preserves migration" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "4"
psql_scalar "DELETE FROM authorization_audits WHERE id=910019;" >/dev/null
run_migrate migrations down >>"$work_dir/migrations.log"
require_equals "down migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "3"
require_equals "down actor ID type" "$(psql_scalar "SELECT data_type FROM information_schema.columns WHERE table_schema='public' AND table_name='authorization_audits' AND column_name='actor_id';")" "bigint"
require_equals "down removes actor kind" "$(psql_scalar "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='authorization_audits' AND column_name='actor_kind';")" "0"
require_equals "down preserves null IDs" "$(psql_scalar 'SELECT count(*) FROM authorization_audits WHERE actor_id IS NULL;')" "2"
require_equals "down preserves numeric user IDs" "$(psql_scalar "SELECT string_agg(actor_id::text,',' ORDER BY actor_id) FROM authorization_audits WHERE actor_id IS NOT NULL;")" "77,123,456"
run_migrate migrations check >>"$work_dir/migrations.log"

run_migrate migrations up >>"$work_dir/migrations.log"
run_migrate migrations check >>"$work_dir/migrations.log"
require_equals "up/down/up migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "4"
require_equals "up/down/up unknown provenance" "$(psql_scalar "SELECT count(*) FROM authorization_audits WHERE actor_kind='legacy_unknown' AND actor_id IS NULL;")" "2"
require_equals "up/down/up user IDs" "$(psql_scalar "SELECT string_agg(actor_id,',' ORDER BY actor_id::bigint) FROM authorization_audits WHERE actor_kind='user';")" "77,123,456"

drift_dir="$work_dir/drift-migrations"
mkdir -p "$drift_dir"
cp migrations/00000[1-4]_*.sql "$drift_dir/"
printf '\n-- intentional checksum drift\n' >>"$drift_dir/000004_v1_2_authorization_audit_actor.up.sql"
if run_migrate "$drift_dir" check >"$work_dir/checksum-drift.log" 2>&1; then
  echo "000004 checksum drift was not rejected" >&2
  exit 1
fi
run_migrate migrations check >>"$work_dir/migrations.log"

CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
  DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database" \
  CAMPUSOS_V1_DRILL_DB=campusos_v1_history_isolated \
  make database-check >"$work_dir/database-check.log" 2>&1

CAMPUSOS_IDENTITY_TEST_DATABASE_URL="$database_url" \
  go test ./internal/modules/core/identity/repository -run TestAuthorizationAuditActor -count=1 -v \
  >"$work_dir/repository-test.log" 2>&1
if ! grep -Eq '^--- PASS: TestAuthorizationAuditActor|^[[:space:]]+--- PASS: TestAuthorizationAuditActor' "$work_dir/repository-test.log"; then
  echo "targeted PostgreSQL repository test did not execute" >&2
  exit 1
fi
if grep -q -- '--- SKIP:' "$work_dir/repository-test.log"; then
  echo "targeted PostgreSQL repository test skipped a required case" >&2
  exit 1
fi

go build -o "$work_dir/campusos-server" ./cmd/server
mkdir -p "$work_dir/app/modules" "$work_dir/app/data/resources" "$work_dir/app/data/plugins" "$work_dir/app/plugins"
cp -a modules/. "$work_dir/app/modules/"
cp -a data/resources/. "$work_dir/app/data/resources/"
(
  cd "$work_dir/app"
  exec env -i PATH="$PATH" \
    CAMPUSOS_ENV=test CAMPUSOS_INSTANCE_MODE=single GOMAXPROCS=8 \
    SERVER_HOST=127.0.0.1 SERVER_PORT="$api_port" DATABASE_DSN="$database_url" \
    REDIS_ENABLED=false HOST_API_ENABLED=false AI_ENABLED=false EMAIL_PROVIDER=fake \
    AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN=false AUTH_BOOTSTRAP_ADMIN_SECRET="$bootstrap_secret" \
    JWT_SECRET="$jwt_secret" AUTH_CHALLENGE_ACTIVE_KEY_ID=v12 \
    AUTH_CHALLENGE_HMAC_KEYS="v12:$challenge_secret" \
    AUTH_CHALLENGE_IP_HASH_SECRET="$challenge_secret" \
    AUTH_SESSION_IP_HASH_SECRET="$challenge_secret" \
    AUTH_MFA_ACTIVE_KEY_ID=v12 AUTH_MFA_ENCRYPTION_KEYS="v12:$mfa_secret" \
    PLUGINS_DIR="$work_dir/app/data/plugins" PLUGIN_DATA_DIR="$work_dir/app/data/plugin_data" \
    CAMPUSOS_PLUGIN_V4_DIR="$work_dir/app/plugins" CAMPUSOS_PLUGIN_V4_DEV_SOURCE=false \
    CAMPUSOS_PLUGIN_UI_ORIGIN="http://127.0.0.1:$plugin_ui_port" \
    MODULE_DATA_DIR="$work_dir/app/data/module_data" RESOURCE_DIR="$work_dir/app/data/resources" \
    "$work_dir/campusos-server"
) >"$work_dir/api.log" 2>&1 &
api_pid="$!"
for attempt in $(seq 1 80); do
  if curl -fsS "http://127.0.0.1:$api_port/api/v1/health" >/dev/null 2>&1; then break; fi
  if ! kill -0 "$api_pid" 2>/dev/null; then
    echo "isolated API exited before health check" >&2
    exit 1
  fi
  if (( attempt == 80 )); then
    echo "isolated API did not become healthy" >&2
    exit 1
  fi
  sleep 0.25
done
curl -fsS "http://127.0.0.1:$api_port/api/v1/threads?page=1&page_size=1" >/dev/null
require_equals "post-start migration checksum" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "4"

V12_01A_REPORT_PATH="$report" V12_01A_PG_VERSION="$pg_version" python3 - <<'PYREPORT'
import hashlib
import json
import os
import platform
from datetime import datetime, timezone
from pathlib import Path

report = Path(os.environ['V12_01A_REPORT_PATH'])
report.parent.mkdir(parents=True, exist_ok=True)
sources = [
    Path('migrations/000004_v1_2_authorization_audit_actor.up.sql'),
    Path('migrations/000004_v1_2_authorization_audit_actor.down.sql'),
    Path('scripts/v12-01a-authorization-audit-drill.sh'),
]
data = {
    'schema': 'campusos.v12-01a-authorization-audit-drill/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-01a',
    'target_environment': 'passed',
    'environment': {
        'os': platform.system().lower(),
        'architecture': platform.machine(),
        'postgres_server_version_num': int(os.environ['V12_01A_PG_VERSION']),
        'postgres_image': 'postgres:16-alpine',
        'database_storage': 'temporary container tmpfs',
        'postgres_bind': '127.0.0.1',
        'api_bind': '127.0.0.1',
    },
    'checks': {
        'v11_only_first_three_migrations': 'passed',
        'legacy_unknown_and_user_backfill': 'passed',
        'actor_kind_and_id_constraints': 'passed',
        'same_id_separate_domains': 'passed',
        'admin_down_guard': 'passed',
        'opaque_user_down_guard': 'passed',
        'noncanonical_numeric_user_down_guard': 'passed',
        'representable_down_preserves_rows': 'passed',
        'up_down_up_and_checksum': 'passed',
        'current_database_and_historical_v11_gate': 'passed',
        'postgres_repository_targeted_test': 'passed',
        'api_health_and_community_list': 'passed',
    },
    'source_sha256': {str(path): hashlib.sha256(path.read_bytes()).hexdigest() for path in sources},
}
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print(f"V12-01a authorization audit PostgreSQL 16 drill passed; evidence: {report}")
PYREPORT
