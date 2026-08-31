#!/usr/bin/env bash
set -euo pipefail

export GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}"

resource_roots=(
  data/resources/homepage-packs
  data/resources/space-style-packs
  data/resources/themes
)

resources=()
for root in "${resource_roots[@]}"; do
  while IFS= read -r -d '' resource; do
    resources+=("$resource")
  done < <(find "$root" -mindepth 1 -maxdepth 1 -type d -print0 | sort -z)
done

if ((${#resources[@]} == 0)); then
  echo "no appearance resource packages found" >&2
  exit 1
fi

for resource in "${resources[@]}"; do
  go run ./cmd/campusosctl resource inspect "$resource" >/dev/null
done

go test ./internal/modules/features/appearance/stylepack ./internal/modules/features/appearance/homepage ./internal/modules/features/appearance/webtheme ./internal/modules/features/personalspace -count=1
node --check web/tests/style-pack-responsive.mjs

echo "v0.13 appearance delivery check passed: ${#resources[@]} resource packages are covered by strict contracts and the browser matrix"
