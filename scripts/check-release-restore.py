#!/usr/bin/env python3
"""Check official native recovery on two owned authentication epochs, offline.

Only cached local PG16, a fresh private work directory, current prebuilt normal
server/CLI and native test ELFs for synthetic fixtures are used. No inherited
database, dotenv, existing API, browser, container API or external model target.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
OWNED = ROOT / '.cache/release-restore'
PROFILE = ROOT / 'scripts/release-restore-profile.json'
# Existing fixture ELFs themselves enforce these exact database names. They
# exist only inside this invocation's newly labelled PostgreSQL container.
SEED = 'campusos_v12_08b_backup_seed'
TARGET = 'campusos_v12_08b_backup_restored'
LABEL = 'campusos.release-restore.owner'


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def sources():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    return {p: sha(ROOT / p) for p in sorted(set(names)) if p and (ROOT / p).is_file()
            and not p.endswith('.md') and not p.startswith('docs/项目计划书v1/项目计划v1.2/evidence/')}


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def main(work, backend='native', browser=False):
    owned = (ROOT / '.cache/release-browser') if browser else (OWNED if backend == 'native' else ROOT / '.cache/release-restore-docker')
    profile_path = (ROOT / 'scripts/release-browser-profile.json') if browser else (PROFILE if backend == 'native' else ROOT / 'scripts/release-restore-docker-profile.json')
    if sys.platform != 'linux':
        raise RuntimeError('native Linux and local Docker socket required; Windows remains deferred')
    if work is None:
        owned.mkdir(parents=True, exist_ok=True)
        work = Path(tempfile.mkdtemp(prefix='acceptance-', dir=owned))
    else:
        work = work.resolve()
        if not work.is_relative_to(owned) or work == owned:
            raise RuntimeError('work must be a new directory below the selected owned restore backend')
        work.mkdir(parents=True, exist_ok=False)
    work.chmod(0o700)
    frozen, profile_hash = sources(), sha(profile_path)
    profile = json.loads(profile_path.read_text())
    (work / 'source-freeze.json').write_text(json.dumps(frozen, indent=2) + '\n')
    machine = {'system': platform.platform(), 'machine': platform.machine(), 'cpu_count': os.cpu_count(),
               'cpu_affinity': sorted(os.sched_getaffinity(0)),
               'cpu_model': next(x.split(':', 1)[1].strip() for x in Path('/proc/cpuinfo').read_text().splitlines() if x.startswith('model name')),
               'mem_total_kib': int(re.search(r'MemTotal:\s+(\d+)', Path('/proc/meminfo').read_text())[1])}
    manifest = {'frozen_at_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                'profile_sha256': profile_hash, 'profile': profile, 'hardware': machine}
    (work / 'frozen-profile.json').write_text(json.dumps(manifest, indent=2) + '\n')
    manifest_hash = sha(work / 'frozen-profile.json')
    for name in ('docker-config', 'native', 'source', 'restored', 'shims'):
        (work / name).mkdir(mode=0o700)
    source, restored, native = (work / x for x in ('source', 'restored', 'native'))
    env = {k: os.environ[k] for k in ('PATH', 'HOME') if k in os.environ}
    env.update(CAMPUSOS_SKIP_DOTENV='true', CAMPUSOS_ENV='test', DOCKER_HOST='unix:///var/run/docker.sock',
               DOCKER_CONFIG=str(work / 'docker-config'), GOCACHE=os.environ.get('GOCACHE', str(ROOT / '.cache/go-build')),
               GOMAXPROCS=str(profile['gomaxprocs']))
    nonce = secrets.token_hex(12)
    name = 'campusos-release-restore-' + nonce
    password, key, login_password, jwt, mfa = [secrets.token_hex(32) for _ in range(5)]
    machine_key = 'cint1_0123456789abcdef.' + secrets.token_urlsafe(32)
    secret_values = [password, key, login_password, jwt, mfa, machine_key]
    commands, checks, http_checks, processes, epochs, seconds = [], [], [], [], [], []
    attempted, container_id, pg_port, api_port, process = False, None, None, None, None
    scope, binaries, docker = {}, {}, None
    api_ports = set()
    cleanup = {'errors': [], 'api_stopped': False, 'container_removed': False,
               'ports_closed': False, 'dotenv_removed': False, 'credentials_persisted': False}

    def write(path, value):
        (work / path).write_text(json.dumps(value, indent=2) + '\n')

    def redact(raw):
        for value in secret_values:
            raw = raw.replace(value.encode(), b'<redacted>')
        return raw

    def run(label, args, extra=None, cwd=ROOT, stdin=None, failure=False, timeout=300):
        transport = docker.rewrite(label, args, extra or {}) if docker else None
        if transport:
            tool_name, args, extra = transport
        begin = time.monotonic()
        if label == 'actual-browser':
            child = subprocess.Popen([str(a) for a in args], cwd=cwd, env=env | (extra or {}),
                                     stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, start_new_session=True)
            try:
                output, _ = child.communicate(input=stdin, timeout=timeout)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                output, _ = child.communicate()
            finally:
                try:
                    os.killpg(child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            result = subprocess.CompletedProcess(args, child.returncode, output)
        else:
            result = subprocess.run([str(a) for a in args], cwd=cwd, env=env | (extra or {}), input=stdin,
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
        (work / (label + '.log')).write_bytes(redact(result.stdout))
        commands.append({'name': label, 'args': [str(a) for a in args], 'exit_code': result.returncode,
                         'expected_failure': failure, 'seconds': round(time.monotonic() - begin, 3)})
        if transport:
            docker.finished(tool_name, result.returncode)
        if (result.returncode == 0) == failure:
            raise RuntimeError(label + ' unexpected status; inspect retained log')
        return result.stdout.decode(errors='replace').strip()

    def sql(label, db, statement):
        return run(label, ['docker', 'exec', '-i', name, 'psql', '-U', 'campusos', '-d', db, '-XqAt', '-v', 'ON_ERROR_STOP=1'], stdin=statement.encode())

    def inspect(label):
        template = ('{"id":{{json .Id}},"owner":{{json (index .Config.Labels "' + LABEL + '")}},'
                    '"ports":{{json .NetworkSettings.Ports}},"mounts":{{json .Mounts}},"tmpfs":{{json .HostConfig.Tmpfs}}}')
        return json.loads(run(label, ['docker', 'inspect', '--format=' + template, name]))

    def dsn(db):
        return f'postgres://campusos:{password}@127.0.0.1:{pg_port}/{db}?sslmode=disable'

    def pg_env(db):
        return {'PSQL_MODE': 'docker', 'POSTGRES_CONTAINER': name, 'DB_HOST': '127.0.0.1', 'DB_PORT': str(pg_port),
                'DB_USER': 'campusos', 'DB_NAME': db, 'DB_PASSWORD': password, 'MIGRATIONS_DIR': str(ROOT / 'migrations')}

    def schema(label, db):
        raw = run(label, ['docker', 'exec', name, 'pg_dump', '-U', 'campusos', '-d', db, '--schema-only', '--no-owner', '--no-privileges'])
        return '\n'.join(x for x in raw.splitlines() if not x.startswith(('\\restrict ', '\\unrestrict ')))

    def mandatory(label, elf, test, extra):
        raw = run(label, [elf, '-test.v', '-test.run=^' + test + '$', '-test.timeout=180s'], extra,
                  cwd=ROOT / 'internal' / Path(elf).stem, timeout=240)
        if '--- PASS: ' + test + ' ' not in raw or any(x in raw for x in ('--- SKIP', '--- FAIL', 'DATA RACE')):
            raise RuntimeError(label + ' missing mandatory native PASS')

    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def http(label, path, method='GET', body=None, token=None, scheme='Bearer', expected=200, record=True, raw_body=False):
        request = urllib.request.Request('http://127.0.0.1:' + str(api_port) + '/api/v1' + path,
                                        data=json.dumps(body).encode() if body is not None else None,
                                        headers={'Content-Type': 'application/json'} | ({'Authorization': scheme + ' ' + token} if token else {}), method=method)
        try:
            response = opener.open(request, timeout=8)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw, status = response.read((4 << 20) + 1), response.status
        if record:
            http_checks.append({'name': label, 'method': method, 'path': path, 'status': status, 'expected': expected})
            write('http-checks.json', http_checks)
        if status != expected or len(raw) > 4 << 20:
            raise RuntimeError(label + ' failed HTTP status/body bounds')
        if raw_body or expected not in (200, 201):
            return raw
        value = json.loads(raw)
        if value.get('code') != 0 or 'data' not in value:
            raise RuntimeError(label + ' failed success envelope')
        return value['data']

    def login(label, admin=False):
        result = http(label, '/admin/identity/login' if admin else '/auth/login', 'POST',
                      {'username': 'restore-operator', 'password': login_password} if admin else
                      {'email': 'v5-chat@example.test', 'password': login_password})
        if 'refresh_token' in result:
            raise RuntimeError('refresh body compatibility must remain disabled')
        token = result['access_token']
        secret_values.append(token)
        return token

    def app_env(db, root):
        return env | {'DATABASE_DSN': dsn(db), 'SERVER_HOST': '127.0.0.1', 'SERVER_PORT': str(api_port),
                      'REDIS_ENABLED': 'false', 'NATS_URL': 'nats://127.0.0.1:1', 'EMAIL_PROVIDER': 'fake',
                      'AI_ENABLED': 'false', 'HOST_API_ENABLED': 'false', 'AUTH_PASSWORD_HASH_ENABLED': 'true',
                      'AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN': 'false', 'AUTH_BOOTSTRAP_ADMIN_SECRET': login_password,
                      'AUTH_REFRESH_BODY_COMPAT': 'false', 'JWT_SECRET': jwt, 'AUTH_MFA_ACTIVE_KEY_ID': 'restore',
                      'AUTH_MFA_ENCRYPTION_KEYS': 'restore:' + mfa, 'PLUGIN_DATA_DIR': str(root / 'data/plugin_data'),
                      'CAMPUSOS_PLUGIN_V5_TRUST_STORE': str(root / 'data/config/v12-backup-public-trust.json'),
                      'OBSERVABILITY_PROMETHEUS_ENABLED': 'false', 'GIN_MODE': 'release', 'CAMPUSOS_RUN_MIGRATIONS': 'false'}

    def start(label, db, root, blocked=False):
        nonlocal process, api_port
        if docker and db == TARGET:
            port = docker.start(label, app_env(db, root), blocked)
            if blocked:
                return
            api_port = port
            api_ports.add(port)
            end = time.monotonic() + 40
            while True:
                try:
                    http(label + '-ready', '/health', record=False)
                    return
                except (OSError, RuntimeError, ValueError):
                    if time.monotonic() > end:
                        raise RuntimeError('Docker API readiness deadline') from None
                    time.sleep(.1)
        log = (work / (label + '-server.log')).open('wb')
        args = ['bwrap', '--die-with-parent', '--ro-bind', '/', '/', '--dev', '/dev', '--proc', '/proc', '--tmpfs', '/tmp',
                '--bind', str(root), str(root), '--ro-bind', str(native), str(native), '--chdir', str(root), '--', str(native / 'server')]
        process = subprocess.Popen(args, cwd=root, env=app_env(db, root), stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        processes.append((process, log, label))
        if blocked:
            if process.wait(timeout=20) == 0:
                raise RuntimeError('unreviewed normal server started')
            log.flush()
            if 'recovery.quarantined' not in (work / (label + '-server.log')).read_text():
                raise RuntimeError('unreviewed startup failed for an unrelated reason')
            return
        end = time.monotonic() + 40
        while True:
            if process.poll() is not None:
                raise RuntimeError(label + ' normal server exited before readiness')
            try:
                http(label + '-ready', '/health', record=False)
                break
            except (OSError, RuntimeError, ValueError):
                if time.monotonic() > end:
                    raise RuntimeError('API readiness deadline') from None
                time.sleep(.1)
        # Attest the actual child ELF and its read-only native artifact mount.
        pending, found = [process.pid], []
        while pending:
            pid = pending.pop()
            try:
                if Path(os.readlink(f'/proc/{pid}/exe')).name == 'server':
                    if sha(f'/proc/{pid}/exe') != binaries[str((native / 'server').relative_to(ROOT))]:
                        raise RuntimeError('actual running ELF changed')
                    found.append(pid)
                pending.extend(map(int, Path(f'/proc/{pid}/task/{pid}/children').read_text().split()))
            except (FileNotFoundError, ProcessLookupError):
                pass
        if len(found) != 1:
            raise RuntimeError('normal server ELF identity missing')
        try:
            fd = os.open(f'/proc/{found[0]}/root{native}/campusosctl', os.O_WRONLY)
        except OSError as error:
            if error.errno != 30:
                raise
        else:
            os.close(fd)
            raise RuntimeError('prebuilt CLI mount was writable')

    def stop_api():
        nonlocal process
        if process is not None and process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
        process = None
        if docker:
            docker.stop()

    try:
        if browser:
            for folder in ('web', 'admin', 'docs-site'):
                run(folder + '-build', ['pnpm', '--dir', folder, 'build'], timeout=600)
        for package, filename in [('./cmd/server', 'server'), ('./cmd/campusosctl', 'campusosctl')]:
            run('build-' + filename, ['go', 'build', '-o', native / filename, package],
                {'CGO_ENABLED': '0'} if backend == 'docker' else {}, timeout=600)
        for package, filename in [('./internal/plugin', 'plugin.test'), ('./internal/server', 'server.test')]:
            run('build-' + filename, ['go', 'test', '-race', '-c', '-o', native / filename, package], timeout=600)
        binaries = {str(p.relative_to(ROOT)): sha(p) for p in sorted(native.iterdir())}
        write('binary-freeze.json', binaries)
        env['CAMPUSOSCTL'] = str(native / 'campusosctl')
        if backend == 'docker':
            from release_restore_docker import DockerRecovery
            docker = DockerRecovery(ROOT, work, native, name, nonce, LABEL, run, profile)
            docker.prepare()
        engine = run('docker-version', ['docker', 'version', '--format={{.Server.Os}}/{{.Server.Arch}}'])
        if not engine.startswith('linux/'):
            raise RuntimeError('local Linux Docker required')
        image = json.loads(run('pg-image', ['docker', 'image', 'inspect', '--format={"id":{{json .Id}},"digests":{{json .RepoDigests}}}', 'postgres:16-alpine']))
        refs = [x for x in image['digests'] if re.fullmatch(r'(?:docker.io/library/)?postgres@sha256:[0-9a-f]{64}', x)]
        if not refs:
            raise RuntimeError('cached PG16 actual RepoDigest required; no image pull')
        attempted = True
        container_id = run('pg-start', ['docker', 'run', '-d', '--pull=never', '--name', name, '--label', LABEL + '=' + nonce,
                                      *(['--network', docker.network_id] if docker else []),
                                      '--tmpfs', '/var/lib/postgresql/data:rw,size=1g', '-e', 'POSTGRES_USER=campusos',
                                      '-e', 'POSTGRES_DB=' + SEED, '-e', 'POSTGRES_PASSWORD', '-p', '127.0.0.1::5432', refs[0]], {'POSTGRES_PASSWORD': password})
        if not re.fullmatch(r'[0-9a-f]{64}', container_id):
            raise RuntimeError('missing owned container identity')
        end = time.monotonic() + 30
        while subprocess.run(['docker', 'exec', name, 'pg_isready', '-h', '127.0.0.1', '-U', 'campusos', '-d', SEED], env=env,
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5).returncode != 0:
            if time.monotonic() > end:
                raise RuntimeError('PG readiness deadline')
            time.sleep(.25)
        metadata = inspect('owned-scope')
        if metadata['id'] != container_id or metadata['owner'] != nonce:
            raise RuntimeError('owned PG identity changed')
        bindings = metadata['ports']['5432/tcp']
        if len(bindings) != 1 or bindings[0]['HostIp'] != '127.0.0.1' or set(metadata['tmpfs'] or {}) != {'/var/lib/postgresql/data'} or any(m['Type'] != 'tmpfs' for m in metadata['mounts']):
            raise RuntimeError('PG must use loopback only and tmpfs only')
        pg_port = int(bindings[0]['HostPort'])
        if run('filesystem', ['docker', 'exec', name, 'stat', '-f', '-c', '%T', '/var/lib/postgresql/data']) != 'tmpfs':
            raise RuntimeError('actual data filesystem is not tmpfs')
        version = json.loads(sql('pg-version', SEED, "SELECT jsonb_build_object('database',current_database(),'user',current_user,'version_num',current_setting('server_version_num')::int,'version',version(),'data_directory',current_setting('data_directory'));"))
        if version['database'] != SEED or version['user'] != 'campusos' or version['data_directory'] != '/var/lib/postgresql/data' or not 160000 <= version['version_num'] < 170000:
            raise RuntimeError('actual PG16 target mismatch')
        scope = {'backend': backend, 'container': name, 'container_id': container_id, 'owner_label': nonce, 'seed_database': SEED,
                 'restored_database': TARGET, 'pg_port': pg_port, 'image_id': image['id'], 'image_digest': refs[0],
                 'engine': engine, 'pg_version': version, 'dotenv_loaded': False, 'operator_DB_variables_inherited': False,
                 'shared_service_used': False}
        write('scope.json', scope)
        for db in (SEED, TARGET):
            if db == TARGET:
                sql('target-create', 'postgres', 'CREATE DATABASE ' + TARGET + ';')
            run('migration-up-' + db, ['bash', 'scripts/migrate.sh', 'up'], pg_env(db))
            run('migration-check-' + db, ['bash', 'scripts/migrate.sh', 'check'], pg_env(db))
            run('schema-contract-' + db, ['bash', 'scripts/database-check.sh', 'all'], pg_env(db))
        counts = sql('schema-counts', SEED, "SELECT count(*) FROM schema_migrations; SELECT count(*) FROM pg_tables WHERE schemaname='public'; SELECT count(*) FROM pg_constraint WHERE contype='f' AND connamespace='public'::regnamespace;")
        if counts.splitlines() != ['35', '132', '143']:
            raise RuntimeError('current release requires reviewed35/132/143')
        before_schema = schema('schema-source', SEED)
        reference = 'campusos_release_restore_schema_reference'
        sql('schema-reference-create', 'postgres', 'CREATE DATABASE ' + reference + ';')
        sql('schema-reference-load', reference, before_schema)
        reference_schema = schema('schema-roundtrip-reference', reference)
        checks.append('owned pinned PG16/loopback/tmpfs; current35/132/143 and full independent Schema roundtrip reference')
        shutil.copytree(ROOT / 'migrations', source / 'migrations')
        if browser:
            names = subprocess.check_output(['git', 'ls-files', '-z', 'data/resources'], cwd=ROOT).decode().split('\0')
            for filename in names:
                if filename and (ROOT / filename).is_file():
                    target = source / filename
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(ROOT / filename, target)
        with os.fdopen(os.open(source / '.env', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
            stream.write('CAMPUSOS_SECRET_ENCRYPTION_KEYS=owned:' + key + '\nJWT_SECRET=' + jwt + '\n')
        seed_env = {'CAMPUSOS_V12_BACKUP_DSN': dsn(SEED), 'CAMPUSOS_V12_BACKUP_ROOT': str(source),
                    'CAMPUSOS_V12_BACKUP_PHASE': 'seed', 'CAMPUSOS_V12_BACKUP_KEY': key}
        mandatory('offline-fixture', native / 'plugin.test', 'TestV12BackupRestoreOffline', seed_env)
        mandatory('identity-and-thread-fixture', native / 'server.test', 'TestV12RecoverySeedAccountsAndCapacity',
                  seed_env | {'CAMPUSOS_RECOVERY_PASSWORD': login_password, 'CAMPUSOS_RECOVERY_MACHINE_KEY': machine_key})
        if browser:
            sql('browser-managed-term-fixture', SEED, "INSERT INTO academic_terms(id,year,semester,first_week_start,status,is_default) VALUES(9808080,2026,'fall','2026-08-31','open',true);")
        with socket.socket() as reservation:
            reservation.bind(('127.0.0.1', 0))
            api_port = reservation.getsockname()[1]
        api_ports.add(api_port)
        scope['api_port'] = api_port
        write('scope.json', scope)
        start('source', SEED, source)
        old = {'user': login('source-user-login'), 'admin': login('source-admin-login', True)}
        http('source-machine-works', '/integration/threads/8800001', token=machine_key, scheme='Integration')
        bob = http('source-bob-login', '/auth/login', 'POST', {'email': 'v5-chat-2801002@example.test', 'password': login_password})['access_token']
        secret_values.append(bob)
        document_text = 'OWNED_FINAL_RESTORE_PRIVATE_DOCUMENT\n' + 'x' * 4070
        document = None
        for owner, token in [('alice', old['user']), ('bob', bob)]:
            for n in range(profile['documents_per_owner']):
                created = http('create-' + owner + '-' + str(n), '/documents', 'POST',
                               {'name': f'restore-{owner}-{n}.txt', 'format': 'text', 'content': document_text}, token, expected=201)
                if document is None:
                    document = created['id']
        stop_api()
        dataset_statement = """SELECT jsonb_build_object(
 'threads',(SELECT count(*) FROM threads),
 'private_threads',(SELECT count(*) FROM threads WHERE publication_status='private'),
 'documents',(SELECT count(*) FROM personal_documents),
 'owners',(SELECT count(*) FROM users WHERE id IN (2801001,2801002)),
 'signed_packages',(SELECT count(*) FROM plugin_package_releases),
 'disabled_installations',(SELECT count(*) FROM plugin_installations WHERE state->>'desired'='disabled')+(SELECT count(*) FROM plugin_user_installations WHERE state->>'desired'='disabled'),
 'encrypted_secret_history_rows',(SELECT count(*) FROM plugin_secret_values WHERE plugin_id=8808001));"""
        dataset = json.loads(sql('dataset-before-backup', SEED, dataset_statement))
        if browser:
            dataset['academic_terms'] = int(sql('browser-term-count', SEED, 'SELECT count(*) FROM academic_terms;'))
        expected_dataset = {k: profile[k] for k in dataset if k != 'documents'}
        expected_dataset['documents'] = profile['documents_per_owner'] * profile['owners']
        fixture = json.loads((source / 'data/config/v12-backup-fixture.json').read_text())
        if dataset != expected_dataset or len(fixture['objects']) != profile['original_objects']:
            raise RuntimeError('actual dataset differs from frozen profile')
        scope['dataset'] = dataset
        write('scope.json', scope)
        checks.append('actual source User/Admin/Integration credentials and4private documents from2owners before backup')
        archive = Path(run('backup', ['bash', ROOT / 'scripts/backup.sh', work / 'archives'], pg_env(SEED), cwd=source))
        if archive.stat().st_mode & 0o777 != 0o600:
            raise RuntimeError('backup is not private')
        run('verify', ['bash', 'scripts/restore.sh', 'verify', archive])
        run('extract', ['bash', 'scripts/restore.sh', 'extract', archive, work / 'extracted'])
        expected_files = {str(p.relative_to(source)): sha(p) for p in source.rglob('*') if p.is_file() and p.name != '.env'}
        extracted_files = {str(p.relative_to(work / 'extracted/files')): sha(p) for p in (work / 'extracted/files').rglob('*') if p.is_file()}
        if extracted_files != expected_files or (work / 'extracted/files/.env').exists():
            raise RuntimeError('backup files differ or include root dotenv')
        for p in (work / 'extracted').rglob('*'):
            if p.is_file() and p.name != 'files.tar.gz' and any(v.encode() in p.read_bytes() for v in secret_values):
                raise RuntimeError('uncompressed backup contains deployment credentials')
        corrupt = work / 'corrupt.tar.gz'
        corrupt.write_bytes(archive.read_bytes() + b'UNEXPECTED_ARCHIVE_TAIL')
        run('corrupt-archive-denied', ['bash', 'scripts/restore.sh', 'verify', corrupt], failure=True)
        checks.append('official v3 backup/private0600/strict verify/extract/exact files/no deployment keys; corrupt tail rejected')
        sql('post-backup-revocation', SEED, "UPDATE users SET auth_version=auth_version+1; UPDATE integration_credentials SET status='revoked';")
        shim = work / 'shims/psql'
        shim.write_text('''#!/usr/bin/env python3
import os,pathlib,subprocess,sys
args=sys.argv[1:];data=None
if '-f' in args:
 i=args.index('-f');data=pathlib.Path(args[i+1]).read_bytes();del args[i:i+2]
if os.environ.get('PGDATABASE')!='campusos_v12_08b_backup_restored':raise SystemExit('owned restored database required')
raise SystemExit(subprocess.run(['docker','exec','-i',os.environ['POSTGRES_CONTAINER'],'psql','-U','campusos','-d',os.environ['PGDATABASE']]+args,input=data).returncode)
''')
        shim.chmod(0o700)
        cli_env = pg_env(TARGET) | {'DATABASE_DSN': dsn(TARGET), 'PATH': str(work / 'shims') + ':' + env['PATH'],
                                  'CAMPUSOS_RECOVERY_DATA_ROOT': str(restored / 'data')}
        apply_args = ['bash', ROOT / 'scripts/restore.sh', 'apply', archive, '--confirm']
        for phase in ('first', 'second'):
            started = time.monotonic()
            applied = json.loads(run(phase + '-apply', apply_args, cli_env))
            if applied['phase'] != 'quarantined' or not applied['egress_paused'] or applied['epoch'] in epochs:
                raise RuntimeError('restore must allocate a fresh quarantined authentication epoch')
            epochs.append(applied['epoch'])
            applied_files = {str(f.relative_to(restored / 'data')): sha(f) for f in (restored / 'data').rglob('*')
                             if f.is_file() and f.name != '.campusos-recovery.json'}
            backup_data = {n.removeprefix('data/'): h for n, h in expected_files.items() if n.startswith('data/')}
            # Entry-point logs/dist folders are operational output, not part of
            # the restored business set; apply replaces every backed-up tree.
            applied_business = {n: h for n, h in applied_files.items() if not n.startswith(('logs/', 'dist/'))}
            if applied_business != backup_data:
                raise RuntimeError('actual applied business files differ from verified archive')
            write(phase + '-applied-files.json', {'status': 'passed', 'files': applied_business})
            mandatory(phase + '-restored-facts', native / 'plugin.test', 'TestV12RecoveryRestoredSecretsPackagesAndObjects',
                      {'CAMPUSOS_V12_BACKUP_DSN': dsn(TARGET), 'CAMPUSOS_RECOVERY_DATA_ROOT': str(restored / 'data'), 'CAMPUSOS_V12_BACKUP_KEY': key})
            reconcile = json.loads(run(phase + '-object-reconcile', [native / 'campusosctl', 'storage', 'reconcile', '--root', restored / 'data/personal-space', '--dry-run'], cli_env))
            if not reconcile['dry_run'] or reconcile.get('differences_truncated', False) or reconcile['report']['differences'] not in (None, []) or any(reconcile['report']['counts'].values()):
                raise RuntimeError('restored storage reconciliation differs')
            facts = json.loads(sql(phase + '-quarantine-facts', TARGET, """SELECT jsonb_build_object(
 'live_user_sessions',(SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
 'live_admin_sessions',(SELECT count(*) FROM admin_sessions WHERE revoked_at IS NULL),
 'machine_active',(SELECT count(*) FROM integration_credentials WHERE status='active'),
 'approved_grants',(SELECT count(*) FROM plugin_v5_admin_grants WHERE (state->>'approved')::boolean),
 'accepted_consents',(SELECT count(*) FROM plugin_v5_user_consents WHERE (state->>'accepted')::boolean),
 'enabled_plugins',(SELECT count(*) FROM plugin_installations WHERE state->>'desired'='enabled')+(SELECT count(*) FROM plugin_user_installations WHERE state->>'desired'='enabled'),
 'active_plugin_generations',(SELECT count(*) FROM plugin_installations WHERE state->>'active_generation'<>'0')+(SELECT count(*) FROM plugin_user_installations WHERE state->>'active_generation'<>'0'),
 'enabled_webhooks',(SELECT count(*) FROM webhook_endpoints WHERE enabled));"""))
            if any(facts.values()):
                raise RuntimeError('old authority revived after official restore')
            start(phase + '-quarantine', TARGET, restored, blocked=True)
            stop_api()
            snapshot = json.loads(run(phase + '-inventory-before', [native / 'campusosctl', 'recovery', 'status'], cli_env))
            # A real authority-version change makes this exact previously read
            # inventory stale. No policy or permission grant is weakened.
            sql(phase + '-inventory-drift', TARGET, "UPDATE admin_accounts SET version=version+1 WHERE id='adm.restore.operator';")
            current = json.loads(run(phase + '-inventory-current', [native / 'campusosctl', 'recovery', 'status'], cli_env))
            if snapshot['inventory_sha256'] == current['inventory_sha256']:
                raise RuntimeError('inventory drift probe did not change current authority')
            review_args = [native / 'campusosctl', 'recovery', 'review', '--data-root', restored / 'data', '--epoch', applied['epoch'],
                           '--inventory-sha256', snapshot['inventory_sha256'], '--operator', 'owned-final',
                           '--reason', 'review exact owned inventory; retain plugin and egress quarantine', '--confirm-offline']
            run(phase + '-stale-review-denied', review_args, cli_env, failure=True)
            review_args[review_args.index('--inventory-sha256') + 1] = current['inventory_sha256']
            reviewed = json.loads(run(phase + '-exact-review', review_args, cli_env))
            if reviewed['phase'] != 'reviewed' or not reviewed['egress_paused']:
                raise RuntimeError('exact review must preserve outbound quarantine')
            start(phase + '-reviewed', TARGET, restored)
            elapsed = round(time.monotonic() - started, 3)
            seconds.append(elapsed)
            if elapsed > profile['max_restore_to_reviewed_health_seconds']:
                raise RuntimeError('frozen automatic restore/review/health deadline exceeded')
            for domain, token in old.items():
                http(phase + '-old-' + domain + '-denied', '/admin/identity/me' if domain == 'admin' else '/documents', token=token, expected=401)
            http(phase + '-machine-denied', '/integration/threads/8800001', token=machine_key, scheme='Integration', expected=401)
            current_user, current_admin = login(phase + '-user-login'), login(phase + '-admin-login', True)
            me = http(phase + '-admin-current', '/admin/identity/me', token=current_admin)
            if me['domain'] != 'admin':
                raise RuntimeError('current independent Admin domain lost')
            documents = http(phase + '-documents', '/documents?status=active', token=current_user)
            if len(documents['items']) != profile['documents_per_owner']:
                raise RuntimeError('restored private document ownership differs')
            body = http(phase + '-own-download', '/documents/' + document + '/download', token=current_user, raw_body=True)
            if body.decode() != document_text:
                raise RuntimeError('restored document bytes differ')
            other = http(phase + '-bob-login', '/auth/login', 'POST', {'email': 'v5-chat-2801002@example.test', 'password': login_password})['access_token']
            secret_values.append(other)
            denied = http(phase + '-cross-owner-denied', '/documents/' + document + '/download', token=other, expected=404)
            if document_text.encode() in denied:
                raise RuntimeError('private bytes crossed owner')
            http(phase + '-private-thread-denied', '/threads/8810000', expected=404)
            inventory = http(phase + '-plugin-inventory', '/admin/plugin-v5', token=current_admin)
            if 'backup-offline' not in json.dumps(inventory):
                raise RuntimeError('restored signed package inventory missing')
            if schema(phase + '-schema', TARGET) != reference_schema:
                raise RuntimeError('full restored Schema differs from independent roundtrip reference')
            # Real active process holds the normal shared maintenance lock.
            if int(sql(phase + '-active-lock', TARGET, "SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND mode='ShareLock' AND granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database());")) < 1:
                raise RuntimeError('active API did not hold maintenance startup lock')
            marker = restored / 'data/.campusos-recovery.json'
            marker_hash = sha(marker)
            run(phase + '-online-apply-denied', apply_args, cli_env, failure=True)
            if sha(marker) != marker_hash:
                raise RuntimeError('online denied apply changed recovery marker')
            http(phase + '-online-denial-keeps-session', '/documents', token=current_user)
            stop_api()
            if phase == 'first':
                old['user'] = current_user
                old['admin'] = current_admin
            run(phase + '-migration-check', ['bash', 'scripts/migrate.sh', 'check'], pg_env(TARGET))
            run(phase + '-database-check', ['bash', 'scripts/database-check.sh', 'all'], pg_env(TARGET))
            restored_dataset = json.loads(sql(phase + '-dataset', TARGET, dataset_statement))
            if browser:
                restored_dataset['academic_terms'] = int(sql(phase + '-term-count', TARGET, 'SELECT count(*) FROM academic_terms;'))
            if restored_dataset != dataset:
                raise RuntimeError('restored dataset differs from frozen source')
            checks.append(phase + ' actual official apply/fresh epoch/exact Secret and Object bytes/reconcile/current inventory/blocked startup/stale review/current HTTP/online apply refusal/full Schema')
        if browser:
            start('browser-reviewed', TARGET, restored)
            private = work / 'private-browser-input.json'
            with os.fdopen(os.open(private, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
                json.dump({'backend': 'http://127.0.0.1:' + str(api_port), 'password': login_password,
                           'chrome': shutil.which('google-chrome') or shutil.which('chromium'), 'output': str(work / 'browser')}, stream)
            try:
                run('actual-browser', ['node', 'scripts/release-browser.mjs', private], timeout=900)
            finally:
                private.unlink(missing_ok=True)
            stop_api()
            checks.append('actual built Web/Admin/Docs/business/style-packs/responsive on owned current-identity API')
        if sources() != frozen or sha(profile_path) != profile_hash or sha(work / 'frozen-profile.json') != manifest_hash or any(sha(ROOT / p) != h for p, h in binaries.items()):
            raise RuntimeError('sources/profile/hardware manifest/prebuilt ELFs changed during gate')
        checks.append('sources/profile/hardware and4prebuilt ELFs unchanged; no shared target or new product Schema')
    except BaseException as error:
        write('failure.json', {'status': 'failed', 'error_type': type(error).__name__, 'scope': scope})
        raise
    finally:
        try:
            stop_api()
        except Exception as error:
            cleanup['errors'].append(str(error))
        for p, log, label in processes:
            log.close()
            path = work / (label + '-server.log')
            path.write_bytes(redact(path.read_bytes()))
        cleanup['api_stopped'] = all(p.poll() is not None for p, _, _ in processes)
        (source / '.env').unlink(missing_ok=True)
        (work / 'private-browser-input.json').unlink(missing_ok=True)
        if browser:
            for path in (work / 'browser').rglob('*'):
                if path.is_file() and path.suffix in ('.log', '.json', '.txt'):
                    path.write_bytes(redact(path.read_bytes()))
        cleanup['dotenv_removed'] = not (source / '.env').exists()
        if not attempted:
            cleanup['container_removed'] = True
            cleanup['ports_closed'] = True
        if attempted:
            try:
                present = run('containers-before-cleanup', ['docker', 'ps', '-a', '--format={{.Names}}']).splitlines()
                if name in present:
                    current = inspect('cleanup-ownership')
                    if current['owner'] != nonce or (container_id and current['id'] != container_id):
                        raise RuntimeError('cleanup refused foreign/replaced container')
                    run('pg-remove', ['docker', 'rm', '-f', '-v', name])
                after = run('containers-after-cleanup', ['docker', 'ps', '-a', '--format={{.Names}}']).splitlines()
                cleanup['container_removed'] = name not in after
                closed = []
                for port in ({pg_port} | api_ports):
                    if port is not None:
                        with socket.socket() as sock:
                            sock.settimeout(.5)
                            closed.append(sock.connect_ex(('127.0.0.1', port)) != 0)
                cleanup['ports_closed'] = bool(closed) and all(closed)
                if not cleanup['container_removed'] or not cleanup['ports_closed']:
                    raise RuntimeError('owned resources remain')
            except Exception as error:
                cleanup['errors'].append(str(error))
        if docker:
            try:
                docker.cleanup()
                cleanup['docker'] = docker.report
            except Exception as error:
                cleanup['errors'].append(str(error))
        write('commands.json', commands)
        cleanup['credentials_persisted'] = any(v.encode() in p.read_bytes() for p in work.rglob('*') if p.is_file() for v in secret_values)
        write('cleanup.json', cleanup)
        if cleanup['errors'] or cleanup['credentials_persisted']:
            raise RuntimeError('cleanup/credential custody failed')
    if not all(cleanup[k] for k in ('api_stopped', 'container_removed', 'ports_closed', 'dotenv_removed')):
        raise RuntimeError('success requires verified cleanup')
    checks.append('owned APIs/container/tmpfs/ports and private dotenv removed; credential bytes absent from retained evidence')
    write('report.json', {'schema': 'campusos.release-restore/v1', 'status': 'passed', 'implementation': 'passed',
                         'automation': 'passed', 'target_environment': 'local_Linux_normal_' + backend + '_API_official_restore_owned_PG16',
                         'scope': scope, 'profile': manifest, 'source_sha256': frozen, 'binary_sha256': binaries,
                         'checks': checks, 'http_checks': http_checks, 'commands': commands, 'cleanup': cleanup,
                         'epochs': epochs, 'restore_review_health_seconds': seconds, 'archive_sha256': sha(archive),
                         'backup_file_count': len(expected_files), 'backup_file_sha256': expected_files,
                         'applied_business_file_count': len(applied_business), 'backend': backend,
                         'schema_roundtrip_reference_sha256': hashlib.sha256(reference_schema.encode()).hexdigest(),
                         'docker': docker.report if docker else None,
                         'browser': json.loads((work / 'browser/browser.json').read_text()) if browser else None,
                         'AI_private_index_capacity_or_full_release': False,
                         'artifact_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file()}})
    print(json.dumps({'status': 'passed', 'checks': len(checks), 'report': str(work / 'report.json')}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', type=Path, help='new exclusive directory below .cache/release-restore')
    parser.add_argument('--backend', choices=('native', 'docker'), default='native')
    parser.add_argument('--browser', action='store_true')
    args = parser.parse_args()
    main(args.work, args.backend, args.browser)
