#!/usr/bin/env bash
# 用法：./scripts/migrate.sh {up|down|reset|status|check}
# reset 会清空目标数据库 public schema，仅允许 development/test，且必须设置
# CAMPUSOS_RESET_CONFIRM 为当前 DB_NAME。up/down 会校验迁移文件配对与 SHA-256。
set -euo pipefail

ACTION="${1:-up}"

if [[ "${CAMPUSOS_SKIP_DOTENV:-false}" != "true" && -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

MIGRATIONS_DIR="${MIGRATIONS_DIR:-migrations}"
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-${POSTGRES_PORT:-5432}}"
DB_USER="${DB_USER:-campusos}"
DB_NAME="${DB_NAME:-campusos}"
DB_PASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-campusos_dev}}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
PSQL_MODE="${PSQL_MODE:-auto}"
LOCK_HELD=false
LOCK_OWNER="${HOSTNAME:-host}:$$"

docker_container_available() {
  command -v docker >/dev/null 2>&1 || return 1
  docker inspect --format '{{.State.Running}}' "$POSTGRES_CONTAINER" 2>/dev/null | grep -qx true
}

select_psql_mode() {
  case "$PSQL_MODE" in
    host)
      command -v psql >/dev/null 2>&1 || {
        echo "psql not found on host. Install postgresql-client or use PSQL_MODE=docker." >&2
        exit 127
      }
      ;;
    docker)
      docker_container_available || {
        echo "docker postgres container '$POSTGRES_CONTAINER' is not running." >&2
        exit 127
      }
      ;;
    auto | "")
      if command -v psql >/dev/null 2>&1; then
        PSQL_MODE=host
      elif docker_container_available; then
        PSQL_MODE=docker
        echo "==> host psql not found; using docker exec $POSTGRES_CONTAINER psql" >&2
      else
        echo "psql is unavailable and PostgreSQL container '$POSTGRES_CONTAINER' is not running." >&2
        exit 127
      fi
      ;;
    *)
      echo "invalid PSQL_MODE '$PSQL_MODE' (expected auto, host, or docker)." >&2
      exit 2
      ;;
  esac
}

run_psql_host() {
  PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 "$@"
}

run_psql_docker() {
  local file=""
  local args=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -f | --file)
        [[ $# -ge 2 ]] || { echo "missing SQL file after $1" >&2; exit 2; }
        file="$2"
        shift 2
        ;;
      --file=*)
        file="${1#--file=}"
        shift
        ;;
      *)
        args+=("$1")
        shift
        ;;
    esac
  done

  if [[ -n "$file" ]]; then
    [[ -f "$file" ]] || { echo "SQL file not found: $file" >&2; exit 2; }
    docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
      psql -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 "${args[@]}" <"$file"
  else
    docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
      psql -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 "${args[@]}"
  fi
}

run_psql() {
  if [[ "$PSQL_MODE" == host ]]; then
    run_psql_host "$@"
  else
    run_psql_docker "$@"
  fi
}

sql_literal() {
  printf "%s" "$1" | sed "s/'/''/g"
}

checksum_for() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print tolower($1)}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print tolower($1)}'
  else
    echo "sha256sum or shasum is required." >&2
    exit 127
  fi
}

version_for() {
  basename "$1" | sed -E 's/^([0-9]+)_.*/\1/'
}

name_for() {
  basename "$1" | sed -E 's/\.(up|down)\.sql$//'
}

validate_migration_files() {
  shopt -s nullglob
  local up_files=("$MIGRATIONS_DIR"/*.up.sql)
  local down_files=("$MIGRATIONS_DIR"/*.down.sql)
  shopt -u nullglob
  [[ ${#up_files[@]} -gt 0 ]] || { echo "No up migrations found in $MIGRATIONS_DIR" >&2; exit 2; }
  [[ ${#up_files[@]} -eq ${#down_files[@]} ]] || {
    echo "migration up/down file count mismatch." >&2
    exit 2
  }

  local previous="" file version down_file
  for file in "${up_files[@]}"; do
    version="$(version_for "$file")"
    [[ "$version" =~ ^[0-9]{6}$ ]] || { echo "invalid migration version: $file" >&2; exit 2; }
    [[ "$version" != "$previous" ]] || { echo "duplicate migration version: $version" >&2; exit 2; }
    previous="$version"
    down_file="${file%.up.sql}.down.sql"
    [[ -f "$down_file" ]] || { echo "missing down migration for $file" >&2; exit 2; }
  done
}

ensure_migration_metadata() {
  run_psql -q -c "
    CREATE TABLE IF NOT EXISTS public.schema_migrations (
      version      VARCHAR(32) PRIMARY KEY,
      name         VARCHAR(255) NOT NULL,
      checksum     CHAR(64) NOT NULL,
      execution_ms BIGINT NOT NULL DEFAULT 0 CHECK (execution_ms >= 0),
      executor     VARCHAR(128) NOT NULL DEFAULT 'unknown',
      applied_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
    );
    CREATE TABLE IF NOT EXISTS public.schema_migration_locks (
      id          SMALLINT PRIMARY KEY CHECK (id = 1),
      owner_name  VARCHAR(160) NOT NULL,
      acquired_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
    );
  " >/dev/null

  local has_checksum
  has_checksum="$(run_psql -tAc "SELECT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema='public' AND table_name='schema_migrations' AND column_name='checksum'
  )")"
  [[ "$has_checksum" == "t" ]] || {
    echo "legacy schema_migrations detected in '$DB_NAME'. This v1 baseline is intentionally incompatible." >&2
    echo "For disposable development/test data, run with CAMPUSOS_ENV=development CAMPUSOS_RESET_CONFIRM=$DB_NAME ./scripts/migrate.sh reset" >&2
    exit 3
  }
}

acquire_lock() {
  local owner escaped acquired current
  owner="$LOCK_OWNER"
  escaped="$(sql_literal "$owner")"
  if [[ "${CAMPUSOS_MIGRATION_LOCK_FORCE:-false}" == "true" ]]; then
    run_psql -q -c "DELETE FROM public.schema_migration_locks WHERE id=1;" >/dev/null
  fi
  acquired="$(run_psql -qAtc "INSERT INTO public.schema_migration_locks(id, owner_name)
    VALUES (1, '$escaped') ON CONFLICT (id) DO NOTHING RETURNING owner_name;")"
  if [[ "$acquired" != "$owner" ]]; then
    current="$(run_psql -tAc "SELECT owner_name || ' since ' || acquired_at FROM public.schema_migration_locks WHERE id=1;")"
    echo "migration lock is held by $current" >&2
    exit 4
  fi
  LOCK_HELD=true
}

release_lock() {
  if [[ "$LOCK_HELD" == true ]]; then
    local escaped
    escaped="$(sql_literal "$LOCK_OWNER")"
    run_psql -q -c "DELETE FROM public.schema_migration_locks WHERE id=1 AND owner_name='$escaped';" >/dev/null || true
    LOCK_HELD=false
  fi
}

run_atomic_migration() {
  local file="$1" metadata_sql="$2" temp_file
  temp_file="$(mktemp)"
  {
    echo "BEGIN;"
    printf '\n'
    sed '/^[[:space:]]*\\\(un\)\?restrict[[:space:]]/d' "$file"
    printf '\n%s\n' "$metadata_sql"
    echo "COMMIT;"
  } >"$temp_file"
  if ! run_psql -q -f "$temp_file"; then
    rm -f -- "$temp_file"
    return 1
  fi
  rm -f -- "$temp_file"
}

verify_applied_checksums() {
  local file version expected stored
  shopt -s nullglob
  local files=("$MIGRATIONS_DIR"/*.up.sql)
  shopt -u nullglob
  for file in "${files[@]}"; do
    version="$(version_for "$file")"
    stored="$(run_psql -tAc "SELECT checksum FROM public.schema_migrations WHERE version='$version';")"
    [[ -z "$stored" ]] && continue
    expected="$(checksum_for "$file")"
    [[ "$stored" == "$expected" ]] || {
      echo "migration drift: $(basename "$file") checksum=$expected recorded=$stored" >&2
      return 1
    }
  done
}

run_up() {
  validate_migration_files
  ensure_migration_metadata
  acquire_lock
  trap release_lock EXIT INT TERM

  local file version name checksum stored_name stored_checksum started elapsed escaped_name escaped_owner
  shopt -s nullglob
  local files=("$MIGRATIONS_DIR"/*.up.sql)
  shopt -u nullglob
  escaped_owner="$(sql_literal "$LOCK_OWNER")"
  for file in "${files[@]}"; do
    version="$(version_for "$file")"
    name="$(name_for "$file")"
    checksum="$(checksum_for "$file")"
    stored_checksum="$(run_psql -tAc "SELECT checksum FROM public.schema_migrations WHERE version='$version';")"
    if [[ -n "$stored_checksum" ]]; then
      stored_name="$(run_psql -tAc "SELECT name FROM public.schema_migrations WHERE version='$version';")"
      [[ "$stored_checksum" == "$checksum" && "$stored_name" == "$name" ]] || {
        echo "migration drift: $version recorded as $stored_name/$stored_checksum, file is $name/$checksum" >&2
        exit 5
      }
      echo "==> skip $name"
      continue
    fi

    echo "==> apply $name"
    started="$(date +%s%3N 2>/dev/null || date +%s000)"
    escaped_name="$(sql_literal "$name")"
    run_atomic_migration "$file" "INSERT INTO public.schema_migrations(version,name,checksum,execution_ms,executor,applied_at)
      VALUES ('$version','$escaped_name','$checksum',0,'$escaped_owner',NOW());"
    elapsed=$(( $(date +%s%3N 2>/dev/null || date +%s000) - started ))
    run_psql -q -c "UPDATE public.schema_migrations SET execution_ms=$elapsed WHERE version='$version';" >/dev/null
  done
  release_lock
  trap - EXIT INT TERM
}

run_down() {
  validate_migration_files
  ensure_migration_metadata
  acquire_lock
  trap release_lock EXIT INT TERM

  local version name file
  version="$(run_psql -tAc "SELECT version FROM public.schema_migrations ORDER BY version DESC LIMIT 1;")"
  if [[ -z "$version" ]]; then
    echo "No applied migration to roll back."
    release_lock
    trap - EXIT INT TERM
    return
  fi
  file="$(find "$MIGRATIONS_DIR" -maxdepth 1 -type f -name "${version}_*.down.sql" -print -quit)"
  [[ -n "$file" ]] || { echo "missing down migration for applied version $version" >&2; exit 2; }
  name="$(name_for "$file")"
  echo "==> rollback $name"
  run_atomic_migration "$file" "DELETE FROM public.schema_migrations WHERE version='$version';"
  release_lock
  trap - EXIT INT TERM
}

reset_database() {
  case "${CAMPUSOS_ENV:-}" in
    development | test) ;;
    *)
      echo "reset is restricted to CAMPUSOS_ENV=development or test." >&2
      exit 6
      ;;
  esac
  [[ "${CAMPUSOS_RESET_CONFIRM:-}" == "$DB_NAME" ]] || {
    echo "reset requires CAMPUSOS_RESET_CONFIRM=$DB_NAME" >&2
    exit 6
  }
  echo "==> reset public schema in database $DB_NAME"
  run_psql -q -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" >/dev/null
  run_up
}

run_status() {
  ensure_migration_metadata
  verify_applied_checksums
  echo "==> schema_migrations"
  run_psql -c "SELECT version, name, left(checksum,12) AS checksum, execution_ms, executor, applied_at FROM public.schema_migrations ORDER BY version;"
}

select_psql_mode

case "$ACTION" in
  up) run_up ;;
  down) run_down ;;
  reset) reset_database ;;
  status) run_status ;;
  check)
    validate_migration_files
    ensure_migration_metadata
    verify_applied_checksums
    echo "migration files and recorded checksums are valid."
    ;;
  *)
    echo "Usage: $0 {up|down|reset|status|check}" >&2
    exit 2
    ;;
esac
