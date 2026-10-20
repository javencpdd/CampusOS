#!/usr/bin/env bash
set -euo pipefail
umask 077
archive="${1:-}"
[[ -n "$archive" && "${2:-}" == --confirm ]] || { echo 'usage: docker-restore.sh <v3.tar.gz> --confirm' >&2; exit 2; }
exec /app/bin/campusosctl recovery apply --archive "$archive" --data-root /app/data \
  --migrations /app/migrations --database "${DB_NAME:?DB_NAME required}" --confirm-offline
