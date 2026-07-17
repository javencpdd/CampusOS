#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test \
  ./internal/modules/core/community/service \
  ./internal/modules/features/richtext \
  -run 'Test(StructuredThreadRequiresFeatureAndCategoryPolicy|StructuredThreadParticipantFailureRollsBackBaseFactsAndEvidence|CategoryThreadTypePoliciesAreVersionedAndSeededForBoards|ReliableRichTextCreateRollsBackThreadRevisionAuditAndOutbox)$' \
  -count=1

rg -q 'CreateStructuredThread' internal/modules/core/community/port/port.go
rg -q 'PersistThreadDetail' internal/modules/core/community/service/thread_service.go
rg -q 'category_thread_type_policies' migrations/000033_v12_structured_threads.up.sql
if rg -n 'CreateStructuredThread' sdk examples/plugins internal/plugin internal/modules/features/mcp 2>/dev/null; then
  echo "structured thread check failed: External Plugin, SDK, or MCP boundary exposes transaction participation" >&2
  exit 1
fi

echo "v12 structured thread checks passed"
