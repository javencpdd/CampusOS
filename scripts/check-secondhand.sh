#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

rg -q 'feature.secondhand' modules/features/secondhand/module.yaml
rg -q 'CREATE TABLE public\.secondhand_details' migrations/000001_v1_schema_baseline.up.sql
rg -q 'CreateStructuredThread' internal/modules/features/secondhand/service.go
rg -q 'ErrThreadNotEditable' internal/modules/features/secondhand/service.go
rg -Fq 'response.WriteError(c, errorTranslator.Translate(err))' internal/modules/features/secondhand/handler.go
rg -Fq 'MustTranslator("feature.secondhand"' internal/modules/features/secondhand/error_contract.go
rg -Uq 'Prefix: "/api/v1/secondhand",\s*FeatureID: "secondhand"' internal/transport/httpapi/router.go

if rg -n 'NewPg(Thread|Category|Post)Repository|pgxpool' internal/modules/features/secondhand/service.go; then
  echo "secondhand service must depend on Community ports, not concrete repositories" >&2
  exit 1
fi

GOCACHE=/tmp/campusos-go-cache go test ./internal/modules/features/secondhand -count=1
echo "secondhand checks passed against the v1 database baseline"
