#!/usr/bin/env python3
"""Final owned PG16/ordinary Server+CLI identity recovery acceptance.

The User OTP is delivered by the real resident Outbox/SMTP sender to an owned
loopback SMTP protocol peer. All verification/Ticket completion uses real HTTP;
Admin maintenance uses actual hidden-input PTYs, never a fake recovery result.
"""
import argparse
import base64
import email
import hashlib
import hmac
import importlib.util
import json
import os
from pathlib import Path
import pty
import queue
import re
import secrets
import select
import shutil
import signal
import socket
import socketserver
import struct
import subprocess
import termios
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
OWNED = ROOT / '.cache/v12-final-admin-recovery'
DB = 'campusos_v12_final_admin_recovery'
UNIT = 'campusos_v12_final_recovery_unit'
LABEL = 'campusos.final-admin-recovery.owner'
A_ID, B_ID = '870901', '1000000000000000001'
A_EMAIL, B_EMAIL = 'owned-final-recovery@example.test', 'admin@campusos.local'


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def normal(raw):
    return '\n'.join(line for line in raw.splitlines() if not line.startswith(('\\restrict ', '\\unrestrict ')))


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def closed(port):
    if port is None:
        return True
    with socket.socket() as sock:
        sock.settimeout(.5)
        return sock.connect_ex(('127.0.0.1', port)) != 0


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, url):
        return None


class SMTPSink(socketserver.ThreadingTCPServer):
    allow_reuse_address = False
    daemon_threads = True

    def __init__(self):
        self.inbox = queue.Queue(maxsize=16)
        self.metadata = []
        self.errors = []
        super().__init__(('127.0.0.1', 0), SMTPHandler)
        self.thread = threading.Thread(target=self.serve_forever, daemon=True)
        self.thread.start()

    def stop(self):
        self.shutdown()
        self.server_close()
        self.thread.join(timeout=5)
        while not self.inbox.empty():
            self.inbox.get_nowait()
        assert not self.thread.is_alive()


class SMTPHandler(socketserver.StreamRequestHandler):
    def handle(self):
        self.connection.settimeout(15)
        recipient = ''
        def reply(value):
            self.wfile.write(value.encode() + b'\r\n')
            self.wfile.flush()
        try:
            reply('220 owned-final-recovery SMTP')
            for _ in range(30):
                line = self.rfile.readline(8193)
                if not line:
                    return
                assert len(line) <= 8192
                command = line.decode('ascii').strip()
                verb = command.split(' ', 1)[0].upper()
                if verb in ('EHLO', 'HELO'):
                    reply('250 owned-loopback')
                elif verb in ('MAIL', 'RSET', 'NOOP'):
                    reply('250 OK')
                elif verb == 'RCPT':
                    recipient = command.split(':', 1)[1].strip().strip('<>')
                    reply('250 OK')
                elif verb == 'DATA':
                    reply('354 End with dot')
                    body = bytearray()
                    while True:
                        part = self.rfile.readline(8193)
                        assert part and len(part) <= 8192
                        if part in (b'.\r\n', b'.\n'):
                            break
                        if part.startswith(b'..'):
                            part = part[1:]
                        body.extend(part)
                        assert len(body) <= 65536
                    message = email.message_from_bytes(bytes(body))
                    content = message.get_payload(decode=True).decode('utf-8')
                    self.server.inbox.put_nowait((recipient, message.get('X-CampusOS-Delivery-ID', ''), content))
                    self.server.metadata.append({'bytes': len(body), 'actual_SMTP_DATA': True})
                    reply('250 accepted')
                elif verb == 'QUIT':
                    reply('221 bye')
                    return
                else:
                    reply('502 unsupported')
        except Exception as error:
            # No message body, recipient, OTP or arbitrary exception is retained.
            self.server.errors.append(type(error).__name__)


def main(work):
    assert os.uname().sysname == 'Linux', 'current Linux target required'
    work = work.resolve()
    assert work.is_relative_to(OWNED) and work != OWNED, 'fresh owned output required'
    work.mkdir(parents=True, exist_ok=False)
    work.chmod(0o700)
    for name in ('native', 'runtime', 'docker-config'):
        (work / name).mkdir(mode=0o700)
    native, runtime = work / 'native', work / 'runtime'
    spec = importlib.util.spec_from_file_location('restore', ROOT / 'scripts/check-release-restore.py')
    shared = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(shared)
    frozen = shared.sources()
    (work / 'source-freeze.json').write_text(json.dumps(frozen, ensure_ascii=False, indent=2) + '\n')
    values = {name: secrets.token_hex(24) for name in ('database', 'bootstrap', 'user', 'admin_a', 'admin_b', 'new_user', 'new_admin', 'jwt', 'mfa', 'challenge', 'ip')}
    secret_values = set(values.values())
    def remember(value):
        if isinstance(value, str) and value:
            secret_values.add(value)
        return value
    def redact(raw):
        for value in sorted(secret_values, key=len, reverse=True):
            raw = raw.replace(value.encode(), b'[redacted]')
        return raw
    env = {key: os.environ[key] for key in ('HOME', 'PATH') if key in os.environ}
    env.update(GOCACHE='/tmp/campusos-go-cache', GOFLAGS='-buildvcs=false', GOMAXPROCS='2', CAMPUSOS_ENV='test',
               CAMPUSOS_SKIP_DOTENV='true', DOCKER_HOST='unix:///var/run/docker.sock', DOCKER_CONFIG=str(work / 'docker-config'))
    nonce = secrets.token_hex(12)
    container = 'campusos-final-admin-recovery-' + nonce
    commands, checks, http_records, tty_records = [], [], [], []
    processes, api_ports = [], set()
    attempted, cid, pg_port, current, smtp, summary = False, None, None, None, None, None
    cleanup = {'errors': []}
    def run(label, args, extra=None, data=None):
        began = time.monotonic()
        result = subprocess.run([str(arg) for arg in args], cwd=ROOT, env=env | (extra or {}), input=data, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        (work / (label + '.log')).write_bytes(redact(result.stdout))
        commands.append({'name': label, 'exit_code': result.returncode, 'seconds': round(time.monotonic() - began, 3), 'args': [str(arg) for arg in args]})
        assert result.returncode == 0, label + ' failed; inspect owned log'
        return result.stdout.decode()
    def sql(statement, db=DB):
        return run('sql-' + str(len(commands)), ['docker', 'exec', '-i', container, 'psql', '-U', 'campusos', '-d', db, '-XAtv', 'ON_ERROR_STOP=1'], data=statement.encode()).strip()
    def schema(db, phase):
        return normal(run(phase + '-' + db, ['docker', 'exec', container, 'pg_dump', '-U', 'campusos', '-d', db, '--schema-only', '--no-owner', '--no-privileges']))
    def dsn(db):
        return f'postgres://campusos:{values["database"]}@127.0.0.1:{pg_port}/{db}?sslmode=disable'
    def mandatory(log, test):
        assert '--- PASS: ' + test + ' ' in log and not any(value in log for value in ('--- SKIP', '--- FAIL', 'DATA RACE'))
    def digest_rows(table, where='true'):
        assert re.fullmatch(r'[a-z_]+', table)
        return sql("SELECT md5(coalesce(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text)::text,'[]')) FROM " + table + ' r WHERE ' + where + ';')
    def user_facts():
        return {table: digest_rows(table) for table in ('users', 'accounts', 'sessions', 'user_roles', 'identity_admin_accounts', 'identity_mfa_totp_methods', 'identity_mfa_recovery_codes', 'identity_mfa_tickets')}
    def admin_facts(account):
        return {table: digest_rows(table, ("id='" if table == 'admin_accounts' else "account_id='") + account + "'") for table in ('admin_accounts', 'admin_credentials', 'admin_sessions', 'admin_role_bindings', 'admin_mfa_totp', 'admin_mfa_tickets', 'admin_recovery_tickets')}
    def roles(account):
        return digest_rows('admin_role_bindings', "account_id='" + account + "'")
    def stop_api():
        nonlocal current
        if current is None:
            return
        process, log, path = current
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
        log.close()
        path.write_bytes(redact(path.read_bytes()))
        current = None
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    base = ''
    def http(method, path, body=None, token=None, expect=(200,)):
        headers = {'Content-Type': 'application/json'}
        if token:
            headers['Authorization'] = 'Bearer ' + token
        request = urllib.request.Request(base + '/api/v1' + path, data=json.dumps(body).encode() if body is not None else None, method=method, headers=headers)
        try:
            with opener.open(request, timeout=15) as response:
                status, raw = response.status, response.read(1 << 20)
        except urllib.error.HTTPError as response:
            status, raw = response.code, response.read(1 << 20)
        obj = json.loads(raw)
        http_records.append({'method': method, 'path': path, 'status': status, 'bytes': len(raw), 'code': obj.get('code')})
        assert status in expect, 'unexpected HTTP status: ' + method + ' ' + path + ' ' + str(status)
        if status == 200:
            assert obj.get('code') == 0
        return obj.get('data')
    def start_api(label):
        nonlocal current, base
        port = free_port()
        api_ports.add(port)
        base = 'http://127.0.0.1:' + str(port)
        settings = app_env | {'SERVER_PORT': str(port)}
        path = work / (label + '-server.log')
        log = path.open('wb')
        args = ['bwrap', '--die-with-parent', '--ro-bind', '/', '/', '--dev', '/dev', '--proc', '/proc', '--tmpfs', '/tmp', '--bind', str(runtime), str(runtime), '--ro-bind', str(native), str(native), '--chdir', str(runtime), '--', str(native / 'server')]
        process = subprocess.Popen(args, cwd=runtime, env=settings, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        current = (process, log, path)
        processes.append(process)
        for _ in range(160):
            assert process.poll() is None, 'actual Server exited before readiness'
            try:
                http('GET', '/health')
                return
            except (urllib.error.URLError, TimeoutError, OSError):
                pass
            time.sleep(.25)
        raise AssertionError('actual Server readiness deadline')
    def login(domain, email_address, password):
        path = '/admin/identity/login' if domain == 'admin' else '/auth/login'
        body = {'username' if domain == 'admin' else 'email': email_address, 'password': password}
        tokens = http('POST', path, body)
        for field in ('access_token', 'refresh_token'):
            assert tokens.get(field), 'real non-browser compatibility credentials required'
            remember(tokens[field])
        return tokens
    def refresh(domain, tokens, expect=(200,)):
        path = '/admin/identity/refresh' if domain == 'admin' else '/auth/refresh'
        result = http('POST', path, {'refresh_token': tokens['refresh_token']}, expect=expect)
        if expect == (200,):
            for field in ('access_token', 'refresh_token'):
                assert result.get(field)
                remember(result[field])
        return result
    def reject_access(domain, tokens):
        http('GET', '/admin/identity/me' if domain == 'admin' else '/auth/me', token=tokens['access_token'], expect=(401,))
    def tty(label, action, account=B_ID, password=None, bootstrap=None, expect=0):
        args = [str(native / 'campusosctl'), 'identity', action]
        if action != 'reset-password':
            args += ['--account-id', account, '--reason', 'owned Final recovery acceptance']
        else:
            args += ['--reason', 'owned Final password acceptance']
        confirmation = ('RESET ' + B_EMAIL) if action == 'reset-password' else ('RESET-MFA ' if action == 'reset-mfa' else 'RESTORE-ADMIN ') + account
        steps = [('Type ' + confirmation + ' to continue: ', confirmation, False), ('Bootstrap administrator secret: ', bootstrap or values['bootstrap'], True)]
        if password is not None:
            steps += [('New password: ', password, True), ('Repeat new password: ', password, True)]
        master, slave = pty.openpty()
        output = bytearray()
        process = subprocess.Popen(args, stdin=slave, stdout=slave, stderr=slave, cwd=runtime, env=app_env, start_new_session=True)
        processes.append(process)
        began = time.monotonic()
        try:
            for prompt, value, hidden in steps:
                deadline = time.monotonic() + 20
                while prompt.encode() not in output:
                    assert process.poll() is None and time.monotonic() < deadline, 'actual CLI did not reach expected prompt: ' + label
                    ready, _, _ = select.select([master], [], [], .1)
                    if ready:
                        output.extend(os.read(master, 8192))
                if hidden:
                    while termios.tcgetattr(slave)[3] & termios.ECHO:
                        assert process.poll() is None and time.monotonic() < deadline, 'CLI did not disable terminal echo'
                        time.sleep(.005)
                os.write(master, value.encode() + b'\n')
            deadline = time.monotonic() + 25
            while process.poll() is None:
                assert time.monotonic() < deadline, 'actual CLI exit deadline'
                ready, _, _ = select.select([master], [], [], .1)
                if ready:
                    output.extend(os.read(master, 8192))
            while select.select([master], [], [], 0)[0]:
                try:
                    chunk = os.read(master, 8192)
                except OSError:
                    break
                if not chunk:
                    break
                output.extend(chunk)
            assert process.returncode == expect, 'unexpected CLI result: ' + label
            assert all(value.encode() not in output for _, value, hidden in steps if hidden), 'secret was echoed by actual CLI'
            tty_records.append({'name': label, 'action': action, 'actual_PTY': True, 'echo_disabled_before_secret_input': True, 'exit_code': process.returncode, 'seconds': round(time.monotonic() - began, 3)})
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                process.wait(timeout=10)
            os.close(master)
            os.close(slave)
            (work / (label + '-cli.log')).write_bytes(redact(bytes(output)))
    def totp(secret, step=None):
        key = base64.b32decode(secret + '=' * ((8 - len(secret) % 8) % 8))
        value = hmac.new(key, struct.pack('>Q', int(time.time() // 30) if step is None else step), hashlib.sha1).digest()
        offset = value[-1] & 15
        return str((struct.unpack('>I', value[offset:offset + 4])[0] & 0x7fffffff) % 1000000).zfill(6)
    def enroll(tokens):
        result = http('POST', '/admin/identity/mfa/enroll', {'issuer': 'Final recovery owned target'}, token=tokens['access_token'])
        secret = urllib.parse.parse_qs(urllib.parse.urlparse(result['otpauth_url']).query)['secret'][0]
        remember(secret)
        step = int(time.time() // 30)
        http('POST', '/admin/identity/mfa/confirm', {'code': totp(secret, step)}, token=tokens['access_token'])
        return secret, step
    try:
        for package, name, test in [('./cmd/server', 'server', False), ('./cmd/campusosctl', 'campusosctl', False), ('./internal/server', 'fixture.test', True), ('./internal/modules/core/identity/service', 'identity.test', True)]:
            args = ['go', 'test', '-race', '-c'] if test else ['go', 'build']
            run('build-' + name, args + ['-o', str(native / name), package])
        binaries = {str(path.relative_to(ROOT)): sha(path) for path in native.iterdir()}
        for directory in ('migrations', 'modules'):
            shutil.copytree(ROOT / directory, runtime / directory)
        image = run('pg-image', ['docker', 'image', 'inspect', 'postgres:16-alpine', '--format={{.Id}}']).strip()
        attempted = True
        cid = run('pg-start', ['docker', 'run', '--rm', '--pull=never', '-d', '--name', container, '--label', LABEL + '=' + nonce, '--tmpfs', '/var/lib/postgresql/data:rw,size=1g', '--cpus=2', '--memory=1g', '--memory-swap=1g', '--pids-limit=256', '-e', 'POSTGRES_USER=campusos', '-e', 'POSTGRES_DB=' + DB, '-e', 'POSTGRES_PASSWORD', '-p', '127.0.0.1::5432', image], {'POSTGRES_PASSWORD': values['database']}).strip()
        for _ in range(120):
            if subprocess.run(['docker', 'exec', container, 'pg_isready', '-h', '127.0.0.1', '-U', 'campusos'], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                break
            time.sleep(.25)
        else:
            raise AssertionError('owned PG readiness deadline')
        scope = json.loads(run('scope', ['docker', 'inspect', container, '--format={{json .}}']))
        assert scope['Id'] == cid and scope['Config']['Labels'].get(LABEL) == nonce
        assert scope['HostConfig']['PortBindings']['5432/tcp'][0]['HostIp'] == '127.0.0.1'
        assert all(mount['Type'] == 'tmpfs' for mount in scope['Mounts'])
        pg_port = int(run('pg-port', ['docker', 'port', container, '5432/tcp']).strip().rsplit(':', 1)[1])
        pg = {'PSQL_MODE': 'docker', 'POSTGRES_CONTAINER': container, 'DB_NAME': DB, 'DB_USER': 'campusos', 'DB_PASSWORD': values['database']}
        run('migrate-up', ['bash', 'scripts/migrate.sh', 'up'], pg)
        assert sql("SELECT count(*) FROM pg_tables WHERE schemaname='public';SELECT max(version)::int FROM schema_migrations;SELECT count(*) FROM pg_constraint WHERE contype='f' AND connamespace='public'::regnamespace;").splitlines() == ['132', '35', '143']
        sql('CREATE DATABASE ' + UNIT + ' TEMPLATE ' + DB, 'postgres')
        before = {db: schema(db, 'schema-before') for db in (DB, UNIT)}
        unit = run('core-postgres', [str(native / 'identity.test'), '-test.v', '-test.run=^TestAdminLocalRecoveryPostgreSQL$', '-test.timeout=180s'], {'CAMPUSOS_ADMIN_LOCAL_RECOVERY_TEST_DATABASE_URL': dsn(UNIT)})
        mandatory(unit, 'TestAdminLocalRecoveryPostgreSQL')
        checks.append('mandatory actual-PG Admin local Core rollback/roles/session/ticket matrix; no skip/race')
        fixture_env = {'CAMPUSOS_FINAL_RECOVERY_DSN': dsn(DB), 'AUTH_BOOTSTRAP_ADMIN_SECRET': values['bootstrap'], 'CAMPUSOS_FINAL_RECOVERY_USER_PASSWORD': values['user'], 'CAMPUSOS_FINAL_RECOVERY_ADMIN_A_PASSWORD': values['admin_a'], 'CAMPUSOS_FINAL_RECOVERY_ADMIN_B_PASSWORD': values['admin_b']}
        fixture = run('initial-fixtures', [str(native / 'fixture.test'), '-test.v', '-test.run=^TestFinalIdentityRecoverySeedPostgres$'], fixture_env)
        mandatory(fixture, 'TestFinalIdentityRecoverySeedPostgres')
        smtp = SMTPSink()
        smtp_port = smtp.server_address[1]
        app_env = env | {'DATABASE_DSN': dsn(DB), 'SERVER_HOST': '127.0.0.1', 'REDIS_ENABLED': 'false', 'NATS_URL': 'nats://127.0.0.1:1', 'HOST_API_ENABLED': 'false',
            'AUTH_PASSWORD_HASH_ENABLED': 'true', 'AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN': 'false', 'AUTH_BOOTSTRAP_ADMIN_SECRET': values['bootstrap'], 'AUTH_REFRESH_BODY_COMPAT': 'true',
            'AUTH_CHALLENGE_ACTIVE_KEY_ID': 'ownedfinal', 'AUTH_CHALLENGE_HMAC_KEYS': 'ownedfinal:' + values['challenge'], 'AUTH_CHALLENGE_IP_HASH_SECRET': values['ip'], 'AUTH_SESSION_IP_HASH_SECRET': values['ip'],
            'AUTH_MFA_ACTIVE_KEY_ID': 'ownedfinal', 'AUTH_MFA_ENCRYPTION_KEYS': 'ownedfinal:' + values['mfa'], 'JWT_SECRET': values['jwt'], 'JWT_ACCESS_TTL': '30m',
            'EMAIL_PROVIDER': 'smtp', 'EMAIL_SMTP_HOST': '127.0.0.1', 'EMAIL_SMTP_PORT': str(smtp_port), 'EMAIL_SMTP_FROM': 'owned-final@example.test', 'EMAIL_SMTP_USERNAME': '', 'EMAIL_SMTP_STARTTLS': 'false', 'EMAIL_SMTP_TIMEOUT': '10s',
            'AI_ENABLED': 'false', 'PLUGIN_DATA_DIR': str(runtime / 'data/plugin_data'), 'OBSERVABILITY_PROMETHEUS_ENABLED': 'false', 'CAMPUSOS_RUN_MIGRATIONS': 'false', 'GIN_MODE': 'release'}
        start_api('original')
        user_a = login('user', A_EMAIL, values['user'])
        admin_a = login('admin', A_EMAIL, values['admin_a'])
        reject_access('admin', user_a)
        reject_access('user', admin_a)
        refresh('admin', user_a, (401,))
        refresh('user', admin_a, (401,))
        user_a, admin_a = refresh('user', user_a), refresh('admin', admin_a)
        checks.append('valid current User/Admin same-ID/email access and refresh credentials rejected by opposite domain without destroying own family')
        unchanged_admin = admin_facts(A_ID)
        receipt = http('POST', '/auth/password-reset/challenge', {'email': A_EMAIL})
        challenge = receipt['challenge_id']
        deadline = time.monotonic() + 20
        code = None
        while time.monotonic() < deadline:
            try:
                to, delivery, content = smtp.inbox.get(timeout=.25)
            except queue.Empty:
                continue
            if to == A_EMAIL and delivery.startswith('identity-challenge:') and '恢复请求编号：' + challenge in content:
                found = re.search(r'验证码：([0-9]{6})', content)
                assert found, 'actual SMTP message lacks OTP'
                code = remember(found.group(1))
                break
        assert code and not smtp.errors, 'normal Outbox/SMTP delivery missing'
        http('POST', '/auth/password-reset/verify', {'challenge_id': challenge, 'code': '000000' if code != '000000' else '111111'}, expect=(400,))
        ticket = remember(http('POST', '/auth/password-reset/verify', {'challenge_id': challenge, 'code': code})['ticket'])
        tty('user-ticket-cannot-authorize-admin', 'reset-password', bootstrap=ticket, expect=1)
        assert admin_facts(A_ID) == unchanged_admin
        http('POST', '/auth/password-reset/complete', {'email': 'foreign@example.test', 'challenge_id': challenge, 'ticket': ticket, 'password': values['new_user']}, expect=(400,))
        completion = {'email': A_EMAIL, 'challenge_id': challenge, 'ticket': ticket, 'password': values['new_user']}
        http('POST', '/auth/password-reset/complete', completion)
        assert admin_facts(A_ID) == unchanged_admin, 'User recovery changed same-ID/email Admin authority'
        http('POST', '/auth/password-reset/complete', completion, expect=(400,))
        reject_access('user', user_a)
        refresh('user', user_a, (401,))
        http('POST', '/auth/login', {'email': A_EMAIL, 'password': values['user']}, expect=(401,))
        user_a = login('user', A_EMAIL, values['new_user'])
        http('GET', '/admin/identity/me', token=admin_a['access_token'])
        admin_a = refresh('admin', admin_a)
        http('POST', '/admin/identity/login', {'username': A_EMAIL, 'password': values['new_user']}, expect=(401,))
        checks.append('actual SMTP-delivered OTP/HTTP verify/true Ticket completion rotates only User credentials and sessions; wrong email/replay fail; same-email Admin unchanged')
        admin_b = login('admin', B_EMAIL, values['admin_b'])
        user_b = login('user', B_EMAIL, values['bootstrap'])
        before_user, before_roles = user_facts(), roles(B_ID)
        stop_api()
        tty('admin-password-reset', 'reset-password', password=values['new_admin'])
        assert user_facts() == before_user and roles(B_ID) == before_roles
        assert sql("SELECT count(*) FROM admin_sessions WHERE account_id='" + B_ID + "' AND revoked_at IS NULL") == '0'
        start_api('after-password')
        reject_access('admin', admin_b)
        refresh('admin', admin_b, (401,))
        http('POST', '/admin/identity/login', {'username': B_EMAIL, 'password': values['admin_b']}, expect=(401,))
        admin_b = login('admin', B_EMAIL, values['new_admin'])
        http('GET', '/auth/me', token=user_b['access_token'])
        user_b = refresh('user', user_b)
        checks.append('ordinary hidden-input PTY password CLI mutates independent Admin only; same-ID/email system_managed User survives normal restart; old Admin access/refresh/password rejected')
        secret_a, step_a = enroll(admin_a)
        secret_b, step_b = enroll(admin_b)
        deadline_step = max(step_a, step_b)
        while int(time.time() // 30) <= deadline_step:
            time.sleep(.25)
        http('POST', '/admin/identity/mfa/step-up', {'code': totp(secret_a)}, token=admin_a['access_token'])
        http('POST', '/admin/identity/mfa/step-up', {'code': totp(secret_b)}, token=admin_b['access_token'])
        assert http('GET', '/admin/identity/me', token=admin_b['access_token'])['authentication_strength'] == 'mfa'
        before_user, before_roles = user_facts(), roles(B_ID)
        stop_api()
        tty('admin-factor-reset', 'reset-mfa')
        assert user_facts() == before_user and roles(B_ID) == before_roles
        assert sql("SELECT count(*) FROM admin_mfa_totp WHERE account_id='" + B_ID + "' AND status='active'") == '0'
        tty('disabled-factor-repeat-denied', 'reset-mfa', expect=1)
        start_api('after-factor')
        reject_access('admin', admin_b)
        refresh('admin', admin_b, (401,))
        admin_b = login('admin', B_EMAIL, values['new_admin'])
        profile_b = http('GET', '/admin/identity/me', token=admin_b['access_token'])
        assert profile_b['mfa_enrolled'] is False
        http('POST', '/admin/identity/mfa/step-up', {'code': totp(secret_b)}, token=admin_b['access_token'], expect=(409,))
        checks.append('real HTTP TOTP enrollment/confirmation/next-step MFA followed by offline PTY factor reset; old MFA credentials rejected; roles/User untouched')
        http('POST', '/admin/identity/accounts/' + B_ID + '/suspend', {'reason': 'owned Final suspension'}, token=admin_a['access_token'])
        reject_access('admin', admin_b)
        before_user, before_roles = user_facts(), roles(B_ID)
        stop_api()
        tty('admin-account-restore', 'restore-admin-admission')
        assert user_facts() == before_user and roles(B_ID) == before_roles
        assert sql("SELECT status FROM admin_accounts WHERE id='" + B_ID + "'") == 'active'
        start_api('after-restore')
        reject_access('admin', admin_b)
        refresh('admin', admin_b, (401,))
        admin_b = login('admin', B_EMAIL, values['new_admin'])
        restored = http('GET', '/admin/identity/me', token=admin_b['access_token'])
        assert sorted(restored['permission_codes']) == sorted(profile_b['permission_codes'])
        assert roles(B_ID) == before_roles
        checks.append('actual MFA Admin HTTP suspension then offline PTY restoration retains exact role bindings/permissions and never revives old Admin credentials')
        http('POST', '/admin/identity/accounts/' + B_ID + '/revoke', {'reason': 'owned Final revocation'}, token=admin_a['access_token'])
        reject_access('admin', admin_b)
        before_user, revoked = user_facts(), admin_facts(B_ID)
        stop_api()
        for action in ('reset-password', 'reset-mfa', 'restore-admin-admission'):
            tty('revoked-' + action + '-denied', action, password=values['new_admin'] if action == 'reset-password' else None, expect=1)
        tty('user-only-id-not-admin', 'restore-admin-admission', account='870903', expect=1)
        assert user_facts() == before_user and admin_facts(B_ID) == revoked
        checks.append('revoked independent Admin and User-only ID denied by actual three PTY commands without changing User/Admin facts')
        for label, args in [('no-terminal', ['identity', 'reset-password']), ('old-user-id', ['identity', 'reset-mfa', '--user-id', B_ID, '--reason', 'owned negative']), ('ticket-flag', ['identity', 'reset-password', '--ticket', ticket])]:
            result = subprocess.run([str(native / 'campusosctl'), *args], cwd=runtime, env=app_env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            assert result.returncode != 0 and all(value.encode() not in result.stdout for value in secret_values), 'forbidden CLI credential channel accepted/leaked'
            (work / (label + '.log')).write_bytes(redact(result.stdout))
        assert user_facts() == before_user and admin_facts(B_ID) == revoked
        checks.append('pipes, legacy user-id and ticket option rejected; no secret argument echo')
        for db in (DB, UNIT):
            assert schema(db, 'schema-after') == before[db], 'whole Schema changed: ' + db
        run('database-after', ['bash', 'scripts/database-check.sh', 'all'], pg)
        assert shared.sources() == frozen, 'source changed during frozen target'
        assert all(sha(ROOT / path) == digest for path, digest in binaries.items()), 'prebuilt ELF changed'
        summary = {'schema': 'campusos.final-admin-recovery/v1', 'status': 'passed', 'implementation': 'passed', 'automation': 'passed', 'target_environment': 'Linux_normal_prebuilt_cmd_server_CLI_PG16_actual_SMTP_HTTP_PTY',
            'checks': checks, 'http_checks': http_records, 'tty_checks': tty_records, 'source_sha256': frozen, 'binary_sha256': binaries, 'scope': {'container': container, 'container_id': cid, 'owner': nonce, 'postgres_image': image, 'pg_port': pg_port, 'main_database': DB, 'unit_database': UNIT, 'migration': 35, 'tables': 132, 'foreign_keys': 143},
            'whole_schema_sha256': {db: hashlib.sha256(value.encode()).hexdigest() for db, value in before.items()}, 'actual_SMTP_messages': smtp.metadata, 'mocked_HTTP': False, 'manufactured_positive_Tickets': False, 'User_password_reset_requires_verified_not_system_managed': True, 'windows': 'deferred_not_run', 'commit_or_push': False}
    finally:
        try:
            stop_api()
        except Exception as error:
            cleanup['errors'].append(type(error).__name__)
        cleanup['owned_processes_exited'] = all(process.poll() is not None for process in processes)
        cleanup['api_ports_closed'] = all(closed(port) for port in api_ports)
        if smtp is not None:
            smtp_port = smtp.server_address[1]
            smtp.stop()
            cleanup['SMTP_port_closed'] = closed(smtp_port)
            cleanup['SMTP_errors'] = smtp.errors
        if attempted:
            try:
                current_scope = subprocess.run(['docker', 'inspect', container, '--format={{json .}}'], env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                if current_scope.returncode == 0:
                    obj = json.loads(current_scope.stdout)
                    assert obj['Config']['Labels'].get(LABEL) == nonce and (not cid or obj['Id'] == cid), 'refuse cleanup of foreign/replaced resource'
                    run('pg-log', ['docker', 'logs', container])
                    run('pg-remove', ['docker', 'rm', '-f', '-v', container])
                run('daemon-after', ['docker', 'info', '--format={{.ServerVersion}}'])
                missing = subprocess.run(['docker', 'inspect', container], env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                cleanup['owned_container_removed'] = missing.returncode == 1 and b'no such object' in missing.stderr.lower()
                cleanup['pg_port_closed'] = closed(pg_port)
            except Exception as error:
                cleanup['errors'].append(type(error).__name__)
        (work / 'commands.json').write_text(json.dumps(commands, ensure_ascii=False, indent=2) + '\n')
        (work / 'http-checks.json').write_text(json.dumps(http_records, indent=2) + '\n')
        (work / 'tty-checks.json').write_text(json.dumps(tty_records, indent=2) + '\n')
        for path in work.rglob('*'):
            if path.is_file() and path.suffix in ('.json', '.jsonl', '.log', '.txt'):
                path.write_bytes(redact(path.read_bytes()))
        cleanup['long_secrets_persisted'] = any(value.encode() in path.read_bytes() for path in work.rglob('*') if path.is_file() for value in secret_values if len(value) >= 16)
        (work / 'cleanup.json').write_text(json.dumps(cleanup, indent=2) + '\n')
        assert not cleanup['errors'] and not cleanup['long_secrets_persisted'], 'owned cleanup/secret custody failed'
        assert cleanup['owned_processes_exited'] and cleanup['api_ports_closed']
        if smtp is not None:
            assert cleanup['SMTP_port_closed'] and not cleanup['SMTP_errors']
        if attempted:
            assert cleanup['owned_container_removed'] and cleanup['pg_port_closed']
    assert summary is not None
    summary['commands'], summary['cleanup'] = commands, cleanup
    summary['artifact_sha256'] = {str(path.relative_to(ROOT)): sha(path) for path in sorted(work.rglob('*')) if path.is_file()}
    (work / 'target.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps({'status': 'passed', 'checks': len(checks), 'HTTP': len(http_records), 'PTY': len(tty_records), 'work': str(work.relative_to(ROOT))}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', type=Path, required=True)
    main(parser.parse_args().work)
