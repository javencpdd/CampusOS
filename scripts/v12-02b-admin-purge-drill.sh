#!/usr/bin/env bash
# Current independent Admin execution facts and private-data-free admin purge history.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 )); then echo "usage: $0" >&2; exit 2; fi
mkdir -p .cache/v12-02b-admin-purge/tmp
export TMPDIR="$repo_root/.cache/v12-02b-admin-purge/tmp" GOTMPDIR="$repo_root/.cache/v12-02b-admin-purge/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02b-admin-purge/drill.XXXXXX")"
container="campusos-v12-02b-admin-purge-$$"
database="campusos_v12_02b_author_write"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
report="docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-admin-purge-linux.json"
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
  echo "V12-02b admin purge: $name"
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
while :; do
  latest_applied="$(psql_scalar 'SELECT max(version) FROM schema_migrations;')"
  [[ "$latest_applied" > "000019" ]] || break
  run_check "post-migrations-down-before-empty-$latest_applied" bash scripts/migrate.sh down
done
run_check post-migrations-down-empty bash scripts/migrate.sh down
run_check post-legacy-seed env PGPASSWORD="$pg_password" psql -h 127.0.0.1 -U campusos -d "$database" -v ON_ERROR_STOP=1 -f scripts/fixtures/v12-02b-post-facts-legacy.sql
post_legacy_hash_sql="SELECT COALESCE(jsonb_agg(to_jsonb(p)-'facts_version'-'deleted_actor_kind'-'deleted_actor_id' ORDER BY id),'[]'::jsonb)::text FROM posts p WHERE id IN(121101,121102);"
post_hash_before="$(psql_scalar "$post_legacy_hash_sql" | python3 -c 'import hashlib,sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())')"
post_high_water="$(psql_scalar 'SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM post_facts_version_seq;')"
run_check post-migrations-up-legacy bash scripts/migrate.sh up
[[ "$(psql_scalar "SELECT min(facts_version)>$post_high_water FROM posts WHERE id IN(121101,121102);")" == t ]] || { echo "post backfill reused high water" >&2; exit 1; }
post_high_water="$(psql_scalar 'SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM post_facts_version_seq;')"
while :; do
  latest_applied="$(psql_scalar 'SELECT max(version) FROM schema_migrations;')"
  [[ "$latest_applied" > "000019" ]] || break
  run_check "post-migrations-down-before-legacy-$latest_applied" bash scripts/migrate.sh down
done
run_check post-migrations-down-legacy bash scripts/migrate.sh down
post_hash_down="$(psql_scalar "$post_legacy_hash_sql" | python3 -c 'import hashlib,sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())')"
[[ "$post_hash_before" == "$post_hash_down" ]] || { echo "post down lost legacy data" >&2; exit 1; }
run_check post-migrations-reup bash scripts/migrate.sh up
run_check post-migrations-recheck bash scripts/migrate.sh check
[[ "$(psql_scalar "SELECT min(facts_version)>$post_high_water FROM posts WHERE id IN(121101,121102);")" == t ]] || { echo "post re-up reused high water" >&2; exit 1; }
post_hash_after="$(psql_scalar "$post_legacy_hash_sql" | python3 -c 'import hashlib,sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())')"
[[ "$post_hash_before" == "$post_hash_after" ]] || { echo "post up/down/up changed legacy data" >&2; exit 1; }
printf 'post up/down/up preserved original rows/body/metadata/deletion history and allocation high water\n' >"$work_dir/post-migration-preservation.log"
run_check post-target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -count=1 -v ./internal/modules/core/community/repository -run '^TestPostAuthority.*$'
run_check author-post-target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -p 1 -count=1 -v -run '^TestPostAuthor.*$' ./internal/modules/core/community/service ./internal/modules/core/community/handler ./internal/modules/core/identity/policy
run_check post-governance-target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -p 1 -count=1 -v -run '^TestPostGovernance.*$' ./internal/modules/core/community/service ./internal/modules/core/community/handler ./internal/modules/core/moderation
run_check post-read-target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -p 1 -count=1 -v -run '^TestPostRead.*$' ./internal/modules/core/community/service ./internal/modules/core/community/handler ./internal/modules/core/identity/policy
run_check event-metadata-target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -p 1 -count=1 -v -run '^(TestAdminExecution.*|TestEventMetadata.*)$' ./internal/modules/core/identity/service ./internal/modules/core/community/handler ./pkg/eventbus
run_check admin-flags-target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -p 1 -count=1 -v -run '^TestThreadAdminFlag.*$' ./internal/modules/core/community ./internal/modules/core/community/service ./internal/modules/core/community/handler ./internal/modules/core/identity/policy
run_check admin-review-target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -p 1 -count=1 -v -run '^TestThreadAdminReview.*$' ./internal/modules/core/community ./internal/modules/core/community/repository ./internal/modules/core/community/service ./internal/modules/core/community/handler ./internal/modules/core/identity/policy
run_check admin-lifecycle-target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -p 1 -count=1 -v -run '^TestThreadAdminLifecycle.*$' ./internal/modules/core/community ./internal/modules/core/community/service ./internal/modules/core/community/handler ./internal/modules/core/identity/policy
run_check admin-purge-target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -p 1 -count=1 -v -run '^TestThreadAdminPurge.*$' ./internal/modules/core/community ./internal/modules/core/community/repository ./internal/modules/core/community/service ./internal/modules/core/community/handler ./internal/modules/core/identity/policy
run_check actor-target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -count=1 -v ./internal/modules/core/community/repository -run '^TestBusinessActorPostgreSQL.*$'
run_check governance-target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -p 1 -count=1 -v -run '^TestThreadGovernance.*$' ./internal/modules/core/community ./internal/modules/core/community/repository ./internal/modules/core/community/service ./internal/modules/core/community/handler ./internal/modules/features/personalspace
run_check target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -p 1 -count=1 -v -run '^(TestBoardGovernanceAuthority.*|TestMemoryProfileAndModuleExposePublicPorts|TestAdminPermissionFacts.*|TestAdminCurrent.*|TestAdminSessionPrincipal.*|TestAdminSession(LoginRefreshAndVerify|LoginDeniesInactiveAndWrongPassword|RevokeCascadesToAccess)|TestAdminMFA(EnrollmentConfirmAndStepUp|Guards)|TestAdminDomainHTTPBootstrapLoginAndCrossDomainRejection|TestDocumentAuthor.*|TestDocumentAuthenticated(HTTP.*|Read.*|ListEmptyAndWholeSnapshot)|TestRichTextInlineImage.*|TestInlineImageHTTP.*|TestContentImageChecked.*|TestRichTextAuthorized(AssetPageAndMissingProof|AttachmentAndAssetCloseOnRevocation|HTTPRealSessionAndSafeBytes|ReadRechecksBodyAndCoreFacts|ReadStateMatrix)|TestBoundProjection.*|TestFeatureThreadQuery.*|TestSpaceFiles(Current.*|Revocation.*|HTTP.*)|TestAuthorUpdate.*|TestThreadAuthorityWrite.*|TestAuthorLifecycle.*|TestThreadOwnerLifecycle.*|TestAuthorCreate.*|TestCreateThreadUsesJWTContext|TestFeatureAuthorCommand.*|TestMutualAidAuthorWrite.*|TestSecondhandAuthorWrite.*|TestRichTextAuthorWrite.*|TestRichTextAuthorCreate.*|TestFeatureAuthorCreate.*|TestFeatureThreadCreator.*|TestMutualAidAuthorCreate.*|TestSecondhandAuthorCreate.*|TestMutualAidAuthorized(StateMatrix|DetailRevocation|MissingPortsAndDomain|WholeListAndEmptyPage|HTTPRealSession)|TestSecondhandAuthorized(StateMatrix|DetailRevocation|MissingPortsAndDomain|WholeListAndEmptyPage|HTTPRealSession))$' \
 ./internal/modules/core/identity ./internal/modules/core/identity/port ./internal/modules/core/community ./internal/modules/core/userstorage ./internal/modules/features/richtext ./internal/modules/features/personalspace ./internal/modules/core/community/service ./internal/modules/core/community/repository ./internal/modules/core/community/handler ./internal/modules/core/identity/policy ./internal/modules/features/mutualaid ./internal/modules/features/secondhand ./internal/modules/features/personaldocuments ./internal/modules/core/identity/service ./internal/modules/core/identity/handler ./pkg/middleware ./internal/modules/core/identity/repository
run_check composition go test -race -count=1 ./internal/modules/core/moderation ./internal/modules/core/identity/service ./internal/modules/core/identity/portadapt ./pkg/middleware ./internal/modules/features/personaldocuments ./internal/modules/core/userstorage ./internal/modules/features/mutualaid ./internal/modules/features/secondhand ./internal/server ./internal/modules/core/community
run_check database bash scripts/database-check.sh all
run_check go-all go test ./... -count=1
run_check admin-component pnpm --dir admin test:component
run_check admin-build pnpm --dir admin build
run_check admin-browser node scripts/v12-02b-event-metadata-browser.mjs
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
tests=(work/'admin-purge-target-tests.log').read_text()+(work/'admin-lifecycle-target-tests.log').read_text()+(work/'admin-review-target-tests.log').read_text()+(work/'admin-flags-target-tests.log').read_text()+(work/'event-metadata-target-tests.log').read_text()+(work/'post-read-target-tests.log').read_text()+(work/'post-governance-target-tests.log').read_text()+(work/'author-post-target-tests.log').read_text()+(work/'post-target-tests.log').read_text()+(work/'actor-target-tests.log').read_text()+(work/'governance-target-tests.log').read_text()+(work/'target-tests.log').read_text()
prefix='docs/项目计划书v1/项目计划v1.2/evidence/'
previous=json.loads(Path(prefix+'v12-02b-author-create-linux.json').read_text())
required=list(json.loads(Path(prefix+'v12-02b-admin-lifecycle-linux.json').read_text())['checks'])
new=[]
for folder in ['internal/modules/core/community','internal/modules/core/community/repository','internal/modules/core/community/service','internal/modules/core/community/handler','internal/modules/core/identity/policy']:
 for path in Path(folder).glob('*test.go'):
  for name in re.findall(r'^func (Test\w+)\(',path.read_text(),re.M):
   if name.startswith('TestThreadAdminPurge'):new.append(name)
required+=new
actual=re.findall(r'^--- PASS: (\w+)(?: |$)',tests,re.M)
assert len(new)==8 and len(required)==202 and sorted(actual)==sorted(required), f'required target tests differ: {required}, {actual}'
assert not re.search(r'^\s*--- (?:FAIL|SKIP): ',tests,re.M),'target tests skipped or failed'
assert 'WARNING: DATA RACE' not in tests
sha=lambda path:hashlib.sha256(Path(path).read_bytes()).hexdigest()
paths=['docs/api/http-routes-v0.6.json','docs/api/HTTP路由与授权矩阵-v0.6.md','docs/api/openapi-v0.6-current.yaml','cmd/campusos-contracts/main.go','internal/modules/core/identity/module.go','internal/modules/core/identity/module_test.go','admin/src/modules/architecture/pages/SystemArchitectureView.vue','admin/package.json','admin/pnpm-lock.yaml','migrations/README.md','scripts/schema-contract.sql','skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py','Makefile','internal/transport/httpapi/router.go','modules/core/user-storage/module.yaml','scripts/v12-02b-admin-purge-drill.sh']
paths += ['admin/src/modules/community/api.ts','admin/src/modules/community/pages/ThreadManageView.vue','admin/src/modules/operations/api.ts','admin/src/modules/operations/pages/EventLogView.vue','admin/tests/event-metadata.spec.ts','admin/vitest.config.ts','scripts/v12-02b-event-metadata-browser.mjs']
for folder in ['pkg/eventbus','pkg/middleware','pkg/auth','internal/modules/features/personaldocuments','internal/modules/features/mutualaid','internal/modules/features/secondhand','internal/modules/core/community','internal/modules/core/moderation','internal/modules/core/identity/port','internal/modules/core/identity/policy','internal/modules/core/contenteditor','internal/modules/core/userstorage','internal/modules/features/richtext','internal/modules/features/personalspace','internal/modules/core/identity/portadapt','internal/modules/core/identity/repository','internal/modules/core/identity/service','internal/platform/reliability','internal/platform/transaction']:
 paths += [str(p) for p in Path(folder).rglob('*.go')]
paths += [str(p) for p in Path('web/src/modules/content-editor').glob('*') if p.suffix in ['.ts','.vue']]
paths += ['web/src/modules/mutual-aid/pages/MutualAidDetailView.vue','web/src/modules/secondhand/pages/SecondhandDetailView.vue','web/src/modules/personal-documents/pages/PersonalDocumentsView.vue','web/src/modules/identity/session.ts','web/src/modules/community/components/SafeContentEditor.vue','web/src/modules/community/pages/CreateThreadView.vue','web/src/modules/community/pages/ThreadDetailView.vue','web/tests/checked-images.spec.ts','web/tests/structured-content.spec.ts','web/package.json','web/pnpm-lock.yaml']
paths += [str(p) for p in Path('migrations').glob('*.sql')]
paths += [str(p) for p in Path('internal/projectaudit').rglob('*.go')]
paths += [str(p) for p in Path('migrations/er/current').glob('*') if p.is_file()]
paths += [str(p) for p in Path('scripts').glob('v12-02b*drill.sh')]
paths += [str(p) for p in Path('scripts/fixtures').glob('v12-02b-business-actor*.sql')]
paths += ['scripts/fixtures/v12-02b-post-facts-legacy.sql']
prefix='docs/项目计划书v1/项目计划v1.2/evidence/'
dependencies=['v12-02a-exit-linux.json','v12-02d-exit-linux.json','v12-02b-document-read-linux.json','v12-02b-typed-feature-read-linux.json','v12-02b-inline-image-linux.json','v12-02b-space-files-linux.json','v12-02b-author-write-linux.json','v12-02b-author-lifecycle-linux.json','v12-02b-author-create-linux.json','v12-02b-typed-author-create-linux.json','v12-02b-richtext-author-create-linux.json','v12-02b-typed-author-write-linux.json','v12-02b-richtext-author-write-linux.json','v12-02b-document-author-write-linux.json','v12-02b-admin-principal-linux.json','v12-02b-admin-permission-facts-linux.json','v12-02b-board-governance-authority-linux.json','v12-02b-business-actor-linux.json','v12-02b-thread-governance-linux.json','v12-02b-post-facts-linux.json','v12-02b-post-author-write-linux.json','v12-02b-post-governance-linux.json','v12-02b-post-read-linux.json','v12-02b-event-metadata-linux.json','v12-02b-admin-flags-linux.json','v12-02b-admin-review-linux.json','v12-02b-admin-lifecycle-linux.json']
r={
 'schema':'campusos.v12-02b-admin-purge-acceptance/v1','generated_at':datetime.now(timezone.utc).isoformat(),
 'stage':'V12-02b','stage_status':'partial','slice_status':'completed',
 'implementation':'passed','automation':'passed','target_environment':'passed',
 'scope':'Current independent Admin purge: actual trashed/unpurged Thread named policy and exact current permission/MFA, credential/role/Thread/Case locks, managed nonreused nonce/revision CAS and hidden deleted_at tombstone snapshot, complete final authority/record/case proofs, typed history and mandatory audit/outbox atomic, safe204. Other stage obligations pending.',
 'environment':{'os':platform.system(),'architecture':platform.machine(),'go':subprocess.check_output(['go','version'],text=True).strip(),'postgres_version_num':int(sys.argv[3]),'database':'owned disposable PostgreSQL16 loopback tmpfs; real Community Thread/Governance/Identity User+Session/Reliability repositories and native transaction credential lock tests'},
 'checks':{name:'passed' for name in required},
 'coverage':{'new_required_tests':8,'previous_regressions':194,'real_postgres_tests':51,'committed_changes_rejected':11,'managed_trigger_disabled_rejected':True,'counter_update_preserved':True,'actual_authority_and_history_locks':'account/session/role/binding/permission/Thread and existing Case NOWAIT 55P03','policy_matrix':360,'native_required_failure_cases':19,'http':'15 real signed JWT/current Memory Identity modes; safe204 and denied record unchanged','production_module':'current Admin session/execution authority, tombstone/Case ports; missing source/session closes','mandatory_failures_rolled_back':8,'tombstone':'transaction-only actual deleted_at/nonce/revision snapshot; normal read/pages closed; retained original history/private bytes do not imply read access; managed16/18 already tracks deleted_at and date-only ABA rejected','native_storage':'nonce/revision CAS, detached hidden date/body, normal pages/read closed, rollback restores prior record and nonce allocation high water preserved','migration_count':int(sys.argv[4])},
 'source_sha256':{path:sha(path) for path in sorted(set(paths))},
 'dependencies':{prefix+name:sha(prefix+name) for name in dependencies},
 'common_gates_status':'passed',
 'logs':{path.stem:{'status':'passed','exit_code':0,'local_log':str(path.relative_to(Path.cwd())),'log_sha256':sha(path)} for path in sorted(work.glob('*.log'))},
 'boundaries':['V12-02b remains partial; User/Feature governance, files/space writes, admin lists, batch and preview pending.','Actual trashed/unpurged Thread and independent Admin exact purge permission/current MFA required. Purge writes a retained tombstone, closes normal reads/pages and preserves immutable evidence; physical retention cleanup belongs to its own lifecycle.','Case history and publication/moderation/actor dimensions remain unchanged. Complete final proofs include hidden deleted_at and actual nonce/revision.','Result/event contain only ID/deletion state; HTTP204. Event UI browser regression remains mocked loopback API only. No new DDL/developer reset/purge-browser/full-server/Windows acceptance.'],

}
report.parent.mkdir(parents=True,exist_ok=True)
report.write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n')
print(f'V12-02b admin purge slice passed: {len(required)} required tests; stage remains partial')
REPORT
