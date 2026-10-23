#!/usr/bin/env python3
"""V12-14a full managed gateway acceptance in disposable Linux PG/HTTP/TLS.
Owned protocol peers exercise shared Egress; no real LLM quality claim.
"""
import argparse,hashlib,json,os,secrets,socket,subprocess,time
from pathlib import Path
REPO=Path(__file__).resolve().parent.parent
PRIOR=REPO/'docs/项目计划书v1/项目计划v1.2/evidence/v12-14a-text-contract-linux.json'
PRIOR_SHA='108e0f31d8072afd61465ae1f344c6b151e420a0b3ec074c0edeefbb2dca3028'
DATABASE='campusos_v12_14a_managed'
def sha(p):
 with Path(p).open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def sources():
 names=set(subprocess.check_output(['git','ls-files','-co','--exclude-standard','-z'],cwd=REPO).decode().split('\0'))
 return {n:sha(REPO/n) for n in sorted(names) if n and (REPO/n).is_file() and not n.endswith('.md') and not n.startswith('docs/项目计划书v1/项目计划v1.2/evidence/')}
def norm(s):return '\n'.join(x for x in s.splitlines() if not x.startswith(('\\restrict ','\\unrestrict ')))
def main(work):
 assert os.uname().sysname=='Linux' and work.is_relative_to(REPO/'.cache/v12-14a-exit') and sha(PRIOR)==PRIOR_SHA
 work.mkdir(parents=True,exist_ok=False);work.chmod(0o700)
 for name in ['native','runtime','shims']:(work/name).mkdir(mode=0o700)
 frozen=sources();(work/'source-freeze.json').write_text(json.dumps(frozen,ensure_ascii=False,indent=2)+'\n')
 env={k:os.environ[k] for k in ['PATH','HOME'] if k in os.environ};env.update(GOCACHE=str(REPO/'.cache/v12-g0/go-cache'),GOMAXPROCS='2',GOTOOLCHAIN='auto',GOFLAGS='-buildvcs=false',CAMPUSOS_SKIP_DOTENV='true',CAMPUSOS_ENV='test',TMPDIR='/tmp')
 name='campusos-v14a-managed-'+secrets.token_hex(6);password=secrets.token_hex(24);commands=[];started=False;port=None;report=None
 def run(label,args,extra={}):
  begin=time.monotonic()
  with (work/(label+'.log')).open('wb') as f:p=subprocess.run(args,cwd=REPO,env=env|extra,stdout=f,stderr=subprocess.STDOUT)
  commands.append(dict(name=label,args=args,exit_code=p.returncode,seconds=round(time.monotonic()-begin,3)));assert p.returncode==0,label+' failed; inspect owned log'
  return (work/(label+'.log')).read_text()
 def sql(db,statement):return run('sql-'+str(len(commands)),['docker','exec',name,'psql','-U','campusos','-d',db,'-XAt','-v','ON_ERROR_STOP=1','-c',statement])
 def mandatory(log,name):assert '--- PASS: '+name+' ' in log and not any(x in log for x in ['--- FAIL','--- SKIP','DATA RACE'])
 try:
  run('build-server-acceptance',['go','test','-race','-tags','campusos_acceptance','-c','-o',str(work/'native/server.test'),'./internal/server'])
  run('build-ai',['go','test','-race','-c','-o',str(work/'native/ai.test'),'./internal/modules/features/ai'])
  run('build-ctl',['go','build','-o',str(work/'native/campusosctl'),'./cmd/campusosctl'])
  run('build-production-server',['go','build','-o',str(work/'native/server'),'./cmd/server'])
  binaries={str(p.relative_to(REPO)):sha(p) for p in (work/'native').iterdir()}
  symbols=run('production-symbols',['go','tool','nm',str(work/'native/server')]);assert 'AcceptanceRemote' not in symbols and 'NewAcceptanceEgress' not in symbols
  run('pg-start',['docker','run','--rm','--pull=never','-d','--name',name,'--tmpfs','/var/lib/postgresql/data:rw,size=768m','-e','POSTGRES_USER=campusos','-e','POSTGRES_DB='+DATABASE,'-e','POSTGRES_PASSWORD','-p','127.0.0.1::5432','postgres:16-alpine'],{'POSTGRES_PASSWORD':password});started=True
  for _ in range(120):
   if subprocess.run(['docker','exec',name,'pg_isready','-U','campusos','-d',DATABASE],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
   time.sleep(.25)
  else:raise AssertionError('owned PG deadline')
  port=int(run('pg-port',['docker','port',name,'5432/tcp']).strip().rsplit(':',1)[1])
  pg={'PSQL_MODE':'docker','POSTGRES_CONTAINER':name,'DB_USER':'campusos','DB_NAME':DATABASE,'DB_PASSWORD':password,'DB_HOST':'127.0.0.1','DB_PORT':str(port)}
  run('migrate-up',['bash','scripts/migrate.sh','up'],pg)
  epoch=int(sql(DATABASE,"SELECT nextval('ai_gateway_epoch_seq')").strip())
  # Every old table/sequence must survive the additive migration round-trip.
  fingerprint="""CREATE TEMP TABLE owned_facts(name text,digest text); DO $$ DECLARE r record; h text; BEGIN
  FOR r IN SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename NOT LIKE 'ai_model_%' AND tablename NOT IN ('schema_migrations','schema_migration_locks','permission_definitions','admin_role_permissions') ORDER BY tablename LOOP
   EXECUTE format('SELECT md5(coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text,''[]'')) FROM public.%I t',r.tablename) INTO h;
   INSERT INTO owned_facts VALUES(r.tablename,h);
  END LOOP;
  FOR r IN SELECT sequencename FROM pg_sequences WHERE schemaname='public' AND sequencename NOT IN ('ai_gateway_epoch_seq','admin_permission_facts_version_seq','user_scope_facts_version_seq') ORDER BY sequencename LOOP
   EXECUTE format('SELECT last_value::text||'':''||is_called::text FROM public.%I',r.sequencename) INTO h;
   INSERT INTO owned_facts VALUES('sequence:'||r.sequencename,h);
  END LOOP;END $$;
  INSERT INTO owned_facts SELECT 'permission_definitions',md5(coalesce(jsonb_agg(to_jsonb(t) ORDER BY id)::text,'[]')) FROM permission_definitions t WHERE code<>'ai.ai.manage';
  INSERT INTO owned_facts SELECT 'admin_role_permissions',md5(coalesce(jsonb_agg(to_jsonb(t) ORDER BY role_id,permission_code)::text,'[]')) FROM admin_role_permissions t WHERE permission_code<>'ai.ai.manage';
  SELECT name||':'||digest FROM owned_facts ORDER BY name;"""
  old=sql(DATABASE,fingerprint)
  authority_before={n:int(sql(DATABASE,'SELECT last_value FROM '+n).strip()) for n in ['user_scope_facts_version_seq','admin_permission_facts_version_seq']}
  run('migrate-empty-down',['bash','scripts/migrate.sh','down'],pg);run('migrate-reup',['bash','scripts/migrate.sh','up'],pg)
  assert sql(DATABASE,fingerprint)==old,'old business rows/unrelated sequences changed'
  for seq,value in authority_before.items():assert int(sql(DATABASE,'SELECT last_value FROM '+seq).strip())>value,'permission reseed rewound authority epoch'
  assert int(sql(DATABASE,"SELECT nextval('ai_gateway_epoch_seq')").strip())>epoch
  run('migrate-check',['bash','scripts/migrate.sh','check'],pg);run('migrate-status',['bash','scripts/migrate.sh','status'],pg);run('database-entry',['bash','scripts/database-check.sh','all'],pg)
  assert sql(DATABASE,"SELECT count(*) FROM pg_tables WHERE schemaname='public';SELECT max(version)::int FROM schema_migrations;SELECT count(*) FROM pg_constraint WHERE contype='f' AND connamespace='public'::regnamespace").splitlines()==['132','35','143']
  dump=['docker','exec',name,'pg_dump','-U','campusos','-d',DATABASE,'--schema-only','--no-owner','--no-privileges'];before=run('schema-before',dump)
  shim=work/'shims/psql';shim.write_text('''#!/usr/bin/env python3
import os,pathlib,subprocess,sys
args=sys.argv[1:];data=None
if '-f' in args:
 i=args.index('-f');data=pathlib.Path(args[i+1]).read_bytes();del args[i:i+2]
db=os.environ.get('PGDATABASE','')
if db!='campusos_v12_14a_restore':raise SystemExit('owned restore required')
raise SystemExit(subprocess.run(['docker','exec','-i',os.environ['POSTGRES_CONTAINER'],'psql','-U','campusos','-d',db]+args,input=data).returncode)
''');shim.chmod(0o700)
  for db in ['campusos_v12_14a_restore','campusos_v12_14a_unit']:
   sql('postgres','CREATE DATABASE '+db+' TEMPLATE '+DATABASE)
   assert sql(db,'SELECT max(version)::int FROM schema_migrations').strip()=='35'
  dsn=lambda db:f'postgres://campusos:{password}@127.0.0.1:{port}/{db}?sslmode=disable'
  unit=run('native-ai-unit',[str(work/'native/ai.test'),'-test.v','-test.timeout=120s'],{'CAMPUSOS_AI_MANAGED_DSN':dsn('campusos_v12_14a_unit')});mandatory(unit,'TestManagedGatewayPostgres');mandatory(unit,'TestTextContractClosedRequests')
  actual_env=pg|{'PATH':str(work/'shims')+':'+env['PATH'],'DATABASE_DSN':dsn(DATABASE),'CAMPUSOS_AI_MANAGED_TARGET_DSN':dsn(DATABASE),'CAMPUSOS_AI_RESTORE_DSN':dsn('campusos_v12_14a_restore'),'CAMPUSOS_AI_MANAGED_ROOT':str(work/'runtime'),'CAMPUSOS_AI_REPO':str(REPO),'CAMPUSOSCTL':str(work/'native/campusosctl'),'REDIS_ENABLED':'false','NATS_URL':'nats://127.0.0.1:1','HOST_API_ENABLED':'false','JWT_SECRET':secrets.token_hex(32),'AUTH_PASSWORD_HASH_ENABLED':'true','AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN':'false','AUTH_BOOTSTRAP_ADMIN_SECRET':secrets.token_hex(24),'EMAIL_PROVIDER':'fake','AI_ENABLED':'false','AUTH_MFA_ACTIVE_KEY_ID':'owned14target','AUTH_MFA_ENCRYPTION_KEYS':'owned14target:'+secrets.token_hex(32),'PLUGIN_DATA_DIR':str(work/'runtime/data/plugin_data'),'GIN_MODE':'release','OBSERVABILITY_PROMETHEUS_ENABLED':'false'}
  actual=run('native-managed-target',[str(work/'native/server.test'),'-test.v','-test.run=^TestAIManagedGatewayPostgres$','-test.timeout=180s'],actual_env);mandatory(actual,'TestAIManagedGatewayPostgres')
  proof=json.loads((work/'runtime/managed-proof.json').read_text());assert proof['status']=='passed' and proof['actual_shared_egress'] and proof['actual_full_backup_restore'] and proof['redirect_target_calls']==0 and proof['external_plugins']==0
  run('database-exit',['bash','scripts/database-check.sh','all'],pg);run('database-restored',['bash','scripts/database-check.sh','all'],pg|{'DB_NAME':'campusos_v12_14a_restore'})
  after=run('schema-after',dump);assert norm(before)==norm(after),'whole Schema changed'
  assert sources()==frozen,'source changed during frozen target';assert all(sha(REPO/p)==h for p,h in binaries.items())
  report=dict(schema='campusos.v12-14a-exit/v1',status='passed',stage='V12-14a',stage_status='completed_current_linux_scope',implementation='passed',automation='passed',target_environment='passed_linux_native_production_modules_router_pg16_shared_egress_TLS_local_HTTP_full_backup_restore',proof=proof,source_sha256=frozen,binary_sha256=binaries,dependency={str(PRIOR.relative_to(REPO)):PRIOR_SHA},schema_before_after_sha256=hashlib.sha256(norm(after).encode()).hexdigest(),public_tables=132,foreign_keys=143,migration_version=35,old_rows_and_unrelated_sequences_preserved=True,windows='deferred_by_user_not_run',commit_or_push=False,scope_limits=['owned deterministic model protocol peers, no real model weights or LLM quality certification','shared Egress DNS/dial use acceptance-only deterministic pinning; production ELF excludes these hooks','internal current-User text Port plus independent Admin metadata/management HTTP; no public Chat or browser feature','request-unit quota is not token pricing or monetary billing; unknown dispatched cost conservatively charged one','no private Knowledge/RAG consent/Embedding/Rerank/AI plugin Host or SDK expansion; 14b/14c remain separate','restored AI remains paused even after authority review; no automatic egress reapproval'])
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
 report['commands'],report['cleanup']=commands,cleanup;(work/'target.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');print(json.dumps({'status':'passed','checks':proof['check_count'],'work':str(work.relative_to(REPO))}))
if __name__=='__main__':
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--work',required=True);main(Path(parser.parse_args().work).resolve())
