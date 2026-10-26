#!/usr/bin/env python3
"""V12-13 full Linux acceptance: encrypted private index, real source/worker/UI,
v3 backup/offline restore and independently retired keys. Only owned PG/data.
"""
import argparse,hashlib,json,os,secrets,shutil,socket,subprocess,time
from pathlib import Path
REPO=Path(__file__).resolve().parent.parent
PRIOR=REPO/'docs/项目计划书v1/项目计划v1.2/evidence/v12-13-private-preflight-linux.json'
PRIOR_SHA='6aa6719e31f45924836f90bead205d70654c9344deb106c8cb40ad0ed8d24778'
DATABASE='campusos_v12_13_index'
def sha(p):
 with Path(p).open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def sources():
 names=set(subprocess.check_output(['git','ls-files','-co','--exclude-standard','-z'],cwd=REPO).decode().split('\0'))
 return {n:sha(REPO/n) for n in sorted(names) if n and (REPO/n).is_file() and not n.endswith('.md') and not n.startswith('docs/项目计划书v1/项目计划v1.2/evidence/')}
def norm(s):return '\n'.join(x for x in s.splitlines() if not x.startswith(('\\restrict ','\\unrestrict ')))
def main(work):
 assert os.uname().sysname=='Linux' and work.is_relative_to(REPO/'.cache/v12-13-exit')
 assert sha(PRIOR)==PRIOR_SHA
 work.mkdir(parents=True,exist_ok=False);work.chmod(0o700)
 for name in ['native','runtime','keys','shims']:(work/name).mkdir(mode=0o700)
 frozen=sources();(work/'source-freeze.json').write_text(json.dumps(frozen,ensure_ascii=False,indent=2)+'\n')
 env={k:os.environ[k] for k in ['PATH','HOME'] if k in os.environ};env.update(GOCACHE=str(REPO/'.cache/v12-g0/go-cache'),GOMAXPROCS='2',GOTOOLCHAIN='auto',GOFLAGS='-buildvcs=false',CAMPUSOS_SKIP_DOTENV='true',CAMPUSOS_ENV='test',TMPDIR='/tmp')
 (work/'docker-config').mkdir(mode=0o700)
 env.update(DOCKER_HOST='unix:///var/run/docker.sock',DOCKER_CONFIG=str(work/'docker-config'))
 name='campusos-v13-index-'+secrets.token_hex(6);password=secrets.token_hex(24);commands=[];started=False;port=None;report=None
 def run(label,args,extra={}):
  begin=time.monotonic()
  with (work/(label+'.log')).open('wb') as f:p=subprocess.run(args,cwd=REPO,env=env|extra,stdout=f,stderr=subprocess.STDOUT)
  commands.append(dict(name=label,args=args,exit_code=p.returncode,seconds=round(time.monotonic()-begin,3)));assert p.returncode==0,label+' failed; inspect owned log'
  return (work/(label+'.log')).read_text()
 def sql(db,statement):return run('sql-'+str(len(commands)),['docker','exec',name,'psql','-U','campusos','-d',db,'-XAt','-v','ON_ERROR_STOP=1','-c',statement])
 def mandatory(log,name):assert '--- PASS: '+name+' ' in log and not any(x in log for x in ['--- FAIL','--- SKIP','DATA RACE'])
 def private_roundtrip(label):
  ddl='BEGIN;\n'+(REPO/'migrations/000034_knowledge_private_index.down.sql').read_text()+'\n'+(REPO/'migrations/000034_knowledge_private_index.up.sql').read_text()+'\nCOMMIT;'
  return sql(DATABASE,ddl)
 try:
  run('build-server',['go','test','-race','-c','-o',str(work/'native/server.test'),'./internal/server'])
  run('build-knowledge',['go','test','-race','-c','-o',str(work/'native/knowledge.test'),'./internal/modules/features/knowledge'])
  run('build-ctl',['go','build','-o',str(work/'native/campusosctl'),'./cmd/campusosctl'])
  binaries={str(p.relative_to(REPO)):sha(p) for p in (work/'native').iterdir()}
  run('web-build',['pnpm','--dir','web','build']);shutil.copytree(REPO/'web/dist',work/'web-dist')
  bundles={str(p.relative_to(REPO)):sha(p) for p in (work/'web-dist').rglob('*') if p.is_file()}
  run('pg-start',['docker','run','--rm','--pull=never','-d','--name',name,'--tmpfs','/var/lib/postgresql/data:rw,size=768m','-e','POSTGRES_USER=campusos','-e','POSTGRES_DB='+DATABASE,'-e','POSTGRES_PASSWORD','-p','127.0.0.1::5432','postgres:16-alpine'],{'POSTGRES_PASSWORD':password});started=True
  for _ in range(120):
   if subprocess.run(['docker','exec',name,'pg_isready','-h','127.0.0.1','-U','campusos','-d',DATABASE],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
   time.sleep(.25)
  else:raise AssertionError('owned PG deadline')
  port=int(run('pg-port',['docker','port',name,'5432/tcp']).strip().rsplit(':',1)[1])
  pg={'PSQL_MODE':'docker','POSTGRES_CONTAINER':name,'DB_USER':'campusos','DB_NAME':DATABASE,'DB_PASSWORD':password,'DB_HOST':'127.0.0.1','DB_PORT':str(port)}
  run('migrate-up',['bash','scripts/migrate.sh','up'],pg);epoch=int(sql(DATABASE,"SELECT nextval('knowledge_private_epoch_seq')").strip());private_roundtrip('empty-private34-roundtrip');assert int(sql(DATABASE,"SELECT nextval('knowledge_private_epoch_seq')").strip())>epoch;run('migrate-check',['bash','scripts/migrate.sh','check'],pg);run('migrate-status',['bash','scripts/migrate.sh','status'],pg);run('database-entry',['bash','scripts/database-check.sh','all'],pg)
  assert sql(DATABASE,"SELECT count(*) FROM pg_tables WHERE schemaname='public';SELECT max(version)::int FROM schema_migrations").splitlines()==['132','35']
  dump=['docker','exec',name,'pg_dump','-U','campusos','-d',DATABASE,'--schema-only','--no-owner','--no-privileges'];before=run('schema-before',dump)
  # The offline recovery CLI needs libpq's psql. This owned shim transports
  # the exact SQL bytes over stdin to the already-owned PG container; no fake DB.
  shim=work/'shims/psql';shim.write_text('''#!/usr/bin/env python3
import os,pathlib,subprocess,sys
args=sys.argv[1:];data=None
if '-f' in args:
 i=args.index('-f');data=pathlib.Path(args[i+1]).read_bytes();del args[i:i+2]
db=os.environ.get('PGDATABASE','')
if db!='campusos_v12_13_restore':raise SystemExit('owned restore database required')
raise SystemExit(subprocess.run(['docker','exec','-i',os.environ['POSTGRES_CONTAINER'],'psql','-U','campusos','-d',db]+args,input=data).returncode)
''');shim.chmod(0o700)
  # Restore and unit domains use independent databases with the current schema.
  for db in ['campusos_v12_13_restore','campusos_v12_13_unit']:
   sql('postgres','CREATE DATABASE '+db)
   run(db+'-migrate',['bash','scripts/migrate.sh','up'],pg|{'DB_NAME':db})
  dsn=lambda db:f'postgres://campusos:{password}@127.0.0.1:{port}/{db}?sslmode=disable'
  unit=run('native-private-index-unit',[str(work/'native/knowledge.test'),'-test.v','-test.run=^TestPrivate(IndexPostgres|Vault.*|Preflight.*)$'],{'CAMPUSOS_PRIVATE_INDEX_DSN':dsn('campusos_v12_13_unit')});mandatory(unit,'TestPrivateIndexPostgres');assert not any(x in unit for x in ['--- SKIP','DATA RACE','--- FAIL'])
  actual_env=pg|{'PATH':str(work/'shims')+':'+env['PATH'],'DATABASE_DSN':dsn(DATABASE),'CAMPUSOS_PRIVATE_TARGET_DSN':dsn(DATABASE),'CAMPUSOS_PRIVATE_RESTORE_DSN':dsn('campusos_v12_13_restore'),'CAMPUSOS_PRIVATE_TARGET_ROOT':str(work/'runtime'),'CAMPUSOS_PRIVATE_REPO':str(REPO),'KNOWLEDGE_PRIVATE_KEY_DIR':str(work/'keys'),'CAMPUSOS_PRIVATE_WEB_DIST':str(work/'web-dist'),'CAMPUSOSCTL':str(work/'native/campusosctl'),'REDIS_ENABLED':'false','NATS_URL':'nats://127.0.0.1:1','HOST_API_ENABLED':'false','JWT_SECRET':secrets.token_hex(32),'AUTH_PASSWORD_HASH_ENABLED':'true','AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN':'false','AUTH_BOOTSTRAP_ADMIN_SECRET':secrets.token_hex(24),'EMAIL_PROVIDER':'fake','AI_ENABLED':'false','AUTH_MFA_ACTIVE_KEY_ID':'owned13index','AUTH_MFA_ENCRYPTION_KEYS':'owned13index:'+secrets.token_hex(32),'PLUGIN_DATA_DIR':str(work/'runtime/data/plugin_data'),'GIN_MODE':'release','OBSERVABILITY_PROMETHEUS_ENABLED':'false'}
  actual=run('native-private-index-target',[str(work/'native/server.test'),'-test.v','-test.run=^TestKnowledgePrivateIndexPostgres$','-test.timeout=300s'],actual_env);mandatory(actual,'TestKnowledgePrivateIndexPostgres')
  proof=json.loads((work/'runtime/private-index-proof.json').read_text());assert proof['status']=='passed' and proof['actual_full_backup_restore'] and proof['browser']['status']=='passed' and not proof['mocked_api_data']
  # The same frozen ELF proves default protection is unavailable with real
  # sessions/PG even though the public Knowledge Feature is enabled.
  legacy_db='campusos_v12_13_private';sql('postgres','CREATE DATABASE '+legacy_db)
  run('default-protection-migrate',['bash','scripts/migrate.sh','up'],pg|{'DB_NAME':legacy_db})
  legacy_root=REPO/'.cache/v12-13-private-preflight'/('current-full-'+secrets.token_hex(8));legacy_root.mkdir(parents=True,mode=0o700)
  legacy_env={k:v for k,v in actual_env.items() if k!='KNOWLEDGE_PRIVATE_KEY_DIR'}
  legacy_env.update(DATABASE_DSN=dsn(legacy_db),CAMPUSOS_KNOWLEDGE_PRIVATE_DSN=dsn(legacy_db),CAMPUSOS_KNOWLEDGE_PRIVATE_ROOT=str(legacy_root),PLUGIN_DATA_DIR=str(legacy_root/'data/plugin_data'))
  default_log=run('native-default-protection',[str(work/'native/server.test'),'-test.v','-test.run=^TestKnowledgePrivatePreflightPostgres$','-test.timeout=180s'],legacy_env);mandatory(default_log,'TestKnowledgePrivatePreflightPostgres')
  default_proof=json.loads((legacy_root/'target-proof.json').read_text());assert default_proof['check_count']==29 and not default_proof['private_index_enabled']
  assert sql(legacy_db,'SELECT (SELECT count(*) FROM knowledge_private_sources)+(SELECT count(*) FROM knowledge_private_tasks)').strip()=='0'
  shutil.move(legacy_root,work/'default-protection-runtime');proof['default_protection']=default_proof
  # Empty private-domain down/up with ALL original business rows populated.
  # The fingerprint covers every old table and sequence; no schema reset.
  fingerprint_sql="""CREATE TEMP TABLE private_drill_facts(name text,digest text); DO $$ DECLARE r record; h text; BEGIN
  FOR r IN SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename NOT IN ('schema_migrations','schema_migration_locks','knowledge_private_sources','knowledge_private_tasks') ORDER BY tablename LOOP
   EXECUTE format('SELECT md5(coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text,''[]'')) FROM public.%I t',r.tablename) INTO h;
   INSERT INTO private_drill_facts VALUES(r.tablename,h);
  END LOOP;
  FOR r IN SELECT sequencename FROM pg_sequences WHERE schemaname='public' AND sequencename<>'knowledge_private_epoch_seq' ORDER BY sequencename LOOP
   EXECUTE format('SELECT last_value::text||'':''||is_called::text FROM public.%I',r.sequencename) INTO h;
   INSERT INTO private_drill_facts VALUES('sequence:'||r.sequencename,h);
  END LOOP;END $$; SELECT name||':'||digest FROM private_drill_facts ORDER BY name;"""
  old_before=sql(DATABASE,fingerprint_sql)
  assert sql(DATABASE,'SELECT (SELECT count(*) FROM knowledge_private_sources)+(SELECT count(*) FROM knowledge_private_tasks)').strip()=='0'
  last_epoch=int(sql(DATABASE,'SELECT last_value FROM knowledge_private_epoch_seq').strip())
  private_roundtrip('populated-old-domain-private34-roundtrip')
  assert sql(DATABASE,fingerprint_sql)==old_before,'original business rows or old sequence high water changed'
  assert int(sql(DATABASE,"SELECT nextval('knowledge_private_epoch_seq')").strip())>last_epoch,'consent/lease epoch reused'
  proof['migration_original_tables_and_sequences_preserved']=True
  run('database-exit',['bash','scripts/database-check.sh','all'],pg);after=run('schema-after',dump);assert norm(before)==norm(after),'whole Schema changed during target'
  assert sources()==frozen,'source changed during frozen target';assert all(sha(REPO/p)==h for p,h in binaries.items());assert all(sha(REPO/p)==h for p,h in bundles.items())
  report=dict(schema='campusos.v12-13-exit/v1',status='passed',stage='V12-13',stage_status='completed_current_linux_scope',implementation='passed',automation='passed',target_environment='passed_linux_actual_modules_router_pg16_browser_full_backup_restore',proof=proof,source_sha256=frozen,binary_sha256=binaries,built_web_sha256=bundles,dependency={str(PRIOR.relative_to(REPO)):PRIOR_SHA},schema_before_after_sha256=hashlib.sha256(norm(after).encode()).hexdigest(),public_tables=132,migration_version=35,windows='deferred_by_user_not_run',commit_or_push=False,scope_limits=['private derived text only; original source storage/backup remains owned by original module','text/Markdown/CampusDoc and owned discussion text posts; PDF/DOCX/article/image extraction unsupported','no AI/egress or private MCP/Integration/plugin Host capability','Linux key vault ACL contract; native Windows protection not accepted','normal production modules/router in native host; no standalone cmd/server deployment claim'])
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
  shutil.rmtree(work/'keys');cleanup['owned_test_key_vault_removed']=not (work/'keys').exists()
  (work/'commands.json').write_text(json.dumps(commands,ensure_ascii=False,indent=2)+'\n');(work/'cleanup.json').write_text(json.dumps(cleanup,indent=2)+'\n')
  if started:assert cleanup['container_removed'] and cleanup['loopback_port_closed'] and not cleanup['errors']
 assert report is not None
 report['commands'],report['cleanup']=commands,cleanup;(work/'target.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');print(json.dumps({'status':'passed','checks':report['proof']['check_count'],'browser_checks':report['proof']['browser']['check_count'],'work':str(work.relative_to(REPO))}))
if __name__=='__main__':
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--work',required=True);main(Path(parser.parse_args().work).resolve())
