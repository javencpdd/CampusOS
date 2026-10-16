#!/usr/bin/env bash
# Bounded V12-02d JWT domain separation acceptance. No database, HTTP route or
# credential verifier is involved.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

if (( $# != 0 && $# != 2 )) || { (( $# == 2 )) && [[ "$1" != "--report" ]]; }; then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi
[[ "$(uname -s)" == "Linux" ]] || { echo "Linux target environment required" >&2; exit 1; }
for executable in go python3; do
  command -v "$executable" >/dev/null || { echo "missing $executable" >&2; exit 127; }
done

report="${2:-docs/项目计划书v1/项目计划v1.2/evidence/v12-02d-jwt-domains-linux.json}"
mkdir -p .cache/v12-02d-jwt/tmp
export TMPDIR="$repo_root/.cache/v12-02d-jwt/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02d-jwt/jwt.XXXXXX")"

run_check() {
  local name="$1"
  shift
  echo "V12-02d jwt domains: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then
    return
  fi
  tail -n 120 "$work_dir/$name.log" >&2
  return 1
}

run_check targeted go test ./pkg/auth -count=1 -v
run_check build go test -race -c -o "$work_dir/auth.test" ./pkg/auth
(
  cd pkg/auth
  run_check native env -i PATH="$PATH" "$work_dir/auth.test" -test.v
)

python3 - "$work_dir" "$report" <<'PY'
import hashlib
import json
import platform
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

work, report = map(Path, sys.argv[1:])
root = Path.cwd()
required_tests = [
    'TestDomainJWTManagersNeverVerifyAcrossDomains',
    'TestDomainJWTManagerRejectsKidAudienceMismatch',
    'TestDomainJWTManagersRefuseSharedIdentity',
    'TestJWTManagerKeepsLegacyNoDomainConfig',
]
for test in required_tests:
    assert re.search(r'^--- PASS: ' + test + r'(?: |$)', (work / 'native.log').read_text(), re.M), test
sha = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
sources = [
    'pkg/auth/jwt.go',
    'pkg/auth/jwt_domains_test.go',
    'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md',
    'scripts/v12-02d-jwt-domains-drill.sh',
]
logs = {
    name: {'path': str((work / f'{name}.log').relative_to(root)), 'sha256': sha(work / f'{name}.log')}
    for name in ['targeted', 'build', 'native']
}
dependencies = {}
for name in ['v12-g0-exit.json', 'v12-g1-exit-linux.json', 'v12-01a-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-02a-exit-linux.json', 'v12-02d-identity-domains-linux.json', 'v12-02d-admin-store-linux.json', 'v12-02d-admin-service-linux.json']:
    path = Path('docs/项目计划书v1/项目计划v1.2/evidence') / name
    dependencies[name] = {
        'report': str(path),
        'sha256': sha(path),
        'role': 'accepted predecessor evidence; historical source snapshots are retained unchanged',
    }
data = {
    'schema': 'campusos.v12-02d-jwt-domains-acceptance/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-02d',
    'stage_status': 'partial',
    'slice_status': 'completed',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'scope': 'pkg/auth JWT issuer/audience/key/kid per identity domain with cross-domain token rejection; legacy single-domain configuration unchanged',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'go': subprocess.check_output(['go', 'version'], text=True).strip(),
        'execution': 'native Linux race-enabled compiled test binary with env -i',
    },
    'checks': {test: 'passed' for test in required_tests},
    'coverage': {
        'bidirectional_cross_domain_rejection': True,
        'kid_audience_issuer_mismatch_rejection': 3,
        'shared_identity_constructor_guards': 3,
        'legacy_configuration_compatibility': True,
    },
    'source_sha256': {path: sha(path) for path in sources},
    'dependencies': dependencies,
    'target_logs': logs,
    'native_binary_sha256': {'auth.test': sha(work / 'auth.test')},
    'common_gates_status': 'pending',
    'common_gates': {},
    'boundaries': [
        'No HTTP middleware, admin login route or session issuance consumes the domain managers yet; that arrives with the admin sessions slice.',
        'User-domain behavior is unchanged when Audience/KeyID stay empty; deployments opt into separation by configuring both domains.',
        'The identity module wires a single manager today; per-domain construction follows the admin sessions/HTTP slices.',
    ],
}
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print('Native Linux target acceptance passed; common gates pending')
PY

run_check go-all go test ./... -count=1
run_check contracts make contracts-check
run_check architecture make architecture-check
run_check docs-build make docs-build
run_check docs-links make docs-links
run_check line-endings python3 scripts/check-line-endings.py --include-untracked
run_check diff git diff --check

python3 - "$work_dir" "$report" <<'PY'
import hashlib
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

work, report = map(Path, sys.argv[1:])
data = json.loads(report.read_text())
commands = {
    'go-all': 'go test ./... -count=1',
    'contracts': 'make contracts-check',
    'architecture': 'make architecture-check',
    'docs-build': 'make docs-build',
    'docs-links': 'make docs-links',
    'line-endings': 'python3 scripts/check-line-endings.py --include-untracked',
    'diff': 'git diff --check',
}
for name, command in commands.items():
    path = work / f'{name}.log'
    data['common_gates'][name] = {
        'command': command,
        'status': 'passed',
        'exit_code': 0,
        'local_log': str(path.relative_to(Path.cwd())),
        'log_sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
    }
for path, digest in data['source_sha256'].items():
    assert hashlib.sha256(Path(path).read_bytes()).hexdigest() == digest, 'source changed during acceptance: ' + path
data['common_gates_status'] = 'passed'
data['generated_at'] = datetime.now(timezone.utc).isoformat()
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print('V12-02d JWT domain slice passed; 7 common gates passed; stage remains partial')
PY
