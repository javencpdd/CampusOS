#!/usr/bin/env python3
"""Owned Linux/PG16 native target for the09a public-thread candidate Port.

Runs production Server module composition, not an HTTP/browser or collection
exit. Builds before target entry; only fresh owned cache roots and Docker
resources are mutated. No shared Compose stack or database is touched.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import subprocess
import time

REPO = Path(__file__).resolve().parent.parent
PRIOR = REPO / 'docs/项目计划书v1/项目计划v1.2/evidence/v12-08b-exit-linux.json'
PRIOR_SHA = 'c4f7e22f6c53c4022207986f8ca7269aa23b3a53a883c195626ed8b35bf62c75'
DATABASE = 'campusos_v12_09a_public_source'


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def source_map():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=REPO).decode().split('\0')
    return {n: sha(REPO / n) for n in sorted(set(names)) if n and (REPO / n).is_file()
            and not n.endswith('.md') and not n.startswith('docs/项目计划书v1/项目计划v1.2/evidence/')}


def main(work):
    assert os.uname().sysname == 'Linux'
    assert sha(PRIOR) == PRIOR_SHA, 'predecessor evidence changed'
    assert work.is_relative_to(REPO / '.cache/v12-09a-public-source'), 'owned work root required'
    work.mkdir(parents=True, exist_ok=False)
    work.chmod(0o700)
    runtime = work / 'runtime'
    runtime.mkdir(mode=0o700)
    binary_dir = work / 'native'
    binary_dir.mkdir()
    frozen = source_map()
    (work / 'source-freeze.json').write_text(json.dumps(frozen, ensure_ascii=False, indent=2) + '\n')
    env = {n: os.environ[n] for n in ('PATH', 'HOME') if n in os.environ}
    env.update(GOCACHE=str(REPO / '.cache/v12-g0/go-cache'), GOTOOLCHAIN='auto', GOFLAGS='-buildvcs=false', GOMAXPROCS='2', CAMPUSOS_SKIP_DOTENV='true')
    password = secrets.token_hex(24)
    container = 'campusos-v12-09a-source-' + secrets.token_hex(6)
    started = False
    commands = []

    def run(name, args, extra=None, required=True):
        begin = time.monotonic()
        with (work / (name + '.log')).open('wb') as out:
            result = subprocess.run(args, cwd=REPO, env=env | (extra or {}), stdout=out, stderr=subprocess.STDOUT)
        commands.append({'name': name, 'exit_code': result.returncode, 'seconds': round(time.monotonic() - begin, 3)})
        assert not required or result.returncode == 0, name + ' failed; inspect private log'
        return (work / (name + '.log')).read_text()

    try:
        for package, name in [('./internal/modules/features/knowledge', 'knowledge.test'), ('./internal/server', 'server.test')]:
            run('build-' + name, ['go', 'test', '-race', '-c', '-o', str(binary_dir / name), package])
        unit = run('native-unit', [str(binary_dir / 'knowledge.test'), '-test.v', '-test.run=^TestPublicCandidate'])
        assert 'PASS\n' in unit and '--- FAIL' not in unit and '--- SKIP' not in unit and 'DATA RACE' not in unit
        run('pg-start', ['docker', 'run', '--rm', '--pull=never', '-d', '--name', container, '--tmpfs', '/var/lib/postgresql/data:rw,size=512m',
                         '-e', 'POSTGRES_USER=campusos', '-e', 'POSTGRES_DB=' + DATABASE, '-e', 'POSTGRES_PASSWORD', '-p', '127.0.0.1::5432', 'postgres:16-alpine'], {'POSTGRES_PASSWORD': password})
        started = True
        for _ in range(120):
            if subprocess.run(['docker', 'exec', container, 'pg_isready', '-h', '127.0.0.1', '-U', 'campusos'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                break
            time.sleep(.25)
        else:
            raise AssertionError('owned PostgreSQL startup deadline')
        port = int(run('pg-port', ['docker', 'port', container, '5432/tcp']).strip().rsplit(':', 1)[1])
        pg_env = {'PSQL_MODE': 'docker', 'POSTGRES_CONTAINER': container, 'DB_USER': 'campusos', 'DB_NAME': DATABASE, 'DB_PASSWORD': password, 'CAMPUSOS_SKIP_DOTENV': 'true'}
        run('migration-up', ['bash', 'scripts/migrate.sh', 'up'], pg_env)
        run('migration-check', ['bash', 'scripts/migrate.sh', 'check'], pg_env)
        run('migration-status', ['bash', 'scripts/migrate.sh', 'status'], pg_env)
        version = run('pg-version', ['docker', 'exec', container, 'psql', '-U', 'campusos', '-d', DATABASE, '-XAtc', 'SHOW server_version']).strip()
        dump = ['docker', 'exec', container, 'pg_dump', '-U', 'campusos', '-d', DATABASE, '--schema-only', '--no-owner', '--no-privileges']
        before = run('schema-before', dump)
        binaries = {str(f.relative_to(REPO)): sha(f) for f in binary_dir.iterdir()}
        assert source_map() == frozen, 'source changed during build'
        actual = run('native-postgres-target', [str(binary_dir / 'server.test'), '-test.v', '-test.run=^TestKnowledgePublicSourceProductionPostgres$'], {
            'CAMPUSOS_KNOWLEDGE_TEST_DSN': f'postgres://campusos:{password}@127.0.0.1:{port}/{DATABASE}?sslmode=disable',
            'CAMPUSOS_KNOWLEDGE_TARGET_ROOT': str(runtime), 'DATABASE_DSN': f'postgres://campusos:{password}@127.0.0.1:{port}/{DATABASE}?sslmode=disable',
            'CAMPUSOS_ENV': 'test', 'REDIS_ENABLED': 'false', 'NATS_URL': 'nats://127.0.0.1:1', 'HOST_API_ENABLED': 'false',
            'JWT_SECRET': secrets.token_hex(32), 'AUTH_PASSWORD_HASH_ENABLED': 'true', 'AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN': 'false',
            'AUTH_BOOTSTRAP_ADMIN_SECRET': secrets.token_hex(24), 'EMAIL_PROVIDER': 'fake', 'AI_ENABLED': 'false',
            'AUTH_MFA_ACTIVE_KEY_ID': 'owned09a', 'AUTH_MFA_ENCRYPTION_KEYS': 'owned09a:' + secrets.token_hex(32),
            'PLUGIN_DATA_DIR': str(runtime / 'data/plugin_data'), 'GIN_MODE': 'release', 'OBSERVABILITY_PROMETHEUS_ENABLED': 'false',
        })
        assert '--- PASS: TestKnowledgePublicSourceProductionPostgres ' in actual and '--- SKIP' not in actual and '--- FAIL' not in actual and 'DATA RACE' not in actual
        proof = json.loads((runtime / 'target-proof.json').read_text())
        assert proof['status'] == 'passed' and proof['actual_server_composition'] and not proof['actual_cmd_server_http'] and proof['check_count'] == 18
        run('database-all', ['bash', 'scripts/database-check.sh', 'all'], pg_env)
        after = run('schema-after', dump)

        def normalize(raw):
            return '\n'.join(line for line in raw.splitlines() if not line.startswith(('\\restrict ', '\\unrestrict ')))

        assert normalize(before) == normalize(after), 'whole schema changed'
        assert source_map() == frozen, 'source changed during target'
        assert all(sha(REPO / name) == h for name, h in binaries.items()), 'native ELF changed'
        report = {'schema': 'campusos.v12-09a-public-source-target/v1', 'status': 'passed', 'stage': 'V12-09a', 'stage_status': 'in_progress',
                  'slice': 'public Community text candidate preview and live revision recheck', 'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed_linux_native_component_pg16',
                  'proof': proof, 'postgres_version': version, 'source_sha256': frozen, 'binary_sha256': binaries,
                  'dependency': {str(PRIOR.relative_to(REPO)): PRIOR_SHA}, 'schema_before_after_sha256': hashlib.sha256(normalize(before).encode()).hexdigest(),
                  'source_and_schema_changed_by_target': False, 'shared_database_reset': False, 'commands': commands,
                  'not_delivered': ['collections and selection persistence', 'official/RichText sources', 'board subscriptions and members', 'indexing/retrieval/RAG', 'HTTP/browser ingress', 'plugin or Integration source authority'],
                  'windows': 'deferred_by_user_not_run', 'commit_or_push': False}
        (work / 'target.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        print(json.dumps({'status': 'passed', 'work': str(work.relative_to(REPO)), 'native_unit_top_level': unit.count('\n--- PASS: TestPublicCandidate'), 'target_checks': proof['check_count']}), flush=True)
    finally:
        if started:
            run('pg-server', ['docker', 'logs', container], required=False)
            run('pg-cleanup', ['docker', 'stop', container], required=False)
        (work / 'cleanup.json').write_text(json.dumps({'owned_postgres': container, 'commands': commands[-2:] if started else [], 'credentials_persisted': False}, indent=2) + '\n')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', required=True)
    arguments = parser.parse_args()
    main(Path(arguments.work).resolve())
