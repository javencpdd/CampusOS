#!/usr/bin/env bash
# Bounded V12-02a pure Policy Port acceptance; no HTTP/DB/credential claim.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 && $# != 2 )) || { (( $# == 2 )) && [[ "$1" != --report ]]; }; then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi
[[ "$(uname -s)" == Linux ]] || { echo "Linux target environment required" >&2; exit 1; }
report="${2:-docs/项目计划书v1/项目计划v1.2/evidence/v12-02a-public-policy-linux.json}"
mkdir -p .cache/v12-02a/tmp
export TMPDIR="$repo_root/.cache/v12-02a/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02a/public-policy.XXXXXX")"
trap 'rm -f -- "$work_dir/principal.test" "$work_dir/policy.test"' EXIT
run_check() {
  local name="$1"
  shift
  echo "V12-02a: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then return; fi
  tail -n 80 "$work_dir/$name.log" >&2
  return 1
}
run_check targeted go test ./internal/modules/core/identity/port ./internal/modules/core/identity/policy -run '^Test(Principal|ThreadPublicRead)' -count=1 -v
run_check build-principal go test -race -c -o "$work_dir/principal.test" ./internal/modules/core/identity/port
run_check build-policy go test -race -c -o "$work_dir/policy.test" ./internal/modules/core/identity/policy
(
  cd internal/modules/core/identity/port
  run_check native-principal env -i PATH="$PATH" "$work_dir/principal.test" -test.run '^TestPrincipal' -test.v
)
(
  cd internal/modules/core/identity/policy
  run_check native-policy env -i PATH="$PATH" CAMPUSOS_POLICY_CORPUS_OUT="$work_dir/go-output.json" "$work_dir/policy.test" -test.run '^TestThreadPublicRead' -test.v
)
run_check wire-interop node scripts/check-v12-02a-policy-output.mjs "$work_dir/go-output.json"
# Save bounded target evidence before docs-links traverses its new reference.
python3 - "$work_dir" "$report" <<'PY'
import hashlib, json, platform, re, subprocess, sys
from datetime import datetime, timezone
from pathlib import Path
work, report = map(Path, sys.argv[1:])
root = Path.cwd()
required = {
 'native-principal': ['TestPrincipalG1Fixtures','TestPrincipalDomainMatrix','TestPrincipalStrictJSON','TestPrincipalContractErrorPriority','TestPrincipalOpaqueIDAndDelegation'],
 'native-policy': ['TestThreadPublicReadG1Fixtures','TestThreadPublicReadStatePrincipalMatrix','TestThreadPublicReadDecisionBindingAndCurrentFacts','TestThreadPublicReadStrictJSON','TestThreadPublicReadParallelWithoutCache'],
}
for name, tests in required.items():
 text=(work/(name+'.log')).read_text()
 assert not re.search(r'^\s*--- (FAIL|SKIP):', text, re.M), name
 for test in tests:
  assert re.search(r'^--- PASS: '+re.escape(test)+r'(?: |$)',text,re.M), test
sha=lambda p: hashlib.sha256(Path(p).read_bytes()).hexdigest()
sources=[
 'internal/modules/core/identity/port/principal.go',
 'internal/modules/core/identity/port/principal_test.go',
 'internal/modules/core/identity/port/strict_json.go',
 'internal/modules/core/identity/port/thread_public_read_policy.go',
 'internal/modules/core/identity/policy/thread_public_read.go',
 'internal/modules/core/identity/policy/thread_public_read_test.go',
 'docs/api/principal-context-v1.schema.json',
 'docs/api/policy-thread-public-read-v1.schema.json',
 'docs/api/principal-context-v1.errors.json',
 'docs/api/policy-thread-public-read-v1.errors.json',
 'sdk/typescript/tests/principal-context-v1.fixtures.json',
 'sdk/typescript/tests/policy-thread-public-read-v1.fixtures.json',
 'sdk/typescript/package.json','sdk/typescript/pnpm-lock.yaml',
 'scripts/check-v12-02a-policy-output.mjs','scripts/v12-02a-public-policy-drill.sh',
 'docs/api/v1.2主体上下文合同.md','docs/api/v1.2公开帖子事实与策略合同.md',
]
logs={name:{'path':str((work/(name+'.log')).relative_to(root)),'sha256':sha(work/(name+'.log'))} for name in ['targeted','build-principal','build-policy','native-principal','native-policy','wire-interop']}
deps={}
for name in ['v12-g0-exit.json','v12-g1-exit-linux.json','v12-01a-exit-linux.json','v12-01b-exit-linux.json']:
 path=Path('docs/项目计划书v1/项目计划v1.2/evidence')/name
 deps[name]={'report':str(path),'sha256':sha(path),'role':'accepted predecessor evidence; historical source snapshots remain unchanged'}
data={
 'schema':'campusos.v12-02a-public-policy-acceptance/v1',
 'generated_at':datetime.now(timezone.utc).isoformat(),
 'stage':'V12-02a','stage_status':'partial','slice_status':'completed',
 'implementation':'passed','automation':'passed','target_environment':'passed',
 'scope':'Principal structural validation -> named public visibility decision -> same-call binding/current facts check; pure Go Port only',
 'environment':{'os':platform.system(),'architecture':platform.machine(),'go':subprocess.check_output(['go','version'],text=True).strip(),'node':subprocess.check_output(['node','--version'],text=True).strip(),'execution':'native Linux race-enabled compiled test binaries, env -i; no DB/cache credentials or network/HTTP entry'},
 'checks':{test:'passed' for tests in required.values() for test in tests},
 'coverage':{'principal_g1_fixtures':63,'principal_domain_combinations':180,'policy_g1_fixtures':60,'state_principal_combinations':360,'binding_current_fact_checks':21,'parallel_callers':64,'native_go_ajv_outputs':360},
 'source_sha256':{p:sha(p) for p in sources},'dependencies':deps,'target_logs':logs,
 'native_binary_sha256':{p.name:sha(p) for p in [work/'principal.test',work/'policy.test']},
 'actual_go_output':{'path':str((work/'go-output.json').relative_to(root)),'sha256':sha(work/'go-output.json')},
 'common_gates_status':'pending','common_gates':{},
 'boundaries':['No production route/session/credential adapter switched; valid JSON is not authentication.',
 'Current facts and content loading, ABA-safe facts version, read/return ordering and list/count/page integration remain V12-02b.',
 'Other named actions, delegation action/scope/time/strength subset policy and existing permission/grant path replacement remain V12-02a.',
 'Independent Admin domain remains V12-02d; this is not HTTP/PG/browser acceptance or full V12-02a exit.'],
}
report.write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
print('Native target acceptance passed; common gates pending')
PY
run_check go-all go test ./... -count=1
run_check contracts make contracts-check
run_check architecture make architecture-check
run_check docs-build make docs-build
run_check docs-links make docs-links
run_check line-endings python3 scripts/check-line-endings.py --include-untracked
run_check diff git diff --check
python3 - "$work_dir" "$report" <<'PY'
import hashlib, json, sys
from datetime import datetime, timezone
from pathlib import Path
work, report=map(Path,sys.argv[1:]); data=json.loads(report.read_text())
commands={'go-all':'go test ./... -count=1','contracts':'make contracts-check','architecture':'make architecture-check','docs-build':'make docs-build','docs-links':'make docs-links','line-endings':'python3 scripts/check-line-endings.py --include-untracked','diff':'git diff --check'}
for name, command in commands.items():
 p=work/(name+'.log')
 data['common_gates'][name]={'command':command,'status':'passed','exit_code':0,'local_log':str(p.relative_to(Path.cwd())),'log_sha256':hashlib.sha256(p.read_bytes()).hexdigest()}
# Guard against editing an accepted implementation while this drill ran.
for p, digest in data['source_sha256'].items():
 assert hashlib.sha256(Path(p).read_bytes()).hexdigest()==digest, 'source changed during acceptance: '+p
data['common_gates_status']='passed'; data['generated_at']=datetime.now(timezone.utc).isoformat()
report.write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
print('V12-02a public Policy slice passed; 7 common gates passed; stage remains partial')
PY
