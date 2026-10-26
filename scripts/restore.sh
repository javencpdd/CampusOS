#!/usr/bin/env bash
# Trusted offline recovery; verification and extraction use the prebuilt CLI.
set -euo pipefail
umask 077
action="${1:-}"
archive="${2:-}"
destination="${3:-}"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
ctl="${CAMPUSOSCTL:-$repo_root/bin/campusosctl}"
if [[ ! -x "$ctl" ]]; then
  echo 'prebuilt campusosctl required; explicitly build with: go build -o bin/campusosctl ./cmd/campusosctl' >&2
  exit 127
fi
case "$action" in
  verify) exec "$ctl" recovery verify --archive "$archive" ;;
  extract) exec "$ctl" recovery extract --archive "$archive" --destination "$destination" ;;
  apply)
    [[ "$destination" == --confirm ]] || { echo 'apply requires --confirm and stopped API/writers' >&2; exit 2; }
    "$ctl" recovery verify --archive "$archive" >/dev/null
    if [[ "${CAMPUSOS_SKIP_DOTENV:-false}" != true && -f .env ]]; then
      set -a
      # shellcheck disable=SC1091
      source .env
      set +a
    fi
    export PGHOST="${DB_HOST:-localhost}" PGPORT="${DB_PORT:-5432}" PGUSER="${DB_USER:-campusos}"
    export PGPASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-}}" PGDATABASE="${DB_NAME:-campusos}"
    exec "$ctl" recovery apply --archive "$archive" --data-root "${CAMPUSOS_RECOVERY_DATA_ROOT:-data}" \
      --migrations "${MIGRATIONS_DIR:-migrations}" --database "$PGDATABASE" --confirm-offline
    ;;
  *) echo "usage: $0 verify <v3.tar.gz> | extract <v3.tar.gz> <empty-dir> | apply <v3.tar.gz> --confirm" >&2; exit 2 ;;
esac
