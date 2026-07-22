#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}"

go test ./internal/platform/reliability -count=1 \
  -run 'TestWorkerMetricsPreserveReceiptsAndConverge|TestStoppedWorkerBacklogIsVisibleAndRecovers|TestDeadLetterGrowthIsObservable'
go test ./internal/modules/core/emaildelivery -count=1 \
  -run 'TestDeliveryMetricsUseBoundedLabelsAndDoNotLeakMessageData|TestDeliveryMetricsShowTransientFailureAndRecovery'
go test ./internal/modules/core/identity/service -count=1 \
  -run 'TestChallengeLifecycleUsesOpaqueOutboxAndOneTimeTicket|TestSessionRefreshRotationRejectsReuseAndRevokesFamily'

grep -q 'CampusOSReliabilityWorkerStopped' deploy/prometheus/campusos-v13.rules.yml
grep -q 'CampusOSEmailDeliveryDegraded' deploy/prometheus/campusos-v13.rules.yml
grep -q 'CampusOSRefreshTokenReuseDetected' deploy/prometheus/campusos-v13.rules.yml

echo "v13 reliability observability fault checks passed"
