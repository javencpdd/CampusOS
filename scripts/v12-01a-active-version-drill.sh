#!/usr/bin/env bash
# V12-01a active plugin version authorization acceptance on an owned PostgreSQL 16 tmpfs container.
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
report="$repo_root/.cache/v12-01a/active-version-drill.json"
if (( $# == 2 )); then report="$2"; fi
work_dir="$(mktemp -d "$repo_root/.cache/v12-01a/active-version.XXXXXX")"
container="campusos-v12-01a-active-$$"
database="campusos_v12_01a_active"
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
      docker logs "$container" >"$repo_root/.cache/v12-01a/active-version-postgres-last-failure.log" 2>&1 || true
      echo "isolated PostgreSQL failure log: .cache/v12-01a/active-version-postgres-last-failure.log" >&2
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
require_equals "current migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "6"
require_equals "active version index" "$(psql_scalar "SELECT count(*) FROM pg_indexes WHERE schemaname='public' AND indexname='uk_plugin_versions_active';")" "1"

CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
  DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database" \
  CAMPUSOS_V1_DRILL_DB=campusos_v1_history_active_isolated \
  make database-check >"$work_dir/database-check.log" 2>&1

CAMPUSOS_PG_INTEGRATION_DSN="$database_url" \
  go test ./internal/plugin -run '^TestPostgresAuthorizationRequiresActiveVersion$' -count=1 -v \
  >"$work_dir/pg-active-version-test.log" 2>&1
if ! grep -Eq '^--- PASS: TestPostgresAuthorizationRequiresActiveVersion' "$work_dir/pg-active-version-test.log"; then
  echo "targeted PostgreSQL active-version test did not execute" >&2
  exit 1
fi
if grep -q -- '--- SKIP:' "$work_dir/pg-active-version-test.log"; then
  echo "targeted PostgreSQL active-version test skipped a required case" >&2
  exit 1
fi
require_equals "no plugin has multiple active versions" "$(psql_scalar "SELECT count(*) FROM (SELECT plugin_id FROM plugin_versions WHERE lifecycle_status='active' GROUP BY plugin_id HAVING count(*) > 1) duplicate_active;")" "0"

CAMPUSOS_PG_INTEGRATION_DSN="$database_url" \
  go test ./internal/plugin -run '^TestAuthorizationVersionedHandlersRequireMatchingActiveVersion$' -count=1 -v \
  >"$work_dir/gin-handler-test.log" 2>&1
if ! grep -Eq '^--- PASS: TestAuthorizationVersionedHandlersRequireMatchingActiveVersion' "$work_dir/gin-handler-test.log"; then
  echo "targeted Gin HTTP handler test did not execute" >&2
  exit 1
fi
if grep -q -- '--- SKIP:' "$work_dir/gin-handler-test.log"; then
  echo "targeted Gin HTTP handler test skipped a required case" >&2
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
require_equals "post-start migration checksum" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "6"

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
    Path('migrations/000006_v1_2_plugin_publication_seal.up.sql'),
    Path('migrations/000006_v1_2_plugin_publication_seal.down.sql'),
    Path('internal/plugin/authorization.go'),
    Path('internal/plugin/authorization_pg.go'),
    Path('internal/plugin/authorization_handler.go'),
    Path('internal/plugin/authorization_active_version_test.go'),
    Path('internal/plugin/authorization_active_version_pg_integration_test.go'),
    Path('internal/plugin/authorization_version_handler_test.go'),
    Path('internal/transport/httpapi/router.go'),
    Path('scripts/v12-01a-active-version-drill.sh'),
]
data = {
    'schema': 'campusos.v12-01a-active-version-drill/v1',
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
        'migrations_up_and_checksum': 'passed',
        'current_database_and_historical_v11_gate': 'passed',
        'postgres_active_version_targeted_test': 'passed',
        'single_active_version_database_invariant': 'passed',
        'gin_http_versioned_handler_targeted_test': 'passed',
        'api_health_and_community_list': 'passed',
    },
    'http_coverage': 'Versioned mutation responses were exercised by the Gin HTTP handler test; the separately booted API was checked for health and community list only.',
    'source_sha256': {str(path): hashlib.sha256(path.read_bytes()).hexdigest() for path in sources},
}
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print(f"V12-01a active-version PostgreSQL 16 drill passed; evidence: {report}")
PYREPORT
