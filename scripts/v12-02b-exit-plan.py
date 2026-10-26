#!/usr/bin/env python3
"""Build the current-source test union; do not infer closure from report names."""
import hashlib
import json
import os
from pathlib import Path
import re
import sys
from collections import defaultdict

source = defaultdict(list)
for base in ['internal', 'pkg', 'cmd']:
    for path in Path(base).rglob('*test.go'):
        for name in re.findall(r'^func (Test\w+)\(', path.read_text(), re.M):
            source[name].append(str(path))
reports = []
required = set()
for path in sorted(Path('docs/项目计划书v1/项目计划v1.2/evidence').glob('v12-02b*.json')):
    report = json.loads(path.read_text())
    # Neither output of this runner can become its own historical input.
    if path.name in {'v12-02b-exit-linux.json', 'v12-02b-admin-spa-linux.json'}:
        continue
    names = [name for name in report.get('checks', {}) if name.startswith('Test')]
    if names:
        reports.append({'path':str(path),'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'tests':names})
        required.update(names)
historical = set(required)
if os.environ.get('CAMPUSOS_FINAL_ACCEPTANCE') == 'true':
    for name, paths in source.items():
        if paths[0].endswith('/identity/service/integration_postgres_test.go') or name == 'TestIntegrationActualServerBrowserLifecycle':
            required.add(name)
for name in source:
    if name.startswith(('TestAdminSPA','TestPostgreSQLAdminMFA','TestPostgreSQLAdminSession','TestAdminMFA')) or name == 'TestAdminDomainHTTPBootstrapLoginAndCrossDomainRejection':
        required.add(name)
assert not required - source.keys(), sorted(required - source.keys())
assert all(len(source[name]) == 1 for name in required), 'test names must be bound to one current source'
community = {
 'public': 'thread_public_read_postgres_test.go',
 'list': 'thread_public_list_postgres_test.go',
 'owner': 'thread_owner_read_postgres_test.go',
 'owner_list': 'thread_owner_list_postgres_test.go',
}
shards = []
assigned = set()
for suffix, filename in community.items():
    names = sorted(name for name in required if source[name][0].endswith('/'+filename))
    assert names
    shards.append({'name':'community_'+suffix,'database':'campusos_v12_02b_'+suffix,'tests':names,'packages':['./internal/modules/core/community/service']})
    assigned.update(names)
main = sorted(required - assigned)
shards.insert(0, {'name':'main','tests':main,'packages':sorted({'./'+str(Path(source[name][0]).parent) for name in main})})
result = {'schema':'campusos.v12-02b-exit-test-plan/v1','historical_reports':reports,'historical_test_count':len(historical),'required_count':len(required),'required_tests':sorted(required),'new_or_changed_tests':sorted(required-historical),'source_files':{n:source[n][0]for n in sorted(required)},'shards':shards,'missing_current_source':[]}
path = Path(sys.argv[1]);path.parent.mkdir(parents=True,exist_ok=True);path.write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
print(f'{len(reports)} historical slice reports; {len(historical)} historical tests; {len(required)} current required tests; {len(shards)} environment shards; none missing')
