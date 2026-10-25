#!/usr/bin/env node
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
execFileSync(process.execPath,['scripts/check-v12-03b-cutover.mjs'],{stdio:'inherit'});
const read=p=>readFileSync(p,'utf8');
const base=JSON.parse(read('docs/api/v1.2旧机制删除与替换清单.json'));
const overlay=JSON.parse(read('docs/api/v1.2-03c旧机制退役覆盖.json'));
assert.equal(overlay.contract,'campusos.v12-03c-cutover/v1');
assert.deepEqual(overlay.overrides.map(x=>x.id),['plugin.legacy_capability_map','plugin.untyped_scope','plugin.legacy_event']);
for(const x of overlay.overrides){assert.equal(base.items.find(b=>b.id===x.id).replacement_stage,'V12-03c');assert.equal(x.runtime_status,'retired');assert.equal(x.retired_evidence,'v12-03c-exit-linux.json');assert(x.retirement_boundary.length>30);}
for(const p of ['v5_managed_runtime.go','v5_managed_host.go','v5_managed_dispatch.go','v5_events.go']){
 const code=read('internal/plugin/'+p);
 for(const forbidden of ['scopeContains(', 'NewAuthorizationService(', '.Permissions', 'Manifest.Event'])assert(!code.includes(forbidden),p+forbidden);
}
const host=read('internal/plugin/v5_managed_host.go');assert(host.includes('reflect.DeepEqual(declared, approved)'));assert(host.includes('endpoint.Authorize('));assert(host.includes('h.credentialForCall('));
const start=read('internal/server/platform_modules.go');assert(start.includes('plugin.NewV5ManagedRuntime(directory)'));assert(start.includes('m.metadata.Register()'));assert(start.includes('m.metadata.ProcessOnce(workerCtx, 16)'));
assert(read('internal/server/bootstrap.go').includes('plugin.NewPgV5EventStore(pool)'));
const events=read('internal/plugin/v5_events.go');assert(events.includes('transaction.Active(ctx)'));assert(events.includes('.CheckedRead('));assert(events.includes('.RegisterConsumer('));assert(!events.includes('source.Payload'));assert(!events.includes('source.Headers'));
const catalog=JSON.parse(read('docs/api/plugin-v5-host-catalog-current.json'));
assert.deepEqual(catalog.items.slice(0,4).map(x=>x.code),['community.thread.public.read','plugin.lifecycle.subscribe','community.thread.public.subscribe','knowledge.source.public.subscribe']);
assert(catalog.items.slice(0,4).every(x=>x.scope_kind==='public'&&x.data_classification==='public'&&x.consent_required===false));
assert.deepEqual(catalog.items.slice(4,6).map(x=>x.code),['ui.resource.selected.read','ui.surface.open']);
assert(catalog.items.slice(4,6).every(x=>x.scope_kind==='self'&&x.data_classification==='sensitive'&&x.consent_required===true));
assert.deepEqual(catalog.items.slice(6,8).map(x=>x.code),['ai.chat.local.invoke','ai.chat.remote.invoke']);
assert(catalog.items.slice(6,8).every(x=>x.scope_kind==='self'&&x.data_classification==='sensitive'&&x.consent_required===true));
assert.deepEqual(catalog.items.slice(8).map(x=>x.code),['knowledge.public.search','knowledge.public.source.read','knowledge.public.answer']);
assert(catalog.items.slice(8).every(x=>x.scope_kind==='collection'&&x.data_classification==='public'&&x.consent_required===false&&Array.isArray(x.available_public_collection_ids)&&x.available_public_collection_ids.length===0));
execFileSync(process.execPath,['scripts/check-v12-03c-native-dispatch.mjs'],{stdio:'inherit'});
console.log('03c composed cutover: typed managed dispatch and independent metadata delivery; legacy production authority retired');
