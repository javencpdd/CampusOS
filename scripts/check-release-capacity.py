#!/usr/bin/env python3
"""Run bounded current-identity capacity reads in an exclusively owned Linux target.

No dotenv, operator DSN, shared container, remote Docker, or credential arguments.
The control and candidate are fresh processes of the SAME normal server ELF;
this checks repeatability, not improvement over a historical release or an SLA.
"""
import argparse
import base64
import hashlib
import hmac
import json
import os
from pathlib import Path
import platform
import re
import secrets
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
OWNED = ROOT / '.cache/release-capacity'
PROFILE = ROOT / 'scripts/release-capacity-profile.json'
DATABASE = 'campusos_release_capacity'
LABEL = 'campusos.release-capacity.owner'


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


def main(work):
    if sys.platform != 'linux':
        raise RuntimeError('native Linux and local Docker socket are required')
    if work is None:
        OWNED.mkdir(parents=True, exist_ok=True)
        work = Path(tempfile.mkdtemp(prefix='acceptance-', dir=OWNED))
    else:
        work = work.resolve()
        if not work.is_relative_to(OWNED) or work == OWNED:
            raise RuntimeError('work must be a new directory below .cache/release-capacity')
        work.mkdir(parents=True, exist_ok=False)
    work.chmod(0o700)
    frozen, profile_hash = sources(), sha(PROFILE)
    profile = json.loads(PROFILE.read_text())
    (work / 'source-freeze.json').write_text(json.dumps(frozen, indent=2) + '\n')
    (work / 'budget.json').write_text(json.dumps(profile['budget'], indent=2) + '\n')
    machine = {'system': platform.platform(), 'machine': platform.machine(), 'cpu_count': os.cpu_count(),
               'cpu_affinity': sorted(os.sched_getaffinity(0)),
               'cpu_model': next(x.split(':', 1)[1].strip() for x in Path('/proc/cpuinfo').read_text().splitlines() if x.startswith('model name')),
               'mem_total_kib': int(re.search(r'MemTotal:\s+(\d+)', Path('/proc/meminfo').read_text())[1])}
    # These immutable inputs are written before either process is measured.
    manifest = {'frozen_at_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                'profile_sha256': profile_hash, 'profile': profile, 'hardware': machine}
    (work / 'frozen-profile.json').write_text(json.dumps(manifest, indent=2) + '\n')
    manifest_hash = sha(work / 'frozen-profile.json')
    (work / 'docker-config').mkdir(mode=0o700)
    runtime = work / 'runtime'
    runtime.mkdir(mode=0o700)
    env = {k: os.environ[k] for k in ('PATH', 'HOME') if k in os.environ}
    env.update(CAMPUSOS_SKIP_DOTENV='true', CAMPUSOS_ENV='test',
               DOCKER_HOST='unix:///var/run/docker.sock', DOCKER_CONFIG=str(work / 'docker-config'),
               GOCACHE=os.environ.get('GOCACHE', str(ROOT / '.cache/go-build')))
    nonce = secrets.token_hex(12)
    name = 'campusos-release-capacity-' + nonce
    password, admin_password = secrets.token_hex(24), secrets.token_hex(24) + 'A!'
    jwt, mfa_key, bootstrap = secrets.token_hex(32), secrets.token_hex(32), secrets.token_hex(32)
    secret_values = [password, admin_password, jwt, mfa_key, bootstrap]
    commands, checks, http_checks, rss, processes = [], [], [], {}, []
    scope, binary, captures = {}, {}, {}
    attempted, container_id, pg_port, api_port, process = False, None, None, None, None
    authorization = work / 'authorization'
    cleanup = {'errors': [], 'api_stopped': False, 'container_removed': False,
               'ports_closed': False, 'credential_file_removed': False, 'credentials_persisted': False}

    def redact(raw):
        for value in secret_values:
            raw = raw.replace(value.encode(), b'<redacted>')
        return raw

    def write(path, value):
        (work / path).write_text(json.dumps(value, indent=2) + '\n')

    def run(label, args, extra=None, failure=False, timeout=300):
        begin = time.monotonic()
        result = subprocess.run([str(a) for a in args], cwd=ROOT, env=env | (extra or {}),
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
        (work / (label + '.log')).write_bytes(redact(result.stdout))
        commands.append({'name': label, 'args': [str(a) for a in args], 'exit_code': result.returncode,
                         'expected_failure': failure, 'seconds': round(time.monotonic() - begin, 3)})
        if (result.returncode == 0) == failure:
            raise RuntimeError(label + ' returned an unexpected status; inspect retained log')
        return result.stdout.decode(errors='replace').strip()

    def sql(label, statement):
        return run(label, ['docker', 'exec', name, 'psql', '-U', 'campusos', '-d', DATABASE,
                           '-XqAt', '-v', 'ON_ERROR_STOP=1', '-c', statement])

    def inspect(label):
        template = ('{"id":{{json .Id}},"owner":{{json (index .Config.Labels "' + LABEL + '")}},'
                    '"ports":{{json .NetworkSettings.Ports}},"mounts":{{json .Mounts}},"tmpfs":{{json .HostConfig.Tmpfs}}}')
        return json.loads(run(label, ['docker', 'inspect', '--format=' + template, name]))

    def schema(label):
        raw = run(label, ['docker', 'exec', name, 'pg_dump', '-U', 'campusos', '-d', DATABASE,
                          '--schema-only', '--no-owner', '--no-privileges'])
        return '\n'.join(x for x in raw.splitlines() if not x.startswith(('\\restrict ', '\\unrestrict ')))

    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def http(label, method, path, body=None, token=None, expected=200, record=True):
        req = urllib.request.Request('http://127.0.0.1:' + str(api_port) + path,
                                     data=json.dumps(body).encode() if body is not None else None,
                                     headers={'Content-Type': 'application/json'} | ({'Authorization': 'Bearer ' + token} if token else {}), method=method)
        try:
            response = opener.open(req, timeout=8)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read((4 << 20) + 1)
            status = response.status
        if record:
            http_checks.append({'name': label, 'method': method, 'path': path, 'status': status, 'expected': expected})
            write('http-checks.json', http_checks)
        if status != expected or len(raw) > 4 << 20:
            raise RuntimeError(label + ' failed HTTP status/body bounds')
        if expected != 200:
            return None
        value = json.loads(raw)
        if value.get('code') != 0 or 'data' not in value:
            raise RuntimeError(label + ' failed success envelope')
        return value['data']

    def start(label):
        nonlocal process
        log = (work / (label + '-server.log')).open('wb')
        process = subprocess.Popen([str(work / 'server')], cwd=runtime, env=app_env, stdout=log, stderr=subprocess.STDOUT)
        processes.append((process, log, label))
        stop = threading.Event()
        points = []

        def sample():
            while not stop.is_set():
                try:
                    status = Path('/proc/' + str(process.pid) + '/status').read_text()
                    points.append(int(re.search(r'VmRSS:\s+(\d+)', status)[1]) * 1024)
                except (OSError, TypeError):
                    pass
                stop.wait(profile['rss_sample_interval_ms'] / 1000)
        thread = threading.Thread(target=sample, daemon=True)
        thread.start()
        process.rss_monitor = (stop, thread, points)
        deadline = time.monotonic() + 40
        while True:
            if process.poll() is not None:
                raise RuntimeError(label + ' normal server exited before readiness')
            try:
                http(label + '-ready', 'GET', '/api/v1/health', record=False)
                break
            except (OSError, RuntimeError, ValueError):
                if time.monotonic() > deadline:
                    raise RuntimeError(label + ' API readiness deadline') from None
                time.sleep(.1)
        # Allow normal resident workers and collector to initialize; no test hook.
        time.sleep(2)

    def stop_api():
        nonlocal process
        if process is None:
            return
        current = process
        if current.poll() is None:
            current.terminate()
            try:
                current.wait(timeout=15)
            except subprocess.TimeoutExpired:
                current.kill()
                current.wait(timeout=5)
        stop, thread, points = current.rss_monitor
        stop.set()
        thread.join(timeout=2)
        label = next(label for p, _, label in processes if p is current)
        rss[label] = {'interval_ms': profile['rss_sample_interval_ms'], 'observations': len(points), 'max_bytes': max(points, default=0)}
        process = None

    try:
        run('build-server', ['go', 'build', '-o', work / 'server', './cmd/server'], timeout=600)
        run('build-capacity', ['go', 'build', '-o', work / 'capacity', './cmd/campusos-capacity'], timeout=600)
        binary = {str(work / x): sha(work / x) for x in ('server', 'capacity')}
        write('binary-freeze.json', binary)
        engine = run('docker-version', ['docker', 'version', '--format={{.Server.Os}}/{{.Server.Arch}}'])
        if not engine.startswith('linux/'):
            raise RuntimeError('local Linux Docker engine required')
        image = json.loads(run('pg-image', ['docker', 'image', 'inspect', '--format={"id":{{json .Id}},"digests":{{json .RepoDigests}}}', 'postgres:16-alpine']))
        refs = [x for x in image['digests'] if re.fullmatch(r'(?:docker.io/library/)?postgres@sha256:[0-9a-f]{64}', x)]
        if not refs:
            raise RuntimeError('cached PG16 requires actual RepoDigest; no image pull')
        attempted = True
        container_id = run('pg-start', ['docker', 'run', '-d', '--pull=never', '--name', name, '--label', LABEL + '=' + nonce,
                                      '--tmpfs', '/var/lib/postgresql/data:rw,size=512m', '-e', 'POSTGRES_USER=campusos',
                                      '-e', 'POSTGRES_DB=' + DATABASE, '-e', 'POSTGRES_PASSWORD', '-p', '127.0.0.1::5432', refs[0]],
                           {'POSTGRES_PASSWORD': password})
        if not re.fullmatch(r'[0-9a-f]{64}', container_id):
            raise RuntimeError('missing owned container identity')
        deadline = time.monotonic() + 30
        while subprocess.run(['docker', 'exec', name, 'pg_isready', '-U', 'campusos', '-d', DATABASE], env=env,
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5).returncode != 0:
            if time.monotonic() > deadline:
                raise RuntimeError('PG readiness deadline')
            time.sleep(.25)
        metadata = inspect('owned-scope')
        if metadata['id'] != container_id or metadata['owner'] != nonce:
            raise RuntimeError('container identity/ownership changed')
        bindings = metadata['ports']['5432/tcp']
        if len(bindings) != 1 or bindings[0]['HostIp'] != '127.0.0.1' or set(metadata['tmpfs'] or {}) != {'/var/lib/postgresql/data'} or any(m['Type'] != 'tmpfs' for m in metadata['mounts']):
            raise RuntimeError('owned PG requires loopback-only port and tmpfs-only storage')
        pg_port = int(bindings[0]['HostPort'])
        if run('filesystem', ['docker', 'exec', name, 'stat', '-f', '-c', '%T', '/var/lib/postgresql/data']) != 'tmpfs':
            raise RuntimeError('actual PG data must be tmpfs')
        version = json.loads(sql('pg-version', "SELECT jsonb_build_object('database',current_database(),'user',current_user,'version_num',current_setting('server_version_num')::int,'version',version(),'data_directory',current_setting('data_directory'));"))
        if version['database'] != DATABASE or version['user'] != 'campusos' or version['data_directory'] != '/var/lib/postgresql/data' or not 160000 <= version['version_num'] < 170000:
            raise RuntimeError('actual PG16 scope mismatch')
        pg_env = {'PSQL_MODE': 'docker', 'POSTGRES_CONTAINER': name, 'DB_HOST': '127.0.0.1', 'DB_PORT': str(pg_port),
                  'DB_USER': 'campusos', 'DB_NAME': DATABASE, 'DB_PASSWORD': password, 'MIGRATIONS_DIR': str(ROOT / 'migrations')}
        for action in ('up', 'check'):
            run('migration-' + action, ['bash', 'scripts/migrate.sh', action], pg_env)
        run('schema-contract', ['bash', 'scripts/database-check.sh', 'all'], pg_env)
        before_schema = schema('schema-before')
        counts = sql('schema-counts', "SELECT count(*) FROM schema_migrations; SELECT count(*) FROM pg_tables WHERE schemaname='public'; SELECT count(*) FROM pg_constraint WHERE contype='f' AND connamespace='public'::regnamespace;")
        if counts.splitlines() != ['35', '132', '143']:
            raise RuntimeError('current schema requires reviewed 35/132/143')
        checks.append('owned pinned PG16 actual identity/tmpfs/loopback and current35 migrations/132tables/143FK')
        sql('capacity-seed', """
INSERT INTO users(id,username,nickname,email) VALUES(9800001,'capacity-author','Capacity Author','capacity@example.test');
INSERT INTO categories(id,name,slug) VALUES(9800010,'Capacity Board','capacity-owned');
INSERT INTO category_thread_type_policies(category_id,thread_type) VALUES(9800010,'discussion');
INSERT INTO threads(id,title,content,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status)
SELECT 9900000+n,CASE WHEN n=10001 THEN 'PRIVATE_CAPACITY_SENTINEL' ELSE 'Capacity public '||n END,
 repeat('Owned capacity public content. ',30),9800001,'Capacity Author',9800010,'published','discussion',
 CASE WHEN n=10001 THEN 'private' ELSE 'published' END,'clear','active' FROM generate_series(1,10001) n;
ANALYZE users; ANALYZE categories; ANALYZE threads;
""")
        dataset = json.loads(sql('dataset', "SELECT jsonb_build_object('discussion_threads',count(*),'public_discussion_threads',count(*) FILTER(WHERE publication_status='published'),'private_discussion_threads',count(*) FILTER(WHERE publication_status='private'),'mutual_aid_threads',count(*) FILTER(WHERE thread_type='mutual_aid'),'secondhand_threads',count(*) FILTER(WHERE thread_type='secondhand')) FROM threads;"))
        if any(dataset[k] != profile[k] for k in dataset):
            raise RuntimeError('dataset differs from frozen profile')
        with socket.socket() as reservation:
            reservation.bind(('127.0.0.1', 0))
            api_port = reservation.getsockname()[1]
        app_env = env | {'DATABASE_DSN': f'postgres://campusos:{password}@127.0.0.1:{pg_port}/{DATABASE}?sslmode=disable',
                         'SERVER_HOST': '127.0.0.1', 'SERVER_PORT': str(api_port), 'REDIS_ENABLED': 'false',
                         'NATS_URL': 'nats://127.0.0.1:1', 'EMAIL_PROVIDER': 'fake', 'AI_ENABLED': 'false',
                         'HOST_API_ENABLED': 'false', 'AUTH_PASSWORD_HASH_ENABLED': 'true',
                         'AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN': 'false', 'AUTH_BOOTSTRAP_ADMIN_SECRET': bootstrap,
                         'AUTH_REFRESH_BODY_COMPAT': 'false', 'JWT_SECRET': jwt, 'AUTH_MFA_ACTIVE_KEY_ID': 'capacity',
                         'AUTH_MFA_ENCRYPTION_KEYS': 'capacity:' + mfa_key, 'PLUGIN_DATA_DIR': str(runtime / 'data/plugin_data'),
                         'OBSERVABILITY_PROMETHEUS_ENABLED': 'false', 'GIN_MODE': 'release',
                         'GOMAXPROCS': str(profile['gomaxprocs']), 'GOMEMLIMIT': profile['gomemlimit']}
        scope = {'container': name, 'container_id': container_id, 'owner_label': nonce, 'database': DATABASE,
                 'pg_port': pg_port, 'api_port': api_port, 'image_id': image['id'], 'image_digest': refs[0],
                 'engine': engine, 'pg_version': version, 'dotenv_loaded': False, 'operator_DB_variables_inherited': False,
                 'shared_service_used': False, 'dataset': dataset}
        write('scope.json', scope)
        start('setup')
        http('bootstrap', 'POST', '/api/v1/admin/identity/bootstrap', {'username': 'capacity-admin', 'email': 'capacity-admin@example.test', 'password': admin_password, 'reason': 'owned capacity gate'})
        login = http('login', 'POST', '/api/v1/admin/identity/login', {'username': 'capacity-admin', 'password': admin_password})
        token = login['access_token']
        secret_values.append(token)
        if 'refresh_token' in login:
            raise RuntimeError('body refresh compatibility was enabled')
        enrollment = http('mfa-enroll', 'POST', '/api/v1/admin/identity/mfa/enroll', {'issuer': 'owned-capacity'}, token)
        totp_secret = urllib.parse.parse_qs(urllib.parse.urlsplit(enrollment['otpauth_url']).query)['secret'][0]
        secret_values.append(totp_secret)

        def totp(step):
            key = base64.b32decode(totp_secret + '=' * (-len(totp_secret) % 8))
            digest = hmac.new(key, struct.pack('>Q', step), hashlib.sha1).digest()
            offset = digest[-1] & 15
            return str((struct.unpack('>I', digest[offset:offset + 4])[0] & 0x7fffffff) % 1000000).zfill(6)
        step = int(time.time()) // 30
        http('mfa-confirm', 'POST', '/api/v1/admin/identity/mfa/confirm', {'code': totp(step)}, token)
        http('mfa-step-up', 'POST', '/api/v1/admin/identity/mfa/step-up', {'code': totp(step + 1)}, token)
        me = http('admin-current', 'GET', '/api/v1/admin/identity/me', token=token)
        if me['domain'] != 'admin' or me['authentication_strength'] != 'mfa':
            raise RuntimeError('current independent Admin MFA authority is required')
        with os.fdopen(os.open(authorization, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
            stream.write('Bearer ' + token + '\n')
        for key, path in profile['endpoints'].items():
            if key.startswith('admin_') or key.endswith('_summary'):
                http('anonymous-denied-' + key, 'GET', path, expected=401)
        http('admin-token-user-domain-denied', 'GET', '/api/v1/documents', token=token, expected=401)
        http('legacy-login-rejects-independent-admin', 'POST', '/api/v1/auth/admin/login',
             {'email': 'capacity-admin@example.test', 'password': admin_password}, expected=401)
        checks.append('real HTTP independent Admin bootstrap/login/TOTP/step-up/current session; anonymous and cross-domain reads denied')
        stop_api()
        for phase in ('control', 'candidate'):
            start(phase)
            current = http(phase + '-admin-current', 'GET', '/api/v1/admin/identity/me', token=token)
            if current['authentication_strength'] != 'mfa':
                raise RuntimeError('persisted current Admin session lost MFA authority')
            page = http(phase + '-public-bounds', 'GET', profile['endpoints']['thread_list'])
            if 'PRIVATE_CAPACITY_SENTINEL' in json.dumps(page) or len(page.get('items', [])) != 20 or page.get('pagination', {}).get('total') != 10000:
                raise RuntimeError('public page did not preserve current visibility/pagination')
            run(phase + '-capture', [work / 'capacity', 'capture', '--base-url', 'http://127.0.0.1:' + str(api_port),
                                    '--samples', str(profile['samples_per_endpoint']), '--include-admin-observability',
                                    '--authorization-file', authorization, '--output', work / (phase + '.json')])
            value = json.loads((work / (phase + '.json')).read_text())
            if len(value['endpoints']) != len(profile['endpoints']):
                raise RuntimeError('capacity endpoint coverage changed')
            for item in value['endpoints']:
                if profile['endpoints'].get(item['name']) != item['path'] or item['samples'] != profile['samples_per_endpoint'] or item['status_counts'] != {'200': profile['samples_per_endpoint']} or item['error_count'] != 0 or not 0 < item['p95_ms'] <= profile['max_p95_ms']:
                    raise RuntimeError(phase + ' failed frozen endpoint/count/status/latency budget')
            if value['runtime']['goroutines'] <= 0 or value['runtime']['heap_alloc_bytes'] <= 0 or 'database' not in value['runtime'] or value['reliability']['health'] != 'healthy':
                raise RuntimeError('protected runtime/reliability evidence is missing or unhealthy')
            captures[phase] = value
            if phase == 'candidate':
                http('logout', 'POST', '/api/v1/admin/identity/logout', {}, token)
                http('revoked-current-denied', 'GET', '/api/v1/admin/identity/me', token=token, expected=401)
                run('revoked-capture-denied', [work / 'capacity', 'capture', '--base-url', 'http://127.0.0.1:' + str(api_port),
                    '--samples', '3', '--include-admin-observability', '--authorization-file', authorization,
                    '--output', work / 'revoked.json'], failure=True)
                if (work / 'revoked.json').exists():
                    raise RuntimeError('revoked authority emitted an accepted capture')
            stop_api()
            if not 0 < rss[phase]['max_bytes'] <= profile['max_sampled_rss_bytes']:
                raise RuntimeError(phase + ' failed frozen sampled RSS budget')
            checks.append(phase + ' fresh normal ELF: 8 bounded reads/20 samples each/zero errors/current visibility/protected snapshots/p95 and sampled RSS')
        run('compare', [work / 'capacity', 'compare', '--baseline', work / 'control.json', '--candidate', work / 'candidate.json', '--budget', work / 'budget.json'])
        # Synthetic comparator input, explicitly distinct from target measurements.
        bad = json.loads(json.dumps(captures['candidate']))
        bad['endpoints'][0]['p95_ms'] = 10000
        bad['reliability']['dead'] = 1
        write('synthetic-regression.json', bad)
        run('synthetic-regression-denied', [work / 'capacity', 'compare', '--baseline', work / 'control.json', '--candidate', work / 'synthetic-regression.json', '--budget', work / 'budget.json'], failure=True)
        checks.append('same-binary repeatability comparison passes frozen budget; synthetic latency/dead-queue regression fails')
        checks.append('actual revoked Admin session returns401 and protected capture fails without output report')
        run('schema-exit-contract', ['bash', 'scripts/database-check.sh', 'all'], pg_env)
        if schema('schema-after') != before_schema:
            raise RuntimeError('capacity exercise changed database schema')
        if sources() != frozen or sha(PROFILE) != profile_hash or sha(work / 'frozen-profile.json') != manifest_hash or any(sha(p) != h for p, h in binary.items()):
            raise RuntimeError('sources/profile/hardware manifest/prebuilt binaries changed during gate')
        checks.append('full schema, frozen hardware/profile, execution sources and both prebuilt ELFs unchanged')
    except BaseException as error:
        write('failure.json', {'status': 'failed', 'error_type': type(error).__name__, 'scope': scope})
        raise
    finally:
        stop_api()
        for p, log, label in processes:
            log.close()
            path = work / (label + '-server.log')
            path.write_bytes(redact(path.read_bytes()))
        cleanup['api_stopped'] = all(p.poll() is not None for p, _, _ in processes)
        authorization.unlink(missing_ok=True)
        cleanup['credential_file_removed'] = not authorization.exists()
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
                for port in (pg_port, api_port):
                    if port is not None:
                        with socket.socket() as sock:
                            sock.settimeout(.5)
                            closed.append(sock.connect_ex(('127.0.0.1', port)) != 0)
                cleanup['ports_closed'] = bool(closed) and all(closed)
                if not cleanup['container_removed'] or not cleanup['ports_closed']:
                    raise RuntimeError('owned resources remain')
            except Exception as error:
                cleanup['errors'].append(str(error))
        write('commands.json', commands)
        write('rss.json', rss)
        cleanup['credentials_persisted'] = any(value.encode() in p.read_bytes() for p in work.rglob('*') if p.is_file() for value in secret_values)
        write('cleanup.json', cleanup)
        if cleanup['errors'] or cleanup['credentials_persisted']:
            raise RuntimeError('cleanup/credential custody failed')
    if not all(cleanup[k] for k in ('api_stopped', 'container_removed', 'ports_closed', 'credential_file_removed')):
        raise RuntimeError('success requires verified cleanup')
    checks.append('owned API/container/tmpfs/ports and credential file removed; credential bytes absent from retained evidence')
    write('report.json', {'schema': 'campusos.release-capacity/v1', 'status': 'passed',
                         'implementation': 'passed', 'automation': 'passed', 'target_environment': 'local_Linux_normal_API_owned_PG16',
                         'scope': scope, 'profile': manifest, 'binary_sha256': binary, 'source_sha256': frozen,
                         'checks': checks, 'http_checks': http_checks, 'captures': captures, 'rss': rss, 'commands': commands,
                         'cleanup': cleanup, 'historical_performance_regression_or_production_SLA': False,
                         'browser_restore_or_full_release_check': False,
                         'artifact_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file()}})
    print(json.dumps({'status': 'passed', 'checks': len(checks), 'report': str(work / 'report.json')}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', type=Path, help='new exclusive directory below .cache/release-capacity')
    main(parser.parse_args().work)
