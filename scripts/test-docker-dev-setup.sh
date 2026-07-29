#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEMP_DIR="$(mktemp -d)"
NATIVE_TEST_PID=""
cleanup() {
  if [[ -n "$NATIVE_TEST_PID" ]]; then
    kill "$NATIVE_TEST_PID" 2>/dev/null || true
    wait "$NATIVE_TEST_PID" 2>/dev/null || true
  fi
  rm -rf "$TEMP_DIR"
}
trap cleanup EXIT

mkdir -p "$TEMP_DIR/bin"
cat >"$TEMP_DIR/bin/docker" <<'EOF'
#!/usr/bin/env bash
if [[ -n "${MOCK_DOCKER_LOG:-}" ]]; then
  printf '%s\n' "$*" >>"$MOCK_DOCKER_LOG"
fi
case "${1:-} ${2:-}" in
  "compose version"|"info ") exit 0 ;;
esac
if [[ "${1:-}" == "compose" ]]; then
  if [[ "$*" == *" ps --services --status running api web admin docs"* ]] \
    && [[ "${MOCK_DOCKER_APPS_RUNNING:-false}" == "true" ]]; then
    printf 'api\nweb\nadmin\ndocs\n'
  fi
  exit 0
fi
exit 1
EOF
chmod +x "$TEMP_DIR/bin/docker"
cat >"$TEMP_DIR/bin/lsof" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
chmod +x "$TEMP_DIR/bin/lsof"

NEW_ENV="$TEMP_DIR/new.env"
PATH="$TEMP_DIR/bin:$PATH" CAMPUSOS_DOCKER_DEV_ENV="$NEW_ENV" \
  "$ROOT_DIR/scripts/docker-dev.sh" setup >"$TEMP_DIR/init.out"
test -f "$NEW_ENV"
grep -q 'Created local Docker development configuration' "$TEMP_DIR/init.out"

LEGACY_ENV="$TEMP_DIR/legacy.env"
cat >"$LEGACY_ENV" <<'EOF'
JWT_SECRET=legacy-shared-jwt
EMAIL_PROVIDER=smtp
EMAIL_SMTP_HOST=smtp.legacy.test
EMAIL_SMTP_PORT=587
EMAIL_SMTP_USERNAME=legacy@example.test
EMAIL_SMTP_PASSWORD=legacy-secret-not-for-output
EMAIL_SMTP_FROM=legacy@example.test
EMAIL_SMTP_STARTTLS=true
DATABASE_DSN=postgres://must-not-be-imported.invalid/campusos
EOF
IMPORTED_ENV="$TEMP_DIR/imported.env"
PATH="$TEMP_DIR/bin:$PATH" \
  CAMPUSOS_DOCKER_DEV_ENV="$IMPORTED_ENV" \
  CAMPUSOS_DOCKER_DEV_IMPORT_SOURCE="$LEGACY_ENV" \
  "$ROOT_DIR/scripts/docker-dev.sh" setup >"$TEMP_DIR/import.out"
grep -q 'Imported .* shared runtime setting(s)' "$TEMP_DIR/import.out"
grep -q '^JWT_SECRET=legacy-shared-jwt$' "$IMPORTED_ENV"
grep -q '^EMAIL_SMTP_HOST=smtp.legacy.test$' "$IMPORTED_ENV"
grep -q '^EMAIL_SMTP_PASSWORD=legacy-secret-not-for-output$' "$IMPORTED_ENV"
if grep -q '^DATABASE_DSN=' "$IMPORTED_ENV"; then
  echo "Legacy database DSN must not be imported into Docker development configuration." >&2
  exit 1
fi
if grep -q 'legacy-secret-not-for-output' "$TEMP_DIR/import.out"; then
  echo "Imported SMTP password was exposed in setup output." >&2
  exit 1
fi

PATH="$TEMP_DIR/bin:$PATH" CAMPUSOS_DOCKER_DEV_ENV="$NEW_ENV" \
  "$ROOT_DIR/scripts/docker-dev.sh" setup >"$TEMP_DIR/fake.out"
grep -q 'no verification email is sent' "$TEMP_DIR/fake.out"

INVALID_ENV="$TEMP_DIR/invalid.env"
cp "$ROOT_DIR/deploy/docker/.env.dev.example" "$INVALID_ENV"
sed -i 's/^EMAIL_PROVIDER=.*/EMAIL_PROVIDER=smtp/' "$INVALID_ENV"
if PATH="$TEMP_DIR/bin:$PATH" CAMPUSOS_DOCKER_DEV_ENV="$INVALID_ENV" \
  "$ROOT_DIR/scripts/docker-dev.sh" setup >"$TEMP_DIR/invalid.out" 2>&1; then
  echo "SMTP configuration without host/from unexpectedly passed." >&2
  exit 1
fi
grep -q 'EMAIL_SMTP_HOST and EMAIL_SMTP_FROM are required' "$TEMP_DIR/invalid.out"

CONFLICT_ENV="$TEMP_DIR/conflict.env"
cp "$ROOT_DIR/deploy/docker/.env.dev.example" "$CONFLICT_ENV"
sed -i 's/^CAMPUSOS_DEV_ADMIN_PORT=.*/CAMPUSOS_DEV_ADMIN_PORT=3000/' "$CONFLICT_ENV"
if PATH="$TEMP_DIR/bin:$PATH" CAMPUSOS_DOCKER_DEV_ENV="$CONFLICT_ENV" \
  "$ROOT_DIR/scripts/docker-dev.sh" setup >"$TEMP_DIR/conflict.out" 2>&1; then
  echo "Conflicting development ports unexpectedly passed." >&2
  exit 1
fi
grep -q 'CAMPUSOS_DEV_ADMIN_PORT conflicts with CAMPUSOS_DEV_WEB_PORT' "$TEMP_DIR/conflict.out"

EXPOSED_ENV="$TEMP_DIR/exposed.env"
cp "$ROOT_DIR/deploy/docker/.env.dev.example" "$EXPOSED_ENV"
sed -i 's/^CAMPUSOS_DEV_BIND=.*/CAMPUSOS_DEV_BIND=0.0.0.0/' "$EXPOSED_ENV"
if PATH="$TEMP_DIR/bin:$PATH" CAMPUSOS_DOCKER_DEV_ENV="$EXPOSED_ENV" \
  "$ROOT_DIR/scripts/docker-dev.sh" setup >"$TEMP_DIR/exposed.out" 2>&1; then
  echo "Non-loopback development bind unexpectedly passed." >&2
  exit 1
fi
grep -q 'CAMPUSOS_DEV_BIND must remain 127.0.0.1' "$TEMP_DIR/exposed.out"

VALID_ENV="$TEMP_DIR/valid.env"
cp "$ROOT_DIR/deploy/docker/.env.dev.example" "$VALID_ENV"
sed -i \
  -e 's/^EMAIL_PROVIDER=.*/EMAIL_PROVIDER=smtp/' \
  -e 's/^EMAIL_SMTP_HOST=.*/EMAIL_SMTP_HOST=smtp.example.test/' \
  -e 's/^EMAIL_SMTP_USERNAME=.*/EMAIL_SMTP_USERNAME=mailer@example.test/' \
  -e 's/^EMAIL_SMTP_PASSWORD=.*/EMAIL_SMTP_PASSWORD=do-not-print-this-secret/' \
  -e 's/^EMAIL_SMTP_FROM=.*/EMAIL_SMTP_FROM=mailer@example.test/' \
  "$VALID_ENV"
PATH="$TEMP_DIR/bin:$PATH" CAMPUSOS_DOCKER_DEV_ENV="$VALID_ENV" \
  "$ROOT_DIR/scripts/docker-dev.sh" setup >"$TEMP_DIR/smtp.out"
grep -q 'Email provider: smtp (smtp.example.test:587' "$TEMP_DIR/smtp.out"
if grep -q 'do-not-print-this-secret' "$TEMP_DIR/smtp.out"; then
  echo "SMTP password was exposed in setup output." >&2
  exit 1
fi

MOCK_DOCKER_LOG="$TEMP_DIR/docker.log" PATH="$TEMP_DIR/bin:$PATH" \
  MOCK_DOCKER_APPS_RUNNING=true CAMPUSOS_DOCKER_DEV_ENV="$VALID_ENV" \
  "$ROOT_DIR/scripts/docker-dev.sh" stop-apps >"$TEMP_DIR/stop-apps.out"
grep -q 'Stopping Docker development application services: api web admin docs' "$TEMP_DIR/stop-apps.out"
grep -q 'stop api web admin docs' "$TEMP_DIR/docker.log"

MOCK_DOCKER_LOG="$TEMP_DIR/docker.log" PATH="$TEMP_DIR/bin:$PATH" \
  CAMPUSOS_DOCKER_DEV_ENV="$VALID_ENV" \
  "$ROOT_DIR/scripts/docker-dev.sh" infra-up >"$TEMP_DIR/infra-up.out"
grep -q 'up -d --wait --wait-timeout 600 postgres redis nats' "$TEMP_DIR/docker.log"

mkdir -p "$TEMP_DIR/native/scripts" "$TEMP_DIR/run"
cat >"$TEMP_DIR/native/scripts/start-dev.sh" <<'EOF'
#!/usr/bin/env bash
sleep 30
EOF
chmod +x "$TEMP_DIR/native/scripts/start-dev.sh"
"$TEMP_DIR/native/scripts/start-dev.sh" &
NATIVE_TEST_PID="$!"
printf '%s\n' "$NATIVE_TEST_PID" >"$TEMP_DIR/run/native-dev.pid"
CAMPUSOS_RUN_DIR="$TEMP_DIR/run" CAMPUSOS_DOCKER_DEV_ENV="$VALID_ENV" \
  PATH="$TEMP_DIR/bin:$PATH" MOCK_DOCKER_LOG="$TEMP_DIR/docker.log" \
  "$ROOT_DIR/scripts/docker-dev.sh" up >"$TEMP_DIR/native-to-docker.out"
wait "$NATIVE_TEST_PID" 2>/dev/null || true
if kill -0 "$NATIVE_TEST_PID" 2>/dev/null; then
  echo "Tracked native development process was not stopped." >&2
  exit 1
fi
NATIVE_TEST_PID=""
grep -q 'Stopping native CampusOS development services' "$TEMP_DIR/native-to-docker.out"
grep -q 'up -d --build --wait --wait-timeout 600' "$TEMP_DIR/docker.log"
test ! -e "$TEMP_DIR/run/native-dev.pid"

echo "Docker development setup guidance tests passed."
