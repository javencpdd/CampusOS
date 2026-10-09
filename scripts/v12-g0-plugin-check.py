#!/usr/bin/env python3
"""Real v4 plugin/document baseline in the disposable G0 environment."""
import hashlib
import io
import json
import os
import urllib.error
import urllib.parse
import urllib.request
import zipfile
from datetime import datetime, timezone
from pathlib import Path

from v12_g0_http import Client, require

PLUGIN = 'campusos.pdf-viewer'
DOCS = '/api/v1/documents'


def pdf_fixture():
    objects = [b'<< /Type /Catalog /Pages 2 0 R >>',
               b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
               b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>',
               b'<< /Length 37 >>\nstream\nBT /F1 12 Tf 30 200 Td (G0 PDF) Tj ET\nendstream',
               b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>']
    data = b'%PDF-1.4\n'
    offsets = []
    for index, value in enumerate(objects, 1):
        offsets.append(len(data))
        data += f'{index} 0 obj\n'.encode() + value + b'\nendobj\n'
    start = len(data)
    data += b'xref\n0 6\n0000000000 65535 f \n'
    data += b''.join(f'{offset:010d} 00000 n \n'.encode() for offset in offsets)
    return data + f'trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n{start}\n%%EOF\n'.encode()


def docx_fixture(external=False):
    out = io.BytesIO()
    with zipfile.ZipFile(out, 'w') as archive:
        archive.writestr('[Content_Types].xml', '<Types><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml" /></Types>')
        archive.writestr('word/document.xml', '<document><body><p>G0 document</p></body></document>')
        if external:
            archive.writestr('word/_rels/document.xml.rels', '<Relationships><Relationship TargetMode="External" Target="https://example.invalid/template" /></Relationships>')
    return out.getvalue()


def upload(client, name, raw):
    boundary = 'campusos-g0-disposable-upload'
    body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{name}"\r\nContent-Type: application/octet-stream\r\n\r\n'.encode()
            + raw + f'\r\n--{boundary}--\r\n'.encode())
    request = urllib.request.Request(client.base + DOCS + '/upload', data=body,
                                    headers={'Authorization': 'Bearer ' + client.token,
                                             'Content-Type': 'multipart/form-data; boundary=' + boundary})
    try:
        response = client.opener.open(request, timeout=10)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        return response.status, json.load(response)


def main():
    base = os.environ['V12_G0_BASE_URL']
    require(urllib.parse.urlsplit(base).hostname == '127.0.0.1', 'isolated loopback required')
    checks = {}

    def result(name, pair, expected=200):
        status, payload = pair
        require(status == expected, f'{name}: expected HTTP {expected}, got {status}, code={payload.get("code")}')
        require((payload.get('code') == 0) == (status < 400), f'{name}: envelope mismatch')
        encoded = json.dumps(payload)
        require('storage_key' not in encoded and os.environ['V12_G0_WORK_DIR'] not in encoded,
                f'{name}: internal path exposed')
        if status >= 400:
            require(payload.get('data') is None, f'{name}: denied payload contains data')
        checks[name] = {'http_status': status}
        if status >= 400:
            checks[name]['code'] = payload['code']
        return payload.get('data')

    def call(name, client, method, path, expected=200, body=None):
        return result(name, client.request(method, path, body=body), expected)

    def download(name, client, path, content):
        status, headers, raw = client.request_bytes('GET', path)
        require(status == 200 and raw == content and headers.get('Cache-Control') == 'private, no-store',
                f'{name}: private bytes mismatch, HTTP {status}')
        checks[name] = {'http_status': status, 'bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}

    a, b, admin, anonymous = [Client(base) for _ in range(4)]
    for name, client, email, password, route in [
        ('a', a, 'g0-owner-a@example.invalid', os.environ['V12_G0_OWNER_A_PASSWORD'], 'login'),
        ('b', b, 'g0-owner-b@example.invalid', os.environ['V12_G0_OWNER_B_PASSWORD'], 'login'),
        ('admin', admin, 'admin@campusos.local', os.environ['V12_G0_BOOTSTRAP_PASSWORD'], 'admin/login'),
    ]:
        client.token = call('login_' + name, client, 'POST', '/api/v1/auth/' + route,
                            body={'email': email, 'password': password})['access_token']
    # The same public URL must recheck source visibility after a committed edit.
    listed = call('public_list_before_private', anonymous, 'GET', '/api/v1/threads?page=1&page_size=20')
    require(listed['pagination']['total'] == 2, 'unexpected visibility fixture')
    call('author_makes_private', a, 'PUT', '/api/v1/threads/3001', body={'status': 'private'})
    listed = call('public_list_after_private', anonymous, 'GET', '/api/v1/threads?page=1&page_size=20')
    require(listed['pagination']['total'] == 1 and {item['id'] for item in listed['items']} == {'3002'},
            'public list leaked newly private content')
    call('private_detail_after_change', anonymous, 'GET', '/api/v1/threads/3001', 404)
    call('author_restores_public', a, 'PUT', '/api/v1/threads/3001', body={'status': 'published'})
    listed = call('public_list_after_publish', anonymous, 'GET', '/api/v1/threads?page=1&page_size=20')
    require(listed['pagination']['total'] == 2, 'republished thread absent')
    plugin = call('installed_plugin', admin, 'GET', '/api/v1/plugins/' + PLUGIN)
    require(plugin['status'] == 'running', 'installed v4 plugin not running')
    call('anonymous_admin_plugin', anonymous, 'GET', '/api/v1/plugins/' + PLUGIN, 401)
    call('user_cannot_disable_plugin', a, 'POST', '/api/v1/plugins/' + PLUGIN + '/disable', 403)
    overview = call('authorization', admin, 'GET', '/api/v1/plugins/' + PLUGIN + '/authorization')
    version = str(overview['version']['id'])
    capability = 'personal_space_file.self.read'
    consent = '/api/v1/plugin-authorizations/' + PLUGIN + '/versions/' + version + '/consents/'
    grant = '/api/v1/plugins/' + PLUGIN + '/versions/' + version + '/grants/' + capability

    fixtures = {'g0.txt': b'G0 text\n', 'g0.md': b'# G0 markdown\n',
                'g0.campusdoc': b'{"version":1,"blocks":[{"type":"paragraph","text":"G0"}]}',
                'g0.pdf': pdf_fixture(), 'g0.docx': docx_fixture()}
    documents = {}
    for name, raw in fixtures.items():
        data = result('upload_' + name, upload(a, name, raw), 201)
        documents[name] = data
        path = DOCS + '/' + data['id']
        download('download_' + name, a, path + '/download', raw)
        call('cross_owner_' + name, b, 'GET', path + '/download', 404)
        preview = call('preview_' + name, a, 'GET', path + '/preview')
        if name.endswith(('.pdf', '.docx')):
            require(preview['status'] == 'converter_unavailable' and preview['download_available'],
                    'binary converter degradation not explicit')
    for name, raw in [('fake.pdf', b'not a pdf'), ('active.pdf', b'%PDF-1.4\n/JavaScript\n%%EOF'),
                      ('external.docx', docx_fixture(True)), ('html.txt', b'<!doctype html><script>alert(1)</script>')]:
        result('reject_' + name, upload(a, name, raw), 400)

    document = documents['g0.pdf']
    path = DOCS + '/' + document['id']
    call('preview_without_consent', a, 'POST', path + '/pdf-invocations', 403, {'presentation': 'modal'})
    for code in ('personal_space_file.self.read',):
        call('consent_' + code, a, 'PUT', consent + code, body={'status': 'granted', 'scope': {'scope': 'self'}})
    call('cross_owner_create_invocation', b, 'POST', path + '/pdf-invocations', 404, {'presentation': 'modal'})
    invocation = call('create_invocation', a, 'POST', path + '/pdf-invocations', 201, {'presentation': 'modal'})
    inv_path = '/api/v1/plugin-ui/invocations/' + invocation['id']
    call('invocation_metadata', a, 'GET', inv_path)
    download('invocation_bytes', a, inv_path + '/content', fixtures['g0.pdf'])
    call('cross_owner_invocation', b, 'GET', inv_path + '/content', 410)
    call('admin_invocation', admin, 'GET', inv_path + '/content', 410)
    call('revoke_user_consent', a, 'PUT', consent + capability, body={'status': 'revoked', 'scope': {'scope': 'self'}})
    call('old_invocation_after_consent_revoke', a, 'GET', inv_path + '/content', 403)
    download('owner_download_after_consent_revoke', a, path + '/download', fixtures['g0.pdf'])
    call('restore_user_consent', a, 'PUT', consent + capability, body={'status': 'granted', 'scope': {'scope': 'self'}})
    call('revoke_admin_grant', admin, 'PUT', grant, body={'status': 'revoked', 'reason': 'G0 isolation check', 'scope': {'scope': 'self'}})
    call('old_invocation_after_admin_revoke', a, 'GET', inv_path + '/content', 403)
    call('restore_admin_grant', admin, 'PUT', grant, body={'status': 'granted', 'reason': 'G0 restore', 'scope': {'scope': 'self'}})
    call('disable_plugin', admin, 'POST', '/api/v1/plugins/' + PLUGIN + '/disable')
    call('disabled_invocation', a, 'GET', inv_path + '/content', 503)
    download('disabled_plugin_owner_download', a, path + '/download', fixtures['g0.pdf'])
    call('enable_plugin', admin, 'POST', '/api/v1/plugins/' + PLUGIN + '/enable')
    fresh = call('new_invocation_after_enable', a, 'POST', path + '/pdf-invocations', 201, {'presentation': 'modal'})
    download('reenabled_plugin_bytes', a, '/api/v1/plugin-ui/invocations/' + fresh['id'] + '/content', fixtures['g0.pdf'])

    text_doc = documents['g0.txt']
    text_path = DOCS + '/' + text_doc['id']
    saved = call('save_text_revision', a, 'PUT', text_path,
                 body={'expected_version': 1, 'name': 'g0.txt', 'content': 'G0 second revision'})
    require(saved['version'] == 2, 'save revision mismatch')
    call('stale_text_save', a, 'PUT', text_path, 409,
         {'expected_version': 1, 'name': 'g0.txt', 'content': 'must not save'})
    restored = call('restore_historical_version', a, 'POST', text_path + '/versions/' + text_doc['current_version_id'] + '/restore',
                    body={'expected_version': 2})
    require(restored['version'] == 3, 'restore version mismatch')
    download('restored_historical_bytes', a, text_path + '/download', fixtures['g0.txt'])

    report = json.loads(Path(os.environ['V12_G0_PUBLIC_EVIDENCE']).read_text())
    report['schema'] = 'campusos.v12-g0-plugin-documents/v1'
    report['generated_at'] = datetime.now(timezone.utc).isoformat()
    report['public_visibility_checks'] = report.pop('checks')
    report['checks'] = checks
    report['fixture'].update({'plugin': PLUGIN, 'plugin_version': '2.0.0-dev.2', 'uploaded_documents': len(documents),
                              'formats': ['text', 'markdown', 'campusdoc', 'pdf', 'docx']})
    report['known_limits'] = ['v4 uses existing authorization adapter', 'binary converter unavailable', 'not v5 lifecycle acceptance']
    Path(os.environ['V12_G0_OUTPUT']).write_text(json.dumps(report, indent=2, ensure_ascii=False) + '\n')
    # Only this disposable directory receives identifiers needed by the browser.
    Path(os.environ['V12_G0_WORK_DIR'], 'browser-fixture.json').write_text(json.dumps({'documents': documents}))
    print(f'G0 plugin/document checks passed: {len(checks)}')


if __name__ == '__main__':
    main()
