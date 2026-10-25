#!/usr/bin/env python3
"""One14c slice: the production AI Port consumes the independent SDK Echo provider."""
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
DATABASE = 'campusos_v12_14c_text'


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def sources():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=REPO).decode().split('\0')
    return {p: sha(REPO / p) for p in sorted(set(names)) if p and (REPO / p).is_file()
            and not p.endswith('.md') and not p.startswith('docs/项目计划书v1/项目计划v1.2/evidence/')}


def main(work):
    assert os.uname().sysname == 'Linux'
    assert work.is_relative_to(REPO / '.cache/v12-14c')
    work.mkdir(parents=True, exist_ok=False)
    runtime, native = work / 'runtime', work / 'native'
    runtime.mkdir(mode=0o700)
    native.mkdir()
    frozen = sources()
    (work / 'source-freeze.json').write_text(json.dumps(frozen, ensure_ascii=False, indent=2) + '\n')
    env = {k: os.environ[k] for k in ('PATH', 'HOME') if k in os.environ}
    env.update(GOCACHE='/tmp/campusos-go-cache', GOMAXPROCS='2', GOFLAGS='-buildvcs=false', CAMPUSOS_SKIP_DOTENV='true')
    commands, started, image_started, port, summary = [], False, False, None, None
    container = 'campusos-owned14c-' + secrets.token_hex(6)
    tag = 'localhost/campusos-owned14c-echo:' + secrets.token_hex(8)
    password = secrets.token_hex(24)

    def run(name, args, extra=None):
        begin = time.monotonic()
        with (work / (name + '.log')).open('wb') as output:
            result = subprocess.run(args, cwd=REPO, env=env | (extra or {}), stdout=output, stderr=subprocess.STDOUT)
        commands.append({'name': name, 'args': args, 'exit_code': result.returncode, 'seconds': round(time.monotonic() - begin, 3)})
        assert result.returncode == 0, name + ' failed; inspect log'
        return (work / (name + '.log')).read_text()

    def psql():
        return ['docker', 'exec', container, 'psql', '-U', 'campusos', '-d', DATABASE, '-XAt', '-v', 'ON_ERROR_STOP=1']

    workers_before = subprocess.check_output(['docker', 'ps', '-a', '--filter=label=campusos.container.worker=v5', '--format={{.Names}}'], env=env).decode().splitlines()
    try:
        for package, name in [('./internal/modules/features/ai', 'ai.test'), ('./internal/server', 'server.test')]:
            run('build-' + name, ['go', 'test', '-race', '-c', '-o', str(native / name), package])
        run('build-production', ['go', 'build', '-o', str(native / 'server'), './cmd/server'])
        unit = run('native-directed', [str(native / 'ai.test'), '-test.run=^TestPluginText', '-test.v'])
        assert len(re.findall(r'^--- PASS: TestPluginText', unit, re.M)) == 2 and not any(x in unit for x in ('--- SKIP', '--- FAIL', 'DATA RACE'))
        toolchain = run('author-toolchain', ['go', 'env', 'GOROOT']).strip()
        run('author-build', ['bash', 'scripts/build-v5-chat-echo.sh', '--out', str(work / 'author-build')], {'CAMPUSOS_AUTHOR_GO': str(Path(toolchain) / 'bin/go')})
        image_started = True
        run('author-image', ['docker', 'build', '--pull=false', '--network=none', '--tag', tag, str(work / 'author-build/image')])
        refs = json.loads(run('image-ref', ['docker', 'image', 'inspect', '--format={{json .RepoDigests}}', tag]))
        assert len(refs) == 1 and re.fullmatch(r'localhost/campusos-owned14c-echo@sha256:[0-9a-f]{64}', refs[0])
        binaries = {str(p.relative_to(REPO)): sha(p) for p in native.iterdir()}
        binaries[str((work / 'author-build/image/provider').relative_to(REPO))] = sha(work / 'author-build/image/provider')
        run('pg-start', ['docker', 'run', '--rm', '--pull=never', '-d', '--name', container, '--tmpfs', '/var/lib/postgresql/data:rw,size=512m', '-e', 'POSTGRES_USER=campusos', '-e', 'POSTGRES_DB=' + DATABASE, '-e', 'POSTGRES_PASSWORD', '-p', '127.0.0.1::5432', 'postgres:16-alpine'], {'POSTGRES_PASSWORD': password})
        started = True
        for _ in range(120):
            if subprocess.run(['docker', 'exec', container, 'pg_isready', '-U', 'campusos', '-d', DATABASE], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                break
            time.sleep(.25)
        else:
            raise AssertionError('PG startup deadline')
        port = int(run('pg-port', ['docker', 'port', container, '5432/tcp']).strip().rsplit(':', 1)[1])
        pg_env = {'PSQL_MODE': 'docker', 'POSTGRES_CONTAINER': container, 'DB_USER': 'campusos', 'DB_NAME': DATABASE, 'DB_PASSWORD': password}
        for command in ('up', 'check', 'status'):
            run('migration-' + command, ['bash', 'scripts/migrate.sh', command], pg_env)
        run('database-before', ['bash', 'scripts/database-check.sh', 'all'], pg_env)
        facts = run('schema-facts', psql() + ['-c', "SELECT count(*) FROM pg_tables WHERE schemaname='public'; SELECT count(*),max(version)::int FROM schema_migrations;"])
        assert facts.splitlines() == ['132', '35|35']
        dump = ['docker', 'exec', container, 'pg_dump', '-U', 'campusos', '-d', DATABASE, '--schema-only', '--no-owner', '--no-privileges']
        normalize = lambda s: '\n'.join(x for x in s.splitlines() if not x.startswith(('\\restrict ', '\\unrestrict ')))
        before = normalize(run('schema-before', dump))
        dsn = f'postgres://campusos:{password}@127.0.0.1:{port}/{DATABASE}?sslmode=disable'
        target_env = {'CAMPUSOS_AI_PLUGIN_DSN': dsn, 'CAMPUSOS_AI_PLUGIN_ROOT': str(runtime), 'CAMPUSOS_AI_PLUGIN_IMAGE': refs[0], 'CAMPUSOS_AI_PLUGIN_TEMPLATE': str(work / 'author-build/package-template/plugin.json'), 'DATABASE_DSN': dsn,
                      'CAMPUSOS_ENV': 'test', 'REDIS_ENABLED': 'false', 'NATS_URL': 'nats://127.0.0.1:1', 'HOST_API_ENABLED': 'false', 'JWT_SECRET': secrets.token_hex(32),
                      'AUTH_PASSWORD_HASH_ENABLED': 'true', 'AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN': 'false', 'AUTH_BOOTSTRAP_ADMIN_SECRET': secrets.token_hex(24),
                      'EMAIL_PROVIDER': 'fake', 'AI_ENABLED': 'true', 'AI_BASE_URL': '', 'AI_MODEL': '', 'AI_API_KEY': '',
                      'AUTH_MFA_ACTIVE_KEY_ID': 'owned14c', 'AUTH_MFA_ENCRYPTION_KEYS': 'owned14c:' + secrets.token_hex(32),
                      'PLUGIN_DATA_DIR': str(runtime / 'data/plugin_data'), 'GIN_MODE': 'release', 'OBSERVABILITY_PROMETHEUS_ENABLED': 'false'}
        log = run('native-target', [str(native / 'server.test'), '-test.run=^TestAIPluginTextPostgres$', '-test.v', '-test.timeout=180s'], target_env)
        assert '--- PASS: TestAIPluginTextPostgres ' in log and not any(x in log for x in ('--- SKIP', '--- FAIL', 'DATA RACE'))
        proof = json.loads((runtime / 'target-proof.json').read_text())
        assert proof['status'] == 'passed' and proof['production_AI_Port'] and proof['actual_Container'] and not proof['synthetic_consumer']
        assert before == normalize(run('schema-after', dump))
        run('database-after', ['bash', 'scripts/database-check.sh', 'all'], pg_env)
        assert sources() == frozen, 'execution sources changed during acceptance'
        assert all(sha(REPO / p) == h for p, h in binaries.items()), 'binary changed'
        summary = {'schema': 'campusos.v12-14c-plugin-text/v1', 'stage': 'V12-14c', 'stage_status': 'in_progress', 'slice_status': 'completed_current_linux_scope', 'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed_linux_production_AI_Port_PG16_actual_SDK_Container', 'proof': proof, 'source_sha256': frozen, 'binary_sha256': binaries, 'schema_sha256': hashlib.sha256(before.encode()).hexdigest(), 'public_tables': 132, 'migration': 35, 'windows': 'deferred_by_user_not_run', 'commit_or_push': False}
    finally:
        errors = []
        if started:
            try:
                run('pg-stop', ['docker', 'rm', '-f', '-v', container])
                # Verify removal against a healthy daemon; command failure alone
                # cannot be treated as evidence that the container disappeared.
                all_names = run('containers-after', ['docker', 'ps', '-a', '--format={{.Names}}']).splitlines()
                assert container not in all_names
                with socket.socket() as sock:
                    sock.settimeout(.5)
                    assert sock.connect_ex(('127.0.0.1', port)) != 0
            except Exception as error:
                errors.append(str(error))
        if image_started:
            try:
                run('image-stop', ['docker', 'image', 'rm', tag])
            except Exception as error:
                errors.append(str(error))
        try:
            after = subprocess.check_output(['docker', 'ps', '-a', '--filter=label=campusos.container.worker=v5', '--format={{.Names}}'], env=env).decode().splitlines()
            assert sorted(after) == sorted(workers_before), 'owned plugin worker leaked'
        except Exception as error:
            errors.append(str(error))
        (work / 'commands.json').write_text(json.dumps(commands, ensure_ascii=False, indent=2) + '\n')
        cleanup = {'errors': errors, 'owned_postgres_removed': started and not errors, 'owned_port_closed': started and not errors, 'owned_image_tag_removed': image_started and not errors, 'plugin_worker_set_unchanged': not errors}
        (work / 'cleanup.json').write_text(json.dumps(cleanup, indent=2) + '\n')
        if errors:
            raise AssertionError('owned cleanup failed; inspect evidence')
    if summary:
        summary['commands'], summary['cleanup'] = commands, cleanup
        summary['artifact_sha256'] = {str(p.relative_to(REPO)): sha(p) for p in sorted(work.rglob('*')) if p.is_file()}
        (work / 'target.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n')
        print(json.dumps({'status': 'passed', 'check_count': summary['proof']['check_count'], 'report': str(work / 'target.json')}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--work', type=Path, required=True)
    main(parser.parse_args().work.resolve())
