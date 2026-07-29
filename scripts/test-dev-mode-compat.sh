#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TEMP_DIR"
}
trap cleanup EXIT

mkdir -p "$TEMP_DIR/bin" "$TEMP_DIR/logs" "$TEMP_DIR/run"

cat >"$TEMP_DIR/bin/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$MOCK_DOCKER_LOG"
if [[ "${1:-}" == "info" ]]; then
  exit 0
fi
if [[ "${1:-}" == "inspect" ]]; then
  printf 'true\n'
  exit 0
fi
if [[ "${1:-}" == "exec" ]]; then
  if [[ "$*" == *" -tAc "* ]]; then
    printf '1\n'
  fi
  exit 0
fi
if [[ "${1:-} ${2:-}" == "compose version" ]]; then
  exit 0
fi
if [[ "${1:-}" == "compose" ]]; then
  if [[ "$*" == *" ps --services --status running api web admin docs"* ]]; then
    printf 'api\nweb\nadmin\ndocs\n'
  elif [[ "$*" == *" ps -q postgres"* ]]; then
    printf 'mock-postgres-container\n'
  fi
  exit 0
fi
exit 1
EOF

cat >"$TEMP_DIR/bin/go" <<'EOF'
#!/usr/bin/env bash
{
  printf 'DB_HOST=%s\n' "${DB_HOST:-}"
  printf 'DB_PORT=%s\n' "${DB_PORT:-}"
  printf 'DB_USER=%s\n' "${DB_USER:-}"
  printf 'REDIS_ADDR=%s\n' "${REDIS_ADDR:-}"
  printf 'NATS_URL=%s\n' "${NATS_URL:-}"
  printf 'EMAIL_PROVIDER=%s\n' "${EMAIL_PROVIDER:-}"
  printf 'JWT_SECRET=%s\n' "${JWT_SECRET:-}"
} >>"$MOCK_SERVICE_LOG"
exec sleep "${MOCK_SERVICE_SLEEP:-1}"
EOF

cat >"$TEMP_DIR/bin/pnpm" <<'EOF'
#!/usr/bin/env bash
printf '%s|api=%s|web=%s|docs=%s\n' \
  "$PWD" \
  "${CAMPUSOS_API_PROXY_TARGET:-}" \
  "${VITE_WEB_URL:-}" \
  "${VITE_DOCS_URL:-}" >>"$MOCK_SERVICE_LOG"
exec sleep "${MOCK_SERVICE_SLEEP:-1}"
EOF

cat >"$TEMP_DIR/bin/curl" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF

cat >"$TEMP_DIR/bin/lsof" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF

chmod +x "$TEMP_DIR/bin/"*

SHARED_ENV="$TEMP_DIR/docker.env"
cp "$ROOT_DIR/deploy/docker/.env.dev.example" "$SHARED_ENV"
sed -i \
  -e 's/^CAMPUSOS_DEV_POSTGRES_PORT=.*/CAMPUSOS_DEV_POSTGRES_PORT=55433/' \
  -e 's/^CAMPUSOS_DEV_REDIS_PORT=.*/CAMPUSOS_DEV_REDIS_PORT=56380/' \
  -e 's/^CAMPUSOS_DEV_NATS_PORT=.*/CAMPUSOS_DEV_NATS_PORT=54223/' \
  -e 's/^JWT_SECRET=.*/JWT_SECRET=shared-mode-jwt/' \
  -e 's/^EMAIL_PROVIDER=.*/EMAIL_PROVIDER=smtp/' \
  -e 's/^EMAIL_SMTP_HOST=.*/EMAIL_SMTP_HOST=smtp.example.test/' \
  -e 's/^EMAIL_SMTP_USERNAME=.*/EMAIL_SMTP_USERNAME=mailer@example.test/' \
  -e 's/^EMAIL_SMTP_PASSWORD=.*/EMAIL_SMTP_PASSWORD=test-only-password/' \
  -e 's/^EMAIL_SMTP_FROM=.*/EMAIL_SMTP_FROM=mailer@example.test/' \
  "$SHARED_ENV"

NATIVE_ENV="$TEMP_DIR/native.env"
cat >"$NATIVE_ENV" <<'EOF'
SERVER_PORT=8080
WEB_PORT=3000
ADMIN_PORT=3001
DOCS_PORT=3002
DATABASE_DSN=postgres://must-be-overridden.invalid/campusos
EOF

MOCK_DOCKER_LOG="$TEMP_DIR/docker.log" \
MOCK_SERVICE_LOG="$TEMP_DIR/services.log" \
PATH="$TEMP_DIR/bin:$PATH" \
CAMPUSOS_DOCKER_DEV_ENV="$SHARED_ENV" \
CAMPUSOS_DEV_ENV_FILE="$NATIVE_ENV" \
CAMPUSOS_RUN_DIR="$TEMP_DIR/run" \
CAMPUSOS_LOG_DIR="$TEMP_DIR/logs" \
CAMPUSOS_DEV_INFRA_MODE=auto \
STOP_EXISTING=true \
SERVER_PORT=18080 \
WEB_PORT=13000 \
ADMIN_PORT=13001 \
DOCS_PORT=13002 \
  "$ROOT_DIR/scripts/start-dev.sh" >"$TEMP_DIR/start.out"

grep -q 'api:   http://localhost:18080/api/v1' "$TEMP_DIR/start.out"
grep -q 'web:   http://localhost:13000' "$TEMP_DIR/start.out"
grep -q 'admin: http://localhost:13001' "$TEMP_DIR/start.out"
grep -q 'docs:  http://localhost:13002' "$TEMP_DIR/start.out"
grep -q '^  docker-dev$' "$TEMP_DIR/start.out"

grep -q '^DB_HOST=127.0.0.1$' "$TEMP_DIR/services.log"
grep -q '^DB_PORT=55433$' "$TEMP_DIR/services.log"
grep -q '^REDIS_ADDR=127.0.0.1:56380$' "$TEMP_DIR/services.log"
grep -q '^NATS_URL=nats://127.0.0.1:54223$' "$TEMP_DIR/services.log"
grep -q '^EMAIL_PROVIDER=smtp$' "$TEMP_DIR/services.log"
grep -q '^JWT_SECRET=shared-mode-jwt$' "$TEMP_DIR/services.log"
grep -q "$ROOT_DIR/admin|api=http://localhost:18080|web=http://localhost:13000|docs=http://localhost:13002" "$TEMP_DIR/services.log"

grep -q 'stop api web admin docs' "$TEMP_DIR/docker.log"
grep -q 'up -d --wait --wait-timeout 600 postgres redis nats' "$TEMP_DIR/docker.log"
grep -q 'ps -q postgres' "$TEMP_DIR/docker.log"
grep -q 'exec -i.*mock-postgres-container.*psql' "$TEMP_DIR/docker.log"
test ! -e "$TEMP_DIR/run/native-dev.pid"

mkdir -p "$TEMP_DIR/run-signal" "$TEMP_DIR/logs-signal"
MOCK_DOCKER_LOG="$TEMP_DIR/docker-signal.log" \
MOCK_SERVICE_LOG="$TEMP_DIR/services-signal.log" \
MOCK_SERVICE_SLEEP=30 \
PATH="$TEMP_DIR/bin:$PATH" \
CAMPUSOS_DOCKER_DEV_ENV="$SHARED_ENV" \
CAMPUSOS_DEV_ENV_FILE="$NATIVE_ENV" \
CAMPUSOS_RUN_DIR="$TEMP_DIR/run-signal" \
CAMPUSOS_LOG_DIR="$TEMP_DIR/logs-signal" \
CAMPUSOS_DEV_INFRA_MODE=docker-dev \
SKIP_INFRA=true \
SKIP_MIGRATE=true \
SERVER_PORT=28080 \
WEB_PORT=23000 \
ADMIN_PORT=23001 \
DOCS_PORT=23002 \
  "$ROOT_DIR/scripts/start-dev.sh" >"$TEMP_DIR/signal.out" &
SIGNAL_PID="$!"
for _ in {1..100}; do
  if grep -q 'CampusOS dev services are running' "$TEMP_DIR/signal.out" 2>/dev/null; then
    break
  fi
  sleep 0.05
done
grep -q 'CampusOS dev services are running' "$TEMP_DIR/signal.out"
kill -TERM "$SIGNAL_PID"
wait "$SIGNAL_PID"
test "$(grep -c 'stopping CampusOS dev services' "$TEMP_DIR/signal.out")" -eq 1
test ! -e "$TEMP_DIR/run-signal/native-dev.pid"

echo "Docker/native development mode compatibility tests passed."
