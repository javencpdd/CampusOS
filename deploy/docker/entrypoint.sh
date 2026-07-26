#!/usr/bin/env bash
set -euo pipefail

cd /app

mkdir -p \
  data/config \
  data/dist \
  data/images \
  data/logs \
  data/module_data \
  data/personal-space \
  data/plugin_data \
  data/plugins \
  data/resources \
  data/skills

# Seed only missing files. Installed plugins, user resources and mutable module
# state in the volume always win over image defaults during upgrades.
if [[ -d /app/seed-data ]]; then
  cp -a -n /app/seed-data/. /app/data/
fi

wait_for_postgres() {
  local attempts="${CAMPUSOS_DATABASE_WAIT_ATTEMPTS:-60}"
  local delay="${CAMPUSOS_DATABASE_WAIT_DELAY:-2}"
  local attempt

  for ((attempt = 1; attempt <= attempts; attempt++)); do
    if PGPASSWORD="${DB_PASSWORD:-}" pg_isready \
      -h "${DB_HOST:-postgres}" \
      -p "${DB_PORT:-5432}" \
      -U "${DB_USER:-campusos}" \
      -d "${DB_NAME:-campusos}" >/dev/null 2>&1; then
      return 0
    fi
    echo "==> waiting for PostgreSQL (${attempt}/${attempts})"
    sleep "$delay"
  done

  echo "PostgreSQL did not become ready in time" >&2
  return 1
}

if [[ "${CAMPUSOS_RUN_MIGRATIONS:-true}" == "true" ]]; then
  wait_for_postgres
  PSQL_MODE=host /app/scripts/migrate.sh up
fi

mkdir -p "${CAMPUSOS_LOG_DIR:-/app/data/logs}"
exec > >(tee -a "${CAMPUSOS_LOG_DIR:-/app/data/logs}/api.log") 2>&1
exec "$@"

