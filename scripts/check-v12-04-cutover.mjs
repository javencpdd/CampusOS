#!/usr/bin/env node
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
execFileSync(process.execPath, ['scripts/check-v12-03c-cutover.mjs'], { stdio: 'inherit' })
const read = p => readFileSync(p, 'utf8')
const overlay = JSON.parse(read('docs/api/v1.2-04界面切换覆盖.json'))
assert.equal(overlay.contract, 'campusos.v12-04-cutover/v1')
assert.deepEqual(overlay.boundaries.map(x => x.id), ['admin.v5-entry', 'user.v5-entry', 'ui.asset-host', 'pdf.current-authority'])
for (const x of overlay.boundaries) { assert.equal(x.runtime_status, 'current'); assert(x.current_path.length && x.retirement_boundary.length > 20) }
for (const p of ['admin/src/modules/plugins/routes.ts', 'web/src/modules/plugin-center/routes.ts']) assert(read(p).includes('PluginV5'))
for (const p of ['web/src/campus-ui/pluginConsent.ts', 'web/src/campus-ui/isolatedPluginHost.ts', 'web/src/modules/plugin-runtime/api.ts']) {
 for (const old of ['PluginAuthorizationService', '/plugins/authorizations', '/plugins/marketplace']) assert(!read(p).includes(old), p + old)
}
const gateway = read('deploy/docker/plugin-ui-server.mjs')
for (const old of ['readFile', '.installed', 'PLUGIN_V4']) assert(!gateway.includes(old))
const start = read('internal/server/bootstrap.go')
assert(start.includes('plugins.platform.PDFUI('))
assert(!start.includes('plugin.NewUIRuntimeSurfaceValidator('))
console.log('04 composed cutover: current v5 forms/entries, isolated asset gateway and current PDF authority; historical code grants no fallback')
