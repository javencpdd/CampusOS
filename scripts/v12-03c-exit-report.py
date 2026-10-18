#!/usr/bin/env python3
"""Seal03c bounded host-contract exit, retaining all earlier frozen evidence."""
import hashlib
import json
import pathlib
import re
import subprocess
import sys
from datetime import datetime
from zoneinfo import ZoneInfo
ROOT=pathlib.Path(__file__).resolve().parents[1]
REPORT=ROOT/'docs/项目计划书v1/项目计划v1.2/evidence/v12-03c-exit-linux.json'
COMMON=['full-go','related-race','sdk','contracts','architecture','database','admin-build','er-tests','readme','docs-links','docs-build','line-endings','diff']
FINAL=['docs-links','docs-build','line-endings','diff','architecture','skill-bridges','onboarding-skill']
DOCS=['docs/进度/v1.2-dev/v1.2.119-dev.md','docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md','docs/api/v1.2插件双向调用事件与生命周期合同.md','docs/architecture/当前架构概览.md','docs-site/guide/architecture.md','skills/sources/campusos-project-onboarding/references/current-project-status.md','migrations/README.md']

def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def sources():
 names=subprocess.check_output(['git','ls-files','-co','--exclude-standard','-z'],cwd=ROOT).decode().split('\0')
 suffixes={'.go','.mod','.sum','.ts','.vue','.js','.mjs','.sql','.py','.sh','.json','.yaml','.yml'}
 return {n:sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (pathlib.Path(n).suffix in suffixes or n=='Makefile')}
def dependencies():return {str(p.relative_to(ROOT)):sha(p) for n in ['v12-03c-chat-dispatch-linux.json','v12-03c-directory-linux.json','v12-03b-exit-linux.json','v12-02a-exit-linux.json','v12-01b-exit-linux.json'] for p in [REPORT.parent/n]}
def native_tests():return set(re.findall(r'^func (Test(?:V5|PluginV5)\w+)\(', '\n'.join(p.read_text() for p in (ROOT/'internal/plugin').glob('*_test.go')), re.M))
def events(p,expected):
 rows=[json.loads(s) for s in p.read_text().splitlines()]
 assert not any(r['Action'] in {'fail','skip'} for r in rows),p
 passed={r['Test'] for r in rows if r['Action']=='pass' and 'Test' in r}
 tops={n for n in passed if '/' not in n};assert tops==expected,(p,tops^expected)
 assert any(r['Action']=='pass' and 'Test' not in r for r in rows),p
 assert 'DATA RACE' not in p.with_suffix('.log').read_text(),p
 stderr=p.with_suffix('.stderr').read_text()
 if p.stem=='assembly':
  # The negative startup test intentionally emits this single fail-closed
  # warning. Preserve the original log; reject every other stderr message.
  assert re.fullmatch(r'\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} 插件 v5 对账暂不可用，入口将保持关闭\n',stderr),p
 else:assert not stderr,p
 return {'top_level':len(tops),'pass_events':len(passed),'fail':0,'skip':0}
def main():
 mode,work=sys.argv[1],pathlib.Path(sys.argv[2]).resolve()
 assert work.parent==ROOT/'.cache/v12-03c-exit' and work.name.startswith('acceptance.')
 frozen={'head':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT).decode().strip(),'source_sha256':sources(),'dependencies':dependencies()}
 snapshot=work/'source-freeze.json'
 if mode=='freeze':snapshot.write_text(json.dumps(frozen,ensure_ascii=False,indent=2)+'\n');return
 executed=json.loads(snapshot.read_text())
 producer=str(pathlib.Path(__file__).resolve().relative_to(ROOT))
 # The report producer is validation tooling, never executed by the native
 # application/PG tests. Keep the original snapshot and attest BOTH hashes;
 # every other source, HEAD and dependency must still match exactly. This
 # permits a reporting-only correction without rerunning unchanged business
 # acceptance or disguising the original execution snapshot.
 def execution_sources(value):
  copy={**value,'source_sha256':dict(value['source_sha256'])}
  assert producer in copy['source_sha256']
  del copy['source_sha256'][producer]
  return copy
 assert execution_sources(executed)==execution_sources(frozen),'execution source/dependency changed after target freeze'
 producer_evidence={'path':producer,'sha256_at_target_freeze':executed['source_sha256'][producer],'sha256_at_seal':frozen['source_sha256'][producer],'original_source_freeze_sha256':sha(snapshot),'execution_sources_unchanged':len(frozen['source_sha256'])-1,'correction':'Accept only the single expected fail-closed startup warning; raw test logs remain unchanged. Other stderr, FAIL/SKIP and race still fail sealing.'}
 if mode=='verify':return
 assert mode=='seal'
 native=events(work/'native/plugin.jsonl',native_tests())
 assembly=events(work/'native/assembly.jsonl',{'TestV5ContributionDirectoryAssembly','TestV5ChatDispatcherAssemblyHasNoFakeRunner'})
 pg=events(work/'native/pg.jsonl',{'TestPostgresV5ManagedExit'})
 reload=events(work/'native/reload.jsonl',{'TestPostgresV5ManagedRestart'})
 assert 'Linux' in (work/'environment.txt').read_text()
 assert (work/'native/binaries.txt').read_text().count('ELF 64-bit')==2
 assert 160000<=int((work/'pg-version.txt').read_text())<170000
 assert 'campusos-v12-03c-exit-' in (work/'cleanup.log').read_text() and 'Error' not in (work/'cleanup.log').read_text()
 for suffix in ['after-up','after-down','after-reup']:assert (work/'legacy-before.json').read_bytes()==(work/f'legacy-{suffix}.json').read_bytes()
 before=json.loads((work/'legacy-before.json').read_text());assert len(before['tables'])==111 and len(before['sequences'])>0
 for prefix,phase in [('legacy','restart'),('queue','restart'),('legacy','guard'),('queue','guard')]:assert (work/f'{prefix}-before-{phase}.json').read_bytes()==(work/f'{prefix}-after-{phase}.json').read_bytes()
 assert 'cannot roll back nonempty v5 event delivery queue' in (work/'migration-nonempty-down-rejected.log').read_text()
 assert (work/'common-passed.txt').read_text().splitlines()==COMMON
 assert (work/'final-passed.txt').read_text().splitlines()==FINAL
 assert 'actual Go' in (work/'native/dispatch-oracle.log').read_text()
 report={'schema':'campusos.v12-03c-exit/v1','generated_at':datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(),'stage':'V12-03c','stage_status':'completed','implementation':'passed','automation':'passed','target_environment':'passed','native':native,'server_assembly':assembly,'postgres':pg,'postgres_restart_reload':reload,'migration':{'current':28,'next':29,'business_tables':112,'executor_tables':2,'all_pre28_tables_preserved':111,'all_old_sequences_preserved':len(before['sequences']),'empty_down_up':'passed','nonempty_down_refused':'passed','restart_persistence':'passed'},'common_gates':{n:'passed' for n in COMMON},'final_document_gates':{n:'passed' for n in FINAL},**frozen,'artifacts_sha256':{str(p.relative_to(ROOT)):sha(p) for p in sorted(work.rglob('*')) if p.is_file()},'documents_sha256':{n:sha(ROOT/n) for n in DOCS},'boundaries':['03c includes bounded compiled contract adapters, not actual Wasm/Container execution or OS isolation (05/06).','Production has no installed runner or contribution publication; enable stays pending/runtime_unavailable. No built-in AI/RAG dependency or fake Feature.','Only opaque per-instance credentials, typed declared/approved public capabilities and compiled current resource policies authorize Host calls.','Preview and Knowledge resource consumption are bounded contract adapters; actual UI/knowledge binding and ingestion remain04/09.','Callbacks honor context; no hard preemption claim. Deployment concurrency8 is process-local, not a cluster quota.','Only committed Outbox sources fan out metadata to a separate transactional queue; actual plugin IO runs in an independent worker. Knowledge metadata requires a compiled authorizer and stays closed without it.','Legacy authority code retained solely for historical tests/closed handlers; new production paths never invoke its capability maps, untyped scopes or event fallbacks.','No browser/Windows/whole-system restore, shared database reset, old migration rewrite, commit or push.'],'commit_or_push':False,'shared_database_reset':False}
 report['report_producer']=producer_evidence
 REPORT.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
 print('sealed03c exit:',native,assembly,pg,reload)
if __name__=='__main__':main()
