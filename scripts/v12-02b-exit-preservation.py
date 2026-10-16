#!/usr/bin/env python3
"""Current-source, data-preserving 000016-000024 drill on an owned database only."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

assert os.environ.get('DB_NAME') == 'campusos_v12_02b_preservation', 'owned preservation database required'
assert os.environ.get('DRILL_DB_CONTAINER', '').startswith('campusos-v12-02b-'), 'owned disposable container required'
assert os.environ.get('CAMPUSOS_SKIP_DOTENV') == 'true', 'external database settings must be disabled'
root = Path.cwd()
out = Path(sys.argv[1]).resolve()
out.mkdir(parents=True, exist_ok=True)
env = dict(os.environ, PGPASSWORD=os.environ['DB_PASSWORD'])
base = ['psql', '-h', '127.0.0.1', '-U', 'campusos', '-d', env['DB_NAME'], '-qAtv', 'ON_ERROR_STOP=1']

def sql(query):
    return subprocess.check_output(base + ['-c', query], env=env, text=True).strip()

def migrate(action, label):
    with (out / f'{label}.log').open('w') as log:
        subprocess.run(['bash', 'scripts/migrate.sh', action], env=env, stdout=log, stderr=subprocess.STDOUT, check=True)

def below(version, label):
    while int(sql('SELECT max(version) FROM schema_migrations')) >= version:
        latest = sql('SELECT max(version) FROM schema_migrations')
        migrate('down', f'{label}-down-{latest}')

def file_fixture(name):
    subprocess.run(base + ['-f', 'scripts/fixtures/' + name], env=env, check=True, stdout=subprocess.DEVNULL)

specs = [
 (16, ['threads'], 'thread_facts_version_seq', None),
 (17, ['admin_role_permissions'], 'admin_permission_facts_version_seq', None),
 (18, ['threads','content_revisions','content_moderation_cases','content_moderation_actions','richtext_article_contents','asset_lifecycle_audits','user_spaces','user_storage_quotas'], None, 'v12-02b-business-actor-legacy.sql'),
 (19, ['posts'], 'post_facts_version_seq', 'v12-02b-post-facts-legacy.sql'),
 (20, ['richtext_article_contents'], 'article_facts_version_seq', 'v12-02b-article-facts-legacy.sql'),
 (21, ['roles','user_roles','role_permissions','permission_definitions'], 'user_scope_facts_version_seq', 'v12-02b-user-scope-facts-legacy.sql'),
 (22, ['user_assets','richtext_article_attachments'], 'richtext_asset_binding_facts_version_seq', 'v12-02b-richtext-asset-facts-legacy.sql'),
 (23, ['user_spaces','user_space_style_snapshots'], 'personal_space_facts_version_seq', 'v12-02b-space-facts-legacy.sql'),
 (24, ['user_storage_quotas'], 'storage_quota_facts_version_seq', 'v12-02b-storage-quota-facts-legacy.sql'),
]
def highwater(sequence):
    return int(sql(f'SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM public.{sequence}'))

def snapshot(tables, fields, path):
    result = {}
    for table in tables:
        # Preserve the complete actual pre-migration column set, including raw
        # lifecycle versions, JSON, dates, old FKs and any earlier typed actors.
        # Authorization nonce allocations are separately checked as monotonic.
        columns = fields[table]
        projection = ','.join("'%s',r.\"%s\"" % (c, c) for c in columns)
        order = 'r.role_id,r.permission_code' if table == 'admin_role_permissions' else 'r.user_id' if table == 'user_storage_quotas' else 'r.id'
        result[table] = json.loads(sql(f"SELECT COALESCE(jsonb_agg(jsonb_build_object({projection}) ORDER BY {order}),'[]'::jsonb)::text FROM public.{table} r"))
    path.write_text(json.dumps(result, sort_keys=True, ensure_ascii=False, indent=2) + '\n')
    return hashlib.sha256(path.read_bytes()).hexdigest()

migrate('up', 'initial-up')
results = []
for version, tables, sequence, fixture in specs:
    label = f'{version:06d}'
    print('preservation', label, flush=True)
    below(version, label + '-initial')
    if version == 16:
        sql("""INSERT INTO users(id,username,nickname,email) VALUES(97301,'backfill-owner','Backfill','backfill@test.local');
        INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(97310,'Backfill group','backfill-group','group','active');
        INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(97311,'Backfill board','backfill-board',97310,'board','active');
        INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(97311,'discussion',true);
        INSERT INTO threads(id,title,content,author_id,author_name,category_id,status,publication_status,moderation_status,deletion_status)
        VALUES(97320,'backfill title','preserved body',97301,'Backfill',97311,'published','published','clear','active');""")
    if fixture:
        file_fixture(fixture)
    fields = {table: [c for c in sql(f"SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='{table}' ORDER BY ordinal_position").splitlines() if c != 'facts_version'] for table in tables}
    assert all(fields.values())
    paths = [out / f'{label}-{phase}.json' for phase in ['before','after-up','after-down','after-reup']]
    hashes = [snapshot(tables, fields, paths[0])]
    values = [highwater(sequence)] if sequence else []
    migrate('up', label + '-up')
    hashes.append(snapshot(tables, fields, paths[1]))
    if sequence:
        lowest = int(sql('SELECT min(facts_version) FROM (' + ' UNION ALL '.join('SELECT facts_version FROM public.' + t for t in tables) + ') facts'))
        assert lowest > values[-1], (label, 'backfill reused an allocated nonce')
        values.append(highwater(sequence))
    below(version, label + '-populated')
    hashes.append(snapshot(tables, fields, paths[2]))
    if sequence:
        current = highwater(sequence)
        assert current >= values[-1], (label, 'down lost high water')
        values.append(current)
    migrate('up', label + '-reup')
    hashes.append(snapshot(tables, fields, paths[3]))
    if sequence:
        lowest = int(sql('SELECT min(facts_version) FROM (' + ' UNION ALL '.join('SELECT facts_version FROM public.' + t for t in tables) + ') facts'))
        assert lowest > values[-1], (label, 're-up reused an allocated nonce')
        values.append(highwater(sequence))
    assert len(set(hashes)) == 1, (label, 'original fields changed')
    result = {'migration': label, 'status': 'passed', 'tables': tables, 'original_columns': fields, 'nonce_sequence': sequence, 'highwater': values, 'snapshots': [{'path':str(p),'sha256':h} for p,h in zip(paths,hashes)]}
    results.append(result)
    (out / 'preservation.json').write_text(json.dumps({'status':'in_progress','migrations':results}, ensure_ascii=False, indent=2) + '\n')
migrate('check', 'final-check')
assert int(sql('SELECT max(version) FROM schema_migrations')) == 24
(out / 'preservation.json').write_text(json.dumps({'status':'passed','current':24,'next':25,'migrations':results}, ensure_ascii=False, indent=2) + '\n')
print('All nine current-source migration preservation paths passed', flush=True)
