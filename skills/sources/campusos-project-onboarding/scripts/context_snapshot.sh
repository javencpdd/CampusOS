#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
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

section "Version plan authority"
sed -n '1,120p' docs/计划书总结/README.md 2>/dev/null || true

section "Latest progress docs"
mapfile -t progress_docs < <(rg --files docs/进度 2>/dev/null | sort -V | tail -n 30)
printf '%s\n' "${progress_docs[@]}"

section "Migrations"
rg --files migrations -g '*.up.sql' | sort

section "Help docs"
rg --files docs/help | sort

section "Skill docs"
rg --files skills/guides 2>/dev/null | sort

section "Project skills"
rg --files skills -g 'SKILL.md' | sort

section "Make targets"
sed -n '1,120p' Makefile

section "Key implementation directories"
printf '%s\n' modules internal/modules internal/platform internal/plugin cmd web/src admin/src docs-site sdk data skills
