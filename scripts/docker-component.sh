#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GLOBAL_ENV="${CAMPUSOS_DOCKER_ENV:-$ROOT_DIR/.env.docker}"
CONFIG_DIR="${CAMPUSOS_COMPONENT_CONFIG_DIR:-$ROOT_DIR/.env.components}"
TEMPLATE_DIR="$ROOT_DIR/deploy/docker/components"

validate_component() {
  case "${1:-}" in
    infra|api|web|admin|docs) ;;
    *)
      echo "component must be one of: infra, api, web, admin, docs" >&2
      return 2
      ;;
  esac
}

component_compose_file() {
  printf '%s/compose.%s.yml\n' "$TEMPLATE_DIR" "$1"
}

component_env_file() {
  printf '%s/%s.env\n' "$CONFIG_DIR" "$1"
}

init_configs() {
  if [[ ! -f "$GLOBAL_ENV" ]]; then
    CAMPUSOS_DOCKER_ENV="$GLOBAL_ENV" "$ROOT_DIR/scripts/docker-deploy.sh" init
  fi
  mkdir -p "$CONFIG_DIR"
  chmod 0700 "$CONFIG_DIR"
  local component target
  for component in infra api web admin docs; do
    target="$(component_env_file "$component")"
    if [[ ! -f "$target" ]]; then
      cp "$TEMPLATE_DIR/.env.$component.example" "$target"
      chmod 0600 "$target"
      echo "Created component config: $target"
    fi
  done
}

ensure_configs() {
  if [[ ! -f "$GLOBAL_ENV" || ! -d "$CONFIG_DIR" ]]; then
    init_configs
  fi
}

read_env_setting() {
  local key="$1"
  local fallback="$2"
  local component_file="$3"
  local value="${!key:-}"
  local file
  if [[ -z "$value" ]]; then
    for file in "$GLOBAL_ENV" "$component_file"; do
      [[ -f "$file" ]] || continue
      value="$(awk -F= -v wanted="$key" '
        $1 == wanted {
          value = substr($0, index($0, "=") + 1)
        }
        END { print value }
      ' "$file")"
    done
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

ensure_component_networks() {
  local component="$1"
  local component_env="$2"
  if [[ "$component" == "infra" || "$component" == "api" ]]; then
    ensure_network "$(read_env_setting CAMPUSOS_BACKEND_NETWORK campusos_backend "$component_env")" internal
  fi
  if [[ "$component" != "infra" ]]; then
    ensure_network "$(read_env_setting CAMPUSOS_EDGE_NETWORK campusos_edge "$component_env")"
  fi
}

ensure_volume() {
  local name="$1"
  if ! docker volume inspect "$name" >/dev/null 2>&1; then
    docker volume create "$name" >/dev/null
    echo "Created Docker volume: $name"
  fi
}

ensure_component_volumes() {
  local component="$1"
  local component_env="$2"
  if [[ "$component" == "infra" ]]; then
    ensure_volume "$(read_env_setting CAMPUSOS_POSTGRES_VOLUME campusos_postgres-data "$component_env")"
    ensure_volume "$(read_env_setting CAMPUSOS_REDIS_VOLUME campusos_redis-data "$component_env")"
    ensure_volume "$(read_env_setting CAMPUSOS_NATS_VOLUME campusos_nats-data "$component_env")"
  elif [[ "$component" == "api" ]]; then
    ensure_volume "$(read_env_setting CAMPUSOS_DATA_VOLUME campusos_campusos-data "$component_env")"
  fi
}

compose_component() {
  local component="$1"
  shift
  local args=(
    docker compose
    --env-file "$GLOBAL_ENV"
    --env-file "$(component_env_file "$component")"
  )
  if [[ -n "${CAMPUSOS_COMPONENT_PROJECT:-}" ]]; then
    args+=(--project-name "$CAMPUSOS_COMPONENT_PROJECT")
  fi
  args+=(-f "$(component_compose_file "$component")")
  "${args[@]}" "$@"
}

command="${1:-}"
component="${2:-}"
case "$command" in
  init)
    init_configs
    ;;
  config|build|up|ps|logs|down)
    validate_component "$component"
    ensure_configs
    component_env="$(component_env_file "$component")"
    case "$command" in
      config)
        compose_component "$component" config --quiet
        echo "Docker component configuration is valid: $component"
        ;;
      build)
        if [[ "$component" == "infra" ]]; then
          echo "infra uses upstream images and has no CampusOS image build."
        else
          compose_component "$component" build "$component"
        fi
        ;;
      up)
        ensure_component_networks "$component" "$component_env"
        ensure_component_volumes "$component" "$component_env"
        compose_component "$component" up -d --build --wait \
          --wait-timeout "${CAMPUSOS_DOCKER_WAIT_TIMEOUT:-300}"
        compose_component "$component" ps
        ;;
      ps)
        compose_component "$component" ps
        ;;
      logs)
        compose_component "$component" logs -f --tail=200
        ;;
      down)
        compose_component "$component" down
        ;;
    esac
    ;;
  *)
    echo "Usage: $0 init | {config|build|up|ps|logs|down} {infra|api|web|admin|docs}" >&2
    exit 2
    ;;
esac
