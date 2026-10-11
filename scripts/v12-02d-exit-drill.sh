#!/usr/bin/env bash
# V12-02d stage exit: re-run every accepted 02d slice drill against the
# current tree and aggregate their evidence. Each sub-drill runs its own
# target environment and common gates; this script adds no new claims.
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

mkdir -p .cache/v12-02d-exit/tmp
export TMPDIR="$repo_root/.cache/v12-02d-exit/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
report="${2:-docs/项目计划书v1/项目计划v1.2/evidence/v12-02d-exit-linux.json}"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02d-exit/exit.XXXXXX")"

run_check() {
  local name="$1"
  shift
  echo "V12-02d exit: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then
    return
  fi
  tail -n 120 "$work_dir/$name.log" >&2
  return 1
}

run_check identity-domains bash scripts/v12-02d-identity-domains-drill.sh
run_check admin-store bash scripts/v12-02d-admin-store-drill.sh
run_check admin-service bash scripts/v12-02d-admin-service-drill.sh
run_check jwt-domains bash scripts/v12-02d-jwt-domains-drill.sh
run_check admin-session bash scripts/v12-02d-admin-session-drill.sh
run_check mfa-security bash scripts/v12-02d-mfa-security-drill.sh
run_check admin-mfa bash scripts/v12-02d-admin-mfa-drill.sh
run_check auto-admission bash scripts/v12-02d-auto-admission-drill.sh
run_check http-switch bash scripts/v12-02d-http-switch-drill.sh

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
    'identity-domains': 'v12-02d-identity-domains-linux.json',
    'admin-store': 'v12-02d-admin-store-linux.json',
    'admin-service': 'v12-02d-admin-service-linux.json',
    'jwt-domains': 'v12-02d-jwt-domains-linux.json',
    'admin-session': 'v12-02d-admin-session-linux.json',
    'mfa-security': 'v12-02d-mfa-security-linux.json',
    'admin-mfa': 'v12-02d-admin-mfa-linux.json',
    'auto-admission': 'v12-02d-auto-admission-linux.json',
    'http-switch': 'v12-02d-http-switch-linux.json',
}
sha = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
slice_reports = {}
for name, filename in slices.items():
    path = evidence_dir / filename
    data = json.loads(path.read_text())
    assert data['slice_status'] == 'completed', name
    assert data['implementation'] == data['automation'] == data['target_environment'] == 'passed', name
    assert data['common_gates_status'] == 'passed', name
    slice_reports[name] = {'report': str(path), 'sha256': sha(path)}
logs = {
    name: {'path': str((work / f'{name}.log').relative_to(root)), 'sha256': sha(work / f'{name}.log')}
    for name in slices
}
data = {
    'schema': 'campusos.v12-02d-exit-acceptance/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-02d',
    'stage_status': 'completed',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'scope': 'V12-02d stage exit: identity domain policy kernel, admin domain stores/services, JWT domain separation, sessions, MFA, auto-admission removal and the HTTP domain switch re-accepted against the current tree',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'go': subprocess.check_output(['go', 'version'], text=True).strip(),
        'node': subprocess.check_output(['node', '--version'], text=True).strip(),
    },
    'slices': slice_reports,
    'target_logs': logs,
    'residual_scope': [
        'Admin recovery-ticket HTTP flows (password reset with TOTP recovery) remain for a follow-up slice.',
        'The legacy /auth/admin/login user-surface route and legacy admission rows remain as transitional compatibility; admin API routes no longer consume them.',
        'Admin MFA policy enforcement gate per route (admin_mfa_policies) arrives with the Admin UI policy management in V12-04.',
    ],
}
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print('V12-02d stage exit passed: 9 slice drills re-accepted; stage completed')
PY
