#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
default_repo="$(cd "$script_dir/../../.." && pwd)"

repo="$default_repo"
stage=""

if [[ $# -ge 1 ]]; then
  if [[ "$1" =~ ^v[0-9]+\.[0-9]+-dev$ ]]; then
    stage="$1"
  else
    repo="$1"
  fi
fi

if [[ $# -ge 2 ]]; then
  stage="$2"
fi

cd "$repo"

detect_stage() {
  find docs/进度 -mindepth 1 -maxdepth 1 -type d -name 'v*-dev' -printf '%f\n' 2>/dev/null | sort -V | tail -n 1
}

if [[ -z "$stage" ]]; then
  stage="$(detect_stage || true)"
fi

if [[ -z "$stage" ]]; then
  echo "stage not found; pass one explicitly, for example: $0 $repo v0.4-dev" >&2
  exit 2
fi

echo "==> repository"
git status -sb

echo "==> branch"
git branch --show-current

echo "==> stage"
echo "$stage"

echo "==> plan docs"
stage_minor="${stage#v0.}"
stage_minor="${stage_minor%-dev}"
plan_dir="docs/项目计划v${stage_minor}"
if [[ -d "$plan_dir" ]]; then
  find "$plan_dir" -maxdepth 1 -type f | sort -V
else
  echo "missing plan directory: $plan_dir"
fi

echo "==> progress docs"
progress_dir="docs/进度/$stage"
if [[ -d "$progress_dir" ]]; then
  find "$progress_dir" -maxdepth 1 -type f -name "${stage%-dev}.*-dev.md" | sort -V || true
else
  echo "missing progress directory: $progress_dir"
fi

echo "==> diff whitespace check"
git diff --check

echo "==> go tests"
GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test ./...
