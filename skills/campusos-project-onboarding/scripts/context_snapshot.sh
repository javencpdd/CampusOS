#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT_DIR"

section() {
  printf '\n== %s ==\n' "$1"
}

section "Repository"
printf 'root: %s\n' "$ROOT_DIR"
printf 'branch: '
git branch --show-current || true
printf 'head: '
git rev-parse --short HEAD 2>/dev/null || true
git status -sb || true

section "Current README status lines"
sed -n '1,40p' README.md

section "Recent v0.5 progress docs"
find docs/进度/v0.5-dev -maxdepth 1 -type f -name 'v0.5.*-dev.md' 2>/dev/null | sort -V

section "Migrations"
find migrations -maxdepth 1 -type f -name '*.up.sql' | sort

section "Help docs"
find docs/help -type f | sort

section "Project skills"
find skills -maxdepth 2 -type f -name SKILL.md | sort

section "Make targets"
sed -n '1,120p' Makefile

section "Key implementation directories"
find internal cmd web/src admin/src sdk examples -maxdepth 2 -type d 2>/dev/null | sort | sed -n '1,160p'
