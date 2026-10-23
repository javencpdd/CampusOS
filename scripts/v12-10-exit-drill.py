#!/usr/bin/env python3
"""V12-10 whole-stage Linux exit: frozen Chinese corpus and bounded capacity.
Only owned PostgreSQL/tmpfs/loopback fixtures; previous slice reports stay immutable.
"""
import argparse,hashlib,json,os,re,secrets,socket,subprocess,time
from pathlib import Path
REPO=Path(__file__).resolve().parent.parent
BASE=REPO/'docs/项目计划书v1/项目计划v1.2/evidence'
PRIOR=BASE/'v12-10-http-linux.json'
PRIOR_SHA='af7863b1d4808a17e8fc322fe025dcfa83e70d8696dd313071a6fdb3af1fa355'
FROZEN={'v12-10-chinese-golden.json':'4a751b59616be06b4501df3d256f497a3d15ef1b07b0ad0eb0d59e7a7455cfbb','v12-10-capacity-profile-linux.json':'600d61d2a335ec17b4d1cb9d611c109bab657fce8a49216e0015de7a295f1955'}
DATABASE='campusos_v12_10_exit'
def sha(p):
 with Path(p).open('rb')as f:return hashlib.file_digest(f,'sha256').hexdigest()
def source_map():
 names=subprocess.check_output(['git','ls-files','-co','--exclude-standard','-z'],cwd=REPO).decode().split('\0')
 return {n:sha(REPO/n) for n in sorted(set(names)) if n and (REPO/n).is_file() and not n.endswith('.md') and not n.startswith('docs/项目计划书v1/项目计划v1.2/evidence/')}
def normalize(s):return '\n'.join(x for x in s.splitlines() if not x.startswith(('\\restrict ','\\unrestrict ')))
def main(work,baseline):
 assert os.uname().sysname=='Linux' and sha(PRIOR)==PRIOR_SHA
 assert all(sha(BASE/n)==v for n,v in FROZEN.items())
 assert work.is_relative_to(REPO/'.cache/v12-10-exit')
 work.mkdir(parents=True,exist_ok=False);work.chmod(0o700);runtime=work/'runtime';runtime.mkdir(mode=0o700);native=work/'native';native.mkdir()
 frozen=source_map();(work/'source-freeze.json').write_text(json.dumps(frozen,ensure_ascii=False,indent=2)+'\n')
 env={k:os.environ[k] for k in ('PATH','HOME') if k in os.environ};env.update(GOCACHE=str(REPO/'.cache/v12-g0/go-cache'),GOMAXPROCS='2',GOTOOLCHAIN='auto',GOFLAGS='-buildvcs=false',CAMPUSOS_SKIP_DOTENV='true',TMPDIR='/tmp')
 password=secrets.token_hex(24);container='campusos-v12-10-exit-'+secrets.token_hex(6);commands=[];started=False;port=None;summary=None
 def run(name,args,extra=None):
  begin=time.monotonic()
  with (work/(name+'.log')).open('wb')as f:p=subprocess.run(args,cwd=REPO,env=env|(extra or {}),stdout=f,stderr=subprocess.STDOUT)
  commands.append({'name':name,'args':args,'cwd':str(REPO),'exit_code':p.returncode,'seconds':round(time.monotonic()-begin,3)})
  (work/'commands.json').write_text(json.dumps(commands,indent=2)+'\n')
  assert p.returncode==0,name+' failed; inspect owned logs'
  return (work/(name+'.log')).read_text()
 def psql(db=DATABASE):return ['docker','exec','-i',container,'psql','-U','campusos','-d',db,'-XAt','-v','ON_ERROR_STOP=1']
 def mandatory(log,name):assert '--- PASS: '+name+' 'in log and not any(x in log for x in ('--- FAIL','--- SKIP','DATA RACE'))
 try:
  run('build-server-capacity',['go','test','-c','-o',str(native/'server-capacity.test'),'./internal/server'])
  if not baseline:
   for package,name in [('./internal/modules/features/knowledge','knowledge.test'),('./internal/server','server.test')]:run('build-'+name,['go','test','-race','-c','-o',str(native/name),package])
   unit=run('native-knowledge-unit',[str(native/'knowledge.test'),'-test.v']);assert 'PASS\n'in unit and not any(x in unit for x in ('--- FAIL','--- SKIP','DATA RACE'))
  binaries={str(p.relative_to(REPO)):sha(p)for p in native.iterdir()}
  run('pg-start',['docker','run','--rm','--pull=never','-d','--name',container,'--tmpfs','/var/lib/postgresql/data:rw,size=1g','--cpus','2','-e','POSTGRES_USER=campusos','-e','POSTGRES_DB='+DATABASE,'-e','POSTGRES_PASSWORD','-p','127.0.0.1::5432','postgres:16-alpine'],{'POSTGRES_PASSWORD':password});started=True
  for _ in range(120):
   if subprocess.run(['docker','exec',container,'pg_isready','-h','127.0.0.1','-U','campusos','-d',DATABASE],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
   time.sleep(.25)
  else:raise AssertionError('PG startup timeout')
  port=int(run('pg-port',['docker','port',container,'5432/tcp']).strip().rsplit(':',1)[1]);pg={'PSQL_MODE':'docker','POSTGRES_CONTAINER':container,'DB_USER':'campusos','DB_NAME':DATABASE,'DB_PASSWORD':password}
  for action in ('up','check','status'):run('migration-'+action,['bash','scripts/migrate.sh',action],pg)
  if not baseline:run('database-entry',['bash','scripts/database-check.sh','all'],pg)
  facts=run('actual-schema-entry',psql()+['-c',"SELECT count(*) FROM pg_tables WHERE schemaname='public'; SELECT count(*),max(version)::int FROM schema_migrations;"]);assert facts.splitlines()==['124','33|33']
  dump=['docker','exec',container,'pg_dump','-U','campusos','-d',DATABASE,'--schema-only','--no-owner','--no-privileges'];before=run('schema-before',dump)
  version=run('pg-version',psql()+['-c','SHOW server_version']).strip();dsn=f'postgres://campusos:{password}@127.0.0.1:{port}/{DATABASE}?sslmode=disable'
  target={'CAMPUSOS_KNOWLEDGE_EXIT_DSN':dsn,'CAMPUSOS_KNOWLEDGE_EXIT_ROOT':str(runtime),'CAMPUSOS_KNOWLEDGE_EXIT_MODE':'baseline'if baseline else'acceptance','CAMPUSOS_KNOWLEDGE_GOLDEN':str(BASE/'v12-10-chinese-golden.json'),'DATABASE_DSN':dsn,'CAMPUSOS_ENV':'test','REDIS_ENABLED':'false','NATS_URL':'nats://127.0.0.1:1','HOST_API_ENABLED':'false','JWT_SECRET':secrets.token_hex(32),'AUTH_PASSWORD_HASH_ENABLED':'true','AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN':'false','AUTH_BOOTSTRAP_ADMIN_SECRET':secrets.token_hex(24),'EMAIL_PROVIDER':'fake','AI_ENABLED':'false','AUTH_MFA_ACTIVE_KEY_ID':'owned10exit','AUTH_MFA_ENCRYPTION_KEYS':'owned10exit:'+secrets.token_hex(32),'PLUGIN_DATA_DIR':str(runtime/'data/plugin_data'),'GIN_MODE':'release','OBSERVABILITY_PROMETHEUS_ENABLED':'false'}
  if not baseline:
   snapdb='campusos_v12_10_snapshot';run('snapshot-db',psql('postgres')+['-c','CREATE DATABASE '+snapdb]);run('snapshot-migrations',['bash','scripts/migrate.sh','up'],pg|{'DB_NAME':snapdb});sd=f'postgres://campusos:{password}@127.0.0.1:{port}/{snapdb}?sslmode=disable'
   log=run('native-snapshot-boundary',[str(native/'server.test'),'-test.v','-test.run=^TestCommunitySourceReadSnapshotPostgres$','-test.timeout=30s'],target|{'CAMPUSOS_KNOWLEDGE_SNAPSHOT_DSN':sd});mandatory(log,'TestCommunitySourceReadSnapshotPostgres')
  log=run('native-exit-target',[str(native/'server-capacity.test'),'-test.v','-test.run=^TestKnowledgeRetrievalExitPostgres$','-test.timeout=15m','-test.cpuprofile='+str(work/'capacity-cpu.pprof')],target);mandatory(log,'TestKnowledgeRetrievalExitPostgres');proof=json.loads((runtime/'target-proof.json').read_text())
  assert proof['status']==('measured_baseline'if baseline else'passed')
  regressions={}
  if not baseline:
   for name,db,testname,prefix,rootprefix in [('http','campusos_v12_10_http','TestKnowledgeRetrievalPublicHTTPPostgres','HTTP','v12-10-http'),('lexical','campusos_v12_10_lexical','TestKnowledgeRetrievalPublicLexicalPostgres','RETRIEVAL','v12-10-lexical'),('execution','campusos_v12_09b_exit','TestKnowledgeIndexExecutionResidentPostgres','EXECUTION','v12-09b-exit')]:
    run(name+'-db',psql('postgres')+['-c','CREATE DATABASE '+db]);run(name+'-migrations',['bash','scripts/migrate.sh','up'],pg|{'DB_NAME':db});rr=REPO/'.cache'/rootprefix/('exit-regression-'+container.rsplit('-',1)[1]);rr.mkdir(mode=0o700,exist_ok=False);rd=f'postgres://campusos:{password}@127.0.0.1:{port}/{db}?sslmode=disable';re=target|{'CAMPUSOS_KNOWLEDGE_'+prefix+'_DSN':rd,'CAMPUSOS_KNOWLEDGE_'+prefix+'_ROOT':str(rr),'DATABASE_DSN':rd,'PLUGIN_DATA_DIR':str(rr/'data/plugin_data')}
    log=run('native-'+name+'-regression',[str(native/'server.test'),'-test.v','-test.run=^'+testname+'$','-test.timeout=5m'],re);mandatory(log,testname);rp=json.loads((rr/'target-proof.json').read_text());assert rp['status']=='passed';regressions[name]=rp;(work/(name+'-regression-proof.json')).write_text(json.dumps(rp,ensure_ascii=False,indent=2)+'\n')
   run('database-exit',['bash','scripts/database-check.sh','all'],pg)
  after=run('schema-after',dump);assert normalize(before)==normalize(after)
  assert source_map()==frozen and all(sha(REPO/n)==h for n,h in binaries.items())
  host=json.loads((BASE/'v12-10-capacity-profile-linux.json').read_text())['host'];import platform
  assert platform.system()==host['os'] and platform.machine()==host['architecture'] and platform.release()==host['kernel'] and os.cpu_count()==host['logical_cpus'] and len(os.sched_getaffinity(0))==host['affinity_cpus']
  report={'schema':'campusos.v12-10-exit/v1','status':'measured_baseline'if baseline else'passed','stage':'V12-10','stage_status':'in_progress'if baseline else'completed_current_linux_scope','implementation':'passed','automation':'not_full_baseline'if baseline else'passed','target_environment':'baseline_observation'if baseline else'passed_linux_native_pg16_production_modules_http_quality_capacity','proof':proof,'regressions':regressions,'source_sha256':frozen,'binary_sha256':binaries,'inputs_sha256':{str((BASE/n).relative_to(REPO)):h for n,h in FROZEN.items()},'dependency':{str(PRIOR.relative_to(REPO)):PRIOR_SHA},'actual_host':host,'postgres_version':version,'schema_before_after_sha256':hashlib.sha256(normalize(before).encode()).hexdigest(),'commands':commands,'schema_tables':124,'migration_version':33,'new_migrations':[],'windows':'deferred_by_user_not_run','commit_or_push':False}
  (work/'target.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');summary={'status':report['status'],'work':str(work.relative_to(REPO))}
 finally:
  cleanup = {'owned_postgres': container, 'credentials_persisted': False, 'postgres_started': started}
  if started:
   cleanup['command_errors'] = []
   for name, args in [('pg-server', ['docker', 'logs', container]), ('pg-cleanup', ['docker', 'stop', container])]:
    try:
     run(name, args)
    except Exception as error:
     cleanup['command_errors'].append(str(error))
   # Docker stop can return before --rm and the port proxy finish.
   # Confirm an explicit missing-object response from a healthy daemon,
   # rather than treating any inspect failure as successful cleanup.
   deadline = time.monotonic() + 10
   attempts = 0
   while True:
    attempts += 1
    try:
     inspect = subprocess.run(['docker', 'inspect', container], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=max(.05, min(1, deadline-time.monotonic())))
     healthy = subprocess.run(['docker', 'info', '--format', '{{.ServerVersion}}'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=max(.05, min(1, deadline-time.monotonic()))).returncode == 0
     cleanup['docker_daemon_healthy'] = healthy
     cleanup['inspect_exit_code'] = inspect.returncode
     cleanup['inspect_error'] = inspect.stderr.decode(errors='replace')[:512]
     cleanup['container_removed'] = healthy and inspect.returncode == 1 and b'no such object' in inspect.stderr.lower()
    except subprocess.TimeoutExpired:
     cleanup['docker_daemon_healthy'] = False
     cleanup['container_removed'] = False
    with socket.socket() as probe:
     probe.settimeout(.25)
     cleanup['loopback_port_closed'] = probe.connect_ex(('127.0.0.1', port)) != 0 if port is not None else None
    if cleanup['container_removed'] and cleanup['loopback_port_closed'] or time.monotonic() >= deadline:
     break
    time.sleep(.05)
   cleanup['verification_attempts'] = attempts
  cleanup['commands'] = commands[-2:] if started else []
  (work / 'cleanup.json').write_text(json.dumps(cleanup, indent=2) + '\n')
  if started:
   assert not cleanup['command_errors'] and cleanup['container_removed'] and cleanup['loopback_port_closed'], 'owned cleanup did not complete within10s'
 print(json.dumps(summary), flush=True)


if __name__=='__main__':
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--work',required=True);p.add_argument('--baseline',action='store_true');a=p.parse_args();main(Path(a.work).resolve(),a.baseline)
