#!/usr/bin/env node
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
execFileSync(process.execPath,['scripts/check-v12-03a-replacement-cutover.mjs'],{stdio:'inherit'});
const read=p=>readFileSync(p,'utf8');
const overlay=JSON.parse(read('docs/api/v1.2-03b旧机制退役覆盖.json'));
assert.equal(overlay.contract,'campusos.v12-03b-cutover/v1');
assert.deepEqual(overlay.overrides.map(x=>x.id),['plugin.legacy_manager']);
assert.equal(overlay.overrides[0].runtime_status,'retired');
const startup=read('internal/server/platform_modules.go');
const start=startup.slice(startup.indexOf('func (m *pluginPlatformModule) Start('),startup.indexOf('// ensurePDFViewerDefaultGrants'));
for(const forbidden of ['StartDesiredPlugins','NewAuthorizationService','NewMarketService','startHostAPI','StartHealthChecker','RegisterRuntime'])assert(!start.includes(forbidden),forbidden);
assert(start.includes('plugin.NewV5Platform('));assert(start.includes('.ReconcileStartup('));assert(start.includes('.ProcessOnce('));
assert(!startup.includes('pluginport.NewCatalogAdapter(m.manager.Catalog())'));
const routes=read('internal/transport/httpapi/router.go');
assert(!/d\.Plugin\./.test(routes),'legacy Handler still reachable');
assert(routes.includes('plugin.V5RetiredHTTP'));
// Preserve the nine 03b routes and account for the six finite 04 read-only
// endpoints explicitly; an arbitrary additional control still fails the gate.
const uiReads=[
 ['/plugin-v5/:publisher/:key/:version/config-options','ConfigOptions(false)'],
 ['/admin/plugin-v5/:publisher/:key/:version/config-options','ConfigOptions(true)'],
 ['/plugin-v5/:publisher/:key/:version/ui','UIState(false)'],
 ['/admin/plugin-v5/:publisher/:key/:version/ui','UIState(true)'],
 ['/plugin-v5/ui','UIManifest(false)'],['/admin/plugin-v5/ui','UIManifest(true)'],
];
for(const [path,handler] of uiReads)assert(routes.includes(`GET("${path}", d.PluginV5.${handler})`),path);
assert.equal((routes.match(/d\.PluginV5\./g)||[]).length,9+uiReads.length);
const state=read('internal/plugin/v5_state_pg.go');assert(state.includes('transaction.ExecutorFor'));
assert(read('internal/server/bootstrap.go').includes('plugin.NewPgV5StateStore(pool)'));
assert(!/\.Observe\(/.test(start),'production fake observer');
console.log('03b composed cutover: nine authoritative controls plus six bounded 04 reads; legacy controls retired');
