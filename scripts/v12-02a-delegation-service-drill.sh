#!/usr/bin/env bash
# V12-02a delegation service acceptance on an owned PostgreSQL 16 tmpfs
# container: real transactions, audit atomicity and revocation visibility.
# The developer database is never touched.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
for executable in docker go python3; do
  command -v "$executable" >/dev/null || { echo "missing $executable" >&2; exit 127; }
done
if (( $# != 0 && $# != 2 )) || { (( $# == 2 )) && [[ "$1" != "--report" ]]; }; then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi

mkdir -p .cache/v12-02a-delegation-service/tmp
export TMPDIR="$repo_root/.cache/v12-02a-delegation-service/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
report="${2:-docs/项目计划书v1/项目计划v1.2/evidence/v12-02a-delegation-service-linux.json}"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02a-delegation-service/service.XXXXXX")"
container="campusos-v12-02a-service-$$"
database="campusos_v12_02a_service"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
container_started=false

cleanup() {
  local status=$?
  if [[ "$container_started" == true ]]; then
    if (( status != 0 )); then
      docker logs "$container" >"$repo_root/.cache/v12-02a-delegation-service/postgres-last-failure.log" 2>&1 || true
      echo "isolated PostgreSQL failure log: .cache/v12-02a-delegation-service/postgres-last-failure.log" >&2
    fi
    docker stop "$container" >/dev/null 2>&1 || true
  fi
  if (( status != 0 )); then
    echo "work directory retained for diagnosis: $work_dir" >&2
  fi
}
trap cleanup EXIT

run_check() {
  local name="$1"
  shift
  echo "V12-02a delegation service: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then
    return
  fi
  tail -n 120 "$work_dir/$name.log" >&2
  return 1
}

psql_scalar() {
  docker exec -e PGPASSWORD="$pg_password" "$container" \
    psql -U campusos -d "$database" -qAtv ON_ERROR_STOP=1 -v VERBOSITY=verbose -c "$1"
}
run_migrate() {
  CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
    DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database" \
    MIGRATIONS_DIR="migrations" bash scripts/migrate.sh "$@"
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
for attempt in $(seq 1 80); do
  if psql_scalar 'SELECT 1;' >/dev/null 2>&1; then break; fi
  if (( attempt == 80 )); then echo "isolated PostgreSQL did not stabilize" >&2; exit 1; fi
  sleep 0.25
done
pg_version="$(psql_scalar 'SHOW server_version_num;')"
if (( pg_version < 160000 || pg_version >= 170000 )); then
  echo "expected PostgreSQL 16, got server_version_num=$pg_version" >&2
  exit 1
fi
pg_port="$(docker port "$container" 5432/tcp | awk -F: '/127\.0\.0\.1/ {print $NF; exit}')"
[[ "$pg_port" =~ ^[0-9]+$ ]] || { echo "PostgreSQL port is not loopback-bound" >&2; exit 1; }
database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/$database?sslmode=disable"

run_check migrations run_migrate up
require_equals "migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "8"

# Full service suite: memory adapters plus the env-gated PostgreSQL profile
# with real transactions, audit rollback and revocation races.
run_check service-tests env CAMPUSOS_IDENTITY_TEST_DATABASE_URL="$database_url" \
  go test -race ./internal/modules/core/identity/delegation ./internal/modules/core/identity/repository -count=1 -v

python3 - "$work_dir" "$report" <<'PY'
import hashlib
import json
import platform
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

work, report = map(Path, sys.argv[1:])
root = Path.cwd()
text = (work / 'service-tests.log').read_text()
required_tests = [
    'TestGrantBoardDelegationsCommitsAtomically',
    'TestGrantBoardDelegationsDenies',
    'TestGrantBoardDelegationsRevocationRaceFailsClosed',
    'TestGrantBoardDelegationsAuditFailureRollsBack',
    'TestDelegationBoundAndRevocationAdministration',
    'TestHasActiveGovernanceGrant',
    'TestPostgreSQLGrantCommitAuditAndOutbox',
    'TestPostgreSQLGrantRecheckFailsClosedOnBoardChange',
    'TestPostgreSQLGrantAuditFailureRollsBack',
    'TestPostgreSQLRevocationVisibleImmediately',
    'TestPostgreSQLConcurrentGrantRevokeRace',
]
for test in required_tests:
    assert re.search(r'^--- PASS: ' + test + r'(?: |$)', text, re.M), test
assert not re.search(r'^--- (?:FAIL|SKIP): ', text, re.M), 'unexpected failure or skip'
sha = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
sources = [
    'internal/modules/core/identity/delegation/service.go',
    'internal/modules/core/identity/delegation/service_test.go',
    'internal/modules/core/identity/delegation/service_postgres_test.go',
    'internal/modules/core/identity/repository/pg_delegation_repository.go',
    'internal/modules/core/identity/repository/pg_delegation_repository_test.go',
    'internal/modules/core/identity/policy/board_delegation.go',
    'internal/modules/core/identity/port/board_delegation_policy.go',
    'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md',
    'scripts/v12-02a-delegation-service-drill.sh',
]
logs = {
    name: {'path': str((work / f'{name}.log').relative_to(root)), 'sha256': sha(work / f'{name}.log')}
    for name in ['migrations', 'service-tests']
}
dependencies = {}
for name in ['v12-g0-exit.json', 'v12-g1-exit-linux.json', 'v12-01a-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-02a-board-delegation-linux.json', 'v12-02a-delegation-store-linux.json']:
    path = Path('docs/项目计划书v1/项目计划v1.2/evidence') / name
    dependencies[name] = {
        'report': str(path),
        'sha256': sha(path),
        'role': 'accepted predecessor evidence; historical source snapshots are retained unchanged',
    }
data = {
    'schema': 'campusos.v12-02a-delegation-service-acceptance/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-02a',
    'stage_status': 'partial',
    'slice_status': 'completed',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'scope': 'DelegationService grant/revoke/bound administration over the board-delegation Policy Port with in-transaction recheck, required audit atomicity and revocation races on real PostgreSQL; no HTTP wiring',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'go': subprocess.check_output(['go', 'version'], text=True).strip(),
        'postgres': 'PostgreSQL 16 (owned tmpfs container, loopback-only port)',
    },
    'checks': {test: 'passed' for test in required_tests},
    'coverage': {
        'service_tests': 11,
        'adapters': ['postgres', 'memory'],
        'race_paths': ['in-transaction bound revocation', 'board status change between evaluations', 'concurrent grant vs bound revoke', 'audit failure rollback'],
    },
    'source_sha256': {path: sha(path) for path in sources},
    'dependencies': dependencies,
    'target_logs': logs,
    'common_gates_status': 'pending',
    'common_gates': {},
    'boundaries': [
        'No HTTP route, Admin UI or business module wiring yet; moderation/PermissionService integration is the next V12-02a slice.',
        'Actor MFA and admin domain are caller-constructed facts here; entry-level principal construction arrives with V12-02b/02d.',
        'Default one-year grant windows and password strength are host defaults for the current moderator flow, not a product term policy.',
        'Non-board scope delegation runtime remains V12-02c/03b; board facts are supplied by the calling host module.',
    ],
}
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print('Native Linux target acceptance passed; common gates pending')
PY

run_check go-all go test ./... -count=1
run_check contracts make contracts-check
run_check architecture make architecture-check
run_check docs-build make docs-build
run_check docs-links make docs-links
run_check line-endings python3 scripts/check-line-endings.py --include-untracked
run_check diff git diff --check

python3 - "$work_dir" "$report" <<'PY'
import hashlib
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

work, report = map(Path, sys.argv[1:])
data = json.loads(report.read_text())
commands = {
    'go-all': 'go test ./... -count=1',
    'contracts': 'make contracts-check',
    'architecture': 'make architecture-check',
    'docs-build': 'make docs-build',
    'docs-links': 'make docs-links',
    'line-endings': 'python3 scripts/check-line-endings.py --include-untracked',
    'diff': 'git diff --check',
}
for name, command in commands.items():
    path = work / f'{name}.log'
    data['common_gates'][name] = {
        'command': command,
        'status': 'passed',
        'exit_code': 0,
        'local_log': str(path.relative_to(Path.cwd())),
        'log_sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
    }
for path, digest in data['source_sha256'].items():
    assert hashlib.sha256(Path(path).read_bytes()).hexdigest() == digest, 'source changed during acceptance: ' + path
data['common_gates_status'] = 'passed'
data['generated_at'] = datetime.now(timezone.utc).isoformat()
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print('V12-02a delegation service slice passed; 7 common gates passed; stage remains partial')
PY
