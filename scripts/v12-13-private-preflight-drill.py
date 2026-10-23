#!/usr/bin/env python3
"""V12-13 first slice: current owner metadata, protected-index refusal, owned PG16.

Builds a current race ELF and runs production modules/router/current User
sessions over real loopback HTTP. No private parser/index, UI or AI acceptance.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import time

REPO = Path(__file__).resolve().parent.parent
PRIOR = REPO / 'docs/项目计划书v1/项目计划v1.2/evidence/v12-12-exit-linux.json'
PRIOR_SHA = 'fbe31fdb5dd6f111b2dc596f481e0605d217af43640ae766bcb913b4793e553e'
DATABASE = 'campusos_v12_13_private'


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def source_map():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=REPO).decode().split('\0')
    return {name: sha(REPO / name) for name in sorted(set(names))
            if name and (REPO / name).is_file() and not name.endswith('.md')
            and not name.startswith('docs/项目计划书v1/项目计划v1.2/evidence/')}


def normalize(raw):
    return '\n'.join(line for line in raw.splitlines()
                     if not line.startswith(('\\restrict ', '\\unrestrict ')))


def main(work):
    assert os.uname().sysname == 'Linux', 'native Linux target required'
    assert sha(PRIOR) == PRIOR_SHA, 'predecessor report changed'
    assert work.is_relative_to(REPO / '.cache/v12-13-private-preflight'), 'owned work root required'
    work.mkdir(parents=True, exist_ok=False)
    work.chmod(0o700)
    runtime, native = work / 'runtime', work / 'native'
    runtime.mkdir(mode=0o700)
    native.mkdir(mode=0o700)
    frozen = source_map()
    (work / 'source-freeze.json').write_text(json.dumps(frozen, ensure_ascii=False, indent=2) + '\n')
    env = {key: os.environ[key] for key in ('PATH', 'HOME') if key in os.environ}
    env.update(GOCACHE=str(REPO / '.cache/v12-g0/go-cache'), GOMAXPROCS='2', GOTOOLCHAIN='auto',
               GOFLAGS='-buildvcs=false', CAMPUSOS_SKIP_DOTENV='true', TMPDIR='/tmp')
    password = secrets.token_hex(24)
    container = 'campusos-v12-13-private-' + secrets.token_hex(6)
    commands, started, port, report = [], False, None, None

    def run(name, args, extra=None):
        begin = time.monotonic()
        with (work / (name + '.log')).open('wb') as output:
            result = subprocess.run(args, cwd=REPO, env=env | (extra or {}),
                                    stdout=output, stderr=subprocess.STDOUT)
        commands.append({'name': name, 'args': args, 'cwd': str(REPO),
                         'exit_code': result.returncode, 'seconds': round(time.monotonic() - begin, 3)})
        assert result.returncode == 0, name + ' failed; inspect owned log'
        return (work / (name + '.log')).read_text()

    def psql():
        return ['docker', 'exec', '-i', container, 'psql', '-U', 'campusos', '-d', DATABASE,
                '-XAt', '-v', 'ON_ERROR_STOP=1']

    def schema_facts(label):
        raw = run(label, psql() + ['-c', "SELECT count(*) FROM pg_tables WHERE schemaname='public'; "
                                 "SELECT count(*),max(version)::int FROM schema_migrations;"])
        assert raw.splitlines() == ['124', '33|33'], 'actual table/migration inventory drift'
        return {'public_tables': 124, 'applied_migrations': 33, 'latest_migration': 33}

    try:
        binary = native / 'server.test'
        run('build-server-race', ['go', 'test', '-race', '-c', '-o', str(binary), './internal/server'])
        binaries = {str(binary.relative_to(REPO)): sha(binary)}
        run('pg-start', ['docker', 'run', '--rm', '--pull=never', '-d', '--name', container,
                         '--tmpfs', '/var/lib/postgresql/data:rw,size=512m',
                         '-e', 'POSTGRES_USER=campusos', '-e', 'POSTGRES_DB=' + DATABASE,
                         '-e', 'POSTGRES_PASSWORD', '-p', '127.0.0.1::5432', 'postgres:16-alpine'],
            {'POSTGRES_PASSWORD': password})
        started = True
        for _ in range(120):
            if subprocess.run(['docker', 'exec', container, 'pg_isready', '-h', '127.0.0.1',
                               '-U', 'campusos', '-d', DATABASE], stdout=subprocess.DEVNULL,
                              stderr=subprocess.DEVNULL).returncode == 0:
                break
            time.sleep(.25)
        else:
            raise AssertionError('owned PG startup deadline')
        port = int(run('pg-port', ['docker', 'port', container, '5432/tcp']).strip().rsplit(':', 1)[1])
        pg_env = {'PSQL_MODE': 'docker', 'POSTGRES_CONTAINER': container, 'DB_USER': 'campusos',
                  'DB_NAME': DATABASE, 'DB_PASSWORD': password}
        run('migration-up', ['bash', 'scripts/migrate.sh', 'up'], pg_env)
        run('migration-check', ['bash', 'scripts/migrate.sh', 'check'], pg_env)
        run('migration-status', ['bash', 'scripts/migrate.sh', 'status'], pg_env)
        run('database-entry', ['bash', 'scripts/database-check.sh', 'all'], pg_env)
        facts = schema_facts('actual-schema-entry')
        version = run('pg-version', psql() + ['-c', 'SHOW server_version']).strip()
        dump = ['docker', 'exec', container, 'pg_dump', '-U', 'campusos', '-d', DATABASE,
                '--schema-only', '--no-owner', '--no-privileges']
        before = run('schema-before', dump)
        dsn = f'postgres://campusos:{password}@127.0.0.1:{port}/{DATABASE}?sslmode=disable'
        target_env = {
            'CAMPUSOS_KNOWLEDGE_PRIVATE_DSN': dsn, 'CAMPUSOS_KNOWLEDGE_PRIVATE_ROOT': str(runtime),
            'DATABASE_DSN': dsn, 'CAMPUSOS_ENV': 'test', 'REDIS_ENABLED': 'false',
            'NATS_URL': 'nats://127.0.0.1:1', 'HOST_API_ENABLED': 'false',
            'JWT_SECRET': secrets.token_hex(32), 'AUTH_PASSWORD_HASH_ENABLED': 'true',
            'AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN': 'false', 'AUTH_BOOTSTRAP_ADMIN_SECRET': secrets.token_hex(24),
            'EMAIL_PROVIDER': 'fake', 'AI_ENABLED': 'false', 'AUTH_MFA_ACTIVE_KEY_ID': 'owned13private',
            'AUTH_MFA_ENCRYPTION_KEYS': 'owned13private:' + secrets.token_hex(32),
            'PLUGIN_DATA_DIR': str(runtime / 'data/plugin_data'), 'GIN_MODE': 'release',
            'OBSERVABILITY_PROMETHEUS_ENABLED': 'false',
        }
        actual = run('native-private-preflight-target', [str(binary), '-test.v',
                     '-test.run=^TestKnowledgePrivatePreflightPostgres$', '-test.timeout=180s'], target_env)
        assert '--- PASS: TestKnowledgePrivatePreflightPostgres ' in actual
        assert not any(marker in actual for marker in ('--- FAIL', '--- SKIP', 'DATA RACE'))
        proof = json.loads((runtime / 'target-proof.json').read_text())
        assert proof['status'] == 'passed' and proof['check_count'] >= 29
        assert all(proof[key] for key in ('actual_server_composition', 'actual_production_router',
                   'actual_loopback_http', 'actual_postgresql', 'actual_user_sessions',
                   'actual_same_id_independent_admin', 'actual_integration_credential',
                   'business_index_task_audit_outbox_files_unchanged_per_request'))
        assert not proof['mocked_api_or_identity'] and not proof['private_index_enabled']
        assert not proof['original_bytes_required'] and proof['private_projection_rows'] == 0
        run('database-exit', ['bash', 'scripts/database-check.sh', 'all'], pg_env)
        assert schema_facts('actual-schema-exit') == facts
        after = run('schema-after', dump)
        assert normalize(before) == normalize(after), 'whole schema changed'
        schema_sha = hashlib.sha256(normalize(after).encode()).hexdigest()
        assert schema_sha == '411f764627d91c5e3c14a9ccf4b63f144724d6d17d0665284ce9aebe7255cb9d', 'predecessor whole schema mismatch'
        assert source_map() == frozen, 'source changed during native target'
        assert all(sha(REPO / name) == value for name, value in binaries.items()), 'native ELF changed'
        report = {
            'schema': 'campusos.v12-13-private-preflight/v1', 'status': 'passed', 'stage': 'V12-13',
            'stage_status': 'in_progress', 'slice_status': 'completed_current_linux_scope',
            'implementation': 'passed', 'automation': 'passed',
            'target_environment': 'passed_linux_native_production_modules_router_pg16_http',
            'proof': proof, 'postgres_version': version, 'source_sha256': frozen,
            'binary_sha256': binaries, 'dependency': {str(PRIOR.relative_to(REPO)): PRIOR_SHA},
            'schema_before_after_sha256': schema_sha, 'schema_tables': facts['public_tables'],
            'migration_version': facts['latest_migration'], 'new_migrations': [],
            'commit_or_push': False, 'windows': 'deferred_by_user_not_run',
            'scope_limits': [
                'single current owned document metadata preflight; always can_index=false; no consent or selection saved',
                'no original bytes read, parser execution, private projection/task/cache/file write, AI or egress',
                'PDF/DOCX unsupported_format; known editable uploads still private_storage_unavailable',
                'V12-13 remains in_progress: private protected storage/key/backup lifecycle, selection/index/search/revocation cleanup unimplemented',
                'native test hosts production modules/router; no browser or standalone cmd/server deployment claim',
                'independent resident global backfill checkpoint excluded from business no-write proof',
            ],
        }
    finally:
        cleanup = {'owned_postgres': container, 'credentials_persisted': False,
                   'postgres_started': started, 'command_errors': []}
        if started:
            for name, args in [('pg-server', ['docker', 'logs', container]),
                               ('pg-cleanup', ['docker', 'stop', container])]:
                try:
                    run(name, args)
                except Exception as error:
                    cleanup['command_errors'].append(str(error))
            deadline, attempts = time.monotonic() + 10, 0
            while True:
                attempts += 1
                try:
                    inspect = subprocess.run(['docker', 'inspect', container], stdout=subprocess.PIPE,
                                             stderr=subprocess.PIPE, timeout=max(.05, min(1, deadline-time.monotonic())))
                    healthy = subprocess.run(['docker', 'info', '--format', '{{.ServerVersion}}'],
                                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                             timeout=max(.05, min(1, deadline-time.monotonic()))).returncode == 0
                    cleanup['docker_daemon_healthy'] = healthy
                    cleanup['inspect_exit_code'] = inspect.returncode
                    cleanup['inspect_error'] = inspect.stderr.decode(errors='replace')[:512]
                    cleanup['container_removed'] = healthy and inspect.returncode == 1 and b'no such object' in inspect.stderr.lower()
                except subprocess.TimeoutExpired:
                    cleanup['docker_daemon_healthy'], cleanup['container_removed'] = False, False
                with socket.socket() as probe:
                    probe.settimeout(.25)
                    cleanup['loopback_port_closed'] = probe.connect_ex(('127.0.0.1', port)) != 0 if port else None
                if cleanup['container_removed'] and cleanup['loopback_port_closed'] or time.monotonic() >= deadline:
                    break
                time.sleep(.05)
            cleanup['verification_attempts'] = attempts
        (work / 'commands.json').write_text(json.dumps(commands, ensure_ascii=False, indent=2) + '\n')
        (work / 'cleanup.json').write_text(json.dumps(cleanup, indent=2) + '\n')
        if started:
            assert not cleanup['command_errors'] and cleanup['container_removed'] and cleanup['loopback_port_closed'], 'owned cleanup incomplete'
    assert report is not None
    report['commands'], report['cleanup'] = commands, cleanup
    (work / 'target.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps({'status': 'passed', 'work': str(work.relative_to(REPO)),
                      'checks': report['proof']['check_count'], 'http_requests': report['proof']['http_requests'],
                      'stage_status': 'in_progress'}), flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', required=True)
    main(Path(parser.parse_args().work).resolve())
