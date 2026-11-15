import assert from 'node:assert/strict'
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { createServer as createPortProbe } from 'node:net'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright-core'
import { createServer } from 'vite'

const root = fileURLToPath(new URL('../../', import.meta.url))
const webRoot = resolve(root, 'web')
const reportDir = resolve(root, process.env.CAMPUSOS_BROWSER_REPORT_DIR || '.cache/home-runtime-browser')
const apiURL = process.env.CAMPUSOS_BROWSER_API_URL || 'http://127.0.0.1:8080'
const webURL = process.env.CAMPUSOS_BROWSER_WEB_URL || 'http://localhost:3000'
const coldOnly = process.argv.includes('--cold-only')
const report = {}
let browser, server, cacheDir, authenticatedPage, phase = 'setup'

function observe(page) {
  const result = { documents: 0, fullReloads: 0 }
  page.on('request', (request) => {
    if (request.isNavigationRequest() && request.frame() === page.mainFrame()) result.documents++
  })
  page.on('websocket', (socket) => socket.on('framereceived', ({ payload }) => {
    try {
      if (JSON.parse(String(payload)).type === 'full-reload') result.fullReloads++
    } catch { /* Non-JSON frames are not HMR reloads. */ }
  }))
  return result
}

try {
  await mkdir(reportDir, { recursive: true })
  browser = await chromium.launch({
    executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome',
    headless: true,
    args: ['--no-sandbox', '--no-proxy-server'],
  })
  phase = 'cold Vite startup'
  cacheDir = await mkdtemp(resolve(tmpdir(), 'campusos-home-vite-'))
  const probe = createPortProbe()
  await new Promise((resolve, reject) => {
    probe.once('error', reject)
    probe.listen(0, '127.0.0.1', resolve)
  })
  const port = probe.address().port
  await new Promise((resolve, reject) => probe.close((error) => error ? reject(error) : resolve()))
  server = await createServer({
    root: webRoot,
    configFile: process.env.CAMPUSOS_BROWSER_VITE_CONFIG || resolve(webRoot, 'vite.config.ts'),
    cacheDir,
    resolve: { alias: { '@': resolve(webRoot, 'src') } },
    server: {
      host: '127.0.0.1', port, strictPort: true,
      fs: { allow: [webRoot, resolve(root, 'sdk/typescript/src')] },
      proxy: { '/api': { target: apiURL, changeOrigin: false } },
    },
  })
  await server.listen()
  const coldURL = `http://127.0.0.1:${server.httpServer.address().port}`
  const coldContext = await browser.newContext()
  const coldPage = await coldContext.newPage()
  report.cold = observe(coldPage)
  phase = 'cold home and lazy routes'
  await coldPage.goto(coldURL, { waitUntil: 'domcontentloaded' })
  await coldPage.locator('[data-campusos-home]').waitFor()
  await coldPage.waitForTimeout(12000)
  assert.equal(report.cold.documents, 1, 'cold home must keep its initial document')
  assert.equal(report.cold.fullReloads, 0, 'dependency discovery must not reload the page')
  await coldPage.getByRole('link', { name: '浏览帖子', exact: true }).click()
  await coldPage.locator('.thread-list').waitFor()
  await coldPage.getByRole('link', { name: '首页', exact: true }).click()
  await coldPage.locator('[data-campusos-home]').waitFor()
  assert.equal(report.cold.documents, 1, 'core navigation must stay within the SPA')
  assert.notEqual(await coldPage.locator('.el-card').first().evaluate((card) => getComputedStyle(card).borderRadius), '0px', 'global Element Plus styles must remain loaded')
  await coldPage.screenshot({ path: resolve(reportDir, 'cold-home.png') })
  await coldContext.close()
  await server.close()
  server = null

  if (!coldOnly) {
    phase = 'authenticated fixture login'
    const profile = process.env.CAMPUSOS_BROWSER_USER_PROFILE
    assert.ok(profile, 'CAMPUSOS_BROWSER_USER_PROFILE must identify a private fixture profile')
    const account = JSON.parse(await readFile(resolve(root, profile), 'utf8'))
    const context = await browser.newContext()
    authenticatedPage = await context.newPage()
    report.live = { origin: new URL(webURL).origin }
    const observed = observe(authenticatedPage)
    await authenticatedPage.goto(`${webURL}/login`, { waitUntil: 'domcontentloaded' })
    await authenticatedPage.getByPlaceholder('请输入邮箱').fill(account.email)
    await authenticatedPage.getByPlaceholder('请输入密码', { exact: true }).fill(account.password)
    const [login] = await Promise.all([
      authenticatedPage.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/auth/login' && response.request().method() === 'POST'),
      authenticatedPage.locator('form').getByRole('button', { name: '登录', exact: true }).click(),
    ])
    const loginData = await login.json()
    assert.ok(loginData.data?.access_token, 'fixture login must return a user session')
    await authenticatedPage.waitForURL((url) => url.pathname === '/')
    await authenticatedPage.locator('[data-campusos-home]').waitFor()
    phase = 'exact live plugin inventory and rejection control'
    const parent = new URL(webURL).origin
    const inventory = await context.request.get(`${webURL}/api/v1/plugin-v5/ui`, { params: { parent_origin: parent }, headers: { Authorization: `Bearer ${loginData.data.access_token}` } })
    report.live.pluginStatus = inventory.status()
    assert.equal(inventory.status(), 200)
    assert.equal((await inventory.json()).code, 0)
    const denied = await context.request.get(`${webURL}/api/v1/plugin-v5/ui`, { params: { parent_origin: 'http://unlisted-parent.invalid' }, headers: { Authorization: `Bearer ${loginData.data.access_token}` } })
    report.live.unlistedParentStatus = denied.status()
    assert.equal(denied.status(), 403, 'an unlisted parent must still be denied')
    await authenticatedPage.waitForTimeout(12000)
    report.live.warningVisible = await authenticatedPage.getByText('插件隔离界面暂不可用，可继续使用内置功能和下载。', { exact: true }).isVisible()
    report.live.documents = observed.documents
    report.live.fullReloads = observed.fullReloads
    assert.equal(report.live.warningVisible, false)
    assert.equal(observed.documents, 1)
    assert.equal(observed.fullReloads, 0)
    await authenticatedPage.screenshot({ path: resolve(reportDir, 'authenticated-home.png') })
  }
  report.passed = true
  console.log(JSON.stringify(report))
} catch (error) {
  // Playwright call logs can include filled credentials; never print them.
  report.failure = { phase, type: error.name }
  if (phase.startsWith('cold')) report.failure.detail = error.message.split('\n')[0]
  console.error(JSON.stringify(report))
  process.exitCode = 1
} finally {
  if (authenticatedPage) {
    await authenticatedPage.evaluate(async () => {
      const { useUserStore } = await import('/src/modules/identity/store.ts')
      await useUserStore().logout()
    }).catch(() => {})
  }
  await mkdir(reportDir, { recursive: true })
  await writeFile(resolve(reportDir, 'report.json'), JSON.stringify(report, null, 2) + '\n')
  await browser?.close()
  await server?.close()
  if (cacheDir) await rm(cacheDir, { recursive: true, force: true })
}
