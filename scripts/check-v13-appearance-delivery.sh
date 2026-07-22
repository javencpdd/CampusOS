#!/usr/bin/env bash
set -euo pipefail

export GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}"

resources=(
  data/resources/homepage-packs/campus-hero
  data/resources/space-style-packs/clean-blog
  data/resources/space-style-packs/kinetic-journal
  data/resources/themes/aurora-campus
  data/resources/themes/campus-canvas
)

for resource in "${resources[@]}"; do
  go run ./cmd/campusosctl resource inspect "$resource" >/dev/null
done

go test ./internal/modules/features/appearance/stylepack ./internal/modules/features/appearance/homepage ./internal/modules/features/appearance/webtheme ./internal/modules/features/personalspace -count=1
node --check web/tests/style-pack-responsive.mjs

echo "v13 appearance delivery check passed: strict resource contracts and matrix runner are available"
