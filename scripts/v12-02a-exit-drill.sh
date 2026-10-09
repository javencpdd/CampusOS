#!/usr/bin/env bash
# V12-02a stage exit: re-run every accepted slice drill against the current
# tree and aggregate their evidence. Each sub-drill runs its own target
# environment and common gates; this script adds no new claims of its own.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
for executable in docker go node python3 make bash; do
  command -v "$executable" >/dev/null || { echo "missing $executable" >&2; exit 127; }
done
if (( $# != 0 && $# != 2 )) || { (( $# == 2 )) && [[ "$1" != "--report" ]]; }; then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi

mkdir -p .cache/v12-02a-exit/tmp
export TMPDIR="$repo_root/.cache/v12-02a-exit/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
report="${2:-docs/项目计划书v1/项目计划v1.2/evidence/v12-02a-exit-linux.json}"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02a-exit/exit.XXXXXX")"

run_check() {
  local name="$1"
  shift
  echo "V12-02a exit: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then
    return
  fi
  tail -n 120 "$work_dir/$name.log" >&2
  return 1
}

run_check public-policy bash scripts/v12-02a-public-policy-drill.sh
run_check resource-policy bash scripts/v12-02a-resource-policy-drill.sh
run_check board-delegation bash scripts/v12-02a-board-delegation-drill.sh
run_check scope-delegation bash scripts/v12-02a-scope-delegation-drill.sh
run_check delegation-store bash scripts/v12-02a-delegation-store-drill.sh
run_check delegation-service bash scripts/v12-02a-delegation-service-drill.sh
run_check permission-path bash scripts/v12-02a-permission-path-drill.sh

python3 - "$work_dir" "$report" <<'PY'
import hashlib
import json
import platform
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

work, report = map(Path, sys.argv[1:])
root = Path.cwd()
evidence_dir = Path('docs/项目计划书v1/项目计划v1.2/evidence')
slices = {
    'public-policy': 'v12-02a-public-policy-linux.json',
    'resource-policy': 'v12-02a-resource-policy-linux.json',
    'board-delegation': 'v12-02a-board-delegation-linux.json',
    'scope-delegation': 'v12-02a-scope-delegation-linux.json',
    'delegation-store': 'v12-02a-delegation-store-linux.json',
    'delegation-service': 'v12-02a-delegation-service-linux.json',
    'permission-path': 'v12-02a-permission-path-linux.json',
}
sha = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
slice_reports = {}
for name, filename in slices.items():
    path = evidence_dir / filename
    data = json.loads(path.read_text())
    assert data['slice_status'] == 'completed', name
    assert data['implementation'] == data['automation'] == data['target_environment'] == 'passed', name
    assert data['common_gates_status'] == 'passed', name
    slice_reports[name] = {'report': str(path), 'sha256': sha(path), 'slice_status': data['slice_status'], 'common_gates_status': data['common_gates_status']}
logs = {
    name: {'path': str((work / f'{name}.log').relative_to(root)), 'sha256': sha(work / f'{name}.log')}
    for name in slices
}
data = {
    'schema': 'campusos.v12-02a-exit-acceptance/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-02a',
    'stage_status': 'completed',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'scope': 'V12-02a stage exit: pure policy kernels, delegation store/service, and the PermissionService governance path replacement re-accepted against the current tree',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'go': subprocess.check_output(['go', 'version'], text=True).strip(),
        'node': subprocess.check_output(['node', '--version'], text=True).strip(),
    },
    'slices': slice_reports,
    'target_logs': logs,
    'residual_scope': [
        'Global role management (assertActorMayGrantCodes callers beyond the moderator category path) keeps the catalog checks pending a future contract; the G1 replacement-map checker still requires pending and is not falsified.',
        'Admin identity domain separation is V12-02d; business fact wiring, ABA versioning and HTTP entry principals are V12-02b; machine/plugin principals and real egress limits are V12-02c/03b.',
        'MFA-required delegation grants fail closed on the read path until V12-02b wires entry principals; seeded and default grants use password strength.',
    ],
}
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print('V12-02a stage exit passed: 7 slice drills re-accepted; stage completed')
PY
