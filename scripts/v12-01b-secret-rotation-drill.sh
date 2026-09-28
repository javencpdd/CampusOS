#!/usr/bin/env bash
# V12-01b leased background Secret rotation worker: isolated PostgreSQL 16 acceptance.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
for executable in docker go python3 make; do
  command -v "$executable" >/dev/null || { echo "missing $executable" >&2; exit 127; }
done
if (( $# != 0 && $# != 2 )); then
  echo "usage: $0 [--report evidence.json]" >&2; exit 2
fi
if (( $# == 2 )) && [[ "$1" != "--report" ]]; then
  echo "usage: $0 [--report evidence.json]" >&2; exit 2
fi
[[ "$(uname -s)" == Linux ]] || { echo "Linux target environment required" >&2; exit 1; }
mkdir -p .cache/v12-01b/tmp
export TMPDIR="$repo_root/.cache/v12-01b/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
report="$repo_root/docs/项目计划书v1/项目计划v1.2/evidence/v12-01b-secret-rotation-linux.json"
if (( $# == 2 )); then report="$2"; fi
work_dir="$(mktemp -d "$repo_root/.cache/v12-01b/secret-rotation.XXXXXX")"
container="campusos-v12-01b-secret-rotation-$$"
database="campusos_v12_01b_rotation"
restored_database="campusos_v12_01b_rotation_restored"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
container_started=false
cleanup() {
  local status=$?
  if [[ "$container_started" == true ]]; then
    if (( status != 0 )); then
      docker logs "$container" >"$repo_root/.cache/v12-01b/secret-rotation-postgres-last-failure.log" 2>&1 || true
      echo "isolated PostgreSQL log: .cache/v12-01b/secret-rotation-postgres-last-failure.log" >&2
    fi
    docker stop "$container" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$work_dir"
}
trap cleanup EXIT
psql_scalar() {
  local target_database="$1" sql="$2"
  docker exec -e PGPASSWORD="$pg_password" "$container" \
    psql -U campusos -d "$target_database" -qAtv ON_ERROR_STOP=1 -c "$sql"
}
run_migrate() {
  local target_database="$1"
  shift
  CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
    DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$target_database" \
    MIGRATIONS_DIR=migrations bash scripts/migrate.sh "$@"
}
require_test_passed() {
  local log="$1"
  if ! grep -Eq '^--- PASS: TestPostgresSecretRotationWorkerRestore( |$)' "$log" ||
    grep -Eq '^--- SKIP: TestPostgresSecretRotationWorkerRestore( |$)' "$log"; then
    echo "required PostgreSQL Secret rotation test did not execute and pass" >&2
    tail -n 100 "$log" >&2
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
  if docker exec "$container" pg_isready -h 127.0.0.1 -U campusos -d "$database" >/dev/null 2>&1 &&
      [[ "$(psql_scalar "$database" 'SELECT 1;' 2>/dev/null || true)" == 1 ]]; then
    break
  fi
  if (( attempt == 80 )); then echo "isolated PostgreSQL did not become ready" >&2; exit 1; fi
  sleep 0.25
done
pg_version="$(psql_scalar "$database" 'SHOW server_version_num;')"
if (( pg_version < 160000 || pg_version >= 170000 )); then
  echo "expected PostgreSQL 16, got $pg_version" >&2; exit 1
fi
tmpfs_options="$(docker inspect --format '{{index .HostConfig.Tmpfs "/var/lib/postgresql/data"}}' "$container")"
[[ "$tmpfs_options" == *size=512m* ]] || { echo "PostgreSQL data directory is not tmpfs" >&2; exit 1; }
pg_port="$(docker port "$container" 5432/tcp | awk -F: '/127\.0\.0\.1/ {print $NF; exit}')"
[[ "$pg_port" =~ ^[0-9]+$ ]] || { echo "PostgreSQL port is not loopback-bound" >&2; exit 1; }

database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/$database?sslmode=disable"
run_migrate "$database" up >"$work_dir/migrate.log"
run_migrate "$database" check >>"$work_dir/migrate.log"
[[ "$(psql_scalar "$database" 'SELECT count(*) FROM schema_migrations;')" == 7 ]] || {
  echo "expected 7 migrations" >&2; exit 1
}
CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
  DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database" \
  CAMPUSOS_V1_DRILL_DB=campusos_v12_01b_rotation_v11 \
  make database-check >"$work_dir/database-check.log"
CAMPUSOS_SECRET_ROTATION_TEST_ISOLATED=1 CAMPUSOS_SECRET_ROTATION_TEST_PHASE=seed \
  CAMPUSOS_PG_INTEGRATION_DSN="$database_url" \
  go test ./internal/plugin -run '^TestPostgresSecretRotationWorkerRestore$' -count=1 -v | tee "$work_dir/seed.log"
require_test_passed "$work_dir/seed.log"
CAMPUSOS_SECRET_ROTATION_TEST_ISOLATED=1 CAMPUSOS_PG_INTEGRATION_DSN="$database_url" \
  go test ./internal/server -run '^TestPostgresSecretRotationServerLifecycle$' -count=1 -v | tee "$work_dir/server.log"
if ! grep -Eq '^--- PASS: TestPostgresSecretRotationServerLifecycle( |$)' "$work_dir/server.log" || grep -Eq '^--- SKIP:' "$work_dir/server.log"; then
  echo "required periodic server lifecycle test did not pass" >&2; exit 1
fi

docker exec -e PGPASSWORD="$pg_password" "$container" \
  pg_dump -U campusos -d "$database" -Fc -f /tmp/v12-01b-secret-rotation.dump
psql_scalar postgres "CREATE DATABASE $restored_database;" >/dev/null
docker exec -e PGPASSWORD="$pg_password" "$container" \
  pg_restore -U campusos -d "$restored_database" --no-owner --no-privileges /tmp/v12-01b-secret-rotation.dump
run_migrate "$restored_database" check >>"$work_dir/migrate.log"
restored_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/$restored_database?sslmode=disable"
CAMPUSOS_SECRET_ROTATION_TEST_ISOLATED=1 CAMPUSOS_SECRET_ROTATION_TEST_PHASE=restore \
  CAMPUSOS_PG_INTEGRATION_DSN="$restored_url" \
  go test ./internal/plugin -run '^TestPostgresSecretRotationWorkerRestore$' -count=1 -v | tee "$work_dir/restore.log"
require_test_passed "$work_dir/restore.log"

V12_01B_PG_VERSION="$pg_version" python3 - "$report" <<'PY'
import hashlib
import json
import os
import platform
import sys
from datetime import datetime, timezone
from pathlib import Path
paths = [
    'internal/plugin/secret_rotation_worker.go',
    'internal/plugin/secret_rotation_worker_pg.go',
    'internal/plugin/secret_rotation_worker_test.go',
    'internal/plugin/secret_rotation_worker_pg_integration_test.go',
    'internal/server/secret_rotation.go',
    'internal/server/secret_rotation_test.go',
    'internal/server/secret_rotation_pg_integration_test.go',
    'internal/server/server.go',
    'scripts/v12-01b-secret-rotation-drill.sh',
]
report = {
    'schema': 'campusos.v12-stage-slice-exit/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-01b',
    'slice': 'leased_bounded_secret_rotation_worker_pg_restore',
    'stage_status': 'partial',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'postgres_server_version_num': int(os.environ['V12_01B_PG_VERSION']),
        'postgres_image': 'postgres:16-alpine',
        'database_storage': 'owned tmpfs; container removed on exit',
        'postgres_bind': '127.0.0.1 random host port',
        'restore_database': 'campusos_v12_01b_rotation_restored',
    },
    'checks': {
        'migrations_000001_to_000007_and_checksum': 'passed',
        'current_and_historical_database_gate': 'passed',
        'bounded_cross_plugin_system_user_rewrap_and_fair_cursor': 'passed',
        'corrupt_plugin_isolated_and_history_unchanged': 'passed',
        'real_pg_lease_busy_lost_release_and_reacquire': 'passed',
        'payload_free_success_failure_operations': 'passed',
        'whole_table_old_key_references_include_bad_and_history': 'passed',
        'server_opt_in_first_tick_and_cancel_join_before_pool_close': 'passed',
        'pg_dump_restore_and_current_key_only_read': 'passed',
    },
    'restore_scope': 'plugin Secret fixture and operation audits in owned database; historical/bad envelopes retain old key; whole-system restore belongs to V12-08b',
    'source_sha256': {path: hashlib.sha256(Path(path).read_bytes()).hexdigest() for path in paths},
}
Path(sys.argv[1]).parent.mkdir(parents=True, exist_ok=True)
Path(sys.argv[1]).write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
print('V12-01b Secret rotation worker: 9/9 checks passed; report ' + sys.argv[1])
PY
