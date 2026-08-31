#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test \
  ./internal/modules/features/mutualaid \
  -run 'Test(CreateAndStatusTransitionKeepsCommunityGovernanceIndependent|DisabledMutualAidDoesNotBlockPlainThreads|ListPublicUsesOneBatchAndPreservesIntegrityFailure|CreateRollsBackThreadAndReliableEvidenceWhenDetailWriteFails|UpdateRollsBackThreadDetailAndReliableEvidenceWhenDetailWriteFails|StatusUpdateRejectsTrashedThreadWithoutNewReliableEvidence|WriteErrorDoesNotExposeUnexpectedFailure|WriteErrorMapsTrashedThreadToConflict)$' \
  -count=1

rg -q 'feature.mutual-aid' modules/features/mutual-aid/module.yaml
rg -q 'mutual_aid_details' migrations/000034_v12_mutual_aid.up.sql
rg -q 'CreateStructuredThread' internal/modules/features/mutualaid/service.go
rg -q 'ErrThreadNotEditable' internal/modules/features/mutualaid/service.go
rg -Fq 'response.WriteError(c, errorTranslator.Translate(err))' internal/modules/features/mutualaid/handler.go
rg -Fq 'MustTranslator("feature.mutual-aid"' internal/modules/features/mutualaid/error_contract.go
rg -Uq 'Prefix: "/api/v1/mutual-aid",\s*FeatureID: "mutual-aid"' internal/transport/httpapi/router.go
if rg -n 'NewPg(Thread|Category|Post)Repository|pgxpool' internal/modules/features/mutualaid/service.go; then
  echo "mutual aid check failed: feature bypasses Community or profile-owned persistence boundaries" >&2
  exit 1
fi

echo "v0.12 mutual aid checks passed"
