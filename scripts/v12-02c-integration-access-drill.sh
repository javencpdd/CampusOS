#!/usr/bin/env bash
# Native pure-component acceptance; no database or production identity mutation.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 && $# != 2 )) || { (( $# == 2 )) && [[ "$1" != --report ]]; };then
 echo 'usage: scripts/v12-02c-integration-access-drill.sh [--report evidence.json]' >&2;exit 2
fi
[[ "$(uname -s)" == Linux ]] || { echo 'Linux native target required' >&2;exit 1; }
mkdir -p .cache/v12-02c-access/tmp
export GOCACHE="$repo_root/.cache/v12-g0/go-cache" TMPDIR="$repo_root/.cache/v12-02c-access/tmp" GOTMPDIR="$repo_root/.cache/v12-02c-access/tmp"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02c-access/drill.XXXXXX")"
report="${2:-docs/项目计划书v1/项目计划v1.2/evidence/v12-02c-integration-access-linux.json}"
trap 'status=$?;echo "$status" >"$work_dir/.exit_code";echo "Integration access exit $status: $work_dir"' EXIT
mkdir -p "$work_dir/check-state"
printf '%s\n' "$report" >"$work_dir/report-path.txt"
if [[ -f .cache/v12-02c-access/preflight.json ]];then cp .cache/v12-02c-access/preflight.json "$work_dir/task-preflight.json";fi
python3 - "$work_dir" <<'PREFLIGHT'
import hashlib,json,subprocess,sys
from pathlib import Path
from datetime import datetime,timezone
base=Path('docs/项目计划书v1/项目计划v1.2/evidence');reports={}
for name in ['v12-01b-exit-linux.json','v12-02a-exit-linux.json','v12-02d-exit-linux.json','v12-02b-exit-linux.json']:
 path=base/name;report=json.loads(path.read_text())
 assert report['stage_status']=='completed'and all(report[k]=='passed'for k in ['implementation','automation','target_environment'])
 reports[str(path)]=hashlib.sha256(path.read_bytes()).hexdigest()
data={'checked_at':datetime.now(timezone.utc).isoformat(),'dependencies':reports,'head':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),'scope':'Predecessor reports remain historical evidence; current component is independently executed by this drill.'}
(Path(sys.argv[1])/'preflight.json').write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
PREFLIGHT
git rev-parse HEAD >"$work_dir/head.txt"
run_check(){
 local name="$1";shift;echo "V12-02c integration access: $name"
 if "$@" >"$work_dir/$name.log" 2>&1;then echo 0 >"$work_dir/check-state/$name.exit_code";return;fi
 echo 1 >"$work_dir/check-state/$name.exit_code";tail -n 60 "$work_dir/$name.log" >&2;return 1
}
python3 - "$work_dir" <<'PY'
import hashlib,json,subprocess,sys
from pathlib import Path
paths=subprocess.check_output(['git','ls-files','--cached','--others','--exclude-standard'],text=True).splitlines()
extensions={'.go','.ts','.mts','.vue','.mjs','.py','.sh','.ps1','.sql','.yaml','.json','.mod','.sum','.toml','.lock','.css','.html'}
prefixes=('internal/','cmd/','pkg/','admin/src/','admin/tests/','web/src/','web/tests/','migrations/','modules/','scripts/','sdk/','packages/','schemas/','contracts/','skills/sources/')
configs={'Makefile','go.mod','go.sum','admin/package.json','admin/pnpm-lock.yaml','admin/vite.config.ts','admin/vitest.config.ts','admin/tsconfig.json','web/package.json','web/pnpm-lock.yaml','web/vite.config.ts','web/vitest.config.ts','web/tsconfig.json'}
source={p:hashlib.sha256(Path(p).read_bytes()).hexdigest()for p in paths if Path(p).is_file()and(p in configs or p.startswith(prefixes)and Path(p).suffix in extensions)}
(Path(sys.argv[1])/'source-freeze.json').write_text(json.dumps(source,ensure_ascii=False,indent=2)+'\n')
print('Frozen',len(source),'source files')
PY
run_check contract node scripts/check-v12-integration-access-contract.mjs
run_check build-port go test -race -c -o "$work_dir/integration-port.test" ./internal/modules/core/identity/port
run_check build-policy go test -race -c -o "$work_dir/integration-policy.test" ./internal/modules/core/identity/policy
run_check native-port env -i PATH="$PATH" "$work_dir/integration-port.test" -test.run '^TestIntegrationAccess' -test.v
run_check native-policy env -i PATH="$PATH" CAMPUSOS_INTEGRATION_ACCESS_OUTPUT="$work_dir/go-output.json" "$work_dir/integration-policy.test" -test.run '^TestIntegrationAccess' -test.v
run_check verify-oracle node scripts/check-v12-integration-access-contract.mjs --output "$work_dir/go-output.json" --report "$work_dir/native-oracle.json"
python3 scripts/v12-02c-integration-access-report.py "$work_dir" --target
run_check related-race go test -race ./internal/modules/core/identity/port ./internal/modules/core/identity/policy -count=1
run_check go-all go test ./... -count=1
run_check sdk pnpm --dir sdk/typescript build
run_check contracts make contracts-check
run_check architecture make architecture-check
run_check docs-build make docs-build
run_check docs-links make docs-links
run_check line-endings python3 scripts/check-line-endings.py --include-untracked
run_check diff git diff --check
python3 scripts/v12-02c-integration-access-report.py "$work_dir"
echo 'Native target and all common gates passed; synchronize final documents, run final seven checks and finalize report. V12-02c remains partial.'
