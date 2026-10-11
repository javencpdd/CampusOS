#!/usr/bin/env bash
# V12-02d admin account lifecycle service acceptance on an owned PostgreSQL 16
# tmpfs container. The developer database is never touched.
#
# The host /tmp is small and intermittently full on this machine, which breaks
# runc's docker exec state files. All PostgreSQL interactions therefore run in
# throwaway client containers sharing the database network namespace, and
# migrate.sh runs in host mode against a psql shim; docker run does not use
# /tmp for runc exec state.
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

mkdir -p .cache/v12-02d-admin-service/tmp
export TMPDIR="$repo_root/.cache/v12-02d-admin-service/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
report="${2:-docs/项目计划书v1/项目计划v1.2/evidence/v12-02d-admin-service-linux.json}"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02d-admin-service/service.XXXXXX")"
container="campusos-v12-02d-service-$$"
database="campusos_v12_02d_service"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
container_started=false

cleanup() {
  local status=$?
  if [[ "$container_started" == true ]]; then
    if (( status != 0 )); then
      docker logs "$container" >"$repo_root/.cache/v12-02d-admin-service/postgres-last-failure.log" 2>&1 || true
      echo "isolated PostgreSQL failure log: .cache/v12-02d-admin-service/postgres-last-failure.log" >&2
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
  echo "V12-02d admin service: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then
    return
  fi
  tail -n 120 "$work_dir/$name.log" >&2
  return 1
}

psql_client() {
  docker run --rm -i --network "container:$container" -e PGPASSWORD="$pg_password" \
    postgres:16-alpine psql -h 127.0.0.1 -U campusos "$@"
}
psql_scalar() { psql_client -d "$database" -qAtv ON_ERROR_STOP=1 -v VERBOSITY=verbose -c "$1"; }

shim_dir="$work_dir/bin"
mkdir -p "$shim_dir"
cat >"$shim_dir/psql" <<'SHIM'
#!/usr/bin/env bash
exec docker run --rm -i --network "container:$DRILL_DB_CONTAINER" -v "$DRILL_TMP:$DRILL_TMP:ro" -e PGPASSWORD postgres:16-alpine psql "$@"
SHIM
chmod +x "$shim_dir/psql"
run_migrate() {
  CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host PATH="$shim_dir:$PATH" \
    DRILL_DB_CONTAINER="$container" DRILL_TMP="$TMPDIR" \
    DB_HOST=127.0.0.1 DB_PORT=5432 \
    DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database" \
    MIGRATIONS_DIR="migrations" bash scripts/migrate.sh "$@"
}

docker run --rm -d --name "$container" \
  --tmpfs /var/lib/postgresql/data:rw,size=512m \
  -e POSTGRES_DB="$database" -e POSTGRES_USER=campusos \
  -e POSTGRES_PASSWORD="$pg_password" \
  -p 127.0.0.1::5432 postgres:16-alpine >/dev/null
container_started=true
for attempt in $(seq 1 80); do
  if psql_scalar 'SELECT 1;' >/dev/null 2>&1; then break; fi
  if (( attempt == 80 )); then echo "isolated PostgreSQL did not stabilize" >&2; exit 1; fi
  sleep 0.5
done
pg_port="$(docker port "$container" 5432/tcp | awk -F: '/127\.0\.0\.1/ {print $NF; exit}')"
[[ "$pg_port" =~ ^[0-9]+$ ]] || { echo "PostgreSQL port is not loopback-bound" >&2; exit 1; }
database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/$database?sslmode=disable"

run_check migrations run_migrate up

run_check service-tests env CAMPUSOS_IDENTITY_TEST_DATABASE_URL="$database_url" \
  go test -race -count=1 -v ./internal/modules/core/identity/service -run '^(TestAdminDomain|TestPostgreSQLAdminDomain)'

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
    'TestAdminDomainBootstrapFirstSecurityAdmin',
    'TestAdminDomainLifecycleGuards',
    'TestAdminDomainAuditWritesPerCommand',
    'TestAdminDomainBootstrapRaceSingleWinner',
    'TestPostgreSQLAdminDomainLifecycleAndRace',
]
for test in required_tests:
    assert re.search(r'^--- PASS: ' + test + r'(?: |$)', text, re.M), test
assert not re.search(r'^--- (?:FAIL|SKIP): ', text, re.M), 'unexpected failure or skip'
sha = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
sources = [
    'internal/modules/core/identity/service/admin_domain_service.go',
    'internal/modules/core/identity/service/admin_domain_service_test.go',
    'internal/modules/core/identity/repository/pg_admin_domain_repository.go',
    'internal/modules/core/identity/policy/identity_domains.go',
    'internal/modules/core/identity/port/identity_domains_policy.go',
    'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md',
    'scripts/v12-02d-admin-service-drill.sh',
]
logs = {
    name: {'path': str((work / f'{name}.log').relative_to(root)), 'sha256': sha(work / f'{name}.log')}
    for name in ['migrations', 'service-tests']
}
dependencies = {}
for name in ['v12-g0-exit.json', 'v12-g1-exit-linux.json', 'v12-01a-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-02a-exit-linux.json', 'v12-02d-identity-domains-linux.json', 'v12-02d-admin-store-linux.json']:
    path = Path('docs/项目计划书v1/项目计划v1.2/evidence') / name
    dependencies[name] = {
        'report': str(path),
        'sha256': sha(path),
        'role': 'accepted predecessor evidence; historical source snapshots are retained unchanged',
    }
data = {
    'schema': 'campusos.v12-02d-admin-service-acceptance/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-02d',
    'stage_status': 'partial',
    'slice_status': 'completed',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'scope': 'AdminDomainService account lifecycle (bootstrap/create/suspend/revoke/restore) over the identity-domains policy with last-security-admin protection, required audit atomicity and bootstrap races on real PostgreSQL; no HTTP wiring',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'go': subprocess.check_output(['go', 'version'], text=True).strip(),
        'postgres': 'PostgreSQL 16 (owned tmpfs container, loopback-only port; client-container psql because host /tmp is unreliable for runc exec state)',
    },
    'checks': {test: 'passed' for test in required_tests},
    'coverage': {
        'service_tests': 5,
        'adapters': ['postgres', 'memory'],
        'race_paths': ['concurrent bootstrap single winner', 'audit failure rollback'],
        'guards': ['bootstrap gate', 'admin+MFA actor', 'last security admin', 'self transition', 'revoked terminal'],
    },
    'source_sha256': {path: sha(path) for path in sources},
    'dependencies': dependencies,
    'target_logs': logs,
    'common_gates_status': 'pending',
    'common_gates': {},
    'boundaries': [
        'No HTTP route or Admin UI consumes the service yet; admin login/session/JWT issuance arrives in later V12-02d slices.',
        'The bootstrap gate uses deployment configuration only; no default administrator is created by migrations or seeds.',
        'The legacy admission table and auto-admission trigger still serve the current Admin UI until the runtime switch slice; G1 replacement-map entries remain pending.',
        'Audit permission_code for bootstrap/create/revoke is intentionally empty pending dedicated admin-domain codes; operation_code carries the precise action.',
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
print('V12-02d admin lifecycle slice passed; 7 common gates passed; stage remains partial')
PY
