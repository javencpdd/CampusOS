#!/usr/bin/env bash
# V12-01b first slice: retired raw Secret Host API methods stay denied on Linux loopback HTTP.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
for executable in go python3 uname; do
  command -v "$executable" >/dev/null || { echo "missing $executable" >&2; exit 127; }
done
if (( $# != 0 && $# != 2 )); then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi
if (( $# == 2 )) && [[ "$1" != "--report" ]]; then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi
if [[ "$(uname -s)" != "Linux" ]]; then
  echo "Linux target environment is required" >&2
  exit 1
fi

mkdir -p .cache/v12-01b/tmp
export TMPDIR="$repo_root/.cache/v12-01b/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
report="$repo_root/.cache/v12-01b/secret-retirement.json"
if (( $# == 2 )); then report="$2"; fi
log="$(mktemp "$repo_root/.cache/v12-01b/secret-retirement.XXXXXX.log")"
trap 'rm -f -- "$log"' EXIT

go test ./internal/plugin/hostapi -run '^TestLegacySecretReadsRejectedOnLoopbackHostAPI$' -count=1 -v | tee "$log"
for name in \
  'TestLegacySecretReadsRejectedOnLoopbackHostAPI' \
  'TestLegacySecretReadsRejectedOnLoopbackHostAPI/GetSystemSecret' \
  'TestLegacySecretReadsRejectedOnLoopbackHostAPI/GetUserSecret'; do
  if ! grep -Fq -- "--- PASS: $name " "$log"; then
    echo "required test did not run and pass: $name" >&2
    exit 1
  fi
done

python3 - "$report" <<'PY'
import hashlib
import json
import platform
import sys
from datetime import datetime, timezone
from pathlib import Path

paths = [
    'internal/plugin/hostapi/grpc_server.go',
    'internal/plugin/hostapi/permissions.go',
    'internal/plugin/hostapi/permissions_test.go',
    'internal/server/platform_modules.go',
    'sdk/go/hostapi.go',
    'docs/api/plugin-permissions-v1.json',
    'docs/api/plugin-permissions-v2.json',
    'docs/api/Host-API-v1权限目录.md',
    'docs/api/v1.2插件宿主资源与非侵入式安装合同.md',
    'docs/help/系统设计相关/插件三层授权与v3开发指南.md',
    'examples/plugins/v1-mail-watcher/README.md',
    'scripts/v12-01b-secret-retirement-drill.sh',
]
report = {
    'schema': 'campusos.v12-stage-slice-exit/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-01b',
    'slice': 'retire_legacy_raw_secret_host_api',
    'stage_status': 'partial',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'transport': 'Linux loopback HTTP via httptest.NewServer',
        'store': 'MemorySecretStore with active system and user secrets',
        'database': 'not used by this pre-store rejection path',
    },
    'checks': {
        'legacy_system_secret_http_403_without_raw_value': 'passed',
        'legacy_user_secret_http_403_without_raw_value': 'passed',
        'unaffected_storage_http_200': 'passed',
        'retired_methods_absent_from_permission_catalog': 'passed',
    },
    'remaining_in_stage': [
        'shared Security/Secret Port and key rotation/recovery',
        'managed Secret use Broker and config repository',
        'Egress Broker DNS/redirect/endpoint checks',
        'host resource policy and purpose budget',
    ],
    'source_sha256': {path: hashlib.sha256(Path(path).read_bytes()).hexdigest() for path in paths},
}
output = Path(sys.argv[1])
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
print(f'wrote {output}')
PY
