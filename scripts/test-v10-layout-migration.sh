#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
backup="$fixture/rollback"
trap 'rm -rf "$fixture"' EXIT

mkdir -p \
  "$fixture/internal/modules" \
  "$fixture/data/plugins/personal-space" \
  "$fixture/data/plugins/example-external" \
  "$fixture/data/plugin_data/personal-space/style-packs/custom-space" \
  "$fixture/data/plugin_data/personal-space/styles" \
  "$fixture/data/module_data" \
  "$fixture/data/personal-space" \
  "$fixture/data/resources"
cp -R "$repo_root/modules" "$fixture/modules"
for kind in themes homepage-packs space-style-packs skills prompts personas knowledge-metadata; do
  mkdir -p "$fixture/data/resources/$kind"
done

cat >"$fixture/data/plugins/personal-space/plugin.yaml" <<'EOF'
name: personal-space
runtime: builtin
EOF
cat >"$fixture/data/plugins/example-external/plugin.yaml" <<'EOF'
name: example-external
runtime: wasm
EOF
cat >"$fixture/data/plugin_data/personal-space/style-packs/custom-space/style.yaml" <<'EOF'
schema: page-style-pack.v1
name: custom-space
target: personal-space
version: 1.0.0
EOF
printf ':root { color: #111; }\n' >"$fixture/data/plugin_data/personal-space/style-packs/custom-space/theme.css"
printf '{"name":"legacy-style"}\n' >"$fixture/data/plugin_data/personal-space/styles/legacy.space-style.json"

CAMPUSOS_ROOT="$fixture" "$repo_root/scripts/migrate-v10-module-plugin-layout.sh" apply "$backup" >/dev/null
test -f "$fixture/data/resources/space-style-packs/custom-space/resource.json"
test -f "$fixture/data/module_data/personal-space/styles/legacy.space-style.json"
test -f "$backup/legacy-plugin-descriptors/personal-space/plugin.yaml"
test ! -e "$fixture/data/plugins/personal-space"
CAMPUSOS_GOVERNANCE_ROOT="$fixture" python3 "$repo_root/scripts/check-data-governance.py" >/dev/null

CAMPUSOS_ROOT="$fixture" "$repo_root/scripts/migrate-v10-module-plugin-layout.sh" rollback "$backup" >/dev/null
test -f "$fixture/data/plugins/personal-space/plugin.yaml"
test -f "$fixture/data/plugin_data/personal-space/styles/legacy.space-style.json"
test -f "$fixture/data/plugin_data/personal-space/style-packs/custom-space/style.yaml"
test ! -e "$fixture/data/resources/space-style-packs/custom-space"

echo "v10 module/plugin layout migration and rollback drill passed"
