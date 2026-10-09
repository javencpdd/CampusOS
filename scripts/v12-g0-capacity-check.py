#!/usr/bin/env python3
"""Fixed, synthetic G0 capacity sample; credentials and bodies never archived."""
import hashlib
import json
import math
import os
import platform
import subprocess
import time
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
from pathlib import Path

from v12_g0_http import Client, require

PROFILE = Path('docs/项目计划书v1/项目计划v1.2/evidence/v12-g0-capacity-profile.json')
SEED = """
INSERT INTO threads(id,title,content,author_id,author_name,category_id,status,thread_type,created_at)
SELECT 1000000+n, 'G0 capacity ' || n, repeat('G0 synthetic content. ',50),
       2001+(n%2), 'G0 fixture', 2003, 'published',
       CASE WHEN n<=6993 THEN 'discussion' WHEN n<=8493 THEN 'mutual_aid' ELSE 'secondhand' END,
       now()-n*interval '1 second'
FROM generate_series(1,9993) n;
INSERT INTO mutual_aid_details(thread_id,aid_type,contact_mode,created_by)
SELECT id,'request','in_app',author_id FROM threads WHERE thread_type='mutual_aid';
INSERT INTO secondhand_details(thread_id,price_minor,item_condition,trade_method,created_by)
SELECT id,1000,'good','in_person',author_id FROM threads WHERE thread_type='secondhand';
ANALYZE threads; ANALYZE mutual_aid_details; ANALYZE secondhand_details;
"""
COUNTS = """
SELECT json_build_object(
 'threads',(SELECT count(*) FROM threads),
 'mutual_aid_threads',(SELECT count(*) FROM mutual_aid_details),
 'secondhand_threads',(SELECT count(*) FROM secondhand_details),
 'documents',(SELECT count(*) FROM personal_documents),
 'versions',(SELECT count(*) FROM personal_document_versions),
 'objects',(SELECT count(*) FROM storage_objects WHERE status='ready'),
 'bytes',(SELECT sum(size_bytes) FROM storage_objects WHERE status='ready'),
 'balanced_ledgers',(SELECT count(*) FROM user_storage_accounts a WHERE a.user_id IN(2001,2002)
   AND a.reserved_bytes=0 AND a.used_bytes=(SELECT sum(size_bytes) FROM storage_objects o
    WHERE o.owner_user_id=a.user_id AND o.status='ready')));
"""


def psql(sql):
    container = os.environ['V12_G0_DB_CONTAINER']
    require(container.startswith('campusos-v12-g0-visibility-'), 'disposable container required')
    env = dict(os.environ, PGPASSWORD=os.environ['V12_G0_DB_PASSWORD'])
    result = subprocess.run(['docker', 'exec', '-i', '-e', 'PGPASSWORD', container, 'psql',
                             '-U', 'campusos', '-d', os.environ['V12_G0_DB_NAME'], '-v', 'ON_ERROR_STOP=1', '-At'],
                            input=sql, text=True, capture_output=True, env=env, check=False)
    require(result.returncode == 0, 'isolated capacity SQL failed: ' + result.stderr[:500])
    return result.stdout


def envelope(client, method, path, body=None):
    status, data = client.request(method, path, body)
    require(status in (200, 201) and data.get('code') == 0, f'capacity fixture/metrics failed: HTTP {status}')
    return data['data']


def proc_memory():
    values = {}
    for line in Path('/proc', os.environ['V12_G0_API_PID'], 'status').read_text().splitlines():
        if line.startswith(('VmRSS:', 'VmHWM:')):
            key, value, _ = line.split()
            values[key.rstrip(':')] = int(value)*1024
    return values


def main():
    profile = json.loads(PROFILE.read_text())
    base = os.environ['V12_G0_BASE_URL']
    owners = []
    for label in ('a', 'b'):
        client = Client(base)
        client.token = envelope(client, 'POST', '/api/v1/auth/login',
                                {'email': f'g0-owner-{label}@example.invalid',
                                 'password': os.environ[f'V12_G0_OWNER_{label.upper()}_PASSWORD']})['access_token']
        existing = envelope(client, 'GET', '/api/v1/documents')['items']
        for index in range(len(existing), profile['personal_documents']//2):
            envelope(client, 'POST', '/api/v1/documents', {'name': f'g0-capacity-{label}-{index}.txt',
                     'format': 'text', 'content': 'G0 synthetic private data\n' + 'x'*4070})
        owners.append(client)
    psql(SEED)
    counts = json.loads(psql(COUNTS))
    require(counts['threads'] == 10000 and counts['documents'] == 200
            and counts['mutual_aid_threads'] == 1500 and counts['secondhand_threads'] == 1500
            and counts['versions'] == counts['objects'] == 202 and counts['balanced_ledgers'] == 2,
            'fixed capacity fixture mismatch')
    admin = Client(base)
    admin.token = envelope(admin, 'POST', '/api/v1/auth/admin/login',
                           {'email': 'admin@campusos.local', 'password': os.environ['V12_G0_BOOTSTRAP_PASSWORD']})['access_token']
    before = envelope(admin, 'GET', '/api/v1/metrics/summary')
    fixtures = json.loads(Path(os.environ['V12_G0_WORK_DIR'], 'browser-fixture.json').read_text())
    pdf_id = fixtures['documents']['g0.pdf']['id']
    endpoints = [('health', '/api/v1/health', ''),
                 ('threads', '/api/v1/threads?page=1&page_size=20', ''),
                 ('mutual_aid', '/api/v1/mutual-aid/threads?page=1&page_size=20', ''),
                 ('secondhand', '/api/v1/secondhand/threads?page=1&page_size=20', ''),
                 ('private_documents', '/api/v1/documents?status=active', owners[0].token),
                 ('private_pdf', '/api/v1/documents/' + pdf_id + '/download', owners[0].token)]
    results = []
    for name, path, token in endpoints:
        def request(_):
            client = Client(base)
            client.token = token
            started = time.perf_counter()
            status, _, raw = client.request_bytes('GET', path)
            elapsed = (time.perf_counter()-started)*1000
            valid = status == 200
            if valid and name != 'private_pdf':
                payload = json.loads(raw)
                valid = payload.get('code') == 0
                if name in ('threads', 'mutual_aid', 'secondhand'):
                    valid = valid and len(payload['data']['items']) == 20
                if name == 'private_documents':
                    valid = valid and len(payload['data']['items']) == 100 and all(item['owner_id']=='2001' for item in payload['data']['items'])
            return {'http_status': status, 'ms': round(elapsed, 3), 'valid': valid, 'bytes': len(raw), 'item_count': len(payload.get('data', {}).get('items', [])) if name not in ('health', 'private_pdf') and status == 200 else None}
        with ThreadPoolExecutor(max_workers=profile['concurrency']) as pool:
            warmup = list(pool.map(request, range(profile['concurrency'])))
            require(all(item['valid'] for item in warmup), name + ': warmup failed: ' + json.dumps(warmup))
            samples = list(pool.map(request, range(profile['samples_per_endpoint'])))
        latencies = sorted(item['ms'] for item in samples)
        p95 = latencies[math.ceil(len(latencies)*.95)-1]
        require(all(item['valid'] for item in samples), name + ': unsuccessful/incorrect request')
        require(p95 <= profile['max_p95_ms'], name + ': p95 exceeds frozen budget')
        # Resource IDs and all response bodies stay out of the public report.
        results.append({'name': name, 'samples': samples, 'p50_ms': latencies[math.ceil(len(latencies)*.5)-1],
                        'p95_ms': p95, 'p99_ms': latencies[math.ceil(len(latencies)*.99)-1]})
    after = envelope(admin, 'GET', '/api/v1/metrics/summary')
    reliability = envelope(admin, 'GET', '/api/v1/platform/reliability/summary')
    memory = proc_memory()
    require(memory['VmHWM'] <= profile['max_api_rss_bytes'], 'API peak RSS exceeds budget')
    require(after['database']['empty_acquire_wait_seconds'] <= profile['max_db_empty_wait_seconds'], 'DB pool wait exceeds budget')
    require(reliability['dead'] == 0 and reliability['oldest_pending_age_seconds'] <= profile['max_queue_age_seconds'],
            'queue budget exceeded')
    cpu_model = next(line.split(':',1)[1].strip() for line in Path('/proc/cpuinfo').read_text().splitlines() if line.startswith('model name'))
    memory_kib = int(Path('/proc/meminfo').read_text().splitlines()[0].split()[1])
    report = {'schema': 'campusos.v12-g0-capacity/v1', 'generated_at': datetime.now(timezone.utc).isoformat(),
              'profile_sha256': hashlib.sha256(PROFILE.read_bytes()).hexdigest(), 'profile': profile,
              'environment': {'os': platform.system(), 'kernel': platform.release(), 'arch': platform.machine(),
                              'cpu_model': cpu_model, 'logical_cpus': os.cpu_count(), 'cpu_affinity_count': len(os.sched_getaffinity(0)),
                              'host_memory_bytes': memory_kib*1024, 'api_gomaxprocs': 8,
                              'database': 'postgres:16-alpine', 'database_mount': 'tmpfs', 'api_bind': 'loopback'},
              'fixture_counts': counts, 'endpoints': results,
              'runtime_before': before['runtime'], 'runtime_after': after['runtime'],
              'database_after': after['database'], 'api_memory_bytes': memory,
              'reliability': {key: reliability[key] for key in ('health','dead','oldest_pending_age_seconds')},
              'initial_reference': True, 'regression_comparison': 'no earlier same-profile reference',
              'gates': 'passed'}
    Path(os.environ['V12_G0_OUTPUT']).write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('G0 capacity passed: 10000 threads, 200 documents, 1 plugin, concurrency 8, 600 measured requests')
    print('p95 ms: ' + ', '.join(f'{item["name"]}={item["p95_ms"]}' for item in results))


if __name__ == '__main__':
    main()
