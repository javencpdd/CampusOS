#!/usr/bin/env python3
"""Build and verify current v5 author packages without installation or execution."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent
OWNED = ROOT / '.cache/v5-release-authors'


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def sources():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    return {name: sha(ROOT / name) for name in sorted(set(names)) if name and (ROOT / name).is_file()
            and not name.endswith('.md') and not name.startswith('docs/项目计划书v1/项目计划v1.2/evidence/')}


def main(work):
    if sys.platform != 'linux':
        raise RuntimeError('this author-build gate requires native Linux; Windows is not certified here')
    if work is None:
        OWNED.mkdir(parents=True, exist_ok=True)
        work = Path(tempfile.mkdtemp(prefix='acceptance-', dir=OWNED))
    else:
        work = work.resolve()
        if not work.is_relative_to(OWNED) or work == OWNED:
            raise RuntimeError('work must be a new directory below .cache/v5-release-authors')
        work.mkdir(parents=True, exist_ok=False)
    work.chmod(0o700)
    frozen = sources()
    (work / 'source-freeze.json').write_text(json.dumps(frozen, indent=2) + '\n')
    env = {key: os.environ[key] for key in ('PATH', 'HOME') if key in os.environ}
    env.update(GOCACHE=os.environ.get('GOCACHE', '/tmp/campusos-go-cache'), GOMAXPROCS='2',
               GOFLAGS='-buildvcs=false', CAMPUSOS_SKIP_DOTENV='true')
    commands, images, packages, checks = [], [], {}, []
    private = work / 'signing-private.b64'
    cleanup = {'errors': [], 'private_key_removed': False, 'owned_image_tags_removed': False}

    def run(name, args, extra=None, error=None):
        begin = time.monotonic()
        result = subprocess.run([str(x) for x in args], cwd=ROOT, env=env | (extra or {}),
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        (work / (name + '.log')).write_bytes(result.stdout)
        commands.append({'name': name, 'args': [str(x) for x in args], 'exit_code': result.returncode,
                         'expected_error': error, 'seconds': round(time.monotonic() - begin, 3)})
        output = result.stdout.decode(errors='replace')
        if error is None:
            if result.returncode != 0:
                raise RuntimeError(name + ' failed; inspect its retained log')
        elif result.returncode == 0 or error not in output:
            raise RuntimeError(name + ' did not reject with the expected public error')
        return output

    def cli(name, *args, error=None):
        output = run(name, [work / 'campusosctl', 'plugin', *args], error=error)
        return None if error else json.loads(output)

    def verify_package(key, directory):
        valid = cli(key + '-validate', 'validate', directory)
        archive, repeated, release = work / (key + '.tar.gz'), work / (key + '-repeat.tar.gz'), work / (key + '.release.json')
        packed = cli(key + '-pack', 'pack', directory, '--out', archive)
        cli(key + '-repeat', 'pack', directory, '--out', repeated)
        if archive.read_bytes() != repeated.read_bytes() or valid['package_digest'] != packed['package_digest']:
            raise RuntimeError('package capture is not deterministic')
        cli(key + '-sign', 'sign', archive, '--key-id', 'owned-release', '--key-file', private, '--out', release)
        verified = cli(key + '-verify', 'verify', archive, '--release', release, '--trust', work / 'trust.json')
        if verified['api_version'] != 'campusos.plugin/v5' or not verified['signature_verified'] or verified['installed']:
            raise RuntimeError('package signature proof is incomplete')
        original = sha(archive)
        cli(key + '-exclusive-output', 'pack', directory, '--out', archive, error='plugin.output_exists_or_unavailable')
        if sha(archive) != original:
            raise RuntimeError('existing package output was overwritten')
        cli(key + '-revoked', 'verify', archive, '--release', release, '--trust', work / 'revoked-trust.json', error='plugin.package_signature_invalid')
        packages[key] = verified
        checks.append(key + ': independent build, deterministic capture, actual CLI sign/verify, exclusive output and revoked trust')

    try:
        run('build-cli', ['go', 'build', '-o', work / 'campusosctl', './cmd/campusosctl'])
        binary = sha(work / 'campusosctl')
        toolchain = run('go-toolchain', ['go', 'env', 'GOROOT']).strip()
        run('node-version', ['node', '--version'])
        run('pnpm-version', ['pnpm', '--version'])
        docker_platform = run('docker-version', ['docker', 'version', '--format', '{{.Server.Os}}/{{.Server.Arch}}']).strip()
        if docker_platform != 'linux/amd64':
            raise RuntimeError('this fixed Linux/amd64 author-build gate requires a matching Docker engine')
        run('current-declarations', ['go', 'run', './cmd/campusos-plugin-v5-check', '-root', 'plugins'])
        run('signing-key', ['node', '-e', """
const {generateKeyPairSync}=require('node:crypto'),{writeFileSync}=require('node:fs');
const {privateKey,publicKey}=generateKeyPairSync('ed25519'),pub=publicKey.export({format:'der',type:'spki'}).subarray(-32);
const seed=privateKey.export({format:'der',type:'pkcs8'}).subarray(-32),root=process.argv[1];
writeFileSync(root+'/signing-private.b64',Buffer.concat([seed,pub]).toString('base64'),{mode:0o600});
writeFileSync(root+'/trust.json',JSON.stringify({contract:'campusos.plugin-trust/v1',keys:[{publisher:'campusos',key_id:'owned-release',public_key:pub.toString('base64'),status:'active'}]})+'\\n');
writeFileSync(root+'/revoked-trust.json',JSON.stringify({contract:'campusos.plugin-trust/v1',keys:[]})+'\\n');
""", work])

        # Build in an exclusive copy. Never rewrite the tracked PDF manifest or
        # its digests, and never rebuild a package during validation/install.
        pdf = work / 'pdf'
        pdf.mkdir()
        frontend = ROOT / 'plugins/campusos.pdf-viewer/frontend'
        shutil.copytree(frontend, pdf / 'frontend', ignore=shutil.ignore_patterns('node_modules', 'dist', '.vite'))
        (pdf / 'frontend/node_modules').symlink_to((frontend / 'node_modules').resolve(), target_is_directory=True)
        run('pdf-build', ['pnpm', '--dir', pdf / 'frontend', 'build'])
        manifest = json.loads((ROOT / 'plugins/campusos.pdf-viewer/plugin.json').read_text())
        for ui in manifest['ui']:
            ui['digest'] = 'sha256:' + sha(pdf / ui['entrypoint'])
        (pdf / 'plugin.json').write_text(json.dumps(manifest, indent=2) + '\n')
        verify_package('pdf-viewer', pdf)
        ui = pdf / manifest['ui'][0]['entrypoint']
        ui.write_bytes(ui.read_bytes() + b'<!--tamper-->')
        cli('pdf-ui-tamper', 'validate', pdf, error='plugin.package_digest_mismatch')
        checks.append('modified UI bytes cannot reuse the declared UI digest')

        author_cache = OWNED / 'go-cache'
        for key in ('chat-echo', 'knowledge-reader', 'ai-knowledge'):
            author = work / key
            run(key + '-build', ['bash', 'scripts/build-v5-' + key + '.sh', '--out', author],
                {'CAMPUSOS_AUTHOR_GO': str(Path(toolchain) / 'bin/go'), 'CAMPUSOS_AUTHOR_GOCACHE': str(author_cache)})
            proof = json.loads((author / 'author-build.json').read_text())
            if not proof['offline'] or proof['host_module_used'] or proof['plugin_executed']:
                raise RuntimeError('author build used a host module or executed a plugin')
            for name, digest in proof['files_sha256'].items():
                if sha(author / name) != digest:
                    raise RuntimeError('author build proof changed')
            tag = 'localhost/campusos-release-' + key + ':' + secrets.token_hex(12)
            images.append(tag)
            run(key + '-image', ['docker', 'build', '--pull=false', '--network=none', '--tag', tag, author / 'image'])
            refs = json.loads(run(key + '-digest', ['docker', 'image', 'inspect', '--format={{json .RepoDigests}}', tag]))
            if len(refs) != 1 or not re.fullmatch(re.escape(tag.split(':')[0]) + r'@sha256:[0-9a-f]{64}', refs[0]):
                raise RuntimeError('actual local RepoDigest is required; tags, IDs and guessed digests cannot substitute')
            image_platform = run(key + '-image-platform', ['docker', 'image', 'inspect', '--format={{.Os}}/{{.Architecture}}', tag]).strip()
            if image_platform != 'linux/amd64':
                raise RuntimeError('image metadata must match the independently built Linux/amd64 ELF')
            directory = author / 'package-template'
            manifest = json.loads((directory / 'plugin.json').read_text())
            manifest['backend']['artifact']['image'] = refs[0]
            (directory / 'plugin.json').write_text(json.dumps(manifest, indent=2) + '\n')
            verify_package(key, directory)
            manifest['backend']['artifact']['image'] = tag
            (directory / 'plugin.json').write_text(json.dumps(manifest, indent=2) + '\n')
            cli(key + '-floating-image', 'validate', directory, error='plugin.manifest_invalid')
            checks.append(key + ': actual Linux/amd64 offline image RepoDigest required; floating tag rejected')
        cli('legacy-command-denied', 'dev', ROOT / 'examples/plugins/campus-welcome', '--json', error='plugin.legacy_protocol_removed')
        checks.append('legacy plugin dev remains retired; no legacy example output cleanup')
        if sources() != frozen or sha(work / 'campusosctl') != binary:
            raise RuntimeError('host inputs or the prebuilt CLI changed during the gate')
        checks.append('host source/dependencies and the prebuilt CLI remain unchanged')
    finally:
        private.unlink(missing_ok=True)
        cleanup['private_key_removed'] = not private.exists()
        for i, tag in enumerate(images):
            try:
                run('cleanup-image-' + str(i), ['docker', 'image', 'rm', tag])
            except Exception as error:
                cleanup['errors'].append(str(error))
        if images and not cleanup['errors']:
            present = run('remaining-image-tags', ['docker', 'image', 'ls', '--format={{.Repository}}:{{.Tag}}']).splitlines()
            cleanup['owned_image_tags_removed'] = all(tag not in present for tag in images)
        (work / 'commands.json').write_text(json.dumps(commands, indent=2) + '\n')
        (work / 'cleanup.json').write_text(json.dumps(cleanup, indent=2) + '\n')
    if cleanup['errors'] or not cleanup['private_key_removed'] or not cleanup['owned_image_tags_removed']:
        raise RuntimeError('owned cleanup incomplete')
    report = {'schema': 'campusos.v5-release-authors/v1', 'status': 'passed',
              'implementation': 'passed', 'automation': 'passed', 'target_environment': 'native_Linux_actual_author_builds_Docker_RepoDigests_and_CLI',
              'source_sha256': frozen, 'cli_sha256': binary, 'packages': packages,
              'checks': checks, 'check_count': len(checks), 'commands': commands, 'cleanup': cleanup,
              'plugin_installed': False, 'plugin_executed': False, 'database_used': False,
              'runtime_or_browser_or_full_release_certification': False,
              'artifact_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*'))
                                  if p.is_file() and not p.is_relative_to(work / 'knowledge-reader/go-cache')
                                  and not p.is_relative_to(work / 'pdf/frontend/node_modules')}}
    (work / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'status': 'passed', 'packages': list(packages), 'checks': len(checks), 'report': str(work / 'report.json')}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', type=Path, help='new exclusive path below .cache/v5-release-authors')
    main(parser.parse_args().work)
