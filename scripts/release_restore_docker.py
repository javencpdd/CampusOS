"""Owned Docker transport for the shared official release-recovery contract."""
import json
import os
from pathlib import Path
import re
import shutil
import urllib.parse


class DockerRecovery:
    def __init__(self, root, work, native, pg, nonce, label, run, profile):
        self.root, self.work, self.native, self.pg = root, work, native, pg
        self.nonce, self.label, self.run, self.profile = nonce, label, run, profile
        self.network = 'campusos-release-recovery-' + nonce
        self.image = 'campusos-release-recovery:' + nonce
        self.base_tag = 'campusos-release-recovery-base:' + nonce
        self.network_id = self.image_id = None
        self.containers, self.ports, self.isolation = {}, [], []
        self.active = None
        self.report = {'backend': 'docker', 'owned_containers_removed': False,
                       'owned_image_removed': False, 'owned_network_removed': False,
                       'borrowed_base_preserved': False, 'owned_base_alias_removed': False}

    def prepare(self):
        base = json.loads(self.run('runtime-base', ['docker', 'image', 'inspect', '--format={"id":{{json .Id}}}', 'campusos-api:0.13.0']))
        if not re.fullmatch(r'sha256:[0-9a-f]{64}', base['id']):
            raise RuntimeError('cached runtime requires its actual local image ID')
        self.base_id = base['id']
        self.run('runtime-base-alias', ['docker', 'tag', self.base_id, self.base_tag])
        self.network_id = self.run('network-create', ['docker', 'network', 'create', '--label', self.label + '=' + self.nonce, self.network])
        if not re.fullmatch(r'[0-9a-f]{64}', self.network_id):
            raise RuntimeError('owned network identity missing')
        context = self.work / 'image'
        context.mkdir()
        for source, target in [('server', 'campusos-server'), ('campusosctl', 'campusosctl')]:
            shutil.copy2(self.native / source, context / target)
        for folder in ('migrations', 'modules'):
            shutil.copytree(self.root / folder, context / folder)
        (context / 'scripts').mkdir()
        for source in ('migrate.sh', 'backup.sh', 'restore.sh'):
            shutil.copy2(self.root / 'scripts' / source, context / 'scripts' / source)
        shutil.copy2(self.root / 'deploy/docker/entrypoint.sh', context / 'entrypoint.sh')
        shutil.copy2(self.root / 'deploy/docker/restore-container.sh', context / 'scripts/docker-restore.sh')
        (context / 'Dockerfile').write_text(
            'FROM ' + self.base_tag + '\nUSER root\n'
            'RUN rm -rf /app/bin /app/migrations /app/modules /app/scripts /app/seed-data /app/.env\n'
            'COPY campusos-server campusosctl /app/bin/\nCOPY migrations /app/migrations\nCOPY modules /app/modules\n'
            'COPY scripts /app/scripts\nCOPY entrypoint.sh /app/entrypoint.sh\n'
            'RUN chmod 0755 /app/bin/* /app/scripts/*.sh /app/entrypoint.sh\n'
            f'USER {os.getuid()}:{os.getgid()}\n')
        self.run('runtime-build', ['docker', 'build', '--pull=false', '--network=none', '--label', self.label + '=' + self.nonce, '-t', self.image, context])
        metadata = json.loads(self.run('runtime-image', ['docker', 'image', 'inspect', '--format={"id":{{json .Id}},"owner":{{json (index .Config.Labels "' + self.label + '")}}}', self.image]))
        self.image_id = metadata['id']
        if metadata['owner'] != self.nonce or not re.fullmatch(r'sha256:[0-9a-f]{64}', self.image_id):
            raise RuntimeError('built image identity/ownership mismatch')
        layers = json.loads(self.run('runtime-layers', ['docker', 'image', 'inspect', '--format={{json .RootFS.Layers}}', self.image_id]))
        base_layers = json.loads(self.run('runtime-base-layers', ['docker', 'image', 'inspect', '--format={{json .RootFS.Layers}}', self.base_id]))
        if layers[:len(base_layers)] != base_layers:
            raise RuntimeError('built image did not use the attested immutable base layers')
        self.report['base_layers'] = base_layers
        self.report.update(base_image_id=self.base_id, image_id=self.image_id, network_id=self.network_id)

    def settings(self, extra):
        # Host paths, Docker context, operator targets and proxy variables do
        # not enter a guest. Values travel through -e NAME, never argv.
        names = {'DATABASE_DSN', 'SERVER_HOST', 'SERVER_PORT', 'REDIS_ENABLED', 'NATS_URL', 'EMAIL_PROVIDER',
                 'AI_ENABLED', 'HOST_API_ENABLED', 'AUTH_PASSWORD_HASH_ENABLED', 'AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN',
                 'AUTH_BOOTSTRAP_ADMIN_SECRET', 'AUTH_REFRESH_BODY_COMPAT', 'JWT_SECRET', 'AUTH_MFA_ACTIVE_KEY_ID',
                 'AUTH_MFA_ENCRYPTION_KEYS', 'OBSERVABILITY_PROMETHEUS_ENABLED', 'GIN_MODE', 'CAMPUSOS_RUN_MIGRATIONS'}
        values = {k: v for k, v in extra.items() if k in names}
        dsn = urllib.parse.urlsplit(extra['DATABASE_DSN'])
        values.update(DATABASE_DSN=urllib.parse.urlunsplit(dsn._replace(netloc=dsn.username + ':' + dsn.password + '@' + self.pg + ':5432')),
                      SERVER_HOST='0.0.0.0', SERVER_PORT='8080', DB_HOST=self.pg, DB_PORT='5432', DB_USER=dsn.username,
                      DB_PASSWORD=dsn.password, DB_NAME=dsn.path.removeprefix('/'), CAMPUSOS_ENV='test',
                      CAMPUSOS_SKIP_DOTENV='true', CAMPUSOS_RUN_MIGRATIONS='false', GOMAXPROCS=str(self.profile['gomaxprocs']),
                      PLUGIN_DATA_DIR='/app/data/plugin_data', CAMPUSOS_PLUGIN_V5_TRUST_STORE='/app/data/config/v12-backup-public-trust.json',
                      MIGRATIONS_DIR='/app/migrations', CAMPUSOS_RECOVERY_DATA_ROOT='/app/data')
        return values

    def flags(self, name, values):
        data = self.work / 'restored/data'
        data.mkdir(parents=True, exist_ok=True, mode=0o700)
        return ['--name', name, '--pull=never', '--label', self.label + '=' + self.nonce,
                '--network', self.network_id, '--read-only', '--user', f'{os.getuid()}:{os.getgid()}',
                '--cap-drop=ALL', '--security-opt=no-new-privileges', '--cpus=2', '--memory=768m', '--memory-swap=768m', '--pids-limit=256',
                '--tmpfs', '/tmp:rw,noexec,nosuid,nodev,size=64m',
                '--mount', f'type=bind,src={data},dst=/app/data',
                '--mount', f'type=bind,src={self.work / "archives"},dst=/backups,readonly'] + [x for k in sorted(values) for x in ('-e', k)]

    def reserve(self, label):
        name = 'campusos-release-recovery-' + self.nonce + '-' + str(len(self.containers))
        self.containers[name] = None
        return name

    def inspect(self, name, label):
        template = ('{"id":{{json .Id}},"owner":{{json (index .Config.Labels "' + self.label + '")}},'
                    '"image":{{json .Image}},"running":{{json .State.Running}},"exit":{{json .State.ExitCode}},'
                    '"readonly":{{json .HostConfig.ReadonlyRootfs}},"user":{{json .Config.User}},'
                    '"cap_drop":{{json .HostConfig.CapDrop}},"security":{{json .HostConfig.SecurityOpt}},'
                    '"memory":{{.HostConfig.Memory}},"swap":{{.HostConfig.MemorySwap}},"cpus":{{.HostConfig.NanoCpus}},'
                    '"pids":{{.HostConfig.PidsLimit}},"ports":{{json .NetworkSettings.Ports}},"mounts":{{json .Mounts}},"tmpfs":{{json .HostConfig.Tmpfs}}}')
        value = json.loads(self.run(label, ['docker', 'inspect', '--format=' + template, name]))
        expected = self.containers[name]
        if value['owner'] != self.nonce or value['image'] != self.image_id or (expected and value['id'] != expected):
            raise RuntimeError('container identity/image/ownership changed')
        self.containers[name] = value['id']
        mounts = {m['Destination']: m for m in value['mounts']}
        if not value['readonly'] or value['user'] != f'{os.getuid()}:{os.getgid()}' or os.getuid() == 0 or 'ALL' not in value['cap_drop'] or 'no-new-privileges' not in value['security']:
            raise RuntimeError('non-root readonly privilege contract failed')
        if value['memory'] != 805306368 or value['swap'] != 805306368 or value['cpus'] != 2000000000 or value['pids'] != 256:
            raise RuntimeError('resource limits changed')
        if set(mounts) != {'/app/data', '/backups'} or mounts['/app/data']['Source'] != str(self.work / 'restored/data') or mounts['/backups']['Source'] != str(self.work / 'archives') or mounts['/backups']['RW'] or set(value['tmpfs'] or {}) != {'/tmp'}:
            raise RuntimeError('only owned data and read-only backup mounts allowed')
        return value

    def remove(self, name):
        self.inspect(name, 'container-cleanup-owner-' + str(len(self.isolation)))
        self.run('container-remove-' + name.rsplit('-', 1)[-1], ['docker', 'rm', '-f', '-v', name])
        del self.containers[name]

    def rewrite(self, label, args, extra):
        args = [str(a) for a in args]
        tail = None
        if len(args) == 5 and args[:3] == ['bash', str(self.root / 'scripts/restore.sh'), 'apply']:
            tail = ['--entrypoint', '/app/scripts/docker-restore.sh', self.image_id, '/backups/' + Path(args[3]).name, '--confirm']
        elif len(args) >= 3 and args[:2] == [str(self.native / 'campusosctl'), 'recovery'] and args[2] in ('status', 'review'):
            translated = [a.replace(str(self.work / 'restored/data'), '/app/data') for a in args[1:]]
            tail = ['--entrypoint', '/app/bin/campusosctl', self.image_id] + translated
        if tail is None:
            return None
        values = self.settings(extra)
        name = self.reserve(label)
        return name, ['docker', 'run'] + self.flags(name, values) + tail, values

    def finished(self, name, code):
        value = self.inspect(name, 'tool-attest-' + name.rsplit('-', 1)[-1])
        if value['running'] or value['exit'] != code:
            raise RuntimeError('tool container exit evidence differs')
        self.isolation.append(value)
        self.remove(name)

    def start(self, label, extra, blocked):
        values = self.settings(extra)
        name = self.reserve(label)
        flags = self.flags(name, values)
        if blocked:
            self.run(label + '-server', ['docker', 'run'] + flags + [self.image_id], values, failure=True)
            self.finished(name, 1)
            if 'recovery.quarantined' not in (self.work / (label + '-server.log')).read_text():
                raise RuntimeError('unreviewed Docker API failed for unrelated reason')
            return None
        self.active = name
        self.run(label + '-server-start', ['docker', 'run', '-d'] + flags + ['-p', '127.0.0.1::8080', self.image_id], values)
        value = self.inspect(name, label + '-server-isolation')
        bindings = value['ports']['8080/tcp']
        if len(bindings) != 1 or bindings[0]['HostIp'] != '127.0.0.1' or not value['running']:
            raise RuntimeError('API must be a running owned loopback target')
        self.isolation.append(value)
        port = int(bindings[0]['HostPort'])
        self.ports.append(port)
        expected = {'/app/bin/campusos-server': self.native / 'server', '/app/bin/campusosctl': self.native / 'campusosctl',
                    '/app/entrypoint.sh': self.root / 'deploy/docker/entrypoint.sh',
                    '/app/scripts/docker-restore.sh': self.root / 'deploy/docker/restore-container.sh'}
        import hashlib
        for i, (guest, host) in enumerate(expected.items()):
            actual = self.run(label + '-guest-hash-' + str(i), ['docker', 'exec', name, 'sha256sum', guest]).split()[0]
            if actual != hashlib.sha256(host.read_bytes()).hexdigest():
                raise RuntimeError('current guest binary/script differs from prebuilt source')
        if self.run(label + '-actual-tmpfs', ['docker', 'exec', name, 'stat', '-f', '-c', '%T', '/tmp']) != 'tmpfs':
            raise RuntimeError('guest temporary directory is not actual tmpfs')
        return port

    def stop(self):
        if self.active:
            name = self.active
            self.inspect(name, 'api-stop-owner-' + str(len(self.isolation)))
            self.run('api-log-' + name.rsplit('-', 1)[-1], ['docker', 'logs', name])
            self.remove(name)
            self.active = None
        for name in list(self.containers):
            self.remove(name)
        self.report['owned_containers_removed'] = not self.containers

    def cleanup(self):
        # Called only after the common gate has removed its owned PG.
        if not self.image_id:
            tags = self.run('image-tags-before-cleanup', ['docker', 'image', 'ls', '--format={{.Repository}}:{{.Tag}}']).splitlines()
            if self.image not in tags:
                self.report['owned_image_removed'] = True
            else:
                candidate = json.loads(self.run('image-partial-owner', ['docker', 'image', 'inspect', '--format={"id":{{json .Id}},"owner":{{json (index .Config.Labels "' + self.label + '")}}}', self.image]))
                if candidate['owner'] != self.nonce:
                    raise RuntimeError('refuse foreign partially built image')
                self.image_id = candidate['id']
        if self.image_id:
            value = json.loads(self.run('image-cleanup-owner', ['docker', 'image', 'inspect', '--format={"id":{{json .Id}},"owner":{{json (index .Config.Labels "' + self.label + '")}}}', self.image]))
            if value != {'id': self.image_id, 'owner': self.nonce}:
                raise RuntimeError('refuse cleanup of replaced image')
            self.run('image-remove', ['docker', 'image', 'rm', self.image])
            self.report['owned_image_removed'] = True
        if self.network_id:
            value = json.loads(self.run('network-cleanup-owner', ['docker', 'network', 'inspect', '--format={"id":{{json .Id}},"owner":{{json (index .Labels "' + self.label + '")}},"containers":{{json .Containers}}}', self.network_id]))
            if value['id'] != self.network_id or value['owner'] != self.nonce or value['containers']:
                raise RuntimeError('refuse cleanup of foreign or occupied network')
            self.run('network-remove', ['docker', 'network', 'rm', self.network_id])
            self.report['owned_network_removed'] = True
        base = json.loads(self.run('runtime-base-after', ['docker', 'image', 'inspect', '--format={"id":{{json .Id}}}', 'campusos-api:0.13.0']))
        self.report['borrowed_base_preserved'] = base['id'] == self.base_id
        alias = json.loads(self.run('runtime-alias-owner', ['docker', 'image', 'inspect', '--format={"id":{{json .Id}}}', self.base_tag]))
        if alias['id'] != self.base_id:
            raise RuntimeError('refuse cleanup of replaced base alias')
        self.run('runtime-alias-remove', ['docker', 'image', 'rm', self.base_tag])
        self.report['owned_base_alias_removed'] = True
        self.report['isolation'] = self.isolation
        if not all(self.report[k] for k in ('owned_containers_removed', 'owned_image_removed', 'owned_network_removed', 'borrowed_base_preserved', 'owned_base_alias_removed')):
            raise RuntimeError('owned Docker cleanup or borrowed base invariance failed')
