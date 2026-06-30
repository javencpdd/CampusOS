#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="${1:-$(cd "$script_dir/../../.." && pwd)}"
cd "$repo"

echo "==> repository"
git status -sb

echo "==> branch"
git branch --show-current

echo "==> v0.3-dev progress docs"
find docs/进度/v0.3-dev -maxdepth 1 -type f -name 'v0.3.*-dev.md' | sort || true

echo "==> diff whitespace check"
git diff --check

echo "==> go tests"
GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test ./...
