#!/usr/bin/env bash
# Authenticated owner detail acceptance on an owned PostgreSQL 16.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 )); then echo "usage: $0" >&2; exit 2; fi
mkdir -p .cache/v12-02b-owner/tmp
export TMPDIR="$repo_root/.cache/v12-02b-owner/tmp" GOTMPDIR="$repo_root/.cache/v12-02b-owner/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02b-owner/drill.XXXXXX")"
container="campusos-v12-02b-owner-$$"
database="campusos_v12_02b_owner"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
report="docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-owner-detail-linux.json"
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
  echo "V12-02b owner detail: $name"
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
run_check owner-tests env CAMPUSOS_COMMUNITY_TEST_DATABASE_URL="$database_url" CAMPUSOS_THREAD_OWNER_POLICY_OUTPUT="$work_dir/owner-policy.jsonl" \
  go test -race -count=1 -v -run '^Test(ThreadOwner|CheckThreadOwner|OwnerThreadDetail|MemoryOwnerThreadSnapshot|PostgreSQLOwnerThread|PublicThreadDetail(Policy|Memory|Retry|Fails|HTTP)|PublicThreadList(State|Retry|Fails|Sustained|Dispatch|HTTP|Forces)|PublicThreadQuery|PublicList|MemoryThreadListSnapshot)' \
  ./internal/modules/core/identity/port ./internal/modules/core/identity/policy ./internal/modules/core/community ./internal/modules/core/community/repository ./internal/modules/core/community/service ./internal/modules/core/community/handler
run_check owner-contract node scripts/check-v12-thread-owner-read-contract.mjs --go-output "$work_dir/owner-policy.jsonl" --report "$work_dir/owner-policy.json"
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
tests=(work/'owner-tests.log').read_text()
required = ['TestCheckThreadOwnerReadDecisionBinding', 'TestCheckThreadOwnerReadDecisionCurrentFactsAndPrincipal', 'TestCheckThreadOwnerReadDecisionCannotForgeOwnerAllow', 'TestCheckThreadOwnerReadDecisionRejectsInvalidInitialRequest', 'TestThreadOwnerReadPolicyRealmStateMatrix', 'TestThreadOwnerReadPolicyNonOwnerAndMFA', 'TestThreadOwnerReadPolicyRejectsInvalidInputs', 'TestThreadOwnerReadPolicyDoesNotAliasOrCache', 'TestThreadOwnerReadPolicyNativeInterop', 'TestMemoryOwnerThreadSnapshotStatesAndNoViews', 'TestMemoryOwnerThreadSnapshotRejectsOwnerABAAndReplacement', 'TestMemoryOwnerThreadSnapshotRawFactsFailClosed', 'TestMemoryOwnerThreadSnapshotCountersAndContent', 'TestMemoryOwnerThreadSnapshotConcurrentOwnerRevocation', 'TestPostgreSQLOwnerThreadSnapshotInvalidVersionAvoidsQuery', 'TestOwnerThreadDetailStateMatrix', 'TestOwnerThreadDetailMemoryRetryAndRevocation', 'TestOwnerThreadDetailPrincipalAndFailureBoundaries', 'TestOwnerThreadDetailPublicBranchNeverFallsBack', 'TestOwnerThreadDetailPostgreSQLStateMatrix', 'TestOwnerThreadDetailPostgreSQLCommittedRaces', 'TestOwnerThreadDetailPostgreSQLFailureBoundaries', 'TestOwnerThreadDetailPostgreSQLCountersAndVersion', 'TestOwnerThreadDetailPostgreSQLPublicBranchNeverFallsBack', 'TestThreadOwnerHTTPVerifiedSessionAndDomain', 'TestThreadOwnerHTTPRejectsIncompleteHostProof', 'TestThreadOwnerHTTPRechecksContentAndSession', 'TestThreadOwnerModuleSessionProofWiring', 'TestPublicThreadDetailPolicyStateMatrix', 'TestPublicThreadDetailMemoryRevocation', 'TestPublicThreadDetailRetryReloadsFactsAndDecision', 'TestPublicThreadDetailFailsClosed', 'TestPublicThreadDetailHTTPErrorBoundary', 'TestPublicThreadListStateMatrixAndPagination', 'TestPublicThreadListRetryWholeSnapshot', 'TestPublicThreadListFailsClosed', 'TestPublicThreadListSustainedConflictAndCancellation', 'TestPublicThreadListDispatchAndBoundaries', 'TestPublicThreadListForcesCanonicalVisibility', 'TestPublicThreadListHTTPErrorBoundary', 'TestPublicThreadListHTTPEmptyPageKeepsPublicCount', 'TestPublicThreadQueryUsesCanonicalList', 'TestPublicThreadQueryRevocationChangesWholePage', 'TestPublicThreadQueryPreservesAuthorList', 'TestPublicThreadQueryUnavailable', 'TestPublicListRechecksPublicationAfterAnotherServiceChangesIt', 'TestMemoryThreadListSnapshotPaginationAndRevocation', 'TestMemoryThreadListSnapshotPaginationBoundaries', 'TestMemoryThreadListSnapshotConcurrentCountAndRows']
paths = ['internal/modules/core/identity/port/thread_owner_read_policy_test.go', 'internal/modules/core/identity/policy/thread_owner_read_test.go', 'internal/modules/core/identity/policy/thread_owner_read_interop_test.go', 'internal/modules/core/community/repository/thread_owner_snapshot_test.go', 'internal/modules/core/community/service/thread_owner_read_test.go', 'internal/modules/core/community/service/thread_owner_read_postgres_test.go', 'internal/modules/core/community/handler/thread_owner_read_test.go', 'internal/modules/core/community/thread_owner_read_test.go', 'Makefile', 'internal/modules/core/identity/port/thread_owner_read_policy.go', 'internal/modules/core/identity/policy/thread_owner_read.go', 'internal/modules/core/community/service/thread_owner_read.go', 'internal/modules/core/community/service/thread_service.go', 'internal/modules/core/community/repository/thread_repository.go', 'internal/modules/core/community/repository/pg_thread_repository.go', 'internal/modules/core/community/handler/thread_handler.go', 'internal/modules/core/community/handler/thread_handler_test.go', 'internal/modules/core/community/module.go', 'docs/api/policy-thread-owner-read-v1.schema.json', 'docs/api/policy-thread-owner-read-decision-v1.schema.json', 'docs/api/policy-thread-owner-read-v1.errors.json', 'scripts/check-v12-thread-owner-read-contract.mjs', 'scripts/v12-02b-owner-detail-drill.sh', 'migrations/000016_v1_2_thread_facts_version.up.sql']
for name in required:
 assert re.search(r'^--- PASS: '+name+r'(?: |$)',tests,re.M),name
assert not re.search(r'^\s*--- (?:FAIL|SKIP): ',tests,re.M),'target tests skipped or failed'
sha=lambda path:hashlib.sha256(Path(path).read_bytes()).hexdigest()
oracle=json.loads((work/'owner-policy.json').read_text())
assert oracle['passed'] and oracle['native_go_decisions']==216
prefix='docs/项目计划书v1/项目计划v1.2/evidence/'
predecessors=['v12-02a-exit-linux.json','v12-02d-exit-linux.json','v12-02b-public-detail-linux.json','v12-02b-public-list-linux.json']
r={
 'schema':'campusos.v12-02b-owner-detail-acceptance/v1','generated_at':datetime.now(timezone.utc).isoformat(),
 'stage':'V12-02b','stage_status':'partial','slice_status':'completed',
 'implementation':'passed','automation':'passed','target_environment':'passed',
 'scope':'Authenticated HTTP thread /me owner detail: independent named Policy, current user-session proofs, raw facts, atomic owner/version snapshot and final complete principal/fact binding.',
 'environment':{'os':platform.system(),'architecture':platform.machine(),'go':subprocess.check_output(['go','version'],text=True).strip(),
   'postgres_version_num':int(sys.argv[3]),'database':'owned disposable PostgreSQL 16 container; loopback only'},
 'checks':{name:'passed' for name in required},
 'coverage':{'state_combinations_per_adapter':36,'owner_policy_realms_and_states':216,'policy_allow_combinations':24,
   'postgres_committed_races':10,'http':'real JWT and Identity SessionService; same-ID admin, revoked/account-changed sessions, forged owner fields, proof faults, safe 500/409',
   'policy_oracle':{'malformed_cases':len(oracle['malformed_cases']),'binding_checks':len(oracle['binding_checks']),'native_go_decisions':oracle['native_go_decisions']},
   'adapters':['memory','postgres'],'consistency':'raw content/facts in one owner+version-conditional read; current session checked before and after, new decisions on retry; no private view increment'},
 'source_sha256':{path:sha(path) for path in paths},
 'dependencies':{prefix+name:sha(prefix+name) for name in predecessors},
 'common_gates_status':'passed',
 'logs':{path.stem:{'status':'passed','exit_code':0,'local_log':str(path.relative_to(Path.cwd())),'log_sha256':sha(path)} for path in sorted(work.glob('*.log'))},
 'artifacts':{str(path.relative_to(Path.cwd())):sha(path) for path in [work/'owner-policy.jsonl',work/'owner-policy.json']},
 'boundaries':[
   'V12-02b remains partial; author lists, private reads in typed Features, moderation/MFA Principal, batch authorization and effective-permission preview await later slices.',
   'Legacy GetThreadForViewer bare-ID private branch remains source compatible and is not accepted as this authenticated owner route.',
   'Public-selected /me stays on public predicate; revocation never falls back to an owner exception.',
   'No new board lifecycle permission, plugin/integration private-read grant, runtime JSON parser or public Policy endpoint is introduced.',
   'Resource bytes linearize at the final owner/version snapshot; credential is freshly checked before/after that read, later changes cannot retract returned bytes.',
   'HTTP real credentials use Memory Identity adapters; resource storage is separately accepted against real PostgreSQL. Full server/browser/Windows/capacity/whole-stage acceptance is not claimed.',
   'No new migration; existing 000016 provides content/owner/state/order ABA-safe versions.',
 ],
}
report.parent.mkdir(parents=True,exist_ok=True)
report.write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n')
print(f'V12-02b owner detail slice passed: {len(required)} required tests; stage remains partial')
REPORT
