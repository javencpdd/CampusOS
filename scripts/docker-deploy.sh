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
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
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
    mkdir -p "$ROOT_DIR/backups"
    container_id="$(compose ps -q api)"
    if [[ -z "$container_id" ]]; then
      echo "CampusOS API container is not running." >&2
      exit 1
    fi
    archive_path="$(compose exec -T api /app/scripts/backup.sh /tmp/campusos-backups | tail -n 1)"
    docker cp "$container_id:$archive_path" "$ROOT_DIR/backups/"
    compose exec -T api rm -f "$archive_path"
    echo "Backup copied to $ROOT_DIR/backups/$(basename "$archive_path")"
    ;;
  down)
    ensure_env
    compose down
    ;;
  *)
    echo "Usage: $0 {init|config|build|up|ps|logs [service]|backup|down}" >&2
    exit 2
    ;;
esac

