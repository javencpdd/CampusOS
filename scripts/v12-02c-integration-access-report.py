#!/usr/bin/env python3
"""Verify bounded native evidence; never promote the whole Integration stage."""
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import platform
import re
import subprocess
import sys

work = Path(sys.argv[1]).resolve()
mode = sys.argv[2] if len(sys.argv) > 2 else ''
assert mode in ['', '--target', '--finalize']
report_path = Path((work/'report-path.txt').read_text().strip())
sha = lambda p: hashlib.sha256(Path(p).read_bytes()).hexdigest()
freeze = json.loads((work/'source-freeze.json').read_text())
for path, wanted in freeze.items():
    assert sha(path) == wanted, ('source changed during acceptance', path)
paths = subprocess.check_output(['git', 'ls-files', '--cached', '--others', '--exclude-standard'], text=True).splitlines()
extensions = {'.go','.ts','.mts','.vue','.mjs','.py','.sh','.ps1','.sql','.yaml','.json','.mod','.sum','.toml','.lock','.css','.html'}
prefixes = ('internal/','cmd/','pkg/','admin/src/','admin/tests/','web/src/','web/tests/','migrations/','modules/','scripts/','sdk/','packages/','schemas/','contracts/','skills/sources/')
configs = {'Makefile','go.mod','go.sum','admin/package.json','admin/pnpm-lock.yaml','admin/vite.config.ts','admin/vitest.config.ts','admin/tsconfig.json','web/package.json','web/pnpm-lock.yaml','web/vite.config.ts','web/vitest.config.ts','web/tsconfig.json'}
actual = {p for p in paths if Path(p).is_file() and (p in configs or p.startswith(prefixes) and Path(p).suffix in extensions)}
assert actual == set(freeze), ('source inventory changed', actual ^ set(freeze))
targets = {
    'native-port': ['TestIntegrationAccessCodecExactSchemaAndSafeIntegers', 'TestIntegrationAccessCodecRejectsAmbiguousInput', 'TestIntegrationAccessTypedValidationBounds', 'TestIntegrationAccessCloneDetachedInventories'],
    'native-policy': ['TestIntegrationAccessExactRulesNoCrossRuleSplicing', 'TestIntegrationAccessDomainsLifecycleEndpointAndScope', 'TestIntegrationAccessCurrentFactsClockAndForgedDecision', 'TestIntegrationAccessParallelAndDetachedDecisions', 'TestIntegrationAccessRuleWindowAndResourceKind', 'TestIntegrationAccessIndependentWireCorpus'],
}
logs = {}
for state in sorted((work/'check-state').glob('*.exit_code')):
    assert state.read_text().strip() == '0', state
    name = state.name.removesuffix('.exit_code')
    path = work/(name+'.log')
    logs[name] = {'status':'passed', 'exit_code':0, 'local_log':str(path), 'log_sha256':sha(path)}
for name, tests in targets.items():
    assert name in logs
    text = Path(logs[name]['local_log']).read_text()
    assert not re.search(r'^\s*--- (?:FAIL|SKIP):', text, re.M), name
    assert 'WARNING: DATA RACE' not in text
    passed = re.findall(r'^--- PASS: (\w+)', text, re.M)
    assert sorted(passed) == sorted(tests), (name, passed)
oracle = json.loads((work/'native-oracle.json').read_text())
assert oracle['status'] == 'passed' and oracle['fixture_count'] == oracle['actual_native_go_decisions'] == 192
assert oracle['matrix_combinations'] == 144
for path, wanted in oracle['input_sha256'].items():
    assert sha(path) == wanted, path
for name in ['contract', 'build-port', 'build-policy', 'verify-oracle']:
    assert name in logs
common = ['related-race', 'go-all', 'sdk', 'contracts', 'architecture', 'docs-build', 'docs-links', 'line-endings', 'diff']
if mode != '--target':
    assert set(common) <= logs.keys(), ('missing common checks', set(common)-logs.keys())
dependencies = {}
for name in ['v12-01b-exit-linux.json','v12-02a-exit-linux.json','v12-02d-exit-linux.json','v12-02b-exit-linux.json']:
    path = Path('docs/项目计划书v1/项目计划v1.2/evidence')/name
    previous = json.loads(path.read_text())
    assert previous['stage_status'] == 'completed'
    assert all(previous[k] == 'passed' for k in ['implementation','automation','target_environment'])
    dependencies[str(path)] = {'sha256':sha(path), 'role':'Accepted predecessor bytes; validated before this slice. Historical source snapshots are not a claim about later edits.'}
versions = [int(p.name.split('_')[0]) for p in Path('migrations').glob('*.up.sql')]
assert max(versions) == 24
artifacts = [work/'source-freeze.json', work/'integration-port.test', work/'integration-policy.test', work/'go-output.json', work/'native-oracle.json', work/'preflight.json']
documents = ['docs/进度/v1.2-dev/v1.2.111-dev.md', 'docs/api/v1.2Integration请求白名单策略合同.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/architecture/当前架构概览.md', 'docs-site/guide/architecture.md', 'skills/sources/campusos-project-onboarding/references/current-project-status.md']
if mode == '--finalize':
    states = sorted((work/'final-docs').glob('*.exit_code'))
    assert len(states) == 7
    for state in states:
        assert state.read_text().strip() == '0', state
        path = state.with_name(state.name.removesuffix('.exit_code')+'.log')
        logs['final-'+path.stem] = {'status':'passed', 'exit_code':0, 'local_log':str(path), 'log_sha256':sha(path)}
    assert (work/'.exit_code').read_text().strip() == '0'
report = {
    'schema':'campusos.v12-02c-integration-access-acceptance/v1',
    'generated_at':datetime.now(timezone.utc).isoformat(), 'stage':'V12-02c', 'stage_status':'partial',
    'slice_status':'completed' if mode != '--target' else 'target_passed_common_pending',
    'implementation':'passed', 'automation':'passed', 'target_environment':'passed',
    'common_gates_status':'passed' if mode != '--target' else 'pending',
    'document_finalize':'passed' if mode == '--finalize' else 'pending',
    'scope':'Host-only Integration whitelist Policy/strict codec, complete same-call decision/current-fact binding and Schema/SDK; public-collection source-read eligibility only.',
    'environment':{'os':platform.system(),'architecture':platform.machine(),'go':subprocess.check_output(['go','version'],text=True).strip(),'node':subprocess.check_output(['node','--version'],text=True).strip(),'target':'Actual Linux race-enabled compiled binaries run with env -i. No production ingress, credential verifier, PostgreSQL, browser, Windows, network isolation or release acceptance claim.'},
    'coverage':{'required_native_top_level_tests':10,'independent_oracle_outputs':192,'domain_status_credential_channel_combinations':144,'parallel_callers':64,'reason_counts':oracle['reason_counts']},
    'checks':{test:{'status':'passed','log':group}for group,tests in targets.items()for test in tests},
    'logs':logs,'source_sha256':freeze,'contract_input_sha256':oracle['input_sha256'],'artifacts_sha256':{str(p):sha(p)for p in artifacts},
    'documents_sha256':{p:sha(p)for p in documents} if mode == '--finalize' else {},
    'dependencies':dependencies,'current_migration':24,'next_migration':25,'schema_changed':False,
    'worktree_context':{'preflight_head':(work/'head.txt').read_text().strip(),'agent_git_mutation':False,'frozen_source_files':len(freeze)},
    'boundaries':[
        'Principal/endpoint/rule facts are host-only trusted inputs. Parsing does not authenticate any raw key or grant access to original content.',
        'Only explicit Integration HTTP/MCP channels and knowledge.source.read/public_collection are eligible; original-module current ACL remains mandatory.',
        'Facts-version currency and non-reuse must be supplied by future real stores; this pure component does not prove persistent ABA safety.',
        'No business migration, Memory/PG credential store, raw-credential issuer/verifier, production Integration route or persistent rotation/revocation implemented by this slice.',
        'The whole V12-02c remains partial. Actual authentication, managed lifecycle, route whitelist and original-resource composition need later slices.',
    ],
}
report_path.parent.mkdir(parents=True, exist_ok=True)
report_path.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
print('Verified10 native tests/192 actual Go decisions; slice',report['slice_status'],'whole02c partial; docs',report['document_finalize'])
