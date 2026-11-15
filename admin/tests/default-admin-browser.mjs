import assert from 'node:assert/strict'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { createServer as portProbe } from 'node:net'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from '../../web/node_modules/playwright-core/index.mjs'
import { createServer } from 'vite'

const config = JSON.parse(await readFile(process.env.CAMPUSOS_DEFAULT_ADMIN_BROWSER_CONFIG, 'utf8'))
assert.equal(config.ownedFixture, true, 'password changes require an explicitly owned isolated fixture')
assert.equal(new URL(config.backend).hostname, '127.0.0.1')
assert.ok(config.database?.startsWith('campusos_admin_default_e2e_'))
const root = fileURLToPath(new URL('../../', import.meta.url))
const report = {}
let browser, server, page, cache, phase = 'setup'
try {
  cache = await mkdtemp(resolve(tmpdir(), 'campusos-admin-default-'))
  const probe = portProbe()
  await new Promise((done) => probe.listen(0, '127.0.0.1', done))
  const port = probe.address().port
  await new Promise((done) => probe.close(done))
  server = await createServer({ root: resolve(root, 'admin'), configFile: resolve(root, 'admin/vite.config.ts'), cacheDir: cache, server: { host: '127.0.0.1', port, strictPort: true, proxy: { '/api': { target: config.backend, changeOrigin: false } } } })
  await server.listen()
  const base = `http://127.0.0.1:${port}`
  browser = await chromium.launch({ executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox', '--no-proxy-server'] })
  const context = await browser.newContext()
  page = await context.newPage()
  async function login(password, expected = 200) {
    await page.getByPlaceholder('请输入管理员用户名或邮箱').fill('admin')
    await page.getByPlaceholder('请输入密码', { exact: true }).fill(password)
    const [response] = await Promise.all([
      page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/admin/identity/login'),
      page.locator('form').getByRole('button', { name: '登录', exact: true }).click(),
    ])
    assert.equal(response.status(), expected)
    if (expected !== 200) return null
    const data = (await response.json()).data
    await page.waitForURL((url) => url.pathname === '/')
    return data.access_token
  }
  phase = 'default login'
  await page.goto(`${base}/login`, { waitUntil: 'domcontentloaded' })
  await page.getByText('空管理域首次启动默认账号：admin / admin；修改后使用新密码。', { exact: true }).waitFor()
  const oldToken = await login('admin')
  report.defaultLogin = true
  phase = 'change password UI'
  await page.getByRole('button', { name: '修改密码', exact: true }).click()
  await page.waitForURL((url) => url.pathname === '/account/security')
  await page.getByPlaceholder('请输入当前密码').fill('admin')
  await page.getByPlaceholder('请输入6–64字节的新密码').fill('New-default-fixture-password42')
  await page.getByPlaceholder('请再次输入新密码').fill('New-default-fixture-password42')
  const [changed] = await Promise.all([
    page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/admin/identity/password'),
    page.getByRole('button', { name: '修改密码并重新登录', exact: true }).click(),
  ])
  report.passwordChangeStatus = changed.status()
  assert.equal(changed.status(), 200)
  await page.waitForURL((url) => url.pathname === '/login')
  const oldSession = await context.request.get(`${base}/api/v1/admin/identity/me`, { headers: { Authorization: `Bearer ${oldToken}` } })
  report.oldSessionStatus = oldSession.status()
  assert.equal(oldSession.status(), 401)
  phase = 'old password rejection and new login'
  await login('admin', 401)
  report.oldPasswordRejected = true
  const token = await login('New-default-fixture-password42')
  const me = await context.request.get(`${base}/api/v1/admin/identity/me`, { headers: { Authorization: `Bearer ${token}` } })
  const profile = (await me.json()).data
  assert.equal(me.status(), 200)
  assert.equal(profile.mfa_required, true)
  assert.equal(profile.mfa_enrolled, false)
  report.newPasswordLogin = true
  report.mfaPolicyPreserved = true
  await page.screenshot({ path: config.screenshot })
  report.passed = true
} catch (error) {
  report.failure = { phase, type: error.name }
  process.exitCode = 1
} finally {
  if (page) await page.evaluate(async () => { const { useAdminStore } = await import('/src/modules/identity/store.ts'); await useAdminStore().logout() }).catch(() => {})
  await writeFile(config.result, JSON.stringify(report, null, 2) + '\n')
  console.log(JSON.stringify(report))
  await browser?.close()
  await server?.close()
  if (cache) await rm(cache, { recursive: true, force: true })
}
