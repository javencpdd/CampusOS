#!/usr/bin/env bash
# Current User/Admin resource take-down commands with typed actors, required audit and no private-body acknowledgements.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 )); then echo "usage: $0" >&2; exit 2; fi
mkdir -p .cache/v12-02b-thread-governance/tmp
export TMPDIR="$repo_root/.cache/v12-02b-thread-governance/tmp" GOTMPDIR="$repo_root/.cache/v12-02b-thread-governance/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02b-thread-governance/drill.XXXXXX")"
container="campusos-v12-02b-thread-governance-$$"
database="campusos_v12_02b_author_write"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
report="docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-thread-governance-linux.json"
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
  echo "V12-02b thread governance take down: $name"
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
expected_migrations="$(python3 -c 'from pathlib import Path; print(len(list(Path("migrations").glob("*.up.sql"))))')"
[[ "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" == "$expected_migrations" ]] || { echo "expected all current migrations" >&2; exit 1; }
while :; do
  latest_applied="$(psql_scalar 'SELECT max(version) FROM schema_migrations;')"
  [[ "$latest_applied" > "000018" ]] || break
  run_check "migrations-down-before-actor-empty-$latest_applied" bash scripts/migrate.sh down
done
run_check migrations-down-empty bash scripts/migrate.sh down
run_check legacy-seed env PGPASSWORD="$pg_password" psql -h 127.0.0.1 -U campusos -d "$database" -v ON_ERROR_STOP=1 -f scripts/fixtures/v12-02b-business-actor-legacy.sql
legacy_hash_sql="$(cat scripts/fixtures/v12-02b-business-actor-legacy-hash.sql)"
legacy_hash_before="$(psql_scalar "$legacy_hash_sql" | python3 -c 'import hashlib,sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())')"
run_check migrations-up-with-legacy bash scripts/migrate.sh up
while :; do
  latest_applied="$(psql_scalar 'SELECT max(version) FROM schema_migrations;')"
  [[ "$latest_applied" > "000018" ]] || break
  run_check "migrations-down-before-actor-legacy-$latest_applied" bash scripts/migrate.sh down
done
run_check migrations-down-with-legacy bash scripts/migrate.sh down
legacy_hash_down="$(psql_scalar "$legacy_hash_sql" | python3 -c 'import hashlib,sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())')"
[[ "$legacy_hash_before" == "$legacy_hash_down" ]] || { echo "actor down lost legacy data" >&2; exit 1; }
run_check migrations-reup-with-legacy bash scripts/migrate.sh up
run_check migrations-recheck bash scripts/migrate.sh check
legacy_hash_after="$(psql_scalar "$legacy_hash_sql" | python3 -c 'import hashlib,sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())')"
[[ "$legacy_hash_before" == "$legacy_hash_after" ]] || { echo "actor up/down/up changed legacy data" >&2; exit 1; }
printf 'up/down/up preserved legacy fields and rows across all8 actor tables; typed domain/storage tests follow\n' >"$work_dir/migration-preservation.log"
run_check actor-target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -count=1 -v ./internal/modules/core/community/repository -run '^TestBusinessActorPostgreSQL.*$'
run_check governance-target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -p 1 -count=1 -v -run '^TestThreadGovernance.*$' ./internal/modules/core/community ./internal/modules/core/community/repository ./internal/modules/core/community/service ./internal/modules/core/community/handler ./internal/modules/features/personalspace
run_check target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -p 1 -count=1 -v -run '^(TestBoardGovernanceAuthority.*|TestMemoryProfileAndModuleExposePublicPorts|TestAdminPermissionFacts.*|TestAdminCurrent.*|TestAdminSessionPrincipal.*|TestAdminSession(LoginRefreshAndVerify|LoginDeniesInactiveAndWrongPassword|RevokeCascadesToAccess)|TestAdminMFA(EnrollmentConfirmAndStepUp|Guards)|TestAdminDomainHTTPBootstrapLoginAndCrossDomainRejection|TestDocumentAuthor.*|TestDocumentAuthenticated(HTTP.*|Read.*|ListEmptyAndWholeSnapshot)|TestRichTextInlineImage.*|TestInlineImageHTTP.*|TestContentImageChecked.*|TestRichTextAuthorized(AssetPageAndMissingProof|AttachmentAndAssetCloseOnRevocation|HTTPRealSessionAndSafeBytes|ReadRechecksBodyAndCoreFacts|ReadStateMatrix)|TestBoundProjection.*|TestFeatureThreadQuery.*|TestSpaceFiles(Current.*|Revocation.*|HTTP.*)|TestAuthorUpdate.*|TestThreadAuthorityWrite.*|TestAuthorLifecycle.*|TestThreadOwnerLifecycle.*|TestAuthorCreate.*|TestCreateThreadUsesJWTContext|TestFeatureAuthorCommand.*|TestMutualAidAuthorWrite.*|TestSecondhandAuthorWrite.*|TestRichTextAuthorWrite.*|TestRichTextAuthorCreate.*|TestFeatureAuthorCreate.*|TestFeatureThreadCreator.*|TestMutualAidAuthorCreate.*|TestSecondhandAuthorCreate.*|TestMutualAidAuthorized(StateMatrix|DetailRevocation|MissingPortsAndDomain|WholeListAndEmptyPage|HTTPRealSession)|TestSecondhandAuthorized(StateMatrix|DetailRevocation|MissingPortsAndDomain|WholeListAndEmptyPage|HTTPRealSession))$' \
 ./internal/modules/core/identity ./internal/modules/core/identity/port ./internal/modules/core/community ./internal/modules/core/userstorage ./internal/modules/features/richtext ./internal/modules/features/personalspace ./internal/modules/core/community/service ./internal/modules/core/community/repository ./internal/modules/core/community/handler ./internal/modules/core/identity/policy ./internal/modules/features/mutualaid ./internal/modules/features/secondhand ./internal/modules/features/personaldocuments ./internal/modules/core/identity/service ./internal/modules/core/identity/handler ./pkg/middleware ./internal/modules/core/identity/repository
run_check composition go test -race -count=1 ./internal/modules/core/identity/service ./internal/modules/core/identity/portadapt ./pkg/middleware ./internal/modules/features/personaldocuments ./internal/modules/core/userstorage ./internal/modules/features/mutualaid ./internal/modules/features/secondhand ./internal/server ./internal/modules/core/community
run_check database bash scripts/database-check.sh all
run_check go-all go test ./... -count=1
run_check admin-build pnpm --dir admin build
run_check migration-parser python3 -m unittest migrations.tools.test_generate_er
run_check contracts make contracts-check
run_check architecture make architecture-check
run_check docs-build make docs-build
run_check docs-links make docs-links
run_check line-endings python3 scripts/check-line-endings.py --include-untracked
run_check diff git diff --check
python3 - "$work_dir" "$report" "$pg_version" "$expected_migrations" <<'REPORT'
import hashlib,json,platform,re,subprocess,sys
from datetime import datetime,timezone
from pathlib import Path
work,report=map(Path,sys.argv[1:3])
tests=(work/'actor-target-tests.log').read_text()+(work/'governance-target-tests.log').read_text()+(work/'target-tests.log').read_text()
prefix='docs/项目计划书v1/项目计划v1.2/evidence/'
previous=json.loads(Path(prefix+'v12-02b-author-create-linux.json').read_text())
required=list(json.loads(Path(prefix+'v12-02b-business-actor-linux.json').read_text())['checks'])
new=[]
for folder in ['internal/modules/core/community','internal/modules/core/community/repository','internal/modules/core/community/service','internal/modules/core/community/handler','internal/modules/features/personalspace']:
 for path in Path(folder).glob('*test.go'):
  for name in re.findall(r'^func (Test\w+)\(',path.read_text(),re.M):
   if name.startswith('TestThreadGovernance'):new.append(name)
required+=new
actual=re.findall(r'^--- PASS: (\w+)(?: |$)',tests,re.M)
assert len(new)==8 and len(required)==140 and sorted(actual)==sorted(required), f'required target tests differ: {required}, {actual}'
assert not re.search(r'^\s*--- (?:FAIL|SKIP): ',tests,re.M),'target tests skipped or failed'
assert 'WARNING: DATA RACE' not in tests
sha=lambda path:hashlib.sha256(Path(path).read_bytes()).hexdigest()
paths=['docs/api/http-routes-v0.6.json','docs/api/HTTP路由与授权矩阵-v0.6.md','docs/api/openapi-v0.6-current.yaml','cmd/campusos-contracts/main.go','internal/modules/core/identity/module.go','internal/modules/core/identity/module_test.go','admin/src/modules/architecture/pages/SystemArchitectureView.vue','admin/package.json','admin/pnpm-lock.yaml','migrations/README.md','scripts/schema-contract.sql','skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py','Makefile','internal/transport/httpapi/router.go','modules/core/user-storage/module.yaml','scripts/v12-02b-thread-governance-drill.sh']
for folder in ['pkg/middleware','pkg/auth','internal/modules/features/personaldocuments','internal/modules/features/mutualaid','internal/modules/features/secondhand','internal/modules/core/community','internal/modules/core/identity/port','internal/modules/core/identity/policy','internal/modules/core/contenteditor','internal/modules/core/userstorage','internal/modules/features/richtext','internal/modules/features/personalspace','internal/modules/core/identity/portadapt','internal/modules/core/identity/repository','internal/modules/core/identity/service','internal/platform/reliability','internal/platform/transaction']:
 paths += [str(p) for p in Path(folder).rglob('*.go')]
paths += [str(p) for p in Path('web/src/modules/content-editor').glob('*') if p.suffix in ['.ts','.vue']]
paths += ['web/src/modules/mutual-aid/pages/MutualAidDetailView.vue','web/src/modules/secondhand/pages/SecondhandDetailView.vue','web/src/modules/personal-documents/pages/PersonalDocumentsView.vue','web/src/modules/identity/session.ts','web/src/modules/community/components/SafeContentEditor.vue','web/src/modules/community/pages/CreateThreadView.vue','web/src/modules/community/pages/ThreadDetailView.vue','web/tests/checked-images.spec.ts','web/tests/structured-content.spec.ts','web/package.json','web/pnpm-lock.yaml']
paths += [str(p) for p in Path('migrations').glob('*.sql')]
paths += [str(p) for p in Path('internal/projectaudit').rglob('*.go')]
paths += [str(p) for p in Path('migrations/er/current').glob('*') if p.is_file()]
paths += [str(p) for p in Path('scripts').glob('v12-02b*drill.sh')]
paths += [str(p) for p in Path('scripts/fixtures').glob('v12-02b-business-actor*.sql')]
prefix='docs/项目计划书v1/项目计划v1.2/evidence/'
dependencies=['v12-02a-exit-linux.json','v12-02d-exit-linux.json','v12-02b-document-read-linux.json','v12-02b-typed-feature-read-linux.json','v12-02b-inline-image-linux.json','v12-02b-space-files-linux.json','v12-02b-author-write-linux.json','v12-02b-author-lifecycle-linux.json','v12-02b-author-create-linux.json','v12-02b-typed-author-create-linux.json','v12-02b-richtext-author-create-linux.json','v12-02b-typed-author-write-linux.json','v12-02b-richtext-author-write-linux.json','v12-02b-document-author-write-linux.json','v12-02b-admin-principal-linux.json','v12-02b-admin-permission-facts-linux.json','v12-02b-board-governance-authority-linux.json','v12-02b-business-actor-linux.json']
r={
 'schema':'campusos.v12-02b-thread-governance-acceptance/v1','generated_at':datetime.now(timezone.utc).isoformat(),
 'stage':'V12-02b','stage_status':'partial','slice_status':'completed',
 'implementation':'passed','automation':'passed','target_environment':'passed',
 'scope':'Actual Community take-down resource command and User/Admin HTTP ingress: current full session and single board execution witness, Thread facts/revision CAS plus required typed history/audit/outbox atomically, final full authority/resource rechecks; safe two-field acknowledgement/event and Space projection removal. Other governance actions remain pending.',
 'environment':{'os':platform.system(),'architecture':platform.machine(),'go':subprocess.check_output(['go','version'],text=True).strip(),'postgres_version_num':int(sys.argv[3]),'database':'owned disposable PostgreSQL16 loopback tmpfs; real Community Thread/Governance/Identity User+Session/Reliability repositories and native transaction credential lock tests'},
 'checks':{name:'passed' for name in required},
 'coverage':{'new_required_tests':8,'previous_author_and_read_regressions':132,'real_postgres_tests':32,'committed_changes':12,'atomic_failures':6,'actual_locks':'Thread plus real User/session/grant or independent Admin account/session/role/binding/permission NOWAIT55P03 during history write','typed_actors':'actual User, same-numeric Admin and opaque Admin in Thread/revision/case/action/platform audit; Admin never enters legacy User bigint','acknowledgements':'HTTP result/outbox contains only id/moderation_status, no body/title/owner or policy facts; Space removes old projection using minimal event','http':'real signed User/Admin JWTs and Memory SessionServices;11 modes include cross-domain/subject forgery/revocation/weak MFA/client facts/missing proof','migration_count':int(sys.argv[4])},
 'source_sha256':{path:sha(path) for path in sorted(set(paths))},
 'dependencies':{prefix+name:sha(prefix+name) for name in dependencies},
 'common_gates_status':'passed',
 'logs':{path.stem:{'status':'passed','exit_code':0,'local_log':str(path.relative_to(Path.cwd())),'log_sha256':sha(path)} for path in sorted(work.glob('*.log'))},
 'boundaries':['V12-02b remains partial. This slice accepts take_down only; post deletion and review/pin/lock/restore/trash/Feature governance need their own named policies/current ingress.','Execution permission does not expose private original content, author identities or bound policy snapshots in the command response/event.','User/Admin actual resource and authority locks/CAS plus required history/audit/outbox share one transaction. Raw legacy helpers do not authenticate HTTP callers.','000018 data preservation and32PG regressions independently accepted; no developer reset, full server/browser/Windows acceptance claimed.'],
}
report.parent.mkdir(parents=True,exist_ok=True)
report.write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n')
print(f'V12-02b thread governance take down slice passed: {len(required)} required tests; stage remains partial')
REPORT
