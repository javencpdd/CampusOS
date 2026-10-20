#!/usr/bin/env bash
set -euo pipefail
umask 077

OUTPUT_DIR="${1:-backups}"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
ctl="${CAMPUSOSCTL:-$repo_root/bin/campusosctl}"
[[ -x "$ctl" ]] || { echo 'prebuilt campusosctl required: go build -o bin/campusosctl ./cmd/campusosctl' >&2; exit 127; }
if [[ "${CAMPUSOS_SKIP_DOTENV:-false}" != "true" && -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-${POSTGRES_PORT:-5432}}"
DB_USER="${DB_USER:-campusos}"
DB_NAME="${DB_NAME:-campusos}"
DB_PASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-campusos_dev}}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
PLUGINS_DIR="${PLUGINS_DIR:-data/plugins}"
PLUGIN_DATA_DIR="${PLUGIN_DATA_DIR:-data/plugin_data}"
MODULES_DIR="${MODULES_DIR:-modules}"
MODULE_DATA_DIR="${MODULE_DATA_DIR:-data/module_data}"
RESOURCE_DIR="${RESOURCE_DIR:-data/resources}"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$OUTPUT_DIR"
# Keep staging on the output filesystem so the final hard-link publication is
# atomic and refuses to replace any existing backup, even in the same second.
work_dir="$(mktemp -d "$OUTPUT_DIR/.campusos-backup-$timestamp.XXXXXX")"
trap 'rm -rf "$work_dir"' EXIT

dump_database() {
  if command -v pg_dump >/dev/null 2>&1; then
    PGPASSWORD="$DB_PASSWORD" pg_dump -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" --clean --if-exists --no-owner --no-privileges >"$work_dir/database.sql"
  elif command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER"; then
    docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" pg_dump -U "$DB_USER" -d "$DB_NAME" --clean --if-exists --no-owner --no-privileges >"$work_dir/database.sql"
  else
    echo "pg_dump is unavailable and PostgreSQL container '$POSTGRES_CONTAINER' is not running" >&2
    return 127
  fi
}

dump_database

files_root="$work_dir/files"
mkdir -p "$files_root"

# Normalize configurable absolute paths to a stable archive layout. Restore
# consumers therefore always receive modules/ and data/* regardless of whether
# the backup was created on a host checkout or inside /app in Docker.
stage_path() {
  local source="$1"
  local destination="$2"
  [[ -e "$source" ]] || return 0
  mkdir -p "$files_root/$(dirname "$destination")"
  if [[ -d "$source" ]]; then
    mkdir -p "$files_root/$destination"
    cp -a "$source/." "$files_root/$destination/"
  else
    cp -a "$source" "$files_root/$destination"
  fi
}

stage_path migrations migrations
stage_path "$MODULES_DIR" modules
stage_path "$PLUGINS_DIR" data/plugins
stage_path "$PLUGIN_DATA_DIR" data/plugin_data
stage_path "$MODULE_DATA_DIR" data/module_data
stage_path "$RESOURCE_DIR" data/resources
stage_path data/personal-space data/personal-space
stage_path data/images data/images
stage_path data/config data/config
stage_path data/skills data/skills
# Deployment keys and credentials in the root .env have separate custody. A
# database/files backup is deliberately insufficient to recover encrypted data.
# cp -a preserves source permissions; remove group/other access before packing.
chmod -R go-rwx "$files_root"

tar -C "$files_root" -czf "$work_dir/files.tar.gz" .

cat >"$work_dir/metadata.txt" <<EOF
format=campusos-single-node-v3
created_at=$timestamp
database=$DB_NAME
root_dotenv=excluded
deployment_keys=separate_custody_required
apply=offline_quarantine_then_current_authorization_review
plugins_dir=$PLUGINS_DIR
plugin_data_dir=$PLUGIN_DATA_DIR
modules_dir=$MODULES_DIR
module_data_dir=$MODULE_DATA_DIR
resource_dir=$RESOURCE_DIR
EOF

(
  cd "$work_dir"
  sha256sum database.sql files.tar.gz metadata.txt >SHA256SUMS
)

archive="$OUTPUT_DIR/campusos-backup-$timestamp-${work_dir##*.}.tar.gz"
tar -C "$work_dir" -czf "$work_dir/archive.tar.gz" database.sql files.tar.gz metadata.txt SHA256SUMS
# Reject unsupported links/types or a malformed bundle before publishing a
# backup that the strict restore entry could never safely consume.
"$ctl" recovery verify --archive "$work_dir/archive.tar.gz" >/dev/null
ln "$work_dir/archive.tar.gz" "$archive"
echo "$archive"
