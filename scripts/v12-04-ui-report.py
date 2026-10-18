#!/usr/bin/env python3
"""Seal full04 only after current-source actual-server and common gates pass."""
import hashlib
import json
import pathlib
import re
import subprocess
import sys
from datetime import datetime
from zoneinfo import ZoneInfo

ROOT = pathlib.Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence'
COMMON = ['sdk-build', 'web-components', 'web-format', 'web-build', 'admin-components', 'admin-build', 'full-go', 'affected-race', 'contracts', 'architecture', 'readme', 'docs-links', 'docs-build', 'docker-dev', 'docker-deploy', 'lan-contract', 'line-endings', 'diff']
FINAL = ['docs-links', 'docs-build', 'architecture', 'line-endings', 'diff']
DOCS = ['README.md', 'docs/README.md', 'docs/进度/v1.2-dev/v1.2.121-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件配置Schema与Secret分流合同.md', 'docs/api/v1.2插件UI宿主与Bridge合同.md', 'docs/api/v1.2插件v5安装与操作合同.md', 'docs/help/插件v5包与签名操作说明.md', 'docs-site/plugins/manifest.md', 'docs-site/plugins/overview.md', 'docs-site/plugins/frontend-runtime.md', 'docs-site/plugins/authorization-v3.md', 'docs-site/deployment/configuration.md', 'docs-site/deployment/docker-development.md', 'docs/help/系统设计相关/v0.13 Docker跨平台部署、迁移与开发指南.md']


def sha(p):
    h = hashlib.sha256()
    with p.open('rb') as f:
        for chunk in iter(lambda: f.read(1 << 20), b''): h.update(chunk)
    return h.hexdigest()


def sources():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.mts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.json', '.yaml', '.yml', '.html', '.css', '.png', '.svg'}
    return {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (pathlib.Path(n).suffix in suffixes or n == 'Makefile')}


def snapshot():
    return {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT).decode().strip(), 'source_sha256': sources(), 'dependencies': {str(p.relative_to(ROOT)): sha(p) for p in [EVIDENCE/n for n in ['v12-03c-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-04-config-ui-linux.json']]}}


def main():
    mode, work = sys.argv[1], pathlib.Path(sys.argv[2]).resolve()
    assert work.parent == ROOT/'.cache/v12-04-ui' and work.name.startswith('acceptance.')
    freeze = work/'source-freeze.json'
    current = snapshot()
    if mode == 'freeze':
        assert not freeze.exists()
        freeze.write_text(json.dumps(current, ensure_ascii=False, indent=2)+'\n'); return
    assert json.loads(freeze.read_text()) == current, 'source, HEAD or accepted dependency changed after freeze'
    if mode == 'verify': return
    assert mode == 'seal'
    rows = [json.loads(line) for line in (work/'native/target.jsonl').read_text().splitlines()]
    assert not any(row['Action'] in {'fail', 'skip'} for row in rows)
    assert {row['Test'] for row in rows if row['Action'] == 'pass' and 'Test' in row} == {'TestPluginV5UIActualServerBrowser'}
    assert any(row['Action'] == 'pass' and 'Test' not in row for row in rows)
    stderr = (work/'native/target.stderr').read_text().splitlines()
    assert len(stderr) == 2 and all(re.fullmatch(r'\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} (default category ready|bootstrap administrator ready \(administrator created using configured bootstrap secret\); credentials were not logged)', line) for line in stderr)
    assert 'DATA RACE' not in (work/'native/target.log').read_text()
    browser = json.loads((work/'browser/browser-result.json').read_text())
    server = json.loads((work/'browser/server-result.json').read_text())
    assert browser['status'] == server['status'] == 'passed' and browser['mocked_api'] is False and browser['actual_cmd_server'] is True
    assert not browser['browser_errors'] and len(browser['checks']) >= 49 and browser['pdf_presentations'] == 24 and browser['diagnostic_tail_only'] is False
    for required in ['actual generic User UI', 'actual Admin operation query', 'elapsed real Grant expiry', 'expired own installation', 'late real range response', 'parent CSP blocks', 'no active plugin']:
        assert any(required in check for check in browser['checks']), required
    assert server['actual_pg_configuration'] is True and server['typed_grant_audits'] >= 3
    assert server['server_binary_sha256'] == sha(work/'native/campusos-server')
    assert 'Linux' in (work/'environment.txt').read_text() and (work/'native/binaries.txt').read_text().count('ELF 64-bit') == 2
    assert 160000 <= int((work/'pg-version.txt').read_text()) < 170000
    assert 'campusos-v12-04-ui-' in (work/'cleanup.log').read_text() and 'Error' not in (work/'cleanup.log').read_text()
    assert (work/'common-passed.txt').read_text().splitlines() == COMMON
    assert (work/'final-passed.txt').read_text().splitlines() == FINAL
    builds = [p for directory in ['admin/dist', 'web/dist', 'plugins/campusos.pdf-viewer/dist', 'sdk/typescript/dist'] for p in (ROOT/directory).rglob('*') if p.is_file()]
    report = {'schema': 'campusos.v12-04-ui-exit/v1', 'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(), 'stage': 'V12-04', 'stage_status': 'completed', 'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed', 'native': {'top_level': 1, 'fail': 0, 'skip': 0}, 'browser': browser, 'server': server, 'migration': {'current': 28, 'next': 29, 'changed': False}, 'common_gates': {n: 'passed' for n in COMMON}, 'final_document_gates': {n: 'passed' for n in FINAL}, **current, 'build_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(builds)}, 'artifacts_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file()}, 'documents_sha256': {n: sha(ROOT/n) for n in DOCS}, 'boundaries': ['Current independent User/Admin identity and permissions, browser CSRF, Admin MFA, immutable declarations, exact CAS and required audit authorize changes.', 'Compiled current-fact selector/ref ports fail closed when no production catalog is bound; Profile/Knowledge business catalogs remain14a/09. No raw Secret or implicit binding removal.', 'UI-only availability is independent of backend observation. No fake ready Runner; real Wasm/Container and hard resource isolation remain05/06.', 'Per-proof isolated subdomains, exact parent frame CSP and current asset/Bridge/resource checks; no JWT, API URL, host path or Secret crosses the plugin boundary.', 'Actual Linux/PG16/Chrome and Node gateway acceptance; Docker configuration/launcher contracts checked, shared development stack not restarted; no Windows runtime claim.', 'Historical120 and prerequisite reports are not rewritten; no new migration, shared database reset, commit or push.'], 'commit_or_push': False, 'shared_database_reset': False}
    (EVIDENCE/'v12-04-ui-exit-linux.json').write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('sealed full04:', len(browser['checks']), 'actual browser checks; 24 PDF presentations; 18 common gates')


if __name__ == '__main__': main()
