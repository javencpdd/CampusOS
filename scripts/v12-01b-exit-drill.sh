#!/usr/bin/env bash
# Validate the complete V12-01b shared Port stage; never resets a shared database.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
reuse=false
report='docs/项目计划书v1/项目计划v1.2/evidence/v12-01b-exit-linux.json'
while (( $# )); do
  case "$1" in
    --reuse-target-evidence) reuse=true; shift ;;
    --report) (( $# >= 2 )) || exit 2; report="$2"; shift 2 ;;
    *) echo 'usage: v12-01b-exit-drill.sh [--reuse-target-evidence] [--report path]' >&2; exit 2 ;;
  esac
done
[[ "$(uname -s)" == Linux ]] || { echo 'Linux target required' >&2; exit 1; }
mkdir -p .cache/v12-01b/tmp
export TMPDIR="$repo_root/.cache/v12-01b/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
evidence_dir='docs/项目计划书v1/项目计划v1.2/evidence'
components=(secret-retirement keyring secret-rewrap secret-batch secret-key-inspect v5-config egress resource-policy secret-rotation secret-broker)
if [[ "$reuse" == false ]]; then
  for component in "${components[@]}"; do
    bash "scripts/v12-01b-$component-drill.sh" --report "$evidence_dir/v12-01b-$component-linux.json"
  done
fi
# Reuse is allowed only when all statuses and every recorded source digest are current.
python3 - "$evidence_dir" <<'PY'
import hashlib,json,sys
from pathlib import Path
components=['secret-retirement','keyring','secret-rewrap','secret-batch','secret-key-inspect','v5-config','egress','resource-policy','secret-rotation','secret-broker']
for component in components:
    p=Path(sys.argv[1])/f'v12-01b-{component}-linux.json'
    data=json.loads(p.read_text())
    for key in ['implementation','automation','target_environment']:
        assert data.get(key)=='passed',f'{p}: {key} not passed'
    assert data.get('checks') and all(v=='passed' for v in data['checks'].values()),f'{p}: incomplete checks'
    if component=='secret-broker':
        for required in ['persistent_usage_outcome_and_charged_units_audit','usage_audit_failure_before_and_after_dispatch']:
            assert data['checks'].get(required)=='passed',f'{p}: missing usage audit acceptance {required}'
    assert data.get('source_sha256'),f'{p}: missing source hashes'
    for source,digest in data['source_sha256'].items():
        assert hashlib.sha256(Path(source).read_bytes()).hexdigest()==digest,f'{p}: stale source {source}'
    print(f'{component}: {len(data["checks"])}/{len(data["checks"])} checks; {len(data["source_sha256"])}/{len(data["source_sha256"])} current hashes')
PY
log_dir="$(mktemp -d "$repo_root/.cache/v12-01b/exit.XXXXXX")"
run_gate(){
  local name="$1"; shift
  if "$@" >"$log_dir/$name.log" 2>&1; then
    echo "$name: passed"
  else
    local code=$?
    tail -n 80 "$log_dir/$name.log" >&2
    echo "$name: failed; $log_dir/$name.log" >&2
    exit "$code"
  fi
}
run_gate go-all go test ./... -count=1
run_gate security-race go test -race ./internal/plugin ./internal/platform/security -run '^Test(SecretUseBroker|SecretBindingScope|SecretGrantResource|SecretPurposeBudgetRegistry|SecretRotationWorker|RewrapActiveSecretBatch|PluginV5Config|Egress|ResourcePolicy|PurposeBudget)' -count=1
run_gate contracts make contracts-check
run_gate architecture make architecture-check
run_gate docker-dev make docker-dev-test
run_gate docker-deploy make docker-deploy-check
run_gate admin-build make admin-build
run_gate docs-build make docs-build
run_gate docs-links make docs-links
run_gate line-endings python3 scripts/check-line-endings.py --include-untracked
run_gate diff-check git diff --check

python3 - "$report" "$log_dir" <<'PY'
import hashlib,json,platform,sys
from datetime import datetime,timezone
from pathlib import Path
sha=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest()
components=['secret-retirement','keyring','secret-rewrap','secret-batch','secret-key-inspect','v5-config','egress','resource-policy','secret-rotation','secret-broker']
entries={};sources={'scripts/v12-01b-exit-drill.sh':sha('scripts/v12-01b-exit-drill.sh')};checks=0
extra_sources=['scripts/check-doc-links.py','docs/api/v1.2插件宿主资源与非侵入式安装合同.md','docs-site/deployment/docker-development.md','docs/help/系统设计相关/v0.13 Docker跨平台部署、迁移与开发指南.md','internal/plugin/secret_budget_registry.go','internal/plugin/secret_budget_registry_test.go','internal/plugin/secret_resource_provider_test.go']
for source in extra_sources:
    sources[source]=sha(source)
for component in components:
    p=Path('docs/项目计划书v1/项目计划v1.2/evidence')/f'v12-01b-{component}-linux.json'
    data=json.loads(p.read_text())
    for source,digest in data['source_sha256'].items():
        assert sha(source)==digest,f'source changed during gates: {source}'
        sources[source]=digest
    checks+=len(data['checks'])
    entries[component]={'report':str(p),'sha256':sha(p),'implementation':data['implementation'],'automation':data['automation'],'target_environment':data['target_environment'],'checks':len(data['checks']),'source_hashes':len(data['source_sha256'])}
commands={
 'go-all':'go test ./... -count=1',
 'security-race':"go test -race ./internal/plugin ./internal/platform/security -run '^Test(SecretUseBroker|SecretBindingScope|SecretGrantResource|SecretPurposeBudgetRegistry|SecretRotationWorker|RewrapActiveSecretBatch|PluginV5Config|Egress|ResourcePolicy|PurposeBudget)' -count=1",
 'contracts':'make contracts-check','architecture':'make architecture-check','docker-dev':'make docker-dev-test','docker-deploy':'make docker-deploy-check',
 'admin-build':'make admin-build','docs-build':'make docs-build','docs-links':'make docs-links','line-endings':'python3 scripts/check-line-endings.py --include-untracked','diff-check':'git diff --check',
}
gates={}
for name,command in commands.items():
    log=Path(sys.argv[2])/f'{name}.log'
    gates[name]={'command':command,'exit_code':0,'status':'passed','local_log':str(log.relative_to(Path.cwd())),'log_sha256':sha(log)}
dependencies={}
for name in ['v12-g0-exit.json','v12-g1-exit-linux.json','v12-01a-exit-linux.json']:
    p=Path('docs/项目计划书v1/项目计划v1.2/evidence')/name
    dependencies[name]={'report':str(p),'sha256':sha(p),'role':'previous accepted bounded stage; historical source digests are not silently rewritten'}
result={
 'schema':'campusos.v12-stage-exit/v1','generated_at':datetime.now(timezone.utc).isoformat(),
 'stage':'V12-01b','stage_status':'completed','implementation':'passed','automation':'passed','target_environment':'passed',
 'environment':{'os':platform.system(),'architecture':platform.machine(),'database':'owned PostgreSQL 16 tmpfs, random loopback port; pg_dump/pg_restore into another owned database','egress':'real loopback TLS with test-only controlled public DNS/dial mapping; no public endpoint contacted','shared_instance_reset':False},
 'coverage':{'accepted_components':len(entries),'component_checks':checks,'current_source_hashes':len(sources),'common_gates':len(gates)},
 'dependencies':dependencies,'components':entries,'common_gates':gates,'source_sha256':sources,
 'scope':'shared Security/Secret/Egress ports and v5 host-owned configuration repository; Broker usage outcome and charged-unit audits required',
 'boundaries':{
  'key_retirement':'full current plugin_secret_values reference snapshot covers all plugins/statuses; history/bad rows and backups still require old keys; snapshot is not automatic deletion permission',
  'broker':'trusted host dispatch port; exact name/ref/Profile/URL/purpose grants, current config and consent; fixed POST/Bearer and status-only result; runtime Host API binding belongs to 03c/04/05/06',
  'budgets':'host process-local purpose reservation and Broker version-wide concurrency; usage outcome/charged units persisted before reporting success; durable/distributed and CPU/memory/runner enforcement belong to 05/06',
  'recovery':'owned configuration/Secret/audit database restore; full system objects/packages/authentication-generation recovery and Windows/WSL2 belong to 08b',
 }
}
p=Path(sys.argv[1]);p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
print(f'V12-01b completed: {len(entries)} components, {checks} checks, {len(gates)} common gates; {p}')
PY
