#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}"

go test ./cmd/campusos-baseline ./cmd/campusos-capacity ./pkg/observability -count=1
go test ./internal/modules/features/mutualaid ./internal/modules/features/secondhand -count=1
go test ./internal/modules/core/identity/service ./internal/platform/reliability -count=1
go run ./cmd/campusos-capacity help >/dev/null
python3 scripts/check-frontend-bundles.py

test -f docs/项目计划v0.13/evidence/v0.13-capacity-budget.json
grep -q '"max_p95_regression_ratio"' docs/项目计划v0.13/evidence/v0.13-capacity-budget.json
test -f docs/项目计划v0.13/evidence/v0.13-capacity-drill-budget.json
grep -q '"max_p95_absolute_regression_ms"' docs/项目计划v0.13/evidence/v0.13-capacity-drill-budget.json

echo "v0.13 capacity gate passed: static budgets and repeatable comparison tooling are available"
