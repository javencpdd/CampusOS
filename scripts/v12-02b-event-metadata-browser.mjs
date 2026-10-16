// UI-only acceptance against the built Admin app and loopback mocked API.
// Actual JWT/Identity and PostgreSQL authority checks run separately in the drill.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import { resolve, extname, sep } from 'node:path'

const require = createRequire(resolve('web/package.json'))
const { chromium } = require('playwright-core')
const root = resolve('admin/dist')
const server = createServer(async (request, response) => {
  try {
    const path = resolve(root, '.' + new URL(request.url, 'http://localhost').pathname)
    if (path !== root && !path.startsWith(root + sep)) { response.writeHead(404).end(); return }
    const file = extname(path) ? path : resolve(root, 'index.html')
    const data = await readFile(file)
    const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' }
    response.writeHead(200, { 'Content-Type': types[extname(file)] || 'application/octet-stream' }).end(data)
  } catch { response.writeHead(404).end() }
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
const origin = `http://127.0.0.1:${server.address().port}`
let browser
try {
  browser = await chromium.launch({ executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox'] })
  const context = await browser.newContext({ viewport: { width: 1366, height: 900 } })
  await context.addCookies([{ name: 'campusos_admin_csrf', value: 'ui-only-nonce', url: origin }])
  const page = await context.newPage()
  const errors = []
  const requests = []
  let denied = false
  page.on('pageerror', error => errors.push(error.message))
  await context.route('**/*', async route => {
    const url = new URL(route.request().url())
    if (url.origin !== origin) { await route.abort(); return }
    if (!url.pathname.startsWith('/api/v1/')) { await route.continue(); return }
    let data = {}
    let status = 200
    switch (url.pathname) {
      case '/api/v1/admin/identity/refresh': data = { access_token: 'ui-only-mock-token' }; break
      case '/api/v1/admin/identity/me': data = { domain: 'admin', account: { id: 'adm.ui', username: 'ui-acceptance', display_name: 'UI acceptance', status: 'active' }, roles: [{ id: 'ui_custom_reader', name: '盘点人员' }], permission_codes: ['platform.reliability.read'], authentication_strength: 'password', mfa_enrolled: false, session_expires_at: new Date(Date.now() + 3600000).toISOString() }; break
      case '/api/v1/events': {
        requests.push(url.searchParams)
        status = denied ? 403 : 200
        const type = url.searchParams.get('type') || 'thread.created'
        data = denied ? null : { items: [{ id: '11111111-1111-4111-8111-111111111111', type, source: 'community', time: '2026-10-13T00:00:00Z', data: { body: 'PRIVATE_PAYLOAD_SENTINEL' }, subject: 'PRIVATE_SUBJECT_SENTINEL' }], total: 1 }
        break
      }
    }
    await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: status === 200 ? 0 : 20004, data, msg: status === 200 ? 'ok' : 'revoked' }) })
  })
  await page.goto(origin + '/events')
  await page.getByRole('cell', { name: 'thread.created', exact: true }).waitFor()
  assert.deepEqual(await page.locator('.admin-events th').allTextContents(), ['ID', '事件类型', '来源', '发生时间'])
  assert(!/PRIVATE_PAYLOAD_SENTINEL|PRIVATE_SUBJECT_SENTINEL|查看详情/.test(await page.locator('.admin-events').innerText()))
  await page.locator('.admin-events .el-select').click()
  await page.getByRole('option', { name: 'post.deleted', exact: true }).click()
  await page.getByRole('cell', { name: 'post.deleted', exact: true }).waitFor()
  assert.equal(requests.at(-1).get('type'), 'post.deleted')
  assert.equal(requests.at(-1).get('limit'), '100')
  denied = true
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await page.getByText('暂无事件记录', { exact: true }).waitFor()
  await page.getByText('加载事件记录失败，请确认当前管理权限', { exact: true }).waitFor()
  assert.equal(await page.getByRole('cell', { name: 'post.deleted', exact: true }).count(), 0)
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ browser: await browser.version(), status: 'passed', scope: 'built Admin UI with mocked loopback API; not real backend browser authentication', checks: ['metadata-only columns', 'private fields not rendered', 'server type filter and limit', 'revoked access clears rows', 'visible failure and empty state', 'no uncaught page errors'] }))
} finally {
  if (browser) await browser.close()
  await new Promise(resolve => server.close(resolve))
}
