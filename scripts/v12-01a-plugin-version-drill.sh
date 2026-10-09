#!/usr/bin/env bash
# V12-01a plugin version publication identity acceptance on an owned PostgreSQL 16 tmpfs container.
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
report="$repo_root/.cache/v12-01a/plugin-version-drill.json"
if (( $# == 2 )); then report="$2"; fi
work_dir="$(mktemp -d "$repo_root/.cache/v12-01a/version.XXXXXX")"
container="campusos-v12-01a-version-$$"
database="campusos_v12_01a_version"
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
      docker logs "$container" >"$repo_root/.cache/v12-01a/plugin-version-postgres-last-failure.log" 2>&1 || true
      echo "isolated PostgreSQL failure log: .cache/v12-01a/plugin-version-postgres-last-failure.log" >&2
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
    psql -U campusos -d "$database" -qAtv ON_ERROR_STOP=1 -v VERBOSITY=verbose -c "$1"
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
  local label="$1" sqlstate="$2" statement="$3"
  if psql_scalar "$statement" >"$work_dir/rejected-$label.log" 2>&1; then
    echo "$label: invalid mutation was accepted" >&2
    exit 1
  fi
  if ! grep -q "ERROR:  $sqlstate:" "$work_dir/rejected-$label.log"; then
    echo "$label: rejection did not have SQLSTATE $sqlstate" >&2
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

run_migrate migrations up >"$work_dir/migrations.log"
run_migrate migrations check >>"$work_dir/migrations.log"
require_equals "current migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "5"
require_equals "version identity trigger" "$(psql_scalar "SELECT count(*) FROM pg_trigger WHERE tgname='trg_plugin_version_identity_immutable' AND NOT tgisinternal;")" "1"
require_equals "declaration identity trigger" "$(psql_scalar "SELECT count(*) FROM pg_trigger WHERE tgname='trg_plugin_declaration_immutable' AND NOT tgisinternal;")" "1"

psql_scalar "INSERT INTO plugins(id,name,display_name,version,runtime) VALUES(990010,'v12-01a-version-fixture','Version Fixture','1.0.0','grpc');
INSERT INTO users(id,username,nickname,email) VALUES(990014,'v12versiontest','Version Test','v12-version-test@example.invalid');
INSERT INTO plugin_versions(id,plugin_id,version,package_digest,manifest_api_version,host_api_version,permission_fingerprint,manifest,lifecycle_status)
VALUES (990011,990010,'1.0.0',repeat('a',64),'v3','v3',repeat('b',64),'{\"name\":\"v12-01a-version-fixture\",\"version\":\"1.0.0\"}'::jsonb,'active'),
       (990013,990010,'2.0.0',repeat('c',64),'v3','v3',repeat('d',64),'{\"name\":\"v12-01a-version-fixture\",\"version\":\"2.0.0\"}'::jsonb,'staged');
INSERT INTO plugin_capability_declarations(id,plugin_version_id,capability_code,purpose,resource_scope)
VALUES(990012,990011,'schedule.self.read','schedule permission','{}'::jsonb);
INSERT INTO plugin_admin_grants(id,plugin_version_id,capability_code,status)
VALUES(990015,990011,'schedule.self.read','revoked');
INSERT INTO plugin_user_consents(id,user_id,plugin_version_id,capability_code,status,purpose_hash,revoked_at)
VALUES(990016,990014,990011,'schedule.self.read','revoked',repeat('e',64),now());" >/dev/null

reject_sql "package-digest" "23514" "UPDATE plugin_versions SET package_digest=repeat('f',64) WHERE id=990011;"
reject_sql "manifest" "23514" "UPDATE plugin_versions SET manifest='{\"name\":\"tampered\"}'::jsonb WHERE id=990011;"
reject_sql "permission-fingerprint" "23514" "UPDATE plugin_versions SET permission_fingerprint=repeat('f',64) WHERE id=990011;"
reject_sql "declaration-purpose" "23514" "UPDATE plugin_capability_declarations SET purpose='tampered' WHERE id=990012;"
reject_sql "declaration-scope" "23514" "UPDATE plugin_capability_declarations SET resource_scope='{\"scope\":\"all\"}'::jsonb WHERE id=990012;"
reject_sql "second-active" "23505" "UPDATE plugin_versions SET lifecycle_status='active' WHERE id=990013;"
psql_scalar "UPDATE plugin_versions SET lifecycle_status='retired',retired_at=now() WHERE id=990011;
UPDATE plugin_versions SET lifecycle_status='active',activated_at=now() WHERE id=990013;
UPDATE plugin_versions SET lifecycle_status='retired',retired_at=now() WHERE id=990013;
UPDATE plugin_versions SET lifecycle_status='active',activated_at=now(),retired_at=NULL WHERE id=990011;" >/dev/null
require_equals "single active after A-B-A" "$(psql_scalar "SELECT string_agg(version,',' ORDER BY version) FROM plugin_versions WHERE plugin_id=990010 AND lifecycle_status='active';")" "1.0.0"
require_equals "revoked decisions retained" "$(psql_scalar "SELECT ag.status || ':' || uc.status FROM plugin_admin_grants ag JOIN plugin_user_consents uc USING(plugin_version_id,capability_code) WHERE ag.id=990015 AND uc.id=990016;")" "revoked:revoked"
require_equals "declaration identity retained" "$(psql_scalar 'SELECT id FROM plugin_capability_declarations WHERE plugin_version_id=990011;')" "990012"

run_migrate migrations down >>"$work_dir/migrations.log"
require_equals "down migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "4"
require_equals "down removes version trigger" "$(psql_scalar "SELECT count(*) FROM pg_trigger WHERE tgname='trg_plugin_version_identity_immutable' AND NOT tgisinternal;")" "0"
require_equals "down removes declaration trigger" "$(psql_scalar "SELECT count(*) FROM pg_trigger WHERE tgname='trg_plugin_declaration_immutable' AND NOT tgisinternal;")" "0"
require_equals "down preserves version rows" "$(psql_scalar 'SELECT count(*) FROM plugin_versions WHERE plugin_id=990010;')" "2"
run_migrate migrations check >>"$work_dir/migrations.log"
run_migrate migrations up >>"$work_dir/migrations.log"
run_migrate migrations check >>"$work_dir/migrations.log"
require_equals "up/down/up migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "5"
reject_sql "reapplied-identity" "23514" "UPDATE plugin_versions SET package_digest=repeat('f',64) WHERE id=990011;"

drift_dir="$work_dir/drift-migrations"
mkdir -p "$drift_dir"
cp migrations/00000[1-5]_*.sql "$drift_dir/"
printf '\n-- intentional checksum drift\n' >>"$drift_dir/000005_v1_2_plugin_version_identity.up.sql"
if run_migrate "$drift_dir" check >"$work_dir/checksum-drift.log" 2>&1; then
  echo "000005 checksum drift was not rejected" >&2
  exit 1
fi
run_migrate migrations check >>"$work_dir/migrations.log"

CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
  DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database" \
  CAMPUSOS_V1_DRILL_DB=campusos_v1_history_version_isolated \
  make database-check >"$work_dir/database-check.log" 2>&1

CAMPUSOS_PG_INTEGRATION_DSN="$database_url" \
  go test ./internal/plugin -run '^TestPostgresVersionIdentityAndConcurrentActivation$' -count=1 -v \
  >"$work_dir/repository-test.log" 2>&1
if ! grep -Eq '^--- PASS: TestPostgresVersionIdentityAndConcurrentActivation' "$work_dir/repository-test.log"; then
  echo "targeted PostgreSQL plugin test did not execute" >&2
  exit 1
fi
if grep -q -- '--- SKIP:' "$work_dir/repository-test.log"; then
  echo "targeted PostgreSQL plugin test skipped a required case" >&2
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
require_equals "post-start migration checksum" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "5"

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
    Path('migrations/000005_v1_2_plugin_version_identity.up.sql'),
    Path('migrations/000005_v1_2_plugin_version_identity.down.sql'),
    Path('internal/plugin/authorization.go'),
    Path('internal/plugin/authorization_pg.go'),
    Path('internal/plugin/authorization_version_pg_integration_test.go'),
    Path('internal/plugin/authorization_version_test.go'),
    Path('scripts/v12-01a-plugin-version-drill.sh'),
]
data = {
    'schema': 'campusos.v12-01a-plugin-version-drill/v1',
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
        'migration_up_and_schema_checksum': 'passed',
        'published_version_and_declaration_immutability': 'passed',
        'single_active_and_a_b_a_reactivation': 'passed',
        'revoked_admin_grant_and_consent_retained': 'passed',
        'down_preserves_data_and_up_restores_guards': 'passed',
        'migration_checksum_drift_rejected': 'passed',
        'current_database_and_historical_v11_gate': 'passed',
        'postgres_repository_targeted_test': 'passed',
        'api_health_and_community_list': 'passed',
    },
    'source_sha256': {str(path): hashlib.sha256(path.read_bytes()).hexdigest() for path in sources},
}
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print(f"V12-01a plugin version PostgreSQL 16 drill passed; evidence: {report}")
PYREPORT
