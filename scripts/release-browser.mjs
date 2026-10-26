import assert from 'node:assert/strict'
import { readFile, writeFile, stat, mkdir } from 'node:fs/promises'
import { createServer, request } from 'node:http'
import { resolve, extname, sep } from 'node:path'
import { spawn } from 'node:child_process'
import { assertResponsiveMatrix } from '../web/tests/helpers/responsive-contract.mjs'
const cfg = JSON.parse(await readFile(process.argv[2], 'utf8'))
assert.equal(new URL(cfg.backend).hostname, '127.0.0.1')
const output = resolve(cfg.output)
await mkdir(output, { recursive: true })
const servers = []
const checks = []
async function serve(folder, docs = false) {
  const root = resolve(folder)
  const server = createServer(async (req, res) => {
    if (req.url.startsWith('/api/')) {
      const upstream = request(new URL(req.url, cfg.backend), { method: req.method, headers: req.headers }, (r) => {
        res.writeHead(r.statusCode, r.headers)
        r.pipe(res)
      })
      upstream.on('error', () => { if (!res.headersSent) res.writeHead(502); res.end() })
      res.on('close', () => upstream.destroy())
      req.pipe(upstream)
      return
    }
    try {
      let file = resolve(root, '.' + decodeURIComponent(new URL(req.url, 'http://localhost').pathname))
      assert.ok(file === root || file.startsWith(root + sep))
      try { if ((await stat(file)).isDirectory()) file = resolve(file, 'index.html') } catch {
        file = docs ? file + '.html' : resolve(root, 'index.html')
      }
      const content = await readFile(file)
      const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png', '.ico': 'image/x-icon' }
      res.writeHead(200, { 'Content-Type': types[extname(file)] || 'application/octet-stream' })
      res.end(content)
    } catch { if (!res.headersSent) res.writeHead(404); res.end() }
  })
  await new Promise((r) => server.listen(0, '127.0.0.1', r))
  servers.push(server)
  return `http://127.0.0.1:${server.address().port}`
}
async function run(name, args, extra = {}) {
  const chunks = []
  const child = spawn('node', args, { env: { ...process.env, ...extra }, stdio: ['ignore', 'pipe', 'pipe'] })
  child.stdout.on('data', (v) => chunks.push(v))
  child.stderr.on('data', (v) => chunks.push(v))
  const code = await new Promise((r) => child.on('close', r))
  await writeFile(resolve(output, name + '.log'), Buffer.concat(chunks))
  assert.equal(code, 0, name + ' failed; inspect retained log')
  checks.push(name)
}
try {
  await run('responsive-contract', ['web/tests/responsive-contract-check.mjs'])
  const web = await serve('web/dist')
  const admin = await serve('admin/dist')
  const docs = await serve('docs-site/.vitepress/dist', true)
  Object.assign(process.env, { WEB_URL: web, ADMIN_URL: admin, DOCS_URL: docs, API_BASE_URL: cfg.backend + '/api/v1',
    CAMPUSOS_ADMIN_EMAIL: 'v5-chat@example.test', CAMPUSOS_ADMIN_USERNAME: 'restore-operator',
    CAMPUSOS_ADMIN_PASSWORD: cfg.password, CAMPUSOS_OWNED_BROWSER_TARGET: 'true', CHROME_BIN: cfg.chrome,
    STYLE_PACK_SCREENSHOT_DIR: resolve(output, 'style-pack'), RESPONSIVE_SCREENSHOT_DIR: resolve(output, 'responsive') })
  for (const [name, origin] of [['web', web], ['admin', admin], ['docs', docs]]) {
    await run('render-' + name, ['web/tests/browser-render-smoke.mjs', origin, resolve(output, name + '.png'), name])
  }
  await run('business', ['web/tests/browser-workflow.mjs'])
  await run('style-packs', ['web/tests/style-pack-responsive.mjs'])
  await run('responsive', ['web/tests/responsive-workflow.mjs'])
  const routes = JSON.parse(await readFile(resolve(output, 'responsive/route-records.json'), 'utf8'))
  assertResponsiveMatrix(routes, { web, admin })
  await writeFile(resolve(output, 'browser.json'), JSON.stringify({ status: 'passed', checks, own_loopback_origins: [web, admin, docs],
    responsive_routes: routes.length, responsive_viewports: 7, responsive_route_contract: 'passed',
    actual_built_UIs: true, mocked_API: false, current_independent_Admin: true, real_Admin_MFA_before_writes: true }, null, 2) + '\n')
} finally {
  for (const server of servers) server.closeAllConnections()
  await Promise.all(servers.map((server) => new Promise((r) => server.close(r))))
}
