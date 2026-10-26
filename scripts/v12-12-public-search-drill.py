#!/usr/bin/env python3
"""V12-12 built Web public search: real Chrome, production modules/router, owned PG16.
Builds both SPAs and runs the same frozen native race ELF public HTTP regression.
Current Linux stage exit; Windows remains explicitly user-deferred.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import socket
import shutil
import subprocess
import time

REPO = Path(__file__).resolve().parent.parent
PRIOR = REPO / 'docs/项目计划书v1/项目计划v1.2/evidence/v12-11-exit-linux.json'
PRIOR_SHA = '7a4af8f21efabd9ea35f0c1b1c60ddc913646f136862079b53993d060131704f'
DATABASE = 'campusos_v12_12_web'


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
    assert work.is_relative_to(REPO / '.cache/v12-12-public-search'), 'owned work root required'
    work.mkdir(parents=True, exist_ok=False)
    work.chmod(0o700)
    runtime, native = work / 'runtime', work / 'native'
    runtime.mkdir(mode=0o700)
    native.mkdir()
    frozen = source_map()
    (work / 'source-freeze.json').write_text(json.dumps(frozen, ensure_ascii=False, indent=2) + '\n')
    env = {key: os.environ[key] for key in ('PATH', 'HOME') if key in os.environ}
    env.update(GOCACHE=str(REPO / '.cache/v12-g0/go-cache'), GOMAXPROCS='2', GOTOOLCHAIN='auto', GOFLAGS='-buildvcs=false', CAMPUSOS_SKIP_DOTENV='true', TMPDIR='/tmp')
    (work/'docker-config').mkdir(mode=0o700)
    env.update(DOCKER_HOST='unix:///var/run/docker.sock',DOCKER_CONFIG=str(work/'docker-config'))
    password = secrets.token_hex(24)
    container = 'campusos-v12-12-web-' + secrets.token_hex(6)
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
        assert raw.splitlines() == ['132','35|35'], 'actual table/migration inventory drift'
        return {'public_tables': 132, 'applied_migrations': 35, 'latest_migration': 35}

    try:
        for package, name in [('./internal/modules/features/knowledge', 'knowledge.test'), ('./internal/server', 'server.test')]:
            run('build-' + name, ['go', 'test', '-race', '-c', '-o', str(native / name), package])
        binaries = {str(path.relative_to(REPO)): sha(path) for path in native.iterdir()}
        run('web-build', ['pnpm', '--dir', 'web', 'build'])
        web_dist = work / 'web-dist'
        shutil.copytree(REPO / 'web/dist', web_dist)
        run('admin-build', ['pnpm', '--dir', 'admin', 'build'])
        admin_dist = work / 'admin-dist'
        shutil.copytree(REPO / 'admin/dist', admin_dist)
        bundles = {str(path.relative_to(REPO)): sha(path) for path in sorted(list(web_dist.rglob('*')) + list(admin_dist.rglob('*'))) if path.is_file()}
        unit = run('native-knowledge-unit', [str(native / 'knowledge.test'), '-test.v', '-test.skip=^TestPrivateIndexPostgres$'])
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
            'CAMPUSOS_KNOWLEDGE_WEB_DSN': dsn, 'CAMPUSOS_KNOWLEDGE_WEB_ROOT': str(runtime), 'CAMPUSOS_KNOWLEDGE_WEB_DIST': str(web_dist), 'CAMPUSOS_KNOWLEDGE_ADMIN_DIST': str(admin_dist), 'DATABASE_DSN': dsn,
            'CAMPUSOS_ENV': 'test', 'REDIS_ENABLED': 'false', 'NATS_URL': 'nats://127.0.0.1:1', 'HOST_API_ENABLED': 'false',
            'JWT_SECRET': secrets.token_hex(32), 'AUTH_PASSWORD_HASH_ENABLED': 'true', 'AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN': 'false',
            'AUTH_BOOTSTRAP_ADMIN_SECRET': secrets.token_hex(24), 'EMAIL_PROVIDER': 'fake', 'AI_ENABLED': 'false',
            'AUTH_MFA_ACTIVE_KEY_ID': 'owned12web', 'AUTH_MFA_ENCRYPTION_KEYS': 'owned12web:' + secrets.token_hex(32),
            'PLUGIN_DATA_DIR': str(runtime / 'data/plugin_data'), 'GIN_MODE': 'release', 'OBSERVABILITY_PROMETHEUS_ENABLED': 'false',
        }
        actual = run('native-public-search-browser', [str(native / 'server.test'), '-test.v', '-test.run=^TestKnowledgePublicSearchActualBrowser$', '-test.timeout=360s'], target_env)
        mandatory(actual, 'TestKnowledgePublicSearchActualBrowser')
        assert 'actual PostgreSQL diagnostic collection-job-member lock order passed' in actual
        assert 'actual normal resident index page/state-delay metrics passed' in actual
        proof = json.loads((runtime / 'browser-result.json').read_text())
        assert proof['status'] == 'passed' and proof['check_count'] >= 22 and proof['admin_check_count'] >= 9 and proof['actual_built_admin']
        assert all(proof[key] for key in ('actual_built_web', 'actual_server_composition', 'actual_production_router', 'actual_postgresql'))
        assert not proof['mocked_api_data'] and proof['public_identity_header_count'] == 0 and proof['knowledge_http_requests'] >= 15
        assert all((runtime / name).is_file() for name in ('desktop.png', 'mobile.png', 'admin-desktop.png', 'admin-mobile.png'))
        regression_db = 'campusos_v12_10_http'
        run('http-regression-db', psql('postgres') + ['-c', 'CREATE DATABASE ' + regression_db])
        run('http-regression-migrations', ['bash', 'scripts/migrate.sh', 'up'], pg_env | {'DB_NAME': regression_db})
        regression_root = REPO / '.cache/v12-10-http' / ('web-regression-' + container.rsplit('-', 1)[1])
        regression_root.mkdir(mode=0o700, exist_ok=False)
        regression_dsn = f'postgres://campusos:{password}@127.0.0.1:{port}/{regression_db}?sslmode=disable'
        regression_env = target_env | {
            'CAMPUSOS_KNOWLEDGE_HTTP_DSN': regression_dsn, 'CAMPUSOS_KNOWLEDGE_HTTP_ROOT': str(regression_root),
            'DATABASE_DSN': regression_dsn, 'PLUGIN_DATA_DIR': str(regression_root / 'data/plugin_data'),
        }
        regression = run('native-public-http-regression', [str(native / 'server.test'), '-test.v', '-test.run=^TestKnowledgeRetrievalPublicHTTPPostgres$', '-test.timeout=180s'], regression_env)
        mandatory(regression, 'TestKnowledgeRetrievalPublicHTTPPostgres')
        regression_proof = json.loads((regression_root / 'target-proof.json').read_text())
        assert regression_proof['status'] == 'passed' and regression_proof['check_count'] == 10
        (work / 'http-regression-proof.json').write_text(json.dumps(regression_proof, ensure_ascii=False, indent=2) + '\n')
        run('database-exit', ['bash', 'scripts/database-check.sh', 'all'], pg_env)
        assert schema_facts('actual-schema-exit') == facts
        after = run('schema-after', dump)
        assert normalize(before) == normalize(after), 'whole schema changed'
        # Full-DDL reference for current35, including private-index34 and AI35.
        # The original33 predecessor remains immutable; both current DDL checks
        # and the registered migration checksums must still match exactly.
        assert hashlib.sha256(normalize(after).encode()).hexdigest() == 'b7d8e975c1ad9b8ad7d613ca21f57fd2330513a5d5de4402645c2a09c34d25c1', 'current35 whole-schema reference mismatch'
        assert source_map() == frozen, 'source changed during target'
        assert all(sha(REPO / name) == value for name, value in binaries.items()), 'native ELF changed'
        assert all(sha(REPO / name) == value for name, value in bundles.items()), 'built asset changed'
        report = {
            'schema': 'campusos.v12-12-exit/v1', 'status': 'passed', 'stage': 'V12-12', 'stage_status': 'completed_current_linux_scope', 'slice_status': 'completed_current_linux_scope',
            'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed_linux_native_production_modules_router_pg16_chrome_built_web',
            'proof': proof, 'diagnostic_lock_order': 'passed_actual_pg_job_barrier_and_member_nowait_probe', 'index_metrics': 'passed_actual_normal_meter_page_counter_duration_and_state_schedule_lag', 'public_http_regression': regression_proof, 'native_unit': unit_counts, 'postgres_version': version,
            'source_sha256': frozen, 'binary_sha256': binaries, 'built_web_sha256': bundles, 'dependency': {str(PRIOR.relative_to(REPO)): PRIOR_SHA},
            'schema_before_after_sha256': hashlib.sha256(normalize(before).encode()).hexdigest(), 'commands': commands,
            'schema_tables': facts['public_tables'], 'migration_version': facts['latest_migration'], 'new_migrations': [],
            'commit_or_push': False, 'windows': 'deferred_by_user_not_run',
            'scope_limits': ['V12-12 anonymous Web search/current citation and independent Admin collection/official/selection/index diagnostics/CAS retry/metrics, desktop and mobile viewport',
                             'actual resident indexer, normal server modules/production router, owned PostgreSQL and built Web assets; no fake API data',
                             'native test hosts production router; standalone cmd/server process/deployment not accepted by this slice',
                             'current Linux stage12 complete; Windows explicitly deferred, no cross-platform or whole-version Final claim',
                             'no private index, external Provider, AI, stage13/14 work; Windows deferred by user'],
        }
        (work / 'target.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        summary = {'status': 'passed', 'work': str(work.relative_to(REPO)), 'checks': proof['check_count'], 'http_requests': proof['knowledge_http_requests'], 'stage_status': 'completed_current_linux_scope'}

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
        (work / 'commands.json').write_text(json.dumps(commands, ensure_ascii=False, indent=2) + '\n')
        cleanup['commands'] = commands[-2:] if started else []
        (work / 'cleanup.json').write_text(json.dumps(cleanup, indent=2) + '\n')
        if started:
            assert not cleanup['command_errors'] and cleanup['container_removed'] and cleanup['loopback_port_closed'], 'owned cleanup did not complete within10s'
    report['cleanup'] = cleanup
    report['commands'] = commands
    (work / 'target.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps(summary), flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', required=True)
    main(Path(parser.parse_args().work).resolve())
