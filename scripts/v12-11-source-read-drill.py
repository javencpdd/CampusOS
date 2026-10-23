#!/usr/bin/env python3
"""V12-11 Integration current public-source read: isolated Linux PG16 target.
Runs the production modules and router over loopback with current credentials,
exact collection whitelist, original source ACL and required audit commit.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import socket
import subprocess
import time

REPO = Path(__file__).resolve().parent.parent
PRIOR = REPO / 'docs/项目计划书v1/项目计划v1.2/evidence/v12-10-exit-linux.json'
PRIOR_SHA = '162303ba78137f08a716651cdca071f1fb14604ef67dedc6a05b31d99d0053c3'
DATABASE = 'campusos_v12_11_source'


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def source_map():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=REPO).decode().split('\0')
    return {name: sha(REPO / name) for name in sorted(set(names)) if name and (REPO / name).is_file()
            and not name.endswith('.md') and not name.startswith('docs/项目计划书v1/项目计划v1.2/evidence/')}


def normalize(raw):
    return '\n'.join(line for line in raw.splitlines() if not line.startswith(('\\restrict ', '\\unrestrict ')))


def main(work):
    assert os.uname().sysname == 'Linux'
    assert sha(PRIOR) == PRIOR_SHA, 'predecessor evidence changed'
    assert work.is_relative_to(REPO / '.cache/v12-11-source-read'), 'owned work root required'
    work.mkdir(parents=True, exist_ok=False)
    work.chmod(0o700)
    runtime, native = work / 'runtime', work / 'native'
    runtime.mkdir(mode=0o700)
    native.mkdir()
    frozen = source_map()
    (work / 'source-freeze.json').write_text(json.dumps(frozen, ensure_ascii=False, indent=2) + '\n')
    env = {key: os.environ[key] for key in ('PATH', 'HOME') if key in os.environ}
    env.update(GOCACHE=str(REPO / '.cache/v12-g0/go-cache'), GOMAXPROCS='2', GOTOOLCHAIN='auto', GOFLAGS='-buildvcs=false', CAMPUSOS_SKIP_DOTENV='true', TMPDIR='/tmp')
    password = secrets.token_hex(24)
    container = 'campusos-v12-11-source-' + secrets.token_hex(6)
    commands = []
    started, port = False, None
    summary = None

    def run(name, args, extra=None, input_bytes=None):
        begin = time.monotonic()
        with (work / (name + '.log')).open('wb') as output:
            result = subprocess.run(args, cwd=REPO, env=env | (extra or {}), input=input_bytes, stdout=output, stderr=subprocess.STDOUT)
        commands.append({'name': name, 'args': args, 'cwd': str(REPO), 'exit_code': result.returncode, 'seconds': round(time.monotonic() - begin, 3)})
        assert result.returncode == 0, name + ' failed; inspect private log'
        return (work / (name + '.log')).read_text()

    def psql(db=DATABASE):
        return ['docker', 'exec', '-i', container, 'psql', '-U', 'campusos', '-d', db, '-XAt', '-v', 'ON_ERROR_STOP=1']

    def mandatory(log, name):
        assert '--- PASS: ' + name + ' ' in log and not any(value in log for value in ('--- FAIL', '--- SKIP', 'DATA RACE'))

    def schema_facts(label):
        raw = run(label, psql() + ['-c', "SELECT count(*) FROM pg_tables WHERE schemaname='public'; SELECT count(*),max(version)::int FROM schema_migrations;"])
        assert raw.splitlines() == ['124', '33|33'], 'actual table/migration inventory drift'
        return {'public_tables': 124, 'applied_migrations': 33, 'latest_migration': 33}

    try:
        for package, name in [('./internal/modules/features/knowledge', 'knowledge.test'), ('./internal/server', 'server.test')]:
            run('build-' + name, ['go', 'test', '-race', '-c', '-o', str(native / name), package])
        binaries = {str(path.relative_to(REPO)): sha(path) for path in native.iterdir()}
        unit = run('native-knowledge-unit', [str(native / 'knowledge.test'), '-test.v'])
        assert 'PASS\n' in unit and not any(value in unit for value in ('--- FAIL', '--- SKIP', 'DATA RACE'))
        unit_counts = {'top_level_pass': len(re.findall(r'^--- PASS:', unit, re.M)), 'all_pass': len(re.findall(r'--- PASS:', unit))}
        assert unit_counts['top_level_pass'] >= 40
        run('pg-start', ['docker', 'run', '--rm', '--pull=never', '-d', '--name', container, '--tmpfs', '/var/lib/postgresql/data:rw,size=512m',
                         '-e', 'POSTGRES_USER=campusos', '-e', 'POSTGRES_DB=' + DATABASE, '-e', 'POSTGRES_PASSWORD', '-p', '127.0.0.1::5432', 'postgres:16-alpine'], {'POSTGRES_PASSWORD': password})
        started = True
        for _ in range(120):
            if subprocess.run(['docker', 'exec', container, 'pg_isready', '-h', '127.0.0.1', '-U', 'campusos', '-d', DATABASE], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                break
            time.sleep(.25)
        else:
            raise AssertionError('owned PG startup deadline')
        port = int(run('pg-port', ['docker', 'port', container, '5432/tcp']).strip().rsplit(':', 1)[1])
        pg_env = {'PSQL_MODE': 'docker', 'POSTGRES_CONTAINER': container, 'DB_USER': 'campusos', 'DB_NAME': DATABASE, 'DB_PASSWORD': password}
        run('migration-up', ['bash', 'scripts/migrate.sh', 'up'], pg_env)
        run('migration-check', ['bash', 'scripts/migrate.sh', 'check'], pg_env)
        run('migration-status', ['bash', 'scripts/migrate.sh', 'status'], pg_env)
        run('database-entry', ['bash', 'scripts/database-check.sh', 'all'], pg_env)
        facts = schema_facts('actual-schema-entry')
        version = run('pg-version', psql() + ['-c', 'SHOW server_version']).strip()
        dump = ['docker', 'exec', container, 'pg_dump', '-U', 'campusos', '-d', DATABASE, '--schema-only', '--no-owner', '--no-privileges']
        before = run('schema-before', dump)
        dsn = f'postgres://campusos:{password}@127.0.0.1:{port}/{DATABASE}?sslmode=disable'
        target_env = {
            'CAMPUSOS_KNOWLEDGE_INTEGRATION_DSN': dsn, 'CAMPUSOS_KNOWLEDGE_INTEGRATION_ROOT': str(runtime), 'DATABASE_DSN': dsn,
            'CAMPUSOS_ENV': 'test', 'REDIS_ENABLED': 'false', 'NATS_URL': 'nats://127.0.0.1:1', 'HOST_API_ENABLED': 'false',
            'JWT_SECRET': secrets.token_hex(32), 'AUTH_PASSWORD_HASH_ENABLED': 'true', 'AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN': 'false',
            'AUTH_BOOTSTRAP_ADMIN_SECRET': secrets.token_hex(24), 'EMAIL_PROVIDER': 'fake', 'AI_ENABLED': 'false',
            'AUTH_MFA_ACTIVE_KEY_ID': 'owned11source', 'AUTH_MFA_ENCRYPTION_KEYS': 'owned11source:' + secrets.token_hex(32),
            'PLUGIN_DATA_DIR': str(runtime / 'data/plugin_data'), 'GIN_MODE': 'release', 'OBSERVABILITY_PROMETHEUS_ENABLED': 'false',
        }
        actual = run('native-postgres-http-target', [str(native / 'server.test'), '-test.v', '-test.run=^TestKnowledgeIntegrationSourcePostgres$', '-test.timeout=180s'], target_env)
        mandatory(actual, 'TestKnowledgeIntegrationSourcePostgres')
        proof = json.loads((runtime / 'target-proof.json').read_text())
        assert proof['status'] == 'passed' and proof['check_count'] >= 12 and proof['actual_server_composition'] and proof['actual_production_router'] and proof['actual_loopback_http'] and proof['actual_postgresql'] and not proof['fake_provider_or_manual_page']
        run('database-exit', ['bash', 'scripts/database-check.sh', 'all'], pg_env)
        assert schema_facts('actual-schema-exit') == facts
        after = run('schema-after', dump)
        assert normalize(before) == normalize(after), 'whole schema changed'
        assert source_map() == frozen, 'source changed during target'
        assert all(sha(REPO / name) == value for name, value in binaries.items()), 'native ELF changed'
        report = {
            'schema': 'campusos.v12-11-source-read/v1', 'status': 'passed', 'stage': 'V12-11', 'stage_status': 'in_progress', 'slice_status': 'completed_current_linux_scope',
            'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed_linux_native_production_modules_router_loopback_pg16',
            'proof': proof, 'native_unit': unit_counts, 'postgres_version': version,
            'source_sha256': frozen, 'binary_sha256': binaries, 'dependency': {str(PRIOR.relative_to(REPO)): PRIOR_SHA},
            'schema_before_after_sha256': hashlib.sha256(normalize(before).encode()).hexdigest(), 'commands': commands,
            'schema_tables': facts['public_tables'], 'migration_version': facts['latest_migration'], 'new_migrations': [],
            'commit_or_push': False, 'windows': 'deferred_by_user_not_run',
            'scope_limits': ['one Integration HTTP endpoint opens one exact current public-source citation in an explicitly allowed collection',
                             'no machine search or standard MCP protocol, no plugin Host/SDK, no private knowledge, AI or egress',
                             'production Server modules/router in native test ELF over loopback, not standalone cmd/server or browser acceptance',
                             'stage11 remains in_progress; no12/13/14 implementation'],
        }
        (work / 'target.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        summary = {'status': 'passed', 'work': str(work.relative_to(REPO)), 'checks': proof['check_count'], 'http_requests': proof['http_requests'], 'stage_status': 'in_progress'}

    finally:
        cleanup = {'owned_postgres': container, 'credentials_persisted': False, 'postgres_started': started}
        if started:
            cleanup['command_errors'] = []
            for name, args in [('pg-server', ['docker', 'logs', container]), ('pg-cleanup', ['docker', 'stop', container])]:
                try:
                    run(name, args)
                except Exception as error:
                    cleanup['command_errors'].append(str(error))
            # Docker stop can return before --rm and the port proxy finish.
            # Confirm an explicit missing-object response from a healthy daemon,
            # rather than treating any inspect failure as successful cleanup.
            deadline = time.monotonic() + 10
            attempts = 0
            while True:
                attempts += 1
                try:
                    inspect = subprocess.run(['docker', 'inspect', container], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=max(.05, min(1, deadline-time.monotonic())))
                    healthy = subprocess.run(['docker', 'info', '--format', '{{.ServerVersion}}'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=max(.05, min(1, deadline-time.monotonic()))).returncode == 0
                    cleanup['docker_daemon_healthy'] = healthy
                    cleanup['inspect_exit_code'] = inspect.returncode
                    cleanup['inspect_error'] = inspect.stderr.decode(errors='replace')[:512]
                    cleanup['container_removed'] = healthy and inspect.returncode == 1 and b'no such object' in inspect.stderr.lower()
                except subprocess.TimeoutExpired:
                    cleanup['docker_daemon_healthy'] = False
                    cleanup['container_removed'] = False
                with socket.socket() as probe:
                    probe.settimeout(.25)
                    cleanup['loopback_port_closed'] = probe.connect_ex(('127.0.0.1', port)) != 0 if port is not None else None
                if cleanup['container_removed'] and cleanup['loopback_port_closed'] or time.monotonic() >= deadline:
                    break
                time.sleep(.05)
            cleanup['verification_attempts'] = attempts
        cleanup['commands'] = commands[-2:] if started else []
        (work / 'commands.json').write_text(json.dumps(commands, ensure_ascii=False, indent=2) + '\n')
        (work / 'cleanup.json').write_text(json.dumps(cleanup, indent=2) + '\n')
        if started:
            assert not cleanup['command_errors'] and cleanup['container_removed'] and cleanup['loopback_port_closed'], 'owned cleanup did not complete within10s'
    print(json.dumps(summary), flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', required=True)
    main(Path(parser.parse_args().work).resolve())
