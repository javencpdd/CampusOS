#!/usr/bin/env bash
set -euo pipefail

ACTION="${1:-check}"
CODE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROOT_DIR="${CAMPUSOS_ROOT:-$CODE_ROOT}"
BACKUP_DIR="${2:-$ROOT_DIR/backups/v10-layout-$(date -u +%Y%m%dT%H%M%SZ)}"
STATE_FILE="$BACKUP_DIR/layout-moves.tsv"

legacy_modules=(
  category-moderation
  personal-space
  controlled-richtext-article
  personal-schedule
  homepage-customizer
  web-theme
)

log() {
  printf '%s\n' "$*"
}

fail() {
  printf 'v10 layout migration: %s\n' "$*" >&2
  exit 1
}

record_move() {
  local source="$1" target="$2"
  mkdir -p "$(dirname "$target")"
  [[ ! -e "$target" ]] || fail "target already exists: ${target#$ROOT_DIR/}"
  printf 'MOVE\t%s\t%s\n' "$source" "$target" >>"$STATE_FILE"
  mv "$source" "$target"
  log "moved ${source#$ROOT_DIR/} -> ${target#$ROOT_DIR/}"
}

check_pack_conflicts() {
  local source_root="$1" target_root="$2"
  [[ -d "$source_root" ]] || return 0
  while IFS= read -r -d '' source; do
    local target="$target_root/$(basename "$source")"
    [[ ! -e "$target" ]] || fail "resource conflict: ${source#$ROOT_DIR/} and ${target#$ROOT_DIR/}"
  done < <(find "$source_root" -mindepth 1 -maxdepth 1 -type d -print0)
}

preflight() {
  check_pack_conflicts \
    "$ROOT_DIR/data/plugin_data/personal-space/style-packs" \
    "$ROOT_DIR/data/resources/space-style-packs"
  check_pack_conflicts \
    "$ROOT_DIR/data/plugin_data/homepage-customizer/style-packs" \
    "$ROOT_DIR/data/resources/homepage-packs"
  check_pack_conflicts \
    "$ROOT_DIR/data/plugin_data/web-theme/style-packs" \
    "$ROOT_DIR/data/resources/themes"

  local legacy_styles="$ROOT_DIR/data/plugin_data/personal-space/styles"
  local module_styles="$ROOT_DIR/data/module_data/personal-space/styles"
  if [[ -e "$legacy_styles" && -e "$module_styles" ]]; then
    fail "module style conflict: both ${legacy_styles#$ROOT_DIR/} and ${module_styles#$ROOT_DIR/} exist"
  fi
}

adopt_resource() {
  local directory="$1" kind="$2"
  if [[ -f "$directory/resource.json" ]]; then
    (cd "$CODE_ROOT" && GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go run ./cmd/campusosctl resource inspect "$directory" >/dev/null)
    return
  fi
  (cd "$CODE_ROOT" && GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go run ./cmd/campusosctl resource adopt "$directory" --type "$kind" --entry style.yaml >/dev/null)
  log "adopted resource manifest for ${directory#$ROOT_DIR/}"
}

move_pack_root() {
  local source_root="$1" target_root="$2" kind="$3"
  [[ -d "$source_root" ]] || return 0
  mkdir -p "$target_root"
  while IFS= read -r -d '' source; do
    local target="$target_root/$(basename "$source")"
    record_move "$source" "$target"
    adopt_resource "$target" "$kind"
  done < <(find "$source_root" -mindepth 1 -maxdepth 1 -type d -print0)
}

legacy_data_target() {
  case "$1" in
    homepage-customizer) printf '%s' "$ROOT_DIR/data/module_data/appearance/homepage-customizer-legacy" ;;
    web-theme) printf '%s' "$ROOT_DIR/data/module_data/appearance/web-theme-legacy" ;;
    *) printf '%s' "$ROOT_DIR/data/module_data/$1/legacy-plugin-data" ;;
  esac
}

apply_layout() {
  preflight
  mkdir -p "$BACKUP_DIR"
  : >"$STATE_FILE"

  move_pack_root \
    "$ROOT_DIR/data/plugin_data/personal-space/style-packs" \
    "$ROOT_DIR/data/resources/space-style-packs" \
    space-style-pack
  move_pack_root \
    "$ROOT_DIR/data/plugin_data/homepage-customizer/style-packs" \
    "$ROOT_DIR/data/resources/homepage-packs" \
    homepage-pack
  move_pack_root \
    "$ROOT_DIR/data/plugin_data/web-theme/style-packs" \
    "$ROOT_DIR/data/resources/themes" \
    theme

  local legacy_styles="$ROOT_DIR/data/plugin_data/personal-space/styles"
  if [[ -e "$legacy_styles" ]]; then
    record_move "$legacy_styles" "$ROOT_DIR/data/module_data/personal-space/styles"
  fi

  local name source target
  for name in "${legacy_modules[@]}"; do
    source="$ROOT_DIR/data/plugin_data/$name"
    if [[ -e "$source" ]]; then
      target="$(legacy_data_target "$name")"
      record_move "$source" "$target"
    fi
    source="$ROOT_DIR/data/plugins/$name"
    if [[ -e "$source" ]]; then
      target="$BACKUP_DIR/legacy-plugin-descriptors/$name"
      record_move "$source" "$target"
    fi
  done

  (cd "$CODE_ROOT" && CAMPUSOS_GOVERNANCE_ROOT="$ROOT_DIR" python3 scripts/check-data-governance.py)
  log "v10 layout migration complete; rollback state: $STATE_FILE"
}

rollback_layout() {
  [[ -f "$STATE_FILE" ]] || fail "rollback state not found: $STATE_FILE"
  while IFS=$'\t' read -r operation source target; do
    [[ "$operation" == "MOVE" ]] || continue
    [[ -e "$target" ]] || fail "rollback target is missing: $target"
    [[ ! -e "$source" ]] || fail "rollback source already exists: $source"
    mkdir -p "$(dirname "$source")"
    mv "$target" "$source"
    log "restored ${target#$ROOT_DIR/} -> ${source#$ROOT_DIR/}"
  done < <(tac "$STATE_FILE")
  log "v10 layout rollback complete"
}

case "$ACTION" in
  check)
    preflight
    found=0
    for name in "${legacy_modules[@]}"; do
      [[ -e "$ROOT_DIR/data/plugins/$name" || -e "$ROOT_DIR/data/plugin_data/$name" ]] && found=1
    done
    if [[ "$found" == "1" ]]; then
      log "legacy built-in paths require migration; no target conflicts detected"
    else
      (cd "$CODE_ROOT" && CAMPUSOS_GOVERNANCE_ROOT="$ROOT_DIR" python3 scripts/check-data-governance.py)
      log "v10 module/plugin/resource layout is current"
    fi
    ;;
  apply) apply_layout ;;
  rollback) rollback_layout ;;
  *) fail "usage: $0 {check|apply|rollback} [backup-directory]" ;;
esac
