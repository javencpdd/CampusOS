#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test \
  ./internal/modules/core/community/... \
  ./internal/transport/httpapi \
  ./internal/projectaudit \
  -count=1

rg -q 'node_kind VARCHAR\(16\)' migrations/000032_v12_category_hierarchy.up.sql
rg -q 'campusos_guard_category_hierarchy' migrations/000032_v12_category_hierarchy.up.sql
rg -q 'community.category.move' migrations/000032_v12_category_hierarchy.up.sql
rg -q 'UpdateIfVersion' internal/modules/core/community/repository/category_repository.go
rg -q 'ArchiveLegacyForActor' internal/modules/core/community/service/category_service.go
rg -q 'validatePostingCategory' internal/modules/core/community/service/post_service.go
rg -q 'GET\("/categories/tree"' internal/transport/httpapi/router.go
rg -q 'PUT\("/categories/:id/parent"' internal/transport/httpapi/router.go
rg -q 'community.category.archive' internal/transport/httpapi/router.go

echo "category hierarchy checks passed"
