#!/usr/bin/env bash
# Linux native declaration/CLI target only; does not install packages or touch DBs.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mkdir -p .cache/v12-03a-manifest
work="$(mktemp -d "$root/.cache/v12-03a-manifest/acceptance.XXXXXX")"
export GOCACHE="$root/.cache/v12-g0/go-cache"
echo "v5 Manifest target: $work"
go test -c -race -o "$work/plugin.test" ./internal/plugin >"$work/native-build.log" 2>&1
(
  cd internal/plugin
  env -i PATH="$PATH" TMPDIR="$work" CAMPUSOS_PLUGIN_V5_MANIFEST_OUTPUT="$work/native-decisions.json" \
    "$work/plugin.test" -test.run '^TestPluginV5Manifest' -test.count=1 -test.v
) >"$work/native.log" 2>"$work/native.stderr"
go tool test2json -p github.com/campusos/CampusOS/internal/plugin -t <"$work/native.log" >"$work/native.jsonl"
go test -race -json ./cmd/campusos-plugin-v5-check -count=1 >"$work/cli-tests.jsonl" 2>"$work/cli-tests.stderr"
go build -o "$work/campusos-plugin-v5-check" ./cmd/campusos-plugin-v5-check >"$work/cli-build.log" 2>&1
env -i PATH="$PATH" TMPDIR="$work" node scripts/check-v12-plugin-v5-native-manifest.mjs \
  --native "$work/native-decisions.json" --cli "$work/campusos-plugin-v5-check" --work "$work" >"$work/oracle-cli.log" 2>&1
python3 - "$work" <<'PY'
import json,pathlib,sys
w=pathlib.Path(sys.argv[1])
events=[json.loads(s) for s in (w/'native.jsonl').read_text().splitlines()]
assert not any(e['Action'] in ['fail','skip'] for e in events)
tops={e['Test'] for e in events if e['Action']=='pass' and 'Test' in e and '/' not in e['Test']}
assert tops=={'TestPluginV5ManifestStrictDecode','TestPluginV5ManifestCatalogAndRelease','TestPluginV5ManifestCatalogIsolation'},tops
assert 'WARNING: DATA RACE' not in (w/'native.log').read_text()+(w/'native.stderr').read_text()
cli=[json.loads(s) for s in (w/'cli-tests.jsonl').read_text().splitlines()]
assert not any(e['Action'] in ['fail','skip'] for e in cli)
assert any(e['Action']=='pass' and e.get('Test')=='TestPluginV5CheckCommand' for e in cli)
assert json.loads((w/'target-result.json').read_text())['status']=='passed'
print('Native and actual CLI target passed:',w)
PY
