#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test \
  ./internal/modules/core/community/service \
  ./internal/modules/features/richtext \
  -run 'Test(AuthorThreadCommandsRollbackWhenRevisionWriteFails|AuthorThreadCommandsWriteOneAuditAndOutboxPerCommittedMutation|ReliableRichTextCreateRollsBackThreadRevisionAuditAndOutbox|ReliableRichTextUpdateRollsBackThreadAndArticleTogether)$' \
  -count=1

rg -q 'executeAuthorCommand' internal/modules/core/community/service/thread_service.go
rg -q 'executeArticleCommand' internal/modules/features/richtext/service.go
rg -q 'transaction.ExecutorFor' internal/modules/features/richtext/store.go
if rg -q 'richtext_create_rollback' internal/modules/features/richtext/service.go; then
  echo "v0.12 failure injection check failed: RichText must not compensate with a trashed Thread" >&2
  exit 1
fi

echo "v0.12 content failure-injection checks passed"
