#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

rg -q 'feature.secondhand' modules/features/secondhand/module.yaml
rg -q 'secondhand_details' migrations/000035_v12_secondhand.up.sql
rg -q 'CreateStructuredThread' internal/modules/features/secondhand/service.go
rg -q 'ErrThreadNotEditable' internal/modules/features/secondhand/service.go
rg -q '"internal server error"' internal/modules/features/secondhand/handler.go
rg -Uq 'Prefix: "/api/v1/secondhand",\s*FeatureID: "secondhand"' internal/transport/httpapi/router.go

if rg -n 'NewPg(Thread|Category|Post)Repository|pgxpool' internal/modules/features/secondhand/service.go; then
  echo "secondhand service must depend on Community ports, not concrete repositories" >&2
  exit 1
fi

GOCACHE=/tmp/campusos-go-cache go test ./internal/modules/features/secondhand -count=1
echo "v12 secondhand checks passed"
