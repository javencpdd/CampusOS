#!/usr/bin/env bash
# Authenticated typed Feature reads acceptance on an owned PostgreSQL 16.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 )); then echo "usage: $0" >&2; exit 2; fi
mkdir -p .cache/v12-02b-typed-feature-read/tmp
export TMPDIR="$repo_root/.cache/v12-02b-typed-feature-read/tmp" GOTMPDIR="$repo_root/.cache/v12-02b-typed-feature-read/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02b-typed-feature-read/drill.XXXXXX")"
container="campusos-v12-02b-typed-feature-read-$$"
database="campusos_v12_02b_typed_read"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
report="docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-typed-feature-read-linux.json"
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
  echo "V12-02b typed Feature reads: $name"
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
run_check typed-feature-tests env CAMPUSOS_TYPED_FEATURE_TEST_DATABASE_URL="$database_url" CAMPUSOS_THREAD_OWNER_POLICY_OUTPUT="$work_dir/owner-policy.jsonl" \
 go test -race -count=1 -v -run '^(TestBoundProjectionExplicitPublicAndFailClosed|TestBoundProjectionFreshProofAndWholeSnapshot|TestCheckThreadOwnerReadDecisionBinding|TestCheckThreadOwnerReadDecisionCannotForgeOwnerAllow|TestCheckThreadOwnerReadDecisionCurrentFactsAndPrincipal|TestCheckThreadOwnerReadDecisionRejectsInvalidInitialRequest|TestFeatureThreadQueryBranchesAndNoViews|TestFeatureThreadQueryRevokedPublicProofAndDrift|TestFeatureThreadQueryUnavailable|TestMemoryOwnerThreadListSnapshotConcurrentOwnerAndCount|TestMemoryOwnerThreadListSnapshotFiltersAndPagination|TestMemoryOwnerThreadListSnapshotRawStateMatrix|TestMemoryOwnerThreadSnapshotConcurrentOwnerRevocation|TestMemoryOwnerThreadSnapshotCountersAndContent|TestMemoryOwnerThreadSnapshotRawFactsFailClosed|TestMemoryOwnerThreadSnapshotRejectsOwnerABAAndReplacement|TestMemoryOwnerThreadSnapshotStatesAndNoViews|TestMemoryThreadListSnapshotConcurrentCountAndRows|TestMemoryThreadListSnapshotPaginationAndRevocation|TestMemoryThreadListSnapshotPaginationBoundaries|TestMutualAidAuthorizedDetailRevocation|TestMutualAidAuthorizedHTTPRealSession|TestMutualAidAuthorizedMissingPortsAndDomain|TestMutualAidAuthorizedPostgreSQLCommittedRaces|TestMutualAidAuthorizedPostgreSQLStateMatrix|TestMutualAidAuthorizedPostgreSQLWholePageAndCount|TestMutualAidAuthorizedStateMatrix|TestMutualAidAuthorizedWholeListAndEmptyPage|TestOwnContentsHTTPEmptyDisabledAndRevocation|TestOwnContentsHTTPEmptyPageKeepsOwnerCount|TestOwnContentsHTTPFailureBoundary|TestOwnContentsHTTPRejectsIncompleteHostProof|TestOwnContentsHTTPVerifiedSessionAndDomain|TestOwnContentsModuleSessionProofWiring|TestOwnContentsPrincipalBoundary|TestOwnContentsPrincipalEmptyAndProofChanges|TestOwnContentsPrincipalFiltersAndProjection|TestOwnContentsPrincipalQueryFailures|TestOwnerThreadDetailMemoryRetryAndRevocation|TestOwnerThreadDetailPrincipalAndFailureBoundaries|TestOwnerThreadDetailPublicBranchNeverFallsBack|TestOwnerThreadDetailStateMatrix|TestOwnerThreadListFailsClosedAndBounded|TestOwnerThreadListMemoryRetryWholePage|TestOwnerThreadListPrincipalAndEmptyPageBoundaries|TestOwnerThreadListQueryPort|TestOwnerThreadListQueryUnavailable|TestOwnerThreadListSnapshotRejectsInvalidInputBeforeQuery|TestOwnerThreadListStateMatrixAndPagination|TestPostgreSQLOwnerThreadSnapshotInvalidVersionAvoidsQuery|TestPublicListRechecksPublicationAfterAnotherServiceChangesIt|TestPublicThreadDetailFailsClosed|TestPublicThreadDetailHTTPErrorBoundary|TestPublicThreadDetailMemoryRevocation|TestPublicThreadDetailPolicyStateMatrix|TestPublicThreadDetailRetryReloadsFactsAndDecision|TestPublicThreadListDispatchAndBoundaries|TestPublicThreadListFailsClosed|TestPublicThreadListForcesCanonicalVisibility|TestPublicThreadListHTTPEmptyPageKeepsPublicCount|TestPublicThreadListHTTPErrorBoundary|TestPublicThreadListRetryWholeSnapshot|TestPublicThreadListStateMatrixAndPagination|TestPublicThreadListSustainedConflictAndCancellation|TestPublicThreadQueryPreservesAuthorList|TestPublicThreadQueryRevocationChangesWholePage|TestPublicThreadQueryUnavailable|TestPublicThreadQueryUsesCanonicalList|TestResolveUserSessionPrincipalFailsClosedOnChangingAuthority|TestResolveUserSessionPrincipalRejectsMissingAndInvalidProof|TestResolveUserSessionPrincipalUsesCurrentProof|TestRichTextAuthorizedAssetPageAndMissingProof|TestRichTextAuthorizedAttachmentAndAssetCloseOnRevocation|TestRichTextAuthorizedHTTPRealSessionAndSafeBytes|TestRichTextAuthorizedReadRechecksBodyAndCoreFacts|TestRichTextAuthorizedReadStateMatrix|TestSecondhandAuthorizedDetailRevocation|TestSecondhandAuthorizedHTTPRealSession|TestSecondhandAuthorizedMissingPortsAndDomain|TestSecondhandAuthorizedPostgreSQLCommittedRaces|TestSecondhandAuthorizedPostgreSQLStateMatrix|TestSecondhandAuthorizedPostgreSQLWholePageAndCount|TestSecondhandAuthorizedStateMatrix|TestSecondhandAuthorizedWholeListAndEmptyPage|TestThreadOwnerHTTPRechecksContentAndSession|TestThreadOwnerHTTPRejectsIncompleteHostProof|TestThreadOwnerHTTPVerifiedSessionAndDomain|TestThreadOwnerModuleSessionProofWiring|TestThreadOwnerReadPolicyDoesNotAliasOrCache|TestThreadOwnerReadPolicyNativeInterop|TestThreadOwnerReadPolicyNonOwnerAndMFA|TestThreadOwnerReadPolicyRealmStateMatrix|TestThreadOwnerReadPolicyRejectsInvalidInputs)$' \
 ./internal/modules/core/identity/port ./internal/modules/core/identity/policy ./internal/modules/core/community ./internal/modules/core/community/repository ./internal/modules/core/community/service ./internal/modules/core/community/handler ./internal/modules/features/personalspace ./internal/modules/features/richtext ./internal/modules/features/mutualaid ./internal/modules/features/secondhand
run_check owner-contract node scripts/check-v12-thread-owner-read-contract.mjs --go-output "$work_dir/owner-policy.jsonl" --report "$work_dir/owner-policy.json"
run_check composition go test -race -count=1 ./internal/modules/features/mutualaid ./internal/modules/features/secondhand ./internal/modules/features/richtext ./internal/server ./internal/modules/core/identity/port ./internal/modules/core/community
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
tests=(work/'typed-feature-tests.log').read_text()
required=['TestBoundProjectionExplicitPublicAndFailClosed', 'TestBoundProjectionFreshProofAndWholeSnapshot', 'TestCheckThreadOwnerReadDecisionBinding', 'TestCheckThreadOwnerReadDecisionCannotForgeOwnerAllow', 'TestCheckThreadOwnerReadDecisionCurrentFactsAndPrincipal', 'TestCheckThreadOwnerReadDecisionRejectsInvalidInitialRequest', 'TestFeatureThreadQueryBranchesAndNoViews', 'TestFeatureThreadQueryRevokedPublicProofAndDrift', 'TestFeatureThreadQueryUnavailable', 'TestMemoryOwnerThreadListSnapshotConcurrentOwnerAndCount', 'TestMemoryOwnerThreadListSnapshotFiltersAndPagination', 'TestMemoryOwnerThreadListSnapshotRawStateMatrix', 'TestMemoryOwnerThreadSnapshotConcurrentOwnerRevocation', 'TestMemoryOwnerThreadSnapshotCountersAndContent', 'TestMemoryOwnerThreadSnapshotRawFactsFailClosed', 'TestMemoryOwnerThreadSnapshotRejectsOwnerABAAndReplacement', 'TestMemoryOwnerThreadSnapshotStatesAndNoViews', 'TestMemoryThreadListSnapshotConcurrentCountAndRows', 'TestMemoryThreadListSnapshotPaginationAndRevocation', 'TestMemoryThreadListSnapshotPaginationBoundaries', 'TestMutualAidAuthorizedDetailRevocation', 'TestMutualAidAuthorizedHTTPRealSession', 'TestMutualAidAuthorizedMissingPortsAndDomain', 'TestMutualAidAuthorizedPostgreSQLCommittedRaces', 'TestMutualAidAuthorizedPostgreSQLStateMatrix', 'TestMutualAidAuthorizedPostgreSQLWholePageAndCount', 'TestMutualAidAuthorizedStateMatrix', 'TestMutualAidAuthorizedWholeListAndEmptyPage', 'TestOwnContentsHTTPEmptyDisabledAndRevocation', 'TestOwnContentsHTTPEmptyPageKeepsOwnerCount', 'TestOwnContentsHTTPFailureBoundary', 'TestOwnContentsHTTPRejectsIncompleteHostProof', 'TestOwnContentsHTTPVerifiedSessionAndDomain', 'TestOwnContentsModuleSessionProofWiring', 'TestOwnContentsPrincipalBoundary', 'TestOwnContentsPrincipalEmptyAndProofChanges', 'TestOwnContentsPrincipalFiltersAndProjection', 'TestOwnContentsPrincipalQueryFailures', 'TestOwnerThreadDetailMemoryRetryAndRevocation', 'TestOwnerThreadDetailPrincipalAndFailureBoundaries', 'TestOwnerThreadDetailPublicBranchNeverFallsBack', 'TestOwnerThreadDetailStateMatrix', 'TestOwnerThreadListFailsClosedAndBounded', 'TestOwnerThreadListMemoryRetryWholePage', 'TestOwnerThreadListPrincipalAndEmptyPageBoundaries', 'TestOwnerThreadListQueryPort', 'TestOwnerThreadListQueryUnavailable', 'TestOwnerThreadListSnapshotRejectsInvalidInputBeforeQuery', 'TestOwnerThreadListStateMatrixAndPagination', 'TestPostgreSQLOwnerThreadSnapshotInvalidVersionAvoidsQuery', 'TestPublicListRechecksPublicationAfterAnotherServiceChangesIt', 'TestPublicThreadDetailFailsClosed', 'TestPublicThreadDetailHTTPErrorBoundary', 'TestPublicThreadDetailMemoryRevocation', 'TestPublicThreadDetailPolicyStateMatrix', 'TestPublicThreadDetailRetryReloadsFactsAndDecision', 'TestPublicThreadListDispatchAndBoundaries', 'TestPublicThreadListFailsClosed', 'TestPublicThreadListForcesCanonicalVisibility', 'TestPublicThreadListHTTPEmptyPageKeepsPublicCount', 'TestPublicThreadListHTTPErrorBoundary', 'TestPublicThreadListRetryWholeSnapshot', 'TestPublicThreadListStateMatrixAndPagination', 'TestPublicThreadListSustainedConflictAndCancellation', 'TestPublicThreadQueryPreservesAuthorList', 'TestPublicThreadQueryRevocationChangesWholePage', 'TestPublicThreadQueryUnavailable', 'TestPublicThreadQueryUsesCanonicalList', 'TestResolveUserSessionPrincipalFailsClosedOnChangingAuthority', 'TestResolveUserSessionPrincipalRejectsMissingAndInvalidProof', 'TestResolveUserSessionPrincipalUsesCurrentProof', 'TestRichTextAuthorizedAssetPageAndMissingProof', 'TestRichTextAuthorizedAttachmentAndAssetCloseOnRevocation', 'TestRichTextAuthorizedHTTPRealSessionAndSafeBytes', 'TestRichTextAuthorizedReadRechecksBodyAndCoreFacts', 'TestRichTextAuthorizedReadStateMatrix', 'TestSecondhandAuthorizedDetailRevocation', 'TestSecondhandAuthorizedHTTPRealSession', 'TestSecondhandAuthorizedMissingPortsAndDomain', 'TestSecondhandAuthorizedPostgreSQLCommittedRaces', 'TestSecondhandAuthorizedPostgreSQLStateMatrix', 'TestSecondhandAuthorizedPostgreSQLWholePageAndCount', 'TestSecondhandAuthorizedStateMatrix', 'TestSecondhandAuthorizedWholeListAndEmptyPage', 'TestThreadOwnerHTTPRechecksContentAndSession', 'TestThreadOwnerHTTPRejectsIncompleteHostProof', 'TestThreadOwnerHTTPVerifiedSessionAndDomain', 'TestThreadOwnerModuleSessionProofWiring', 'TestThreadOwnerReadPolicyDoesNotAliasOrCache', 'TestThreadOwnerReadPolicyNativeInterop', 'TestThreadOwnerReadPolicyNonOwnerAndMFA', 'TestThreadOwnerReadPolicyRealmStateMatrix', 'TestThreadOwnerReadPolicyRejectsInvalidInputs']
actual=re.findall(r'^--- PASS: (\w+)(?: |$)',tests,re.M)
assert sorted(actual)==sorted(required),'required target tests differ'
assert not re.search(r'^\s*--- (?:FAIL|SKIP): ',tests,re.M),'target tests skipped or failed'
assert 'WARNING: DATA RACE' not in tests
sha=lambda path:hashlib.sha256(Path(path).read_bytes()).hexdigest()
paths=['Makefile', 'docs/api/policy-thread-owner-read-decision-v1.schema.json', 'docs/api/policy-thread-owner-read-v1.errors.json', 'docs/api/policy-thread-owner-read-v1.schema.json', 'internal/modules/core/community/feature_read_query_test.go', 'internal/modules/core/community/handler/thread_handler.go', 'internal/modules/core/community/handler/thread_handler_test.go', 'internal/modules/core/community/handler/thread_owner_read_test.go', 'internal/modules/core/community/moderation_gateway.go', 'internal/modules/core/community/module.go', 'internal/modules/core/community/owner_list_query_test.go', 'internal/modules/core/community/port/port.go', 'internal/modules/core/community/repository/pg_thread_repository.go', 'internal/modules/core/community/repository/thread_owner_list_snapshot.go', 'internal/modules/core/community/repository/thread_owner_list_snapshot_test.go', 'internal/modules/core/community/repository/thread_owner_snapshot_test.go', 'internal/modules/core/community/repository/thread_repository.go', 'internal/modules/core/community/service/thread_owner_list.go', 'internal/modules/core/community/service/thread_owner_list_postgres_test.go', 'internal/modules/core/community/service/thread_owner_list_test.go', 'internal/modules/core/community/service/thread_owner_read.go', 'internal/modules/core/community/service/thread_owner_read_postgres_test.go', 'internal/modules/core/community/service/thread_owner_read_test.go', 'internal/modules/core/community/service/thread_service.go', 'internal/modules/core/community/thread_owner_read_test.go', 'internal/modules/core/identity/policy/thread_owner_read.go', 'internal/modules/core/identity/policy/thread_owner_read_interop_test.go', 'internal/modules/core/identity/policy/thread_owner_read_test.go', 'internal/modules/core/identity/port/bound_projection.go', 'internal/modules/core/identity/port/bound_projection_test.go', 'internal/modules/core/identity/port/session_principal_context.go', 'internal/modules/core/identity/port/thread_owner_read_policy.go', 'internal/modules/core/identity/port/thread_owner_read_policy_test.go', 'internal/modules/core/identity/port/user_session_principal.go', 'internal/modules/core/identity/port/user_session_principal_test.go', 'internal/modules/features/mutualaid/authorized_read.go', 'internal/modules/features/mutualaid/authorized_read_http_test.go', 'internal/modules/features/mutualaid/authorized_read_postgres_test.go', 'internal/modules/features/mutualaid/authorized_read_test.go', 'internal/modules/features/mutualaid/handler.go', 'internal/modules/features/mutualaid/model.go', 'internal/modules/features/mutualaid/module.go', 'internal/modules/features/mutualaid/service.go', 'internal/modules/features/mutualaid/store.go', 'internal/modules/features/personalspace/handler.go', 'internal/modules/features/personalspace/module.go', 'internal/modules/features/personalspace/owner_contents.go', 'internal/modules/features/personalspace/owner_contents_http_test.go', 'internal/modules/features/personalspace/owner_contents_module_test.go', 'internal/modules/features/personalspace/owner_contents_test.go', 'internal/modules/features/personalspace/sync.go', 'internal/modules/features/richtext/asset_pagination.go', 'internal/modules/features/richtext/attachments.go', 'internal/modules/features/richtext/authorized_invocation.go', 'internal/modules/features/richtext/authorized_read.go', 'internal/modules/features/richtext/authorized_read_http_test.go', 'internal/modules/features/richtext/authorized_read_postgres_test.go', 'internal/modules/features/richtext/authorized_read_test.go', 'internal/modules/features/richtext/handler.go', 'internal/modules/features/richtext/model.go', 'internal/modules/features/richtext/module.go', 'internal/modules/features/richtext/resource_contexts.go', 'internal/modules/features/richtext/service.go', 'internal/modules/features/richtext/store.go', 'internal/modules/features/secondhand/authorized_read.go', 'internal/modules/features/secondhand/authorized_read_http_test.go', 'internal/modules/features/secondhand/authorized_read_postgres_test.go', 'internal/modules/features/secondhand/authorized_read_test.go', 'internal/modules/features/secondhand/handler.go', 'internal/modules/features/secondhand/model.go', 'internal/modules/features/secondhand/module.go', 'internal/modules/features/secondhand/service.go', 'internal/modules/features/secondhand/store.go', 'internal/server/personal_document_pdf_viewer.go', 'migrations/000016_v1_2_thread_facts_version.up.sql', 'modules/catalog_test.go', 'modules/features/controlled-richtext-article/module.yaml', 'modules/features/mutual-aid/module.yaml', 'modules/features/secondhand/module.yaml', 'scripts/check-v12-thread-owner-read-contract.mjs', 'scripts/v12-02b-owner-detail-drill.sh', 'scripts/v12-02b-owner-list-drill.sh', 'scripts/v12-02b-typed-feature-read-drill.sh']
oracle=json.loads((work/'owner-policy.json').read_text())
assert oracle['passed'] and oracle['native_go_decisions']==216
prefix='docs/项目计划书v1/项目计划v1.2/evidence/'
dependencies=['v12-02a-exit-linux.json','v12-02d-exit-linux.json','v12-02b-owner-list-linux.json','v12-02b-document-read-linux.json','v12-02b-richtext-read-linux.json']
r={
 'schema':'campusos.v12-02b-typed-feature-read-acceptance/v1','generated_at':datetime.now(timezone.utc).isoformat(),
 'stage':'V12-02b','stage_status':'partial','slice_status':'completed',
 'implementation':'passed','automation':'passed','target_environment':'passed',
 'scope':'MutualAid/Secondhand checked public/owner details and public whole-page projection: raw Community Policy plus current typed Detail/version/creator validation, full user Session on /me and final list/count/membership recheck.',
 'environment':{'os':platform.system(),'architecture':platform.machine(),'go':subprocess.check_output(['go','version'],text=True).strip(),'postgres_version_num':int(sys.argv[3]),'database':'owned disposable PostgreSQL 16 loopback tmpfs; real Community/MutualAid/Secondhand/Reliability repositories'},
 'checks':{name:'passed' for name in required},
 'coverage':{'new_required_tests':16,'previous_read_regression_tests':77,'real_postgres_tests':6,'raw_state_combinations_per_feature':36,'memory_domain_checks_per_feature':108,'postgres_domain_checks_per_feature':72,'postgres_committed_races':12,'http':'each Feature uses real User/Admin domain JWT and SessionService with Memory Identity/resources; public detail/list, owner, missing proof, same-ID admin, owner forgery, other-owner and revoked session','consistency':'Core nonreused facts version plus complete typed Detail/creator/version; type/lifecycle/raw enums fail closed; entire projected list/page/count reread including empty/off-page changes; current full Session proof on owner ingress'},
 'policy_oracle':{'state_principal_combinations':216,'allows':24,'malformed_cases':len(oracle['malformed_cases']),'binding_checks':len(oracle['binding_checks']),'native_go_decisions':216},
 'artifacts':{str(path.relative_to(Path.cwd())):sha(path) for path in [work/'owner-policy.jsonl',work/'owner-policy.json']},
 'source_sha256':{path:sha(path) for path in paths},
 'dependencies':{prefix+name:sha(prefix+name) for name in dependencies},
 'common_gates_status':'passed',
 'logs':{path.stem:{'status':'passed','exit_code':0,'local_log':str(path.relative_to(Path.cwd())),'log_sha256':sha(path)} for path in sorted(work.glob('*.log'))},
 'boundaries':['This slice does not complete V12-02b; legacy inline images/private files, author/governance writes, Admin lists, batch/permission preview remain.','HTTP components use Memory Identity/resources; real PostgreSQL typed resources/transactions accepted separately. No full browser, Windows or stage acceptance claimed.','Public counters are retained as a non-authoritative projection and never incremented; authorization binds Core facts version and full Detail.','No new migration; original public predicate is unchanged and /me requires the current owner, including for published content.'],
}
report.parent.mkdir(parents=True,exist_ok=True)
report.write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n')
print(f'V12-02b typed Feature read slice passed: {len(required)} required tests; stage remains partial')
REPORT
