#!/usr/bin/env bash
# V12-01b shared Egress component: owned local TLS fixture, controlled DNS/dial.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 && $# != 2 )); then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi
if (( $# == 2 )) && [[ "$1" != "--report" ]]; then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi
[[ "$(uname -s)" == Linux ]] || { echo "Linux target environment required" >&2; exit 1; }
command -v go >/dev/null || { echo "Go is required" >&2; exit 127; }
command -v python3 >/dev/null || { echo "Python 3 is required" >&2; exit 127; }

mkdir -p .cache/v12-01b/tmp
export TMPDIR="$repo_root/.cache/v12-01b/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
report="$repo_root/.cache/v12-01b/egress-linux.json"
if (( $# == 2 )); then report="$2"; fi
work_dir="$(mktemp -d "$repo_root/.cache/v12-01b/egress.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT

# Each test starts a genuine loopback TLS server with an ephemeral test CA.
# The package-private resolver returns controlled public/private addresses and
# a recording dialer maps only validated public-IP dials to that server.
go test ./internal/platform/security -run '^TestEgress' -count=1 -v >"$work_dir/go-test.log"
cat "$work_dir/go-test.log"

V12_GO_VERSION="$(go version)" python3 - "$work_dir/go-test.log" "$report" <<'PY'
import hashlib
import json
import os
import platform
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

expected = {
    'TestEgressPolicyExactHTTPSOrigin': 'exact_https_origin_and_input_policy',
    'TestEgressPolicyRejectsUnboundedLimits': 'bounded_policy_limits',
    'TestEgressPolicyRejectsPrivateAndSpecialUseDNSResults': 'nonpublic_address_rejection',
    'TestEgressBrokerLocalTLSAndFreshDNSPerConnection': 'local_tls_proxy_bypass_and_fresh_dns_per_connection',
    'TestEgressBrokerDNSRebindingFailsBeforeDial': 'dns_rebinding_and_mixed_answer_rejected_before_dial',
    'TestEgressBrokerRedirectNeverForwardsSecretCrossOrigin': 'redirects_checked_before_secret_forward',
    'TestEgressBrokerExactTargetRejectsSameOriginCredentialRedirect': 'exact_target_same_origin_307_rejected_before_secret_forward',
    'TestEgressBrokerBoundsAndTLSVerification': 'request_response_header_timeout_and_tls_bounds',
    'TestEgressBrokerDefaultDeny': 'empty_approval_set_denied',
}
log = Path(sys.argv[1]).read_text()
passed = set(re.findall(r'^--- PASS: (TestEgress\w+)\b', log, flags=re.MULTILINE))
skipped = set(re.findall(r'^--- SKIP: (TestEgress\w+)\b', log, flags=re.MULTILINE))
failed = set(re.findall(r'^--- FAIL: (TestEgress\w+)\b', log, flags=re.MULTILINE))
if passed != set(expected) or skipped or failed:
    raise SystemExit(f'Egress target tests missing, skipped, or failed: passed={sorted(passed)} skipped={sorted(skipped)} failed={sorted(failed)}')
paths = [
    'internal/platform/security/egress_policy.go',
    'internal/platform/security/egress_policy_test.go',
    'internal/platform/security/egress.go',
    'internal/platform/security/egress_test.go',
    'scripts/v12-01b-egress-drill.sh',
]
report = {
    'schema': 'campusos.v12-stage-slice-exit/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-01b',
    'slice': 'shared_egress_component_exact_https_origin_dns_pinned_tls',
    'stage_status': 'partial',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'go_version': os.environ['V12_GO_VERSION'],
        'tls_target': 'real local ephemeral HTTPS server with synthetic CA',
        'network_fixture': 'package-private deterministic resolver and recording dialer; validated public-IP dial mapped to local TLS socket',
        'production_dns_or_external_network_tested': False,
        'ipv6_address_policy': 'public 2000::/3 only; full IETF protocol 2001::/23, documentation and transition blocks denied conservatively after IPv4-mapped unmapping',
        'ipv6_special_registry': 'https://www.iana.org/assignments/iana-ipv6-special-registry/',
    },
    'checks': {label: 'passed' for label in expected.values()},
    'scope': 'shared in-process Security Egress component only; Host API and Runner routing/containment remain separate stage work',
    'source_sha256': {path: hashlib.sha256(Path(path).read_bytes()).hexdigest() for path in paths},
}
output = Path(sys.argv[2])
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
print(f'wrote {output}')
PY
