#!/usr/bin/env bash
set -euo pipefail

GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test \
  ./internal/platform/transaction \
  ./internal/platform/reliability \
  ./internal/modules/core/identity/service \
  ./internal/modules/core/community/service \
  ./internal/modules/core/userstorage \
  ./internal/modules/features/personaldocuments \
  ./internal/plugin \
  -count=1 \
  -run 'Test(MemoryRestoresSnapshotsOnPanic|ExecuteRollsBackMemoryStateWhenRequiredAuditFails|ReplayCommandRollsBackWhenRequiredAuditFails|StaleLeaseCannotCompleteAfterRecovery|ConsumerReceiptSkipsRepeatedExternalDelivery|RegisterRollsBackUserWhenAccountOrRequiredAuditFails|GovernanceCommandRollsBackWhenRequiredEvidenceFails|GovernanceDenyAuditSurvivesReliableTransactionRollback|ManagerImportPackageRestoresPriorVersionAfterCatalogFailure|ObjectServiceCommitFailureCleansRenamedProviderFileAndReservation|DocumentVersionCommitEnqueuesMinimalDurablePreviewRequest|PreviewRequestConsumerAcknowledgesSafeFallback)'
echo "failure-injection check passed: transaction rollback, required audit rollback, lease fencing, consumer receipts, governance evidence, storage commit cleanup, document-version Outbox atomicity/safe fallback, registration rollback, and package replacement recovery are covered"
