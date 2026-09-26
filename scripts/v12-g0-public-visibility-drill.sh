#!/usr/bin/env bash
set -euo pipefail

# Disposable Linux/PostgreSQL HTTP drill for Community visibility. The
# database lives in container tmpfs; no existing Compose project or DB is used.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

for executable in docker go python3 curl; do
  command -v "$executable" >/dev/null || { echo "missing $executable" >&2; exit 127; }
done
mkdir -p .cache/v12-g0/tmp .cache/v12-g0/go-cache
export TMPDIR="$repo_root/.cache/v12-g0/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
authenticated=false
admin_admission=false
personal_documents=false
case "${1:-}" in
  --authenticated) authenticated=true; shift ;;
  --admin-admission) authenticated=true; admin_admission=true; shift ;;
  --personal-documents) authenticated=true; personal_documents=true; shift ;;
esac
if (( $# > 1 )) || [[ "${1:-}" == --* ]]; then
  echo "usage: $0 [--authenticated|--admin-admission|--personal-documents] [evidence.json]" >&2
  exit 2
fi
work_dir="$(mktemp -d "$repo_root/.cache/v12-g0-visibility.XXXXXX")"
container="campusos-v12-g0-visibility-$$"
database="campusos_v12_g0_visibility"
output="${1:-$repo_root/.cache/v12-g0-public-visibility.json}"
public_output="$output"
if [[ "$authenticated" == true ]]; then
  output="${1:-$repo_root/.cache/v12-g0-owner-session.json}"
  public_output="$work_dir/public-visibility.json"
fi
if [[ "$admin_admission" == true ]]; then
  output="${1:-$repo_root/.cache/v12-g0-admin-admission.json}"
fi
if [[ "$personal_documents" == true ]]; then
  output="${1:-$repo_root/.cache/v12-g0-personal-documents.json}"
fi
api_pid=""
container_started=false

cleanup() {
  if [[ -n "$api_pid" ]]; then
    kill "$api_pid" 2>/dev/null || true
    wait "$api_pid" 2>/dev/null || true
  fi
  if [[ "$container_started" == true ]]; then
    docker stop "$container" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$work_dir"
}
trap cleanup EXIT

random_secret() { python3 -c 'import secrets; print(secrets.token_hex(32))'; }
postgres_password="$(random_secret)"
bootstrap_secret="$(random_secret)"
jwt_secret="$(random_secret)"
challenge_secret="$(random_secret)"
mfa_secret="$(random_secret)"
api_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"

docker run --rm -d --name "$container" \
  --tmpfs /var/lib/postgresql/data:rw,size=512m \
  -e POSTGRES_DB="$database" -e POSTGRES_USER=campusos \
  -e POSTGRES_PASSWORD="$postgres_password" \
  -p 127.0.0.1::5432 postgres:16-alpine >/dev/null
container_started=true
for attempt in $(seq 1 80); do
  if docker exec "$container" pg_isready -U campusos -d "$database" >/dev/null 2>&1; then
    break
  fi
  if (( attempt == 80 )); then
    echo "isolated PostgreSQL did not become ready" >&2
    exit 1
  fi
  sleep 0.25
done
tmpfs_options="$(docker inspect --format '{{index .HostConfig.Tmpfs "/var/lib/postgresql/data"}}' "$container")"
if [[ "$tmpfs_options" != *size=512m* ]]; then
  echo "isolated PostgreSQL data directory is not configured as the expected tmpfs" >&2
  exit 1
fi
postgres_port="$(docker port "$container" 5432/tcp | awk -F: '/127\.0\.0\.1/ {print $NF; exit}')"
[[ "$postgres_port" =~ ^[0-9]+$ ]] || { echo "isolated PostgreSQL is not loopback-bound" >&2; exit 1; }

CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
  DB_USER=campusos DB_NAME="$database" DB_PASSWORD="$postgres_password" \
  bash scripts/migrate.sh up >"$work_dir/migration.log"
CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
  DB_USER=campusos DB_NAME="$database" DB_PASSWORD="$postgres_password" \
  bash scripts/migrate.sh check >>"$work_dir/migration.log"
migration_count="$(docker exec -e PGPASSWORD="$postgres_password" "$container" \
  psql -U campusos -d "$database" -Atqc 'SELECT count(*) FROM schema_migrations')"

# The last two rows intentionally carry a stale legacy status. Public reads
# must obey the current publication and moderation facts in both cases.
docker exec -i -e PGPASSWORD="$postgres_password" "$container" \
  psql -U campusos -d "$database" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
INSERT INTO users(id, username, nickname, email) VALUES
  (2001, 'g0_owner_a', 'G0 Owner A', 'g0-owner-a@example.invalid'),
  (2002, 'g0_owner_b', 'G0 Owner B', 'g0-owner-b@example.invalid');
INSERT INTO categories(id, name, slug) VALUES (2003, 'G0 visibility board', 'g0-visibility');
INSERT INTO threads(id, title, content, author_id, author_name, category_id,
                    status, publication_status, moderation_status, deletion_status) VALUES
  (3001, 'G0 public A', 'public sample A', 2001, 'G0 Owner A', 2003, 'published', 'published', 'clear', 'active'),
  (3002, 'G0 public B', 'public sample B', 2002, 'G0 Owner B', 2003, 'published', 'published', 'clear', 'active'),
  (3003, 'G0 private', 'private sample', 2001, 'G0 Owner A', 2003, 'private', 'private', 'clear', 'active'),
  (3004, 'G0 taken down', 'moderated sample', 2001, 'G0 Owner A', 2003, 'archived', 'published', 'taken_down', 'active'),
  (3005, 'G0 trash', 'deleted sample', 2002, 'G0 Owner B', 2003, 'archived', 'published', 'clear', 'trashed'),
  (3006, 'G0 stale private', 'legacy private sample', 2002, 'G0 Owner B', 2003, 'published', 'private', 'clear', 'active'),
  (3007, 'G0 stale moderated', 'legacy moderated sample', 2002, 'G0 Owner B', 2003, 'published', 'published', 'taken_down', 'active');
SQL

# Credentials are disposable, independently generated and bcrypt hashed.
# Direct fixture seeding is not registration/email-verification acceptance.
if [[ "$authenticated" == true ]]; then
  owner_a_password="$(random_secret)"
  owner_b_password="$(random_secret)"
  owner_a_hash="$(printf '%s' "$owner_a_password" | go run ./scripts/hash-password.go)"
  owner_b_hash="$(printf '%s' "$owner_b_password" | go run ./scripts/hash-password.go)"
  docker exec -i -e PGPASSWORD="$postgres_password" "$container" \
    psql -U campusos -d "$database" -v ON_ERROR_STOP=1 \
    -v owner_a_hash="$owner_a_hash" -v owner_b_hash="$owner_b_hash" >/dev/null <<'SQL'
INSERT INTO accounts(id, user_id, type, identifier, identifier_normalized,
                     credential, verified, verification_state, verified_at,
                     verification_source, password_changed_at)
SELECT id + 100, id, 'email', email, email,
       CASE id WHEN 2001 THEN :'owner_a_hash' ELSE :'owner_b_hash' END,
       true, 'verified', now(), 'g0_disposable_fixture', now()
FROM users WHERE id IN (2001, 2002);
SQL
fi

go build -o "$work_dir/campusos-server" ./cmd/server
mkdir -p "$work_dir/modules" "$work_dir/data/resources" "$work_dir/data/plugins" "$work_dir/plugins"
cp -a modules/. "$work_dir/modules/"
cp -a data/resources/. "$work_dir/data/resources/"
(
  cd "$work_dir"
  exec env -i PATH="$PATH" \
    CAMPUSOS_ENV=test CAMPUSOS_INSTANCE_MODE=single \
    SERVER_HOST=127.0.0.1 SERVER_PORT="$api_port" \
    DATABASE_DSN="postgres://campusos:$postgres_password@127.0.0.1:$postgres_port/$database?sslmode=disable" \
    REDIS_ENABLED=false HOST_API_ENABLED=false AI_ENABLED=false EMAIL_PROVIDER=fake \
    AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN=false AUTH_BOOTSTRAP_ADMIN_SECRET="$bootstrap_secret" \
    JWT_SECRET="$jwt_secret" AUTH_CHALLENGE_ACTIVE_KEY_ID=g0 \
    AUTH_CHALLENGE_HMAC_KEYS="g0:$challenge_secret" \
    AUTH_CHALLENGE_IP_HASH_SECRET="$challenge_secret" \
    AUTH_SESSION_IP_HASH_SECRET="$challenge_secret" \
    AUTH_MFA_ACTIVE_KEY_ID=g0 AUTH_MFA_ENCRYPTION_KEYS="g0:$mfa_secret" \
    PLUGINS_DIR="$work_dir/data/plugins" PLUGIN_DATA_DIR="$work_dir/data/plugin_data" \
    CAMPUSOS_PLUGIN_V4_DIR="$work_dir/plugins" CAMPUSOS_PLUGIN_V4_DEV_SOURCE=false \
    MODULE_DATA_DIR="$work_dir/data/module_data" RESOURCE_DIR="$work_dir/data/resources" \
    "$work_dir/campusos-server"
) >"$work_dir/api.log" 2>&1 &
api_pid="$!"
for attempt in $(seq 1 80); do
  if curl -fsS "http://127.0.0.1:$api_port/api/v1/health" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$api_pid" 2>/dev/null; then
    echo "isolated API exited before health check" >&2
    exit 1
  fi
  if (( attempt == 80 )); then
    echo "isolated API did not become healthy" >&2
    exit 1
  fi
  sleep 0.25
done

V12_G0_BASE_URL="http://127.0.0.1:$api_port" \
V12_G0_MIGRATIONS="$migration_count" V12_G0_OUTPUT="$public_output" \
python3 - <<'PY'
import json
import os
import platform
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

base = os.environ['V12_G0_BASE_URL']
def get(path):
    try:
        with urllib.request.urlopen(base + path, timeout=5) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, None

checks = {}
for name, path in {
    'public_list': '/api/v1/threads?page=1&page_size=20',
    'override_attempt': '/api/v1/threads?page=1&page_size=20&status=all&publication_status=private&moderation_status=taken_down&deletion_status=trashed',
}.items():
    status, payload = get(path)
    if status != 200 or payload.get('code') != 0:
        raise SystemExit(f'{name}: expected successful list, got HTTP {status}')
    data = payload['data']
    ids = {str(item['id']) for item in data['items']}
    if ids != {'3001', '3002'} or data['pagination']['total'] != 2:
        raise SystemExit(f'{name}: public visibility mismatch; total={data["pagination"]["total"]}, ids={sorted(ids)}')
    checks[name] = {'http_status': status, 'visible_count': len(ids), 'total': data['pagination']['total']}

for name, thread_id, expected in [
    ('public_detail', 3001, 200),
    ('private_detail', 3003, 404),
    ('taken_down_detail', 3004, 404),
    ('trashed_detail', 3005, 404),
    ('stale_private_detail', 3006, 404),
    ('stale_moderated_detail', 3007, 404),
]:
    status, _ = get(f'/api/v1/threads/{thread_id}')
    if status != expected:
        raise SystemExit(f'{name}: expected HTTP {expected}, got {status}')
    checks[name] = {'http_status': status}

report = {
    'schema': 'campusos.v12-g0-public-visibility/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'environment': {'os': platform.system().lower(), 'arch': platform.machine(), 'database': 'postgres:16-alpine', 'database_mount': 'tmpfs', 'api_bind': 'loopback'},
    'migration_count': int(os.environ['V12_G0_MIGRATIONS']),
    'fixture': {'owner_users': 2, 'threads': 7, 'public': 2, 'private': 2, 'moderated': 2, 'trashed': 1, 'stale_legacy_status': 2},
    'checks': checks,
}
output = Path(os.environ['V12_G0_OUTPUT'])
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(report, indent=2, ensure_ascii=False) + '\n')
print(f'G0 public visibility passed: 2 of 7 fixture threads visible; evidence={output}')
PY

if [[ "$authenticated" == true && "$admin_admission" == false && "$personal_documents" == false ]]; then
  V12_G0_BASE_URL="http://127.0.0.1:$api_port" \
  V12_G0_OWNER_A_PASSWORD="$owner_a_password" V12_G0_OWNER_B_PASSWORD="$owner_b_password" \
  V12_G0_PUBLIC_EVIDENCE="$public_output" V12_G0_OUTPUT="$output" \
    python3 scripts/v12-g0-owner-session-check.py
fi

if [[ "$admin_admission" == true ]]; then
  V12_G0_BASE_URL="http://127.0.0.1:$api_port" \
  V12_G0_OWNER_A_PASSWORD="$owner_a_password" V12_G0_OWNER_B_PASSWORD="$owner_b_password" \
  V12_G0_BOOTSTRAP_PASSWORD="$bootstrap_secret" \
  V12_G0_PUBLIC_EVIDENCE="$public_output" V12_G0_OUTPUT="$output" \
    python3 scripts/v12-g0-admin-admission-check.py
fi

if [[ "$personal_documents" == true ]]; then
  V12_G0_BASE_URL="http://127.0.0.1:$api_port" \
  V12_G0_OWNER_A_PASSWORD="$owner_a_password" V12_G0_OWNER_B_PASSWORD="$owner_b_password" \
  V12_G0_BOOTSTRAP_PASSWORD="$bootstrap_secret" \
  V12_G0_STORAGE_ROOT="$work_dir/data/personal-space" \
  V12_G0_PUBLIC_EVIDENCE="$public_output" V12_G0_OUTPUT="$work_dir/document-http.json" \
    python3 scripts/v12-g0-personal-documents-check.py

  # Read-only postconditions against this drill's database. Only aggregate
  # counts leave PostgreSQL; physical keys, credentials and names stay private.
  docker exec -i -e PGPASSWORD="$postgres_password" "$container" \
    psql -U campusos -d "$database" -v ON_ERROR_STOP=1 -At >"$work_dir/document-database.json" <<'SQL'
SELECT json_build_object(
  'documents', (SELECT count(*) FROM personal_documents),
  'owners', (SELECT count(DISTINCT owner_user_id) FROM personal_documents),
  'active_documents', (SELECT count(*) FROM personal_documents WHERE status='active'),
  'versions', (SELECT count(*) FROM personal_document_versions),
  'objects', (SELECT count(*) FROM storage_objects),
  'valid_current_object_refs', (SELECT count(*) FROM personal_documents d
    JOIN personal_document_versions v ON v.id=d.current_version_id AND v.document_id=d.id
    JOIN storage_objects o ON o.id=v.source_object_id AND o.owner_user_id=d.owner_user_id
      AND o.status='ready' AND o.namespace='personal-documents'
      AND o.size_bytes=v.size_bytes AND o.sha256=v.sha256),
  'object_bytes', (SELECT COALESCE(sum(size_bytes),0) FROM storage_objects WHERE status='ready'),
  'balanced_owner_ledgers', (SELECT count(*) FROM user_storage_accounts a
    WHERE a.user_id IN (2001,2002) AND a.reserved_bytes=0
      AND a.used_bytes=(SELECT sum(o.size_bytes) FROM storage_objects o
        WHERE o.owner_user_id=a.user_id AND o.status='ready'))
);
SQL
  V12_G0_HTTP_REPORT="$work_dir/document-http.json" \
  V12_G0_DB_REPORT="$work_dir/document-database.json" V12_G0_OUTPUT="$output" \
  python3 - <<'PYREPORT'
import json
import os
from pathlib import Path
report = json.loads(Path(os.environ['V12_G0_HTTP_REPORT']).read_text())
database = json.loads(Path(os.environ['V12_G0_DB_REPORT']).read_text())
expected = {key: 2 for key in ('documents', 'owners', 'active_documents', 'versions', 'objects',
                             'valid_current_object_refs', 'balanced_owner_ledgers')}
expected['object_bytes'] = report['fixture']['document_bytes']
if database != expected:
    raise SystemExit('G0 personal document database/ledger postconditions failed')
report['database_checks'] = database
output = Path(os.environ['V12_G0_OUTPUT'])
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(report, indent=2, ensure_ascii=False) + '\n')
print(f'G0 personal documents HTTP/storage/database checks passed; evidence={output}')
PYREPORT
fi
