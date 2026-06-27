#!/usr/bin/env bash
set -euo pipefail

./scripts/docker-up.sh
./scripts/migrate.sh up

echo "==> Starting CampusOS API"
go run ./cmd/server/main.go
