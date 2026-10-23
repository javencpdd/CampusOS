#!/usr/bin/env python3
"""Accept one V12-14a text-protocol boundary slice in owned Linux PG/HTTP.
No public model, authorized egress, browser, profile/budget or full-stage claim.
"""
import argparse,hashlib,json,os,secrets,socket,subprocess,time
from pathlib import Path
REPO=Path(__file__).resolve().parent.parent
PRIOR=REPO/'docs/项目计划书v1/项目计划v1.2/evidence/v12-13-exit-linux.json'
PRIOR_SHA='6127ca238f3ab05c6566eb9d53605c6e51de49fa54662018c804c6dbc5d98782'
def sha(p):
 with Path(p).open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def sources():
 names=set(subprocess.check_output(['git','ls-files','-co','--exclude-standard','-z'],cwd=REPO).decode().split('\0'))
 return {n:sha(REPO/n) for n in sorted(names) if n and (REPO/n).is_file() and not n.endswith('.md') and not n.startswith('docs/项目计划书v1/项目计划v1.2/evidence/')}
def norm(s):return '\n'.join(x for x in s.splitlines() if not x.startswith(('\\restrict ','\\unrestrict ')))
def main(work):
 assert os.uname().sysname=='Linux' and work.is_relative_to(REPO/'.cache/v12-14a') and sha(PRIOR)==PRIOR_SHA
 work.mkdir(parents=True,exist_ok=False);work.chmod(0o700)
 for name in ['native','runtime']:(work/name).mkdir(mode=0o700)
 frozen=sources();(work/'source-freeze.json').write_text(json.dumps(frozen,ensure_ascii=False,indent=2)+'\n')
 env={k:os.environ[k] for k in ['PATH','HOME'] if k in os.environ};env.update(GOCACHE=str(REPO/'.cache/v12-g0/go-cache'),GOMAXPROCS='2',GOTOOLCHAIN='auto',GOFLAGS='-buildvcs=false',CAMPUSOS_SKIP_DOTENV='true',CAMPUSOS_ENV='test',TMPDIR='/tmp')
 name='campusos-v14a-text-'+secrets.token_hex(6);password=secrets.token_hex(24);database='campusos_v12_14a_text';commands=[];started=False;port=None;report=None
 def run(label,args,extra={}):
  start=time.monotonic()
  with (work/(label+'.log')).open('wb') as f:p=subprocess.run(args,cwd=REPO,env=env|extra,stdout=f,stderr=subprocess.STDOUT)
  commands.append(dict(name=label,args=args,exit_code=p.returncode,seconds=round(time.monotonic()-start,3)));assert p.returncode==0,label+' failed; inspect owned log'
  return (work/(label+'.log')).read_text()
 try:
  run('build-server',['go','test','-race','-c','-o',str(work/'native/server.test'),'./internal/server'])
  run('build-ai',['go','test','-race','-c','-o',str(work/'native/ai.test'),'./internal/modules/features/ai'])
  binaries={str(p.relative_to(REPO)):sha(p) for p in (work/'native').iterdir()}
  unit=run('native-ai-unit',[str(work/'native/ai.test'),'-test.v','-test.timeout=90s'])
  assert '--- PASS: TestTextContractClosedRequests ' in unit and not any(x in unit for x in ['--- FAIL','--- SKIP','DATA RACE'])
  run('pg-start',['docker','run','--rm','--pull=never','-d','--name',name,'--tmpfs','/var/lib/postgresql/data:rw,size=512m','-e','POSTGRES_USER=campusos','-e','POSTGRES_DB='+database,'-e','POSTGRES_PASSWORD','-p','127.0.0.1::5432','postgres:16-alpine'],{'POSTGRES_PASSWORD':password});started=True
  for _ in range(120):
   if subprocess.run(['docker','exec',name,'pg_isready','-U','campusos','-d',database],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
   time.sleep(.25)
  else:raise AssertionError('owned PG deadline')
  port=int(run('pg-port',['docker','port',name,'5432/tcp']).strip().rsplit(':',1)[1])
  pg={'PSQL_MODE':'docker','POSTGRES_CONTAINER':name,'DB_USER':'campusos','DB_NAME':database,'DB_PASSWORD':password,'DB_HOST':'127.0.0.1','DB_PORT':str(port)}
  run('migrate-up',['bash','scripts/migrate.sh','up'],pg);run('migrate-check',['bash','scripts/migrate.sh','check'],pg);run('migrate-status',['bash','scripts/migrate.sh','status'],pg);run('database-entry',['bash','scripts/database-check.sh','all'],pg)
  count=run('schema-count',['docker','exec',name,'psql','-U','campusos','-d',database,'-XAt','-v','ON_ERROR_STOP=1','-c',"SELECT count(*) FROM pg_tables WHERE schemaname='public';SELECT max(version)::int FROM schema_migrations"]);assert count.splitlines()==['126','34']
  dump=['docker','exec',name,'pg_dump','-U','campusos','-d',database,'--schema-only','--no-owner','--no-privileges'];before=run('schema-before',dump)
  dsn=f'postgres://campusos:{password}@127.0.0.1:{port}/{database}?sslmode=disable'
  actual_env={'DATABASE_DSN':dsn,'CAMPUSOS_AI_TEXT_DSN':dsn,'CAMPUSOS_AI_TEXT_ROOT':str(work/'runtime'),'REDIS_ENABLED':'false','NATS_URL':'nats://127.0.0.1:1','HOST_API_ENABLED':'false','JWT_SECRET':secrets.token_hex(32),'AUTH_PASSWORD_HASH_ENABLED':'true','AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN':'false','AUTH_BOOTSTRAP_ADMIN_SECRET':secrets.token_hex(24),'EMAIL_PROVIDER':'fake','AI_ENABLED':'false','AUTH_MFA_ACTIVE_KEY_ID':'owned14text','AUTH_MFA_ENCRYPTION_KEYS':'owned14text:'+secrets.token_hex(32),'PLUGIN_DATA_DIR':str(work/'runtime/data/plugin_data'),'GIN_MODE':'release','OBSERVABILITY_PROMETHEUS_ENABLED':'false'}
  target=run('native-ai-text-target',[str(work/'native/server.test'),'-test.v','-test.run=^TestAITextContractPostgres$','-test.timeout=120s'],actual_env)
  assert '--- PASS: TestAITextContractPostgres ' in target and not any(x in target for x in ['--- FAIL','--- SKIP','DATA RACE'])
  proof=json.loads((work/'runtime/ai-text-proof.json').read_text());assert proof['status']=='passed' and proof['actual_postgresql'] and proof['actual_loopback_http_adapter'] and proof['redirect_target_calls']==0
  run('database-exit',['bash','scripts/database-check.sh','all'],pg);after=run('schema-after',dump);assert norm(before)==norm(after)
  assert sources()==frozen,'source changed during frozen target';assert all(sha(REPO/p)==h for p,h in binaries.items())
  report=dict(schema='campusos.v12-14a-text-contract/v1',status='passed',stage='V12-14a',stage_status='in_progress',slice_status='completed_current_linux_scope',implementation='passed',automation='passed',target_environment='passed_linux_native_production_modules_pg16_loopback_http_protocol_peer',proof=proof,source_sha256=frozen,binary_sha256=binaries,dependency={str(PRIOR.relative_to(REPO)):PRIOR_SHA},schema_before_after_sha256=hashlib.sha256(norm(after).encode()).hexdigest(),schema_tables=126,migration_version=34,new_migrations=[],commit_or_push=False,windows='deferred_by_user_not_run',scope_limits=['text-only v1 request/response/definition and safe errors/cancellation; no complete AI Gateway stage claim','no managed Profile/Instance/Binding, persistent usage budget, required audit or ordered response events','owned deterministic local HTTP protocol peer is not a real LLM, controlled Egress acceptance or public endpoint certification','no AI/RAG user endpoint, private content authority, plugin Host/SDK expansion, browser or standalone cmd/server acceptance'])
 finally:
  cleanup={'postgres_started':started,'errors':[],'credentials_persisted':False}
  if started:
   for label,args in [('pg-server',['docker','logs',name]),('pg-stop',['docker','stop',name])]:
    try:run(label,args)
    except Exception as e:cleanup['errors'].append(str(e))
   for _ in range(30):
    inspect=subprocess.run(['docker','inspect',name],stdout=subprocess.PIPE,stderr=subprocess.PIPE);healthy=subprocess.run(['docker','info','--format','{{.ServerVersion}}'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0
    cleanup.update(docker_daemon_healthy=healthy,container_removed=healthy and inspect.returncode==1 and b'no such object' in inspect.stderr.lower())
    with socket.socket() as s:s.settimeout(.25);cleanup['loopback_port_closed']=port is not None and s.connect_ex(('127.0.0.1',port))!=0
    if cleanup['container_removed'] and cleanup['loopback_port_closed']:break
    time.sleep(.1)
  (work/'commands.json').write_text(json.dumps(commands,ensure_ascii=False,indent=2)+'\n');(work/'cleanup.json').write_text(json.dumps(cleanup,indent=2)+'\n')
  if started:assert cleanup['container_removed'] and cleanup['loopback_port_closed'] and not cleanup['errors']
 assert report is not None
 report['commands'],report['cleanup']=commands,cleanup;(work/'target.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');print(json.dumps({'status':'passed','checks':report['proof']['check_count'],'work':str(work.relative_to(REPO))}))
if __name__=='__main__':
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--work',required=True);main(Path(parser.parse_args().work).resolve())
