#!/usr/bin/env bash
# Authenticated personal document reads acceptance on an owned PostgreSQL 16.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 )); then echo "usage: $0" >&2; exit 2; fi
mkdir -p .cache/v12-02b-document-read/tmp
export TMPDIR="$repo_root/.cache/v12-02b-document-read/tmp" GOTMPDIR="$repo_root/.cache/v12-02b-document-read/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02b-document-read/drill.XXXXXX")"
container="campusos-v12-02b-document-read-$$"
database="campusos_v12_02b_document_read"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
report="docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-document-read-linux.json"
started=false
cleanup() {
  local status=$?
  if [[ "$started" == true ]]; then
    if (( status != 0 )); then docker logs "$container" >"$work_dir/postgres-failure.log" 2>&1 || true; fi
    docker stop "$container" >/dev/null 2>&1 || true
  fi
  if (( status != 0 )); then echo "failed drill logs retained: $work_dir" >&2; fi
}
trap cleanup EXIT
run_check() {
  local name="$1"
  shift
  echo "V12-02b document reads: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then return; fi
  tail -n 80 "$work_dir/$name.log" >&2
  return 1
}
psql_scalar() {
  docker run --rm -i --network "container:$container" -e PGPASSWORD="$pg_password" \
    postgres:16-alpine psql -h 127.0.0.1 -U campusos -d "$database" -qAtv ON_ERROR_STOP=1 -c "$1"
}
mkdir -p "$work_dir/bin"
cat >"$work_dir/bin/psql" <<'SHIM'
#!/usr/bin/env bash
exec docker run --rm -i --network "container:$DRILL_DB_CONTAINER" -v "$DRILL_REPO:$DRILL_REPO:ro" -w "$DRILL_REPO" -e PGPASSWORD postgres:16-alpine psql "$@"
SHIM
chmod +x "$work_dir/bin/psql"
export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host PATH="$work_dir/bin:$PATH"
export DRILL_DB_CONTAINER="$container" DRILL_REPO="$repo_root"
export DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database"
docker run --rm -d --name "$container" --tmpfs /var/lib/postgresql/data:rw,size=512m \
  -e POSTGRES_DB="$database" -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$pg_password" \
  -p 127.0.0.1::5432 postgres:16-alpine >/dev/null
started=true
for attempt in $(seq 1 80); do
  if psql_scalar 'SELECT 1;' >/dev/null 2>&1; then break; fi
  if (( attempt == 80 )); then echo "PostgreSQL did not become ready" >&2; exit 1; fi
  sleep 0.5
done
pg_version="$(psql_scalar 'SHOW server_version_num;')"
(( pg_version >= 160000 && pg_version < 170000 )) || { echo "PostgreSQL 16 required" >&2; exit 1; }
pg_port="$(docker port "$container" 5432/tcp | awk -F: '/127\.0\.0\.1/ {print $NF; exit}')"
[[ "$pg_port" =~ ^[0-9]+$ ]] || { echo "loopback PostgreSQL port required" >&2; exit 1; }
database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/$database?sslmode=disable"
run_check migrations-up bash scripts/migrate.sh up
run_check migrations-check bash scripts/migrate.sh check
[[ "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" == 16 ]] || { echo "expected 16 migrations" >&2; exit 1; }
run_check document-tests env CAMPUSOS_DOCUMENT_TEST_DATABASE_URL="$database_url" \
  go test -race -count=1 -v -run '^TestDocumentAuthenticated' ./internal/modules/features/personaldocuments
run_check composition go test -race -count=1 ./internal/modules/features/personaldocuments ./internal/server ./internal/modules/core/identity/port
run_check database bash scripts/database-check.sh all
run_check go-all go test ./... -count=1
run_check contracts make contracts-check
run_check architecture make architecture-check
run_check docs-build make docs-build
run_check docs-links make docs-links
run_check line-endings python3 scripts/check-line-endings.py --include-untracked
run_check diff git diff --check
python3 - "$work_dir" "$report" "$pg_version" <<'REPORT'
import hashlib,json,platform,re,subprocess,sys
from datetime import datetime,timezone
from pathlib import Path
work,report=map(Path,sys.argv[1:3])
tests=(work/'document-tests.log').read_text()
required=['TestDocumentAuthenticatedHTTPRealSession', 'TestDocumentAuthenticatedHTTPRejectsRevocationDuringRead', 'TestDocumentAuthenticatedListEmptyAndWholeSnapshot', 'TestDocumentAuthenticatedPostgreSQLCommittedRaces', 'TestDocumentAuthenticatedPostgreSQLFormatsAndLifecycle', 'TestDocumentAuthenticatedPostgreSQLWholeListAndHistoricalVersion', 'TestDocumentAuthenticatedReadLifecycle', 'TestDocumentAuthenticatedReadRejectsCommittedFactsChanges', 'TestDocumentAuthenticatedReadRejectsRevocationAndDrift']
actual=re.findall(r'^--- PASS: (\w+)(?: |$)',tests,re.M)
assert sorted(actual)==sorted(required),'required target tests differ'
assert not re.search(r'^\s*--- (?:FAIL|SKIP): ',tests,re.M),'target tests skipped or failed'
assert 'WARNING: DATA RACE' not in tests
sha=lambda path:hashlib.sha256(Path(path).read_bytes()).hexdigest()
paths=['internal/modules/features/personaldocuments/authorized_read.go', 'internal/modules/features/personaldocuments/authorized_read_test.go', 'internal/modules/features/personaldocuments/authorized_read_http_test.go', 'internal/modules/features/personaldocuments/authorized_read_postgres_test.go', 'internal/modules/features/personaldocuments/handler.go', 'internal/modules/features/personaldocuments/module.go', 'internal/modules/features/personaldocuments/read_port.go', 'internal/modules/features/personaldocuments/repository.go', 'internal/modules/features/personaldocuments/service.go', 'internal/modules/features/personaldocuments/service_test.go', 'internal/modules/features/personaldocuments/model.go', 'internal/server/personal_document_pdf_viewer.go', 'internal/modules/core/identity/port/session_principal_context.go', 'internal/modules/core/identity/port/user_session_principal.go', 'internal/modules/core/identity/port/user_session_principal_test.go', 'internal/modules/core/identity/port/resource_policy.go', 'internal/modules/core/identity/port/resource_policy_test.go', 'internal/modules/core/identity/policy/resource.go', 'internal/modules/core/identity/policy/resource_test.go', 'scripts/v12-02b-document-read-drill.sh']
prefix='docs/项目计划书v1/项目计划v1.2/evidence/'
dependencies=['v12-02a-exit-linux.json','v12-02d-exit-linux.json','v12-02b-owner-list-linux.json']
r={
 'schema':'campusos.v12-02b-document-read-acceptance/v1','generated_at':datetime.now(timezone.utc).isoformat(),
 'stage':'V12-02b','stage_status':'partial','slice_status':'completed',
 'implementation':'passed','automation':'passed','target_environment':'passed',
 'scope':'Private Personal Documents metadata/list, immutable versions, text/preview/download and PDF invocation; current full user Session Principal, document read Policy, selected version/Object facts and final proof recheck. Tombstones expose only owned lifecycle metadata and no byte/version reference.',
 'environment':{'os':platform.system(),'architecture':platform.machine(),'go':subprocess.check_output(['go','version'],text=True).strip(),'postgres_version_num':int(sys.argv[3]),'database':'owned disposable PostgreSQL 16 loopback tmpfs; real document/object repositories plus local bytes'},
 'checks':{name:'passed' for name in required},
 'coverage':{'required_tests':len(required),'real_postgres_tests':3,'formats':5,'postgres_lifecycle_domain_combinations':30,'postgres_committed_races':9,'http':'actual User/Admin domain JWT and Identity SessionService using Memory Identity; missing proof, revoked sessions, same-ID admin, forged owner fields and download body boundaries','consistency':'whole metadata list/member/order binding; document lifecycle version plus complete metadata, immutable selected version and Object metadata rechecked; byte reads on active owner only; fresh complete session proof before return'},
 'source_sha256':{path:sha(path) for path in paths},
 'dependencies':{prefix+name:sha(prefix+name) for name in dependencies},
 'common_gates_status':'passed',
 'logs':{path.stem:{'status':'passed','exit_code':0,'local_log':str(path.relative_to(Path.cwd())),'log_sha256':sha(path)} for path in sorted(work.glob('*.log'))},
 'boundaries':['This document slice alone does not complete V12-02b; author/edit, moderation/MFA, other Feature resources, batch and permission preview remain subsequent paths.','HTTP credentials use Memory Identity; document/Object storage is accepted separately on real PostgreSQL plus local bytes. No full browser, Windows or whole-stage acceptance claimed.','Trusted in-process raw service primitives remain implementation helpers; authenticated HTTP/ReadPort and server composition do not authorize private reads with a bare owner string.','Streaming is authorized immediately before returning a reader; later changes cannot retract already returned bytes.','No new migration. Existing document versions advance on service save/trash/restore; superuser tampering is outside the application trust boundary.'],
}
report.parent.mkdir(parents=True,exist_ok=True)
report.write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n')
print(f'V12-02b document read slice passed: {len(required)} required tests; stage remains partial')
REPORT
