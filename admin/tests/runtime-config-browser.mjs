import assert from 'node:assert/strict'
import { createHmac } from 'node:crypto'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { createServer as portProbe } from 'node:net'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from '../../web/node_modules/playwright-core/index.mjs'
import { createServer } from 'vite'

const config = JSON.parse(await readFile(process.env.CAMPUSOS_RUNTIME_CONFIG_BROWSER_CONFIG, 'utf8'))
assert.equal(config.ownedFixture, true)
assert.equal(new URL(config.backend).hostname, '127.0.0.1')
assert.ok(config.database?.startsWith('campusos_runtime_config_e2e_'))
const root = fileURLToPath(new URL('../../', import.meta.url))
const report = {}
let browser, server, page, cache, phase = 'setup'
try {
  cache = await mkdtemp(resolve(tmpdir(), 'campusos-runtime-config-'))
  const probe = portProbe()
  await new Promise(done => probe.listen(0, '127.0.0.1', done))
  const port = probe.address().port
  await new Promise(done => probe.close(done))
  server = await createServer({ root: resolve(root, 'admin'), configFile: resolve(root, 'admin/vite.config.ts'), cacheDir: cache, server: { host: '127.0.0.1', port, strictPort: true, proxy: { '/api': { target: config.backend, changeOrigin: false } } } })
  await server.listen()
  const base = `http://127.0.0.1:${port}`
  browser = await chromium.launch({ executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox', '--no-proxy-server'] })
  const context = await browser.newContext()
  page = await context.newPage()
  phase = 'login'
  await page.goto(`${base}/login`)
  await page.getByPlaceholder('请输入管理员用户名或邮箱').fill('admin')
  await page.getByPlaceholder('请输入密码', { exact: true }).fill('admin')
  const [login] = await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/admin/identity/login' && r.request().method() === 'POST'),
    page.locator('form').getByRole('button', { name: '登录', exact: true }).click(),
  ])
  assert.equal(login.status(), 200)
  const token = (await login.json()).data.access_token
  await page.waitForURL(u => u.pathname === '/')
  phase = 'configuration form'
  const [loaded] = await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/platform/runtime-config'),
    page.getByRole('menuitem', { name: '运行配置', exact: true }).click(),
  ])
  assert.equal(loaded.status(), 200)
  const initial = (await loaded.json()).data
  assert.equal(initial.available, true)
  assert.equal(initial.pending, false)
  assert.ok(!JSON.stringify(initial).includes('fixture-hidden-credential'))
  assert.equal(initial.fields.find(f => f.kind === 'secret').value, '')
  assert.equal(initial.fields.find(f => f.key === 'AUTH_ADMIN_MFA_REQUIRED').value, config.expectedMFARequired ? 'true' : 'false')
  report.redactionAndManagedPrecedence = true
  await page.screenshot({ path: config.screenshot, fullPage: true })
  phase = 'confirmed env document draft'
  await page.getByRole('button', { name: '打开配置文件', exact: true }).click()
  const openDialog = page.getByRole('dialog', { name: '验证并打开配置文件', exact: true })
  await openDialog.getByPlaceholder('请再次输入当前管理员密码').fill('admin')
  const [disclosed] = await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/platform/runtime-config/document'),
    openDialog.getByRole('button', { name: '验证并打开', exact: true }).click(),
  ])
  assert.equal(disclosed.status(), 200)
  assert.ok(disclosed.headers()['cache-control'].includes('no-store'))
  const editor = page.getByRole('dialog', { name: '编辑 env 配置文件', exact: true })
  const content = editor.getByRole('textbox', { name: 'env 配置文件内容', exact: true })
  const raw = await content.inputValue()
  assert.ok(raw.includes('fixture-hidden-credential'))
  await content.fill(raw.replace('AI_TIMEOUT=30s', 'AI_TIMEOUT=19s') + "EXTRA_DEPLOYMENT_SETTING='fixture-only'\n")
  await editor.getByRole('button', { name: '应用到运行配置', exact: true }).click()
  await page.getByRole('textbox', { name: 'AI 请求超时', exact: true }).waitFor()
  assert.equal(await page.getByRole('textbox', { name: 'AI 请求超时', exact: true }).inputValue(), '19s')
  await page.getByRole('textbox', { name: 'AI 请求超时', exact: true }).fill('18s')
  await page.getByRole('button', { name: '打开配置文件', exact: true }).click()
  assert.ok((await content.inputValue()).includes("AI_TIMEOUT='18s'"))
  await editor.getByRole('button', { name: '应用到运行配置', exact: true }).click()
  report.confirmedFullEnvAndBidirectionalDraft = true
  phase = 'save only'
  await page.getByRole('textbox', { name: 'AI 请求超时', exact: true }).fill('17s')
  await page.getByRole('button', { name: '仅保存', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByPlaceholder('请再次输入当前管理员密码').fill('incorrect-fixture-password')
  const [rejected] = await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/platform/runtime-config' && r.request().method() === 'PUT'),
    dialog.getByRole('button', { name: '验证并保存', exact: true }).click(),
  ])
  assert.equal(rejected.status(), 400)
  await dialog.getByText('管理员密码不正确，请重新输入', { exact: true }).waitFor()
  await dialog.getByPlaceholder('请再次输入当前管理员密码').fill('admin')
  report.wrongPasswordRetainsSession = true
  const [saved] = await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/platform/runtime-config' && r.request().method() === 'PUT'),
    dialog.getByRole('button', { name: '验证并保存', exact: true }).click(),
  ])
  assert.equal(saved.status(), 200)
  const savedData = (await saved.json()).data
  assert.equal(savedData.pending, true)
  assert.ok(savedData.deployment_keys.includes('EXTRA_DEPLOYMENT_SETTING'))
  report.deploymentChangesNotReportedAsActive = true
  assert.equal(savedData.generation, initial.generation)
  await page.getByRole('status').filter({ hasText: '其他修改需部署工具应用'  }).waitFor()
  report.saveOnlyPending = true
  phase = 'stale revision'
  const csrf = (await context.cookies()).find(c => c.name === 'campusos_admin_csrf').value
  const authHeaders = { Authorization: `Bearer ${token}`, 'X-Admin-CSRF-Token': csrf }
  const passwordOnly = await context.request.get(`${base}/api/v1/admin/identity/me`, { headers: authHeaders })
  assert.equal(passwordOnly.status(), 200)
  const passwordProfile = (await passwordOnly.json()).data
  assert.equal(passwordProfile.authentication_strength, 'password')
  assert.equal(passwordProfile.mfa_required, !!config.expectedMFARequired)
  report.passwordDoesNotUpgradeMFA = true
  const stale = await context.request.put(`${base}/api/v1/platform/runtime-config`, { headers: { Authorization: `Bearer ${token}`, 'X-Admin-CSRF-Token': csrf }, data: { revision: initial.revision, changes: { AI_TIMEOUT: '18s' }, restart: false, verification_method: 'password', current_password: 'admin' } })
  assert.equal(stale.status(), 409)
  report.staleWriteRejected = true
  phase = 'restart and verify'
  await page.getByRole('button', { name: '保存并重启 API', exact: true }).click()
  await dialog.getByPlaceholder('请再次输入当前管理员密码').fill('admin')
  const [restart] = await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/platform/runtime-config' && r.request().method() === 'PUT'),
    dialog.getByRole('button', { name: '验证并保存重启', exact: true }).click(),
  ])
  assert.equal(restart.status(), 200)
  assert.equal((await restart.json()).data.restarting, true)
  await page.getByRole('status').filter({ hasText: 'API 已重启，常用运行配置已生效' }).waitFor({ timeout: 65000 })
  const active = await context.request.get(`${base}/api/v1/platform/runtime-config`, { headers: { Authorization: `Bearer ${token}` } })
  assert.equal(active.status(), 200)
  const activeData = (await active.json()).data
  assert.equal(activeData.pending, false)
  assert.notEqual(activeData.generation, initial.generation)
  assert.equal(activeData.fields.find(f => f.key === 'AI_TIMEOUT').value, '17s')
  report.restartAndEffectiveValues = true
  report.sessionPreserved = true
  if (config.testMFA) {
    phase = 'MFA alternative'
    const enrollment = await context.request.post(`${base}/api/v1/admin/identity/mfa/enroll`, { headers: authHeaders, data: { issuer: 'CampusOS' } })
    assert.equal(enrollment.status(), 200)
    const secret = new URL((await enrollment.json()).data.otpauth_url).searchParams.get('secret')
    function totp(step) {
      const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
      let bits = ''
      for (const c of secret.replace(/=/g, '').toUpperCase()) bits += alphabet.indexOf(c).toString(2).padStart(5, '0')
      const bytes = []
      for (let i = 0; i + 8 <= bits.length; i += 8) bytes.push(parseInt(bits.slice(i, i + 8), 2))
      const counter = Buffer.alloc(8); counter.writeBigUInt64BE(BigInt(step))
      const mac = createHmac('sha1', Buffer.from(bytes)).update(counter).digest()
      const offset = mac[mac.length - 1] & 15
      return String((mac.readUInt32BE(offset) & 0x7fffffff) % 1000000).padStart(6, '0')
    }
    const confirmed = await context.request.post(`${base}/api/v1/admin/identity/mfa/confirm`, { headers: authHeaders, data: { code: totp(Math.floor(Date.now() / 30000) - 1) } })
    assert.equal(confirmed.status(), 200)
    await page.evaluate(async () => { const { useAdminStore } = await import('/src/modules/identity/store.ts'); await useAdminStore().restore() })
    await page.getByRole('button', { name: '保存并重启 API', exact: true }).click()
    await dialog.getByText('MFA 验证码', { exact: true }).click()
    await dialog.getByRole('textbox', { name: 'MFA 验证码', exact: true }).fill(totp(Math.floor(Date.now() / 30000)))
    const [mfaStep, mfaRestart] = await Promise.all([
      page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/admin/identity/mfa/step-up'),
      page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/platform/runtime-config' && r.request().method() === 'PUT'),
      dialog.getByRole('button', { name: '验证并保存重启', exact: true }).click(),
    ])
    assert.equal(mfaStep.status(), 200); assert.equal(mfaRestart.status(), 200)
    await page.getByRole('status').filter({ hasText: 'API 已重启，常用运行配置已生效' }).waitFor({ timeout: 65000 })
    report.mfaAlternative = true
  }
  await page.setViewportSize({ width: 375, height: 812 })
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
  await page.screenshot({ path: config.mobileScreenshot, fullPage: true })
  report.mobileNoOverflow = true
  report.passed = true
} catch (error) {
  report.failure = { phase, type: error.name, message: String(error.message).slice(0, 500) }
  if (page) await page.screenshot({ path: config.failureScreenshot, fullPage: true }).catch(() => {})
  process.exitCode = 1
} finally {
  await writeFile(config.result, JSON.stringify(report, null, 2) + '\n')
  console.log(JSON.stringify(report))
  await browser?.close()
  await server?.close()
  if (cache) await rm(cache, { recursive: true, force: true })
}
