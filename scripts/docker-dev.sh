#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEMPLATE_FILE="$ROOT_DIR/deploy/docker/.env.dev.example"
LOCAL_ENV_FILE="$ROOT_DIR/deploy/docker/.env.dev.local"
COMPOSE_FILE="$ROOT_DIR/compose.dev.yml"
RUN_DIR="${CAMPUSOS_RUN_DIR:-$ROOT_DIR/.campusos/run}"
NATIVE_PID_FILE="$RUN_DIR/native-dev.pid"
command="${1:-up}"
NATIVE_HANDOFF=false

if [[ -n "${CAMPUSOS_DOCKER_DEV_ENV:-}" ]]; then
  ENV_FILE="$CAMPUSOS_DOCKER_DEV_ENV"
elif [[ "$command" == "setup" || -f "$LOCAL_ENV_FILE" ]]; then
  ENV_FILE="$LOCAL_ENV_FILE"
else
  ENV_FILE="$TEMPLATE_FILE"
fi

read_file_setting() {
  local file="$1"
  local key="$2"
  awk -F= -v wanted="$key" '
    $1 == wanted {
      value = substr($0, index($0, "=") + 1)
    }
    END { print value }
  ' "$file" | sed -e 's/\r$//' -e 's/^"//' -e 's/"$//'
}

set_file_setting() {
  local file="$1"
  local key="$2"
  local value="$3"
  local temp_file
  temp_file="$(mktemp "${file}.tmp.XXXXXX")"
  CAMPUSOS_ENV_REPLACEMENT="$value" awk -F= -v wanted="$key" '
    $1 == wanted {
      print wanted "=" ENVIRON["CAMPUSOS_ENV_REPLACEMENT"]
      found = 1
      next
    }
    { print }
    END {
      if (!found) {
        print wanted "=" ENVIRON["CAMPUSOS_ENV_REPLACEMENT"]
      }
    }
  ' "$file" >"$temp_file"
  mv "$temp_file" "$file"
  chmod 0600 "$file"
}

import_existing_local_settings() {
  local source_file="${CAMPUSOS_DOCKER_DEV_IMPORT_SOURCE:-$ROOT_DIR/.env}"
  if [[ "$ENV_FILE" != "$LOCAL_ENV_FILE" && -z "${CAMPUSOS_DOCKER_DEV_IMPORT_SOURCE:-}" ]]; then
    return
  fi
  if [[ ! -f "$source_file" ]]; then
    return
  fi

  local key value imported=0
  local keys=(
    POSTGRES_DB POSTGRES_USER POSTGRES_PASSWORD REDIS_PASSWORD
    CAMPUSOS_ENV CAMPUSOS_INSTANCE_MODE
    JWT_SECRET JWT_ACCESS_TTL JWT_REFRESH_TTL JWT_ISSUER
    AUTH_PASSWORD_HASH_ENABLED AUTH_BOOTSTRAP_ADMIN_SECRET
    AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN AUTH_CHALLENGE_ACTIVE_KEY_ID
    AUTH_CHALLENGE_HMAC_KEYS AUTH_CHALLENGE_IP_HASH_SECRET
    AUTH_SESSION_IP_HASH_SECRET AUTH_REFRESH_BODY_COMPAT
    AUTH_MFA_ACTIVE_KEY_ID AUTH_MFA_ENCRYPTION_KEYS AUTH_MFA_ISSUER
    EMAIL_PROVIDER EMAIL_SMTP_HOST EMAIL_SMTP_PORT EMAIL_SMTP_USERNAME
    EMAIL_SMTP_PASSWORD EMAIL_SMTP_FROM EMAIL_SMTP_TIMEOUT EMAIL_SMTP_STARTTLS
  )
  for key in "${keys[@]}"; do
    value="$(read_file_setting "$source_file" "$key")"
    if [[ -n "$value" ]]; then
      set_file_setting "$ENV_FILE" "$key" "$value"
      imported=$((imported + 1))
    fi
  done
  if (( imported > 0 )); then
    echo "Imported $imported shared runtime setting(s) from the existing root .env without printing values."
  fi
}

init_env() {
  if [[ -f "$ENV_FILE" ]]; then
    echo "Docker development configuration already exists: $ENV_FILE"
    return 1
  fi
  mkdir -p "$(dirname "$ENV_FILE")"
  cp "$TEMPLATE_FILE" "$ENV_FILE"
  chmod 0600 "$ENV_FILE"
  import_existing_local_settings
  echo "Created local Docker development configuration: $ENV_FILE"
  echo "Edit the ports and EMAIL_* settings, then run this setup command again."
  return 0
}

read_env_setting() {
  local key="$1"
  local fallback="${2:-}"
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

validate_port() {
  local key="$1"
  local fallback="$2"
  local value
  value="$(read_env_setting "$key" "$fallback")"
  if [[ ! "$value" =~ ^[0-9]+$ ]] || (( value < 1 || value > 65535 )); then
    echo "$key must be an integer between 1 and 65535." >&2
    return 1
  fi
  printf '%s\n' "$value"
}

validate_env() {
  if [[ ! -f "$ENV_FILE" ]]; then
    echo "Docker development configuration does not exist: $ENV_FILE" >&2
    echo "Run '$0 setup' first." >&2
    return 1
  fi

  local bind allow_lan provider smtp_host smtp_port smtp_username smtp_password smtp_from smtp_timeout smtp_starttls
  bind="$(read_env_setting CAMPUSOS_DEV_BIND 127.0.0.1)"
  if [[ "$bind" != "127.0.0.1" ]]; then
    echo "CAMPUSOS_DEV_BIND must remain 127.0.0.1 because this stack uses development credentials." >&2
    return 1
  fi

  allow_lan="$(read_env_setting CAMPUSOS_DEV_ALLOW_LAN false)"
  allow_lan="${allow_lan,,}"
  if [[ "$allow_lan" != "true" && "$allow_lan" != "false" ]]; then
    echo "CAMPUSOS_DEV_ALLOW_LAN must be true or false." >&2
    return 1
  fi

  local bind_key bind_value bind_spec
  local lan_surface_count=0
  local bind_specs=(
    "CAMPUSOS_DEV_WEB_BIND|127.0.0.1"
    "CAMPUSOS_DEV_ADMIN_BIND|127.0.0.1"
    "CAMPUSOS_DEV_DOCS_BIND|127.0.0.1"
  )
  for bind_spec in "${bind_specs[@]}"; do
    IFS='|' read -r bind_key bind_value <<<"$bind_spec"
    bind_value="$(read_env_setting "$bind_key" "$bind_value")"
    if [[ "$bind_value" != "127.0.0.1" && "$bind_value" != "0.0.0.0" ]]; then
      echo "$bind_key must be 127.0.0.1 or 0.0.0.0." >&2
      return 1
    fi
    if [[ "$bind_value" == "0.0.0.0" ]]; then
      if [[ "$allow_lan" != "true" ]]; then
        echo "$bind_key=0.0.0.0 requires CAMPUSOS_DEV_ALLOW_LAN=true." >&2
        return 1
      fi
      ((lan_surface_count += 1))
    fi
  done
  if (( lan_surface_count > 0 )); then
    echo "Warning: LAN exposure is enabled for $lan_surface_count UI service(s); API and data services remain loopback-only."
  fi

  local port_key port_default port_value port_spec
  local -A used_ports=()
  local port_specs=(
    "CAMPUSOS_DEV_WEB_PORT|3000"
    "CAMPUSOS_DEV_ADMIN_PORT|3001"
    "CAMPUSOS_DEV_DOCS_PORT|3002"
    "CAMPUSOS_DEV_API_PORT|8080"
    "CAMPUSOS_DEV_POSTGRES_PORT|55432"
    "CAMPUSOS_DEV_REDIS_PORT|56379"
    "CAMPUSOS_DEV_NATS_PORT|54222"
    "CAMPUSOS_DEV_NATS_MONITOR_PORT|58222"
    "CAMPUSOS_DEV_PGADMIN_PORT|5050"
  )
  for port_spec in "${port_specs[@]}"; do
    IFS='|' read -r port_key port_default <<<"$port_spec"
    port_value="$(validate_port "$port_key" "$port_default")" || return 1
    if [[ -n "${used_ports[$port_value]:-}" ]]; then
      echo "$port_key conflicts with ${used_ports[$port_value]} on port $port_value." >&2
      return 1
    fi
    used_ports[$port_value]="$port_key"
  done

  provider="$(read_env_setting EMAIL_PROVIDER fake)"
  provider="${provider,,}"
  case "$provider" in
    fake)
      echo "Email provider: fake (registration requests succeed, but no verification email is sent)."
      ;;
    smtp)
      smtp_host="$(read_env_setting EMAIL_SMTP_HOST)"
      smtp_port="$(validate_port EMAIL_SMTP_PORT 587)"
      smtp_username="$(read_env_setting EMAIL_SMTP_USERNAME)"
      smtp_password="$(read_env_setting EMAIL_SMTP_PASSWORD)"
      smtp_from="$(read_env_setting EMAIL_SMTP_FROM)"
      smtp_timeout="$(read_env_setting EMAIL_SMTP_TIMEOUT 10s)"
      smtp_starttls="$(read_env_setting EMAIL_SMTP_STARTTLS true)"
      smtp_starttls="${smtp_starttls,,}"
      if [[ -z "$smtp_host" || -z "$smtp_from" ]]; then
        echo "EMAIL_SMTP_HOST and EMAIL_SMTP_FROM are required when EMAIL_PROVIDER=smtp." >&2
        return 1
      fi
      if [[ -n "$smtp_username" && -z "$smtp_password" ]] || [[ -z "$smtp_username" && -n "$smtp_password" ]]; then
        echo "EMAIL_SMTP_USERNAME and EMAIL_SMTP_PASSWORD must either both be set or both be empty." >&2
        return 1
      fi
      if [[ -z "$smtp_timeout" ]]; then
        echo "EMAIL_SMTP_TIMEOUT must not be empty." >&2
        return 1
      fi
      if [[ "$smtp_starttls" != "true" && "$smtp_starttls" != "false" ]]; then
        echo "EMAIL_SMTP_STARTTLS must be true or false." >&2
        return 1
      fi
      echo "Email provider: smtp ($smtp_host:$smtp_port, from $smtp_from, STARTTLS=$smtp_starttls)."
      ;;
    *)
      echo "EMAIL_PROVIDER must be fake or smtp." >&2
      return 1
      ;;
  esac
}

check_compose() {
  if ! command -v docker >/dev/null 2>&1; then
    echo "Docker is not installed or is not available on PATH." >&2
    return 1
  fi
  if ! docker compose version >/dev/null 2>&1; then
    echo "Docker Compose v2 is required." >&2
    return 1
  fi
}

check_docker() {
  check_compose
  if ! docker info >/dev/null 2>&1; then
    echo "Docker daemon is not running or the current user cannot access it." >&2
    return 1
  fi
}

set_developer_identity() {
  if [[ -n "${CAMPUSOS_DEV_UID:-}" && -n "${CAMPUSOS_DEV_GID:-}" ]]; then
    return
  fi
  case "$(uname -s 2>/dev/null || true)" in
    MINGW*|MSYS*|CYGWIN*)
      export CAMPUSOS_DEV_UID="${CAMPUSOS_DEV_UID:-1000}"
      export CAMPUSOS_DEV_GID="${CAMPUSOS_DEV_GID:-1000}"
      ;;
    *)
      export CAMPUSOS_DEV_UID="${CAMPUSOS_DEV_UID:-$(id -u)}"
      export CAMPUSOS_DEV_GID="${CAMPUSOS_DEV_GID:-$(id -g)}"
      ;;
  esac
}

set_developer_identity

compose() {
  local args=(docker compose --env-file "$ENV_FILE")
  if [[ -n "${CAMPUSOS_DOCKER_PROJECT:-}" ]]; then
    args+=(--project-name "$CAMPUSOS_DOCKER_PROJECT")
  fi
  args+=(-f "$COMPOSE_FILE")
  "${args[@]}" "$@"
}

native_process_command() {
  local pid="$1"
  if [[ -r "/proc/$pid/cmdline" ]]; then
    tr '\0' ' ' <"/proc/$pid/cmdline"
    return
  fi
  ps -p "$pid" -o args= 2>/dev/null || true
}

stop_native_dev() {
  if [[ ! -f "$NATIVE_PID_FILE" ]]; then
    return
  fi

  local pid command_line
  IFS= read -r pid <"$NATIVE_PID_FILE" || true
  if [[ ! "$pid" =~ ^[0-9]+$ ]] || ! kill -0 "$pid" 2>/dev/null; then
    rm -f "$NATIVE_PID_FILE"
    return
  fi

  command_line="$(native_process_command "$pid")"
  if [[ "$command_line" != *"scripts/start-dev.sh"* ]]; then
    echo "Refusing to stop PID $pid because it is not a tracked CampusOS native development process." >&2
    echo "Remove stale state only after checking: $NATIVE_PID_FILE" >&2
    return 1
  fi

  echo "Stopping native CampusOS development services (PID $pid)."
  NATIVE_HANDOFF=true
  kill -TERM "$pid"
  for _ in {1..100}; do
    if ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$NATIVE_PID_FILE"
      return
    fi
    sleep 0.1
  done
  echo "Native CampusOS development process $pid did not stop within 10 seconds." >&2
  return 1
}

application_port_is_listening() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1
    return $?
  fi
  if command -v ss >/dev/null 2>&1; then
    ss -ltn 2>/dev/null | awk '{print $4}' | grep -Eq "[:.]${port}$"
    return $?
  fi
  return 1
}

wait_for_application_ports() {
  local ports=()
  ports+=("$(read_env_setting CAMPUSOS_DEV_API_PORT 8080)")
  ports+=("$(read_env_setting CAMPUSOS_DEV_WEB_PORT 3000)")
  ports+=("$(read_env_setting CAMPUSOS_DEV_ADMIN_PORT 3001)")
  ports+=("$(read_env_setting CAMPUSOS_DEV_DOCS_PORT 3002)")

  local attempt port busy
  for attempt in {1..100}; do
    busy=()
    for port in "${ports[@]}"; do
      if application_port_is_listening "$port"; then
        busy+=("$port")
      fi
    done
    if [[ "${#busy[@]}" -eq 0 ]]; then
      return
    fi
    sleep 0.1
  done
  echo "CampusOS application port(s) remain occupied after native shutdown: ${busy[*]}" >&2
  return 1
}

start_infrastructure() {
  compose up -d --wait --wait-timeout "${CAMPUSOS_DOCKER_WAIT_TIMEOUT:-600}" postgres redis nats
}

start_stack() {
  local build_mode="${1:-no-build}"
  local up_args=(up -d)
  NATIVE_HANDOFF=false
  stop_native_dev
  if [[ "$NATIVE_HANDOFF" == "true" ]]; then
    wait_for_application_ports
  fi
  if [[ "$build_mode" == "build" ]]; then
    echo "Rebuilding Docker development application images before startup."
    up_args+=(--build --force-recreate)
  else
    echo "Starting with existing Docker development images (no registry-backed rebuild)."
    echo "Use '$0 rebuild' after dependency, Dockerfile, Compose build, or container entry-script changes."
    up_args+=(--no-build)
  fi
  up_args+=(--wait --wait-timeout "${CAMPUSOS_DOCKER_WAIT_TIMEOUT:-600}")
  if [[ "$build_mode" == "build" ]]; then
    up_args+=(api web admin docs)
  fi
  compose "${up_args[@]}"
  compose ps
}

run_migrations() {
  local action="${1:-up}"
  local postgres_container
  postgres_container="$(compose ps -q postgres)"
  if [[ -z "$postgres_container" ]]; then
    echo "Docker development PostgreSQL is not running; run '$0 infra-up' first." >&2
    return 1
  fi

  CAMPUSOS_SKIP_DOTENV=true \
    MIGRATIONS_DIR="$ROOT_DIR/migrations" \
    DB_USER="$(read_env_setting POSTGRES_USER campusos)" \
    DB_NAME="$(read_env_setting POSTGRES_DB campusos)" \
    DB_PASSWORD="$(read_env_setting POSTGRES_PASSWORD campusos_dev)" \
    POSTGRES_CONTAINER="$postgres_container" \
    PSQL_MODE=docker \
    "$ROOT_DIR/scripts/migrate.sh" "$action"
}

stop_application_services() {
  check_docker
  local running
  running="$(compose ps --services --status running api web admin docs)"
  if [[ -z "$running" ]]; then
    echo "No Docker development application services are running."
    return
  fi
  echo "Stopping Docker development application services: $(tr '\n' ' ' <<<"$running")"
  compose stop api web admin docs
}

case "$command" in
  setup)
    if init_env; then
      echo
      echo "Next:"
      echo "  1. Edit $ENV_FILE"
      echo "  2. Keep EMAIL_PROVIDER=fake for API-only development, or configure smtp for real registration mail."
      echo "  3. Run '$0 setup --start' to validate and start."
      exit 0
    fi
    validate_env
    check_compose
    compose config --quiet
    echo "Docker development configuration is valid: $ENV_FILE"
    if [[ "${2:-}" == "--start" ]]; then
      check_docker
      start_stack build
    else
      echo "Run '$0 setup --start' to start now, or '$0 up' later."
    fi
    ;;
  config)
    validate_env
    check_compose
    compose config --quiet
    echo "Docker development configuration is valid: $ENV_FILE"
    ;;
  build)
    check_docker
    compose build api web admin docs
    ;;
  up)
    validate_env
    check_docker
    start_stack
    ;;
  rebuild)
    validate_env
    check_docker
    start_stack build
    ;;
  infra-up)
    validate_env
    check_docker
    start_infrastructure
    compose ps postgres redis nats
    ;;
  migrate)
    validate_env
    check_docker
    run_migrations "${2:-up}"
    ;;
  lan-check)
    if command -v python3 >/dev/null 2>&1 &&
      python3 -c 'import sys; raise SystemExit(0 if sys.version_info >= (3, 10) else 1)' >/dev/null 2>&1; then
      python3 "$ROOT_DIR/scripts/check-lan-access.py" --env-file "$ENV_FILE" --compose-file "$COMPOSE_FILE"
    elif command -v python >/dev/null 2>&1 &&
      python -c 'import sys; raise SystemExit(0 if sys.version_info >= (3, 10) else 1)' >/dev/null 2>&1; then
      python "$ROOT_DIR/scripts/check-lan-access.py" --env-file "$ENV_FILE" --compose-file "$COMPOSE_FILE"
    else
      echo "Python 3.10 or newer is required for lan-check." >&2
      exit 2
    fi
    ;;
  ps)
    compose ps
    ;;
  logs)
    shift || true
    compose logs -f --tail=200 "$@"
    ;;
  test)
    compose run --rm --no-deps \
      -e PLUGINS_DIR=/workspace/data/plugins \
      -e PLUGIN_DATA_DIR=/tmp/campusos-go-test/plugin_data \
      -e MODULE_DATA_DIR=/tmp/campusos-go-test/module_data \
      -e RESOURCE_DIR=/workspace/data/resources \
      api bash -c \
      'GOCACHE=/go-cache/build GOMODCACHE=/go-cache/modules GOFLAGS=-buildvcs=false go test ./... -count=1'
    ;;
  shell)
    compose exec api bash
    ;;
  down)
    stop_native_dev
    compose down
    ;;
  stop-apps)
    stop_application_services
    ;;
  stop-native)
    stop_native_dev
    ;;
  reset)
    [[ "${2:-}" == "--confirm" ]] || {
      echo "reset deletes only the campusos-dev containers and named volumes; pass --confirm" >&2
      exit 2
    }
    stop_native_dev
    compose down --volumes --remove-orphans
    ;;
  *)
    echo "Usage: $0 {setup [--start]|config|build|up|rebuild|infra-up|migrate [action]|lan-check|ps|logs [service]|test|shell|down|stop-apps|stop-native|reset --confirm}" >&2
    exit 2
    ;;
esac
