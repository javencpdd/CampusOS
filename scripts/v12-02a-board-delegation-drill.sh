#!/usr/bin/env bash
# Bounded V12-02a board-delegation Policy Port acceptance. This exercise has no
# HTTP, database, credential verifier, authority loader, or audit writer claim.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

if (( $# != 0 && $# != 2 )) || { (( $# == 2 )) && [[ "$1" != "--report" ]]; }; then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi
[[ "$(uname -s)" == "Linux" ]] || { echo "Linux target environment required" >&2; exit 1; }
for executable in go node python3; do
  command -v "$executable" >/dev/null || { echo "missing $executable" >&2; exit 127; }
done

report="${2:-docs/项目计划书v1/项目计划v1.2/evidence/v12-02a-board-delegation-linux.json}"
mkdir -p .cache/v12-02a-delegation/tmp
export TMPDIR="$repo_root/.cache/v12-02a-delegation/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02a-delegation/board-delegation.XXXXXX")"

run_check() {
  local name="$1"
  shift
  echo "V12-02a board delegation: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then
    return
  fi
  tail -n 120 "$work_dir/$name.log" >&2
  return 1
}

# First verify the unchanged G1 schema/semantic specification, then use it to
# generate an oracle corpus. The production Go policy never imports the Node
# checker or accepts its JSON as a grant.
run_check g1-contract node scripts/check-v12-board-delegation-contract.mjs
run_check generate-oracle node scripts/check-v12-02a-board-delegation-output.mjs --generate "$work_dir/g1-cases.json"
run_check targeted go test ./internal/modules/core/identity/port ./internal/modules/core/identity/policy -run '^TestBoardDelegation' -count=1 -v

# Compile native race-enabled test binaries, then run them with a cleared
# environment. The logs, corpus, output, and binaries remain below .cache as
# evidence; no shared database or developer data is mutated.
run_check build-port go test -race -c -o "$work_dir/delegation-port.test" ./internal/modules/core/identity/port
run_check build-policy go test -race -c -o "$work_dir/delegation-policy.test" ./internal/modules/core/identity/policy
(
  cd internal/modules/core/identity/port
  run_check native-port env -i PATH="$PATH" "$work_dir/delegation-port.test" -test.run '^TestBoardDelegation' -test.v
)
(
  cd internal/modules/core/identity/policy
  run_check native-policy env -i PATH="$PATH" \
    CAMPUSOS_DELEGATION_CORPUS_IN="$work_dir/g1-cases.json" \
    CAMPUSOS_DELEGATION_CORPUS_OUT="$work_dir/go-output.json" \
    "$work_dir/delegation-policy.test" -test.run '^TestBoardDelegation' -test.v
)
run_check verify-oracle node scripts/check-v12-02a-board-delegation-output.mjs --verify "$work_dir/g1-cases.json" "$work_dir/go-output.json"

# Write target evidence before documentation link checking traverses its final
# reference. Common gates are appended below after all target tests pass.
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
required = {
    'native-port': [
        'TestBoardDelegationCodecRoundTrip',
        'TestBoardDelegationCodecRejectsAmbiguousInput',
        'TestBoardDelegationTimePrecision',
        'TestBoardDelegationDecisionCodec',
    ],
    'native-policy': [
        'TestBoardDelegationG1WireCorpus',
        'TestBoardDelegationAllowAndWitnessOrder',
        'TestBoardDelegationDenySequence',
        'TestBoardDelegationBatchIsAtomic',
        'TestBoardDelegationMalformedTypedInput',
        'TestBoardDelegationDecisionBindingAndCurrentFacts',
        'TestBoardDelegationDenyDecisionConsumption',
        'TestBoardDelegationParallelWithoutCache',
    ],
}
for name, tests in required.items():
    text = (work / f'{name}.log').read_text()
    assert not re.search(r'^--- (?:FAIL|SKIP): ', text, re.M), name
    for test in tests:
        assert re.search(r'^--- PASS: ' + re.escape(test) + r'(?: |$)', text, re.M), test

sha = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
sources = [
    'internal/modules/core/identity/port/principal.go',
    'internal/modules/core/identity/port/strict_json.go',
    'internal/modules/core/identity/port/resource_policy.go',
    'internal/modules/core/identity/port/board_delegation_policy.go',
    'internal/modules/core/identity/port/board_delegation_policy_test.go',
    'internal/modules/core/identity/policy/board_delegation.go',
    'internal/modules/core/identity/policy/board_delegation_test.go',
    'internal/modules/core/identity/policy/board_delegation_interop_test.go',
    'docs/api/principal-context-v1.schema.json',
    'docs/api/board-delegation-v1.schema.json',
    'docs/api/board-delegation-v1.errors.json',
    'docs/api/v1.2版块治理委托上限合同.md',
    'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md',
    'sdk/typescript/src/principal.ts',
    'sdk/typescript/src/board-delegation.ts',
    'sdk/typescript/tests/board-delegation-v1.fixtures.json',
    'sdk/typescript/tests/principal-context-v1.fixtures.json',
    'sdk/typescript/package.json',
    'sdk/typescript/pnpm-lock.yaml',
    'scripts/check-v12-board-delegation-contract.mjs',
    'scripts/check-v12-02a-board-delegation-output.mjs',
    'scripts/v12-02a-board-delegation-drill.sh',
]
logs = {
    name: {
        'path': str((work / f'{name}.log').relative_to(root)),
        'sha256': sha(work / f'{name}.log'),
    }
    for name in ['g1-contract', 'generate-oracle', 'targeted', 'build-port', 'build-policy', 'native-port', 'native-policy', 'verify-oracle']
}
dependencies = {}
for name in ['v12-g0-exit.json', 'v12-g1-exit-linux.json', 'v12-01a-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-02a-public-policy-linux.json', 'v12-02a-resource-policy-linux.json']:
    path = Path('docs/项目计划书v1/项目计划v1.2/evidence') / name
    dependencies[name] = {
        'report': str(path),
        'sha256': sha(path),
        'role': 'accepted predecessor evidence; historical source snapshots are retained unchanged',
    }

data = {
    'schema': 'campusos.v12-02a-board-delegation-acceptance/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-02a',
    'stage_status': 'partial',
    'slice_status': 'completed',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'scope': 'campusos.board-delegation/v1 proposal judgement, strict host DTO codecs, and pre-commit decision/current-snapshot checking; pure Go Port only',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'go': subprocess.check_output(['go', 'version'], text=True).strip(),
        'node': subprocess.check_output(['node', '--version'], text=True).strip(),
        'execution': 'native Linux race-enabled compiled test binaries with env -i; no database, HTTP endpoint, credential verifier, cache, network, or shared instance mutated',
    },
    'checks': {test: 'passed' for tests in required.values() for test in tests},
    'coverage': {
        'g1_fixtures': 54,
        'subset_combinations': 72,
        'principal_domain_status_combinations': 90,
        'native_go_oracle_outputs': 216,
        'go_allow_outputs': 10,
        'go_deny_outputs': 189,
        'go_input_rejections': 17,
        'strict_codec_test_groups': 4,
        'same_call_binding_checks': 21,
        'parallel_callers': 64,
    },
    'source_sha256': {path: sha(path) for path in sources},
    'dependencies': dependencies,
    'target_logs': logs,
    'native_binary_sha256': {
        path.name: sha(path)
        for path in [work / 'delegation-port.test', work / 'delegation-policy.test']
    },
    'oracle_corpus': {
        'path': str((work / 'g1-cases.json').relative_to(root)),
        'sha256': sha(work / 'g1-cases.json'),
    },
    'actual_go_output': {
        'path': str((work / 'go-output.json').relative_to(root)),
        'sha256': sha(work / 'go-output.json'),
    },
    'common_gates_status': 'pending',
    'common_gates': {},
    'boundaries': [
        'No production HTTP route, session/credential verifier, database repository, global authority loader, cache, or authorization write path changed.',
        'The Port validates host-supplied facts only; JSON validity does not authenticate a caller, and delegable:true is never client-asserted.',
        'The caller must reload management/bounds/recipient/boards in its own transaction, re-evaluate, write the grant and required audit atomically, and reject after revocation commits.',
        'The current PermissionService execution/grant path replacement and real identity wiring remain later V12-02a work; admin identity domain separation remains V12-02d; application service and transaction integration remain V12-02b.',
        'Non-board scope delegation (knowledge.source.read, plugin.config.system.read, integration.webhook.invoke) follows the separate scope-delegation contract in V12-02c/03b.',
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
print('V12-02a board-delegation Policy slice passed; 7 common gates passed; stage remains partial')
PY
