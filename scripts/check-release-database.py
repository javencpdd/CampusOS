#!/usr/bin/env python3
"""Check the release migration chain in an exclusively owned local PG16 container."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import socket
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent
OWNED = ROOT / '.cache/release-database'
DATABASE = 'campusos_release_gate'
BASELINE = 'campusos_release_v1_baseline'
LABEL = 'campusos.release-database.owner'


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def sources():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    return {p: sha(ROOT / p) for p in sorted(set(names)) if p and (ROOT / p).is_file()
            and not p.endswith('.md') and not p.startswith('docs/项目计划书v1/项目计划v1.2/evidence/')}


def main(work):
    if sys.platform != 'linux':
        raise RuntimeError('this database gate requires native Linux and its local Docker socket')
    if work is None:
        OWNED.mkdir(parents=True, exist_ok=True)
        work = Path(tempfile.mkdtemp(prefix='acceptance-', dir=OWNED))
    else:
        work = work.resolve()
        if not work.is_relative_to(OWNED) or work == OWNED:
            raise RuntimeError('work must be a new directory below .cache/release-database')
        work.mkdir(parents=True, exist_ok=False)
    work.chmod(0o700)
    frozen = sources()
    (work / 'source-freeze.json').write_text(json.dumps(frozen, indent=2) + '\n')
    (work / 'docker-config').mkdir(mode=0o700)
    # No operator DB/DSN/Compose/Docker-context variables or dotenv are inherited.
    env = {k: os.environ[k] for k in ('PATH', 'HOME') if k in os.environ}
    env.update(CAMPUSOS_SKIP_DOTENV='true', CAMPUSOS_ENV='test',
               DOCKER_HOST='unix:///var/run/docker.sock', DOCKER_CONFIG=str(work / 'docker-config'))
    nonce = secrets.token_hex(12)
    name = 'campusos-release-db-' + nonce
    password = secrets.token_hex(24)
    commands, checks, scope = [], [], {}
    attempted, container_id, port = False, None, None
    cleanup = {'errors': [], 'container_removed': False, 'loopback_port_closed': False,
               'credentials_persisted': False}

    def run(label, args, extra=None, error=None, timeout=180):
        begin = time.monotonic()
        result = subprocess.run([str(a) for a in args], cwd=ROOT, env=env | (extra or {}),
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
        (work / (label + '.log')).write_bytes(result.stdout)
        commands.append({'name': label, 'args': [str(a) for a in args], 'exit_code': result.returncode,
                         'expected_error': error, 'seconds': round(time.monotonic() - begin, 3)})
        output = result.stdout.decode(errors='replace')
        if error is None:
            if result.returncode != 0:
                raise RuntimeError(label + ' failed; inspect its retained log')
        elif result.returncode == 0 or error.lower() not in output.lower():
            raise RuntimeError(label + ' did not reject with the expected error')
        return output

    def sql(label, statement):
        return run(label, ['docker', 'exec', name, 'psql', '-U', 'campusos', '-d', DATABASE,
                           '-XqAt', '-v', 'ON_ERROR_STOP=1', '-c', statement]).strip()

    def inspect(label):
        template = ('{"id":{{json .Id}},"owner":{{json (index .Config.Labels "' + LABEL + '")}},'
                    '"ports":{{json .NetworkSettings.Ports}},"mounts":{{json .Mounts}},'
                    '"tmpfs":{{json .HostConfig.Tmpfs}}}')
        return json.loads(run(label, ['docker', 'inspect', '--format=' + template, name]))

    def schema(label):
        raw = run(label, ['docker', 'exec', name, 'pg_dump', '-U', 'campusos', '-d', DATABASE,
                          '--schema-only', '--no-owner', '--no-privileges'])
        return '\n'.join(line for line in raw.splitlines() if not line.startswith(('\\restrict ', '\\unrestrict ')))

    def facts(label):
        # Only hashes/sequence metadata leave this synthetic database; no rows or
        # credentials are retained in evidence. The temporary table is session-local.
        return json.loads(sql(label, """
CREATE TEMP TABLE release_fingerprint(name text,digest text);
DO $$ DECLARE r record; h text; BEGIN
 FOR r IN SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename LOOP
  EXECUTE format('SELECT md5(coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text,''[]'')) FROM public.%I t',r.tablename) INTO h;
  INSERT INTO release_fingerprint VALUES(r.tablename,h);
 END LOOP;
END $$;
SELECT jsonb_build_object('tables',(SELECT jsonb_object_agg(name,digest) FROM release_fingerprint),
 'sequences',(SELECT jsonb_agg(to_jsonb(s) ORDER BY sequencename) FROM pg_sequences s WHERE schemaname='public'));
"""))

    try:
        engine = run('docker-version', ['docker', 'version', '--format={{.Server.Os}}/{{.Server.Arch}}']).strip()
        if not engine.startswith('linux/'):
            raise RuntimeError('a local Linux Docker engine is required')
        image = json.loads(run('pg-image', ['docker', 'image', 'inspect', '--format={"id":{{json .Id}},"digests":{{json .RepoDigests}}}', 'postgres:16-alpine']))
        refs = [x for x in image['digests'] if re.fullmatch(r'(?:docker.io/library/)?postgres@sha256:[0-9a-f]{64}', x)]
        if not refs:
            raise RuntimeError('cached PG16 image requires its actual RepoDigest; no image pull or guessed digest')
        attempted = True
        container_id = run('pg-start', ['docker', 'run', '-d', '--pull=never', '--name', name,
                                      '--label', LABEL + '=' + nonce,
                                      '--tmpfs', '/var/lib/postgresql/data:rw,size=512m',
                                      '-e', 'POSTGRES_USER=campusos', '-e', 'POSTGRES_DB=' + DATABASE,
                                      '-e', 'POSTGRES_PASSWORD', '-p', '127.0.0.1::5432', refs[0]],
                           {'POSTGRES_PASSWORD': password}).strip()
        if not re.fullmatch(r'[0-9a-f]{64}', container_id):
            raise RuntimeError('container creation did not return a current identity')
        end = time.monotonic() + 30
        while True:
            ready = subprocess.run(['docker', 'exec', name, 'pg_isready', '-U', 'campusos', '-d', DATABASE],
                                   env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
            if ready.returncode == 0:
                break
            if time.monotonic() > end:
                raise RuntimeError('owned PostgreSQL readiness deadline')
            time.sleep(.25)
        metadata = inspect('owned-scope')
        if metadata['id'] != container_id or metadata['owner'] != nonce:
            raise RuntimeError('owned container identity changed')
        bindings = metadata['ports']['5432/tcp']
        if len(bindings) != 1 or bindings[0]['HostIp'] != '127.0.0.1':
            raise RuntimeError('only one loopback PostgreSQL port is allowed')
        port = int(bindings[0]['HostPort'])
        if set(metadata['tmpfs'] or {}) != {'/var/lib/postgresql/data'} or any(m['Type'] != 'tmpfs' for m in metadata['mounts']):
            raise RuntimeError('owned PostgreSQL must not use host or persistent volumes')
        filesystem = run('data-filesystem', ['docker', 'exec', name, 'stat', '-f', '-c', '%T', '/var/lib/postgresql/data']).strip()
        if filesystem != 'tmpfs':
            raise RuntimeError('actual PostgreSQL data filesystem must be tmpfs')
        with socket.socket() as sock:
            sock.settimeout(1)
            if sock.connect_ex(('127.0.0.1', port)) != 0:
                raise RuntimeError('the actual owned loopback port is not reachable')
        scope = {'container': name, 'container_id': container_id, 'owner_label': nonce,
                 'database': DATABASE, 'baseline_database': BASELINE, 'port': port,
                 'image_id': image['id'], 'image_digest': refs[0], 'engine': engine,
                 'dotenv_loaded': False, 'operator_DB_variables_inherited': False,
                 'persistent_or_bind_mounts': False}
        (work / 'scope.json').write_text(json.dumps(scope, indent=2) + '\n')
        version = json.loads(sql('pg-version', "SELECT jsonb_build_object('database',current_database(),'user',current_user,'version_num',current_setting('server_version_num')::int,'version',version(),'data_directory',current_setting('data_directory'));"))
        if version['database'] != DATABASE or version['user'] != 'campusos' or version['data_directory'] != '/var/lib/postgresql/data' or not 160000 <= version['version_num'] < 170000:
            raise RuntimeError('current PostgreSQL version/target does not match the owned scope')
        checks.append('local pinned PG16, exact container ownership, tmpfs-only storage and actual loopback target')
        pg = {'PSQL_MODE': 'docker', 'POSTGRES_CONTAINER': name, 'DB_HOST': '127.0.0.1',
              'DB_PORT': str(port), 'DB_USER': 'campusos', 'DB_NAME': DATABASE,
              'DB_PASSWORD': password, 'MIGRATIONS_DIR': str(ROOT / 'migrations'),
              'CAMPUSOS_V1_DRILL_DB': BASELINE}
        for action in ('up', 'check', 'status'):
            run('migration-' + action, ['bash', 'scripts/migrate.sh', action], pg)
        registry = json.loads(sql('applied-migrations', "SELECT jsonb_agg(jsonb_build_object('version',version,'name',name,'checksum',checksum) ORDER BY version) FROM schema_migrations;"))
        files = sorted((ROOT / 'migrations').glob('*.up.sql'))
        if len(files) != 35 or len(registry) != len(files):
            raise RuntimeError('current release requires the reviewed 35-migration chain')
        for applied, path in zip(registry, files):
            if applied != {'version': path.name[:6], 'name': path.name[:-7], 'checksum': sha(path)}:
                raise RuntimeError('applied migration registry does not match exact current SQL')
        counts = sql('schema-counts', "SELECT count(*) FROM pg_tables WHERE schemaname='public';SELECT count(*) FROM pg_constraint WHERE contype='f' AND connamespace='public'::regnamespace;").splitlines()
        if counts != ['132', '143']:
            raise RuntimeError('current release table/FK counts require review')
        checks.append('all35 current migration names and full checksums match actual PG registry;132tables/143FK')
        run('database-entry', ['bash', 'scripts/database-check.sh', 'all'], pg)
        checks.append('actual database audit, current Schema contract and index/constraint hygiene pass')
        before_schema, before_facts = schema('schema-before'), facts('data-before')
        run('migration-repeat-up', ['bash', 'scripts/migrate.sh', 'up'], pg)
        if schema('schema-after-repeat') != before_schema or facts('data-after-repeat') != before_facts:
            raise RuntimeError('repeat up changed schema, existing rows, registry or sequence state')
        checks.append('repeat current up preserves full Schema, all table hashes, registry rows and sequence state')

        drift = work / 'drift-migrations'
        shutil.copytree(ROOT / 'migrations', drift, ignore=shutil.ignore_patterns('er', 'tools', '*.md'))
        with (drift / files[0].name).open('a') as stream:
            stream.write('\n-- isolated checksum fault\n')
        run('checksum-drift-denied', ['bash', 'scripts/migrate.sh', 'check'], pg | {'MIGRATIONS_DIR': str(drift)}, error='migration drift:')
        checks.append('actual copied-SQL checksum drift rejects without changing applied history')
        sql('disable-package-guard', 'ALTER TABLE plugin_package_identities DISABLE TRIGGER plugin_package_identity_write_once;')
        try:
            run('schema-guard-denied', ['bash', 'scripts/database-check.sh', 'schema'], pg,
                error='missing or disabled plugin package identity immutability guards')
        finally:
            sql('restore-package-guard', 'ALTER TABLE plugin_package_identities ENABLE TRIGGER plugin_package_identity_write_once;')
        run('schema-guard-restored', ['bash', 'scripts/database-check.sh', 'schema'], pg)
        checks.append('actual disabled v5 identity guard rejects current Schema; re-enabled guard passes')
        sql('duplicate-index', 'CREATE INDEX owned_release_duplicate_index ON users(id);')
        try:
            run('duplicate-index-denied', ['bash', 'scripts/database-check.sh', 'hygiene'], pg, error='migration hygiene failed')
        finally:
            sql('remove-duplicate-index', 'DROP INDEX owned_release_duplicate_index;')
        run('hygiene-restored', ['bash', 'scripts/database-check.sh', 'hygiene'], pg)
        checks.append('actual redundant index rejects hygiene; removal restores the contract')
        run('frozen-v1-baseline', ['make', 'v1-database-baseline-check'], pg)
        if sql('baseline-cleanup', "SELECT count(*) FROM pg_database WHERE datname='" + BASELINE + "';") != '0':
            raise RuntimeError('historical baseline drill left its owned temporary database')
        checks.append('frozen1-3 baseline checksum/guard/up-down-up passes in separate owned DB and removes it')
        run('database-exit', ['bash', 'scripts/database-check.sh', 'all'], pg)
        if schema('schema-after') != before_schema or facts('data-after') != before_facts:
            raise RuntimeError('fault probes or baseline drill changed the current database')
        checks.append('all current rows/sequences and whole Schema preserved after negative probes and historical drill')
        if sources() != frozen:
            raise RuntimeError('execution sources changed during the gate')
        checks.append('source/SQL/locks unchanged; no shared target, service or production data used')
    except BaseException as error:
        (work / 'failure.json').write_text(json.dumps({'status': 'failed', 'stage': 'database_gate',
                                                      'error_type': type(error).__name__, 'scope': scope}, indent=2) + '\n')
        raise
    finally:
        if attempted:
            try:
                present = run('containers-before-cleanup', ['docker', 'ps', '-a', '--format={{.Names}}']).splitlines()
                if name in present:
                    current = inspect('cleanup-ownership')
                    if current['owner'] != nonce or (container_id and current['id'] != container_id):
                        raise RuntimeError('cleanup refused a replaced or foreign container')
                    run('pg-remove', ['docker', 'rm', '-f', '-v', name])
                after = run('containers-after-cleanup', ['docker', 'ps', '-a', '--format={{.Names}}']).splitlines()
                cleanup['container_removed'] = name not in after
                if port is not None:
                    with socket.socket() as sock:
                        sock.settimeout(.5)
                        cleanup['loopback_port_closed'] = sock.connect_ex(('127.0.0.1', port)) != 0
                if not cleanup['container_removed'] or (port is not None and not cleanup['loopback_port_closed']):
                    raise RuntimeError('owned PostgreSQL or loopback port remains')
            except Exception as error:
                cleanup['errors'].append(str(error))
        (work / 'commands.json').write_text(json.dumps(commands, indent=2) + '\n')
        cleanup['credentials_persisted'] = any(password.encode() in p.read_bytes() for p in work.rglob('*') if p.is_file())
        (work / 'cleanup.json').write_text(json.dumps(cleanup, indent=2) + '\n')
        if cleanup['errors'] or cleanup['credentials_persisted']:
            raise RuntimeError('owned cleanup or credential custody failed')
    if not cleanup['container_removed'] or not cleanup['loopback_port_closed']:
        raise RuntimeError('a completed gate requires verified owned cleanup')
    checks.append('owned container/tmpfs/loopback port removed; credential bytes absent from retained evidence')
    report = {'schema': 'campusos.release-database/v1', 'status': 'passed',
              'implementation': 'passed', 'automation': 'passed', 'target_environment': 'local_Linux_actual_owned_PG16',
              'scope': scope, 'version': version, 'checks': checks, 'check_count': len(checks),
              'migration_version': 35, 'public_tables': 132, 'foreign_keys': 143,
              'schema_sha256': hashlib.sha256(before_schema.encode()).hexdigest(),
              'data_and_sequences_sha256': hashlib.sha256(json.dumps(before_facts, sort_keys=True).encode()).hexdigest(),
              'source_sha256': frozen, 'commands': commands, 'cleanup': cleanup,
              'shared_database_used': False, 'server_or_browser_or_full_release_check': False,
              'artifact_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file()}}
    (work / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'status': 'passed', 'checks': len(checks), 'report': str(work / 'report.json')}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', type=Path, help='new exclusive path below .cache/release-database')
    main(parser.parse_args().work)
