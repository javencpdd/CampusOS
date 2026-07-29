#!/usr/bin/env bash
set -euo pipefail

archive="${1:-}"
confirmation="${2:-}"

if [[ -z "$archive" || "$confirmation" != "--confirm" ]]; then
  echo "usage: $0 /backups/campusos-backup-<timestamp>.tar.gz --confirm" >&2
  exit 2
fi
if [[ ! -f "$archive" ]]; then
  echo "backup archive does not exist inside the maintenance container: $archive" >&2
  exit 1
fi

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

/app/scripts/restore.sh extract "$archive" "$work_dir"
if [[ ! -d "$work_dir/files/data" ]]; then
  echo "backup does not contain the persistent data/ directory" >&2
  exit 1
fi

echo "==> replacing the CampusOS data volume"
find /app/data -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
cp -a "$work_dir/files/data/." /app/data/
chown -R 10001:10001 /app/data

echo "==> restoring PostgreSQL"
export PGPASSWORD="${DB_PASSWORD:?DB_PASSWORD is required}"
psql \
  -h "${DB_HOST:-postgres}" \
  -p "${DB_PORT:-5432}" \
  -U "${DB_USER:-campusos}" \
  -d "${DB_NAME:-campusos}" \
  -v ON_ERROR_STOP=1 \
  -f "$work_dir/database.sql"

echo "Docker data and database restore completed. Start the API to apply forward migrations."
