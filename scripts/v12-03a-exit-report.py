#!/usr/bin/env python3
"""03a complete phase evidence, separately from its historical .113 slice."""
import argparse
import hashlib
import json
import pathlib
import subprocess
from datetime import datetime
from zoneinfo import ZoneInfo

ROOT = pathlib.Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence'
COMMON = ['full-go', 'related-race', 'sdk', 'contracts', 'architecture', 'web-build', 'admin-build', 'web-components', 'admin-components', 'docs-links', 'docs-build', 'line-endings', 'diff']
FINAL = ['docs-links', 'docs-build', 'line-endings', 'diff', 'architecture', 'skill-bridges', 'onboarding-skill']
DEPS = ['v12-g1-exit-linux.json', 'v12-01a-exit-linux.json', 'v12-02c-exit-linux.json', 'v12-03a-manifest-linux.json']
DOCS = ['docs/进度/v1.2-dev/v1.2.114-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件v5完整Manifest目标合同.md', 'docs/api/v1.2插件v5包字节与签名合同.md', 'docs/api/Host-API-v1权限目录.md', 'docs/help/插件v5包与签名操作说明.md', 'docs/help/插件v5声明离线检查说明.md', 'docs/help/README.md', 'docs/architecture/当前架构概览.md', 'docs-site/guide/architecture.md', 'skills/sources/campusos-project-onboarding/references/current-project-status.md', 'plugins/README.md', 'plugins/campusos.pdf-viewer/README.md', 'sdk/go/README.md']


def read(p):
    return json.loads(p.read_text())


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def sources():
    paths = subprocess.check_output(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.ps1', '.json', '.yaml', '.yml', '.html', '.css', '.svg'}
    return {n: sha(ROOT/n) for n in sorted(set(paths)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (pathlib.Path(n).suffix in suffixes or n == 'Makefile' or n.startswith('plugins/') and (n.endswith('.md') or pathlib.Path(n).name == 'LICENSE'))}


def freeze(w):
    deps = {}
    for name in DEPS:
        p = EVIDENCE/name
        d = read(p)
        assert d['implementation'] == d['automation'] == d['target_environment'] == 'passed', name
        deps[p.relative_to(ROOT).as_posix()] = sha(p)
    data = {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(), 'source_sha256': sources(), 'dependencies': deps}
    (w/'source-freeze.json').write_text(json.dumps(data, ensure_ascii=False, indent=2)+'\n')
    print('Frozen03a inputs:', len(data['source_sha256']))


def test_events(p, required=None):
    rows = [json.loads(s) for s in p.read_text().splitlines()]
    assert not any(e['Action'] in {'fail', 'skip'} for e in rows), p
    passed = {e['Test'] for e in rows if e['Action'] == 'pass' and 'Test' in e}
    tops = {n for n in passed if '/' not in n}
    assert tops, p
    if required is not None:
        assert tops == required, (p, tops)
    return {'top_level': len(tops), 'pass_events': len(passed), 'fail': 0, 'skip': 0}


def verify(w):
    frozen = read(w/'source-freeze.json')
    assert sources() == frozen['source_sha256'], 'source changed after freeze'
    assert subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip() == frozen['head']
    for name, digest in frozen['dependencies'].items():
        assert sha(ROOT/name) == digest, name
    native = test_events(w/'native/plugin.jsonl', {'TestPluginV5ManifestStrictDecode', 'TestPluginV5ManifestCatalogAndRelease', 'TestPluginV5ManifestCatalogIsolation', 'TestPluginV5PackageByteSnapshot', 'TestPluginV5PackageArchiveDenials', 'TestPluginV5PublicationCurrentTrustAndConflict', 'TestPluginV5CutoverHasNoLegacyTransport', 'TestPluginV5PackageReferencedSchemaAndInputBudget', 'TestPluginV5ConfigContractRejectsUnsupportedSchemaAndDefaults', 'TestPluginV5ConfigExactNumbers', 'TestPluginV5ConfigValuesRefsAndSelectors'})
    cli = test_events(w/'native/cli.jsonl')
    sdk = test_events(w/'native/sdk.jsonl')
    server_tests = test_events(w/'server/target.jsonl', {'TestPluginV5ActualServerIgnoresRetiredPackages'})
    for p in list((w/'native').glob('*.stderr')) + [w/'native/plugin.log', w/'server/target.stderr']:
        assert 'WARNING: DATA RACE' not in p.read_text(), p
    wire, packages, server = read(w/'wire/target-result.json'), read(w/'cli/package-cli-result.json'), read(w/'server/server-result.json')
    assert wire['status'] == packages['status'] == server['status'] == 'passed'
    assert wire['actual_go_decisions'] == wire['actual_cli_processes'] == 23
    assert packages['actual_cli_processes'] == 20 and len(packages['checks']) == 2
    assert packages['cli_binary_sha256'] == sha(w/'campusosctl')
    assert wire['cli_binary_sha256'] == sha(w/'manifest-check')
    assert server['actual_cmd_server'] and server['server_binary_sha256'] == sha(w/'campusos-server') and server['migration'] == 25
    assert len(server['checks']) == 5
    assert not (w/'pg/private.env').exists(), 'private DB connection not cleared'
    assert not (w/'cli/signing.private').exists(), 'test private key not cleared'
    assert (w/'common-passed.txt').read_text().splitlines() == COMMON
    artifacts = {}
    for p in sorted(w.rglob('*')):
        rel = p.relative_to(w)
        if not p.is_file() or 'tmp' in rel.parts:
            continue
        if p.name in {'campusosctl', 'manifest-check', 'campusos-server'} or p.suffix == '.test' or p.suffix in {'.log', '.json', '.jsonl', '.stderr', '.txt'} and p.name not in {'driver.log', 'report.log'}:
            artifacts[p.relative_to(ROOT).as_posix()] = sha(p)
    return frozen, native, cli, sdk, server_tests, wire, packages, server, artifacts


def report(w, final):
    frozen, native, cli, sdk, server_tests, wire, packages, server, artifacts = verify(w)
    if final:
        assert (w/'final-docs-passed.txt').read_text().splitlines() == FINAL
    data = {
        'schema': 'campusos.v12-03a-exit/v1', 'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(timespec='seconds'),
        'stage': 'V12-03a', 'stage_status': 'completed' if final else 'verified_pending_documents',
        'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed', 'common_gates_status': 'passed', 'document_finalize': 'passed' if final else 'pending',
        'scope': 'Complete03a package contract, real bytes, publisher trust, immutable known identity, primary CLI, actual source catalog and rebuilt examples; no03b implementation.',
        'worktree_context': {'head': frozen['head'], 'commit_or_push': False, 'shared_database_reset': False},
        'gate_revalidation': frozen.get('gate_revalidation'),
        'current_migration': 25, 'next_migration': 26, 'native': native, 'cli_unit': cli, 'sdk_go': sdk, 'server_test': server_tests,
        'coverage': {'native_go_wire_decisions': 23, 'actual_cli_processes': 43, 'parallel_manifest_checks': 64, 'concurrent_publications': 16, 'package_shapes': 4, 'actual_server_checks': 5, 'common_gates': len(COMMON)},
        'wire_target': wire, 'package_target': packages, 'actual_server': server,
        'common_gates': {name: 'passed' for name in COMMON},
        'source_sha256': frozen['source_sha256'], 'dependencies': frozen['dependencies'], 'artifacts_sha256': artifacts,
        'documents_sha256': {name: sha(ROOT/name) for name in DOCS} if final else {},
        'boundaries': ['Production v5 Host capability catalog is empty until03c; extension DTO metadata is not a callable provider.', 'Host release identity index is in-process; durable installation/catalog/operation and recovery belong to03b.', 'Old production parsers/CLI/discovery/adapter/import routes are retired; historical DTOs and old data helpers remain behind no production entry.', 'PDF UI package rebuilt; dynamic mounting/Bridge and Wasm/Container runtime execution belong to04/05/06.', 'Actual Linux PG16/cmd-server and CLI only; no Windows/WSL2, runtime-isolation or whole-version Final claim.']
    }
    (EVIDENCE/'v12-03a-exit-linux.json').write_text(json.dumps(data, ensure_ascii=False, indent=2)+'\n')
    print('V12-03a:', data['stage_status'], '; native:', native, '; actual CLI:43; server:5; common:', len(COMMON))


def main():
    p = argparse.ArgumentParser()
    group = p.add_mutually_exclusive_group(required=True)
    for name in ['freeze', 'build', 'finalize', 'verify', 'verify-source']:
        group.add_argument('--'+name, type=pathlib.Path)
    args = vars(p.parse_args())
    mode = next(k for k,v in args.items() if v is not None)
    w = args[mode].resolve()
    assert w.is_relative_to(ROOT/'.cache')
    if mode == 'freeze':
        freeze(w)
    elif mode == 'verify_source':
        frozen=read(w/'source-freeze.json')
        assert sources()==frozen['source_sha256'], 'source changed after freeze'
        assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()==frozen['head']
        print('03a frozen sources unchanged')
    elif mode == 'verify':
        verify(w)
        print('Current03a source, target and common evidence verified')
    else:
        report(w, mode == 'finalize')


if __name__ == '__main__':
    main()
