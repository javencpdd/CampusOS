#!/usr/bin/env bash
# V12-01b resource decision and purpose-budget Linux target-environment evidence.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
for executable in go node python3; do
  command -v "$executable" >/dev/null || { echo "missing $executable" >&2; exit 127; }
done
[[ "$(uname -s)" == Linux ]] || { echo "Linux target environment required" >&2; exit 1; }
if (( $# != 0 && $# != 2 )); then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi
if (( $# == 2 )) && [[ "$1" != "--report" ]]; then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi
mkdir -p .cache/v12-01b/tmp
export TMPDIR="$repo_root/.cache/v12-01b/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
report="$repo_root/docs/项目计划书v1/项目计划v1.2/evidence/v12-01b-resource-policy-linux.json"
if (( $# == 2 )); then report="$2"; fi
work_dir="$(mktemp -d "$repo_root/.cache/v12-01b/resource-policy.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT

node scripts/check-v12-plugin-v5-resource-install-contract.mjs >"$work_dir/g1-contract.log"
go test -race ./internal/platform/security -run '^(TestResourcePolicy|TestPurposeBudget)' -count=1 -v >"$work_dir/policy-budget.log"
for test_name in \
  TestResourcePolicyFourWayIntersection \
  TestResourcePolicyRevocationAndExpiry \
  TestResourcePolicyRejectsUnsafeNetworkOrigins \
  TestResourcePolicyRejectsUnknownPrivilegesAndInvalidBounds \
  TestPurposeBudgetReserveSettleCancelAndUnknown \
  TestPurposeBudgetConcurrentReservationsNeverOverspend \
  TestPurposeBudgetTimeoutKeepsUnknownReservation \
  TestPurposeBudgetRevokeAndExpiryFailClosed \
  TestPurposeBudgetRejectsInvalidInputs; do
  if ! grep -Eq "^--- PASS: ${test_name}( |$)" "$work_dir/policy-budget.log" ||
      grep -Eq "^--- SKIP: ${test_name}( |$)" "$work_dir/policy-budget.log"; then
    echo "required resource policy test did not execute and pass: $test_name" >&2
    tail -n 100 "$work_dir/policy-budget.log" >&2
    exit 1
  fi
done
if grep -Eq '^--- (FAIL|SKIP): ' "$work_dir/policy-budget.log"; then
  echo "resource policy test failed or skipped" >&2
  tail -n 100 "$work_dir/policy-budget.log" >&2
  exit 1
fi

V12_01B_GO_VERSION="$(go version)" python3 - "$report" <<'PY'
import hashlib
import json
import os
import platform
import sys
from datetime import datetime, timezone
from pathlib import Path

paths = [
    'internal/platform/security/resource_policy.go',
    'internal/platform/security/resource_policy_test.go',
    'internal/platform/security/resource_policy_budget.go',
    'internal/platform/security/resource_policy_budget_test.go',
    'docs/api/plugin-v5-host-resources-v1.schema.json',
    'sdk/typescript/src/plugin-v5-resource-install.ts',
    'scripts/check-v12-plugin-v5-resource-install-contract.mjs',
    'scripts/v12-01b-resource-policy-drill.sh',
]
report = {
    'schema': 'campusos.v12-stage-slice-exit/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-01b',
    'slice': 'resource_policy_four_way_intersection_and_purpose_budget',
    'stage_status': 'partial',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'go': os.environ['V12_01B_GO_VERSION'],
        'race_detector': 'enabled',
        'network': 'offline policy evaluation',
    },
    'checks': {
        'g1_schema_sdk_offline_contract': 'passed',
        'four_way_numeric_and_exact_https_origin_intersection': 'passed',
        'revoked_and_expired_grant_denied': 'passed',
        'unsafe_targets_and_undeclared_host_capabilities_denied': 'passed',
        'purpose_reservation_settlement_cancel_unknown_charge': 'passed',
        'concurrent_reservations_no_overspend': 'passed',
        'timeout_keeps_unknown_usage_reserved': 'passed',
        'revocation_cancels_inflight_and_expiry_denies_new': 'passed',
    },
    'enforcement_scope': 'pure resource decision and process-local purpose budget; actual CPU/memory/container isolation belongs to V12-05/06 and shared distributed accounting is not claimed',
    'source_sha256': {path: hashlib.sha256(Path(path).read_bytes()).hexdigest() for path in paths},
}
output = Path(sys.argv[1])
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
print(f'wrote {output}')
PY
