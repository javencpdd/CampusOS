#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${CAMPUSOS_DOCKER_ENV:-$ROOT_DIR/.env.docker}"
TEMPLATE_FILE="$ROOT_DIR/deploy/docker/.env.example"
COMPOSE_FILE="$ROOT_DIR/compose.deploy.yml"

random_hex() {
  local bytes="${1:-32}"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$bytes"
    return
  fi
  od -An -N "$bytes" -tx1 /dev/urandom | tr -d ' \n'
}

replace_placeholder() {
  local placeholder="$1"
  local value="$2"
  local temporary
  temporary="$(mktemp)"
  sed "s|$placeholder|$value|g" "$ENV_FILE" >"$temporary"
  mv "$temporary" "$ENV_FILE"
}

init_env() {
  if [[ -e "$ENV_FILE" ]]; then
    echo "Docker environment already exists: $ENV_FILE"
    return
  fi

  cp "$TEMPLATE_FILE" "$ENV_FILE"
  chmod 0600 "$ENV_FILE"
  replace_placeholder "__POSTGRES_PASSWORD__" "$(random_hex 24)"
  replace_placeholder "__JWT_SECRET__" "$(random_hex 32)"
  replace_placeholder "__BOOTSTRAP_ADMIN_SECRET__" "$(random_hex 18)"
  replace_placeholder "__CHALLENGE_HMAC_SECRET__" "$(random_hex 32)"
  replace_placeholder "__CHALLENGE_IP_HASH_SECRET__" "$(random_hex 32)"
  replace_placeholder "__SESSION_IP_HASH_SECRET__" "$(random_hex 32)"
  replace_placeholder "__MFA_ENCRYPTION_SECRET__" "$(random_hex 32)"
  echo "Created $ENV_FILE with mode 0600."
  echo "Review email, public URL and production settings before exposing CampusOS to a network."
}

ensure_env() {
  if [[ ! -f "$ENV_FILE" ]]; then
    init_env
  fi
}

compose() {
  local args=(docker compose --env-file "$ENV_FILE")
  if [[ -n "${CAMPUSOS_DOCKER_PROJECT:-}" ]]; then
    args+=(--project-name "$CAMPUSOS_DOCKER_PROJECT")
  fi
  args+=(-f "$COMPOSE_FILE")
  "${args[@]}" "$@"
}

read_env_setting() {
  local key="$1"
  local fallback="$2"
  local value="${!key:-}"
  if [[ -z "$value" && -f "$ENV_FILE" ]]; then
    value="$(awk -F= -v wanted="$key" '
      $1 == wanted {
        value = substr($0, index($0, "=") + 1)
      }
      END { print value }
    ' "$ENV_FILE")"
  fi
  value="${value%$'\r'}"
  value="${value#\"}"
  value="${value%\"}"
  printf '%s\n' "${value:-$fallback}"
}

ensure_network() {
  local name="$1"
  local isolation="${2:-edge}"
  if docker network inspect "$name" >/dev/null 2>&1; then
    if [[ "$isolation" == "internal" ]] \
      && [[ "$(docker network inspect --format '{{.Internal}}' "$name")" != "true" ]]; then
      echo "Docker backend network '$name' exists but is not internal." >&2
      echo "Stop attached CampusOS containers, remove only that network, then run this command again." >&2
      return 1
    fi
    return 0
  fi
  if [[ "$isolation" == "internal" ]]; then
    docker network create --internal "$name" >/dev/null
  else
    docker network create "$name" >/dev/null
  fi
  echo "Created Docker network: $name"
}

ensure_runtime_networks() {
  ensure_network "$(read_env_setting CAMPUSOS_BACKEND_NETWORK campusos_backend)" internal
  ensure_network "$(read_env_setting CAMPUSOS_EDGE_NETWORK campusos_edge)"
}

ensure_volume() {
  local name="$1"
  if ! docker volume inspect "$name" >/dev/null 2>&1; then
    docker volume create "$name" >/dev/null
    echo "Created Docker volume: $name"
  fi
}

ensure_runtime_volumes() {
  ensure_volume "$(read_env_setting CAMPUSOS_POSTGRES_VOLUME campusos_postgres-data)"
  ensure_volume "$(read_env_setting CAMPUSOS_REDIS_VOLUME campusos_redis-data)"
  ensure_volume "$(read_env_setting CAMPUSOS_NATS_VOLUME campusos_nats-data)"
  ensure_volume "$(read_env_setting CAMPUSOS_DATA_VOLUME campusos_campusos-data)"
  ensure_volume "$(read_env_setting CAMPUSOS_PGADMIN_VOLUME campusos_pgadmin-data)"
}

backup_stack() {
  local container_id backup_output archive_path archive_name staging_dir
  mkdir -p "$ROOT_DIR/backups"
  container_id="$(compose ps -q api)"
  if [[ -z "$container_id" ]]; then
    echo "CampusOS API container is not running." >&2
    return 1
  fi

  staging_dir="/app/data/.backup-staging"
  compose exec -T api sh -c \
    'mkdir -p "$1" && rm -f "$1"/campusos-backup-*.tar.gz' _ "$staging_dir"
  backup_output="$(compose exec -T api /app/scripts/backup.sh "$staging_dir")"
  archive_path="${backup_output##*$'\n'}"
  if [[ "$archive_path" != "$staging_dir"/campusos-backup-*.tar.gz ]]; then
    echo "CampusOS API returned an unexpected backup path: $archive_path" >&2
    return 1
  fi

  archive_name="$(basename "$archive_path")"
  docker cp "$container_id:$archive_path" "$ROOT_DIR/backups/"
  compose exec -T api rm -f "$archive_path"
  LAST_BACKUP_HOST_PATH="$ROOT_DIR/backups/$archive_name"
  chmod 0600 "$LAST_BACKUP_HOST_PATH"
  echo "Backup copied to $LAST_BACKUP_HOST_PATH"
}

restore_stack() {
  local archive="${1:-}"
  local confirmation="${2:-}"
  local archive_path backup_root relative_path container_id

  if [[ -z "$archive" || "$confirmation" != "--confirm" ]]; then
    echo "Usage: $0 restore backups/<archive>.tar.gz --confirm" >&2
    return 2
  fi

  archive_path="$(realpath "$archive")"
  backup_root="$(realpath "$ROOT_DIR/backups")"
  if [[ ! -f "$archive_path" ]]; then
    echo "Backup archive not found: $archive_path" >&2
    return 1
  fi
  case "$archive_path" in
    "$backup_root"/*) ;;
    *)
      echo "Place the archive below $backup_root before restoring it." >&2
      return 1
      ;;
  esac
  relative_path="${archive_path#"$backup_root"/}"

  compose build api
  ensure_runtime_networks
  ensure_runtime_volumes
  compose run --rm --no-deps maintenance \
    /app/scripts/restore.sh verify "/backups/$relative_path"

  container_id="$(compose ps -q api)"
  if [[ -n "$container_id" ]]; then
    echo "==> creating an automatic pre-restore safety backup"
    backup_stack
  else
    echo "==> API is not running; no automatic pre-restore backup was created"
  fi

  compose stop web admin api
  compose up -d --wait --wait-timeout "${CAMPUSOS_DOCKER_WAIT_TIMEOUT:-300}" postgres redis nats
  compose run --rm --no-deps maintenance \
    /app/scripts/docker-restore.sh "/backups/$relative_path" --confirm
  compose up -d --wait --wait-timeout "${CAMPUSOS_DOCKER_WAIT_TIMEOUT:-300}"
  compose ps
  echo "Restore completed. Preserve the source .env.docker until login, MFA and integration checks pass."
}

command="${1:-up}"
case "$command" in
  init)
    init_env
    ;;
  config)
    ensure_env
    compose config --quiet
    echo "Docker deployment configuration is valid."
    ;;
  build)
    ensure_env
    compose build api web admin docs
    ;;
  up)
    ensure_env
    ensure_runtime_networks
    ensure_runtime_volumes
    compose up -d --build --wait --wait-timeout "${CAMPUSOS_DOCKER_WAIT_TIMEOUT:-300}"
    compose ps
    ;;
  ps)
    ensure_env
    compose ps
    ;;
  logs)
    ensure_env
    shift || true
    compose logs -f --tail=200 "$@"
    ;;
  backup)
    ensure_env
    backup_stack
    ;;
  restore)
    ensure_env
    restore_stack "${2:-}" "${3:-}"
    ;;
  down)
    ensure_env
    compose down
    ;;
  *)
    echo "Usage: $0 {init|config|build|up|ps|logs [service]|backup|restore backups/<archive> --confirm|down}" >&2
    exit 2
    ;;
esac
