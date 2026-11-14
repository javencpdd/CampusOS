// Run only against an owned, disposable fixture; never an existing deployment.
import assert from 'node:assert/strict'
import { readFile, writeFile, mkdtemp, rm } from 'node:fs/promises'
import { resolve } from 'node:path'
import { tmpdir } from 'node:os'
import { chromium } from '../node_modules/playwright-core/index.mjs'
import { createServer } from '../node_modules/vite/dist/node/index.js'

const config = JSON.parse(await readFile(process.env.CAMPUSOS_AUTH_BROWSER_CONFIG, 'utf8'))
assert.equal(config.ownedFixture, true)
assert.match(config.database, /^campusos_auth_e2e_\d+$/)
assert.equal(new URL(config.backend).hostname, '127.0.0.1')
const report = { errors: [], refreshes: 0 }
let browser,
  server,
  page,
  cache,
  phase = 'setup'
try {
  cache = await mkdtemp(resolve(tmpdir(), 'campusos-auth-web-'))
  server = await createServer({
    root: resolve('web'),
    configFile: resolve('web/vite.config.ts'),
    cacheDir: cache,
    server: {
      host: '127.0.0.1',
      allowedHosts: ['campusos-auth.test'],
      port: config.webPort,
      strictPort: true,
      proxy: { '/api': { target: config.backend, changeOrigin: false } },
    },
  })
  await server.listen()
  const base = `http://campusos-auth.test:${config.webPort}`,
    api = config.backend + '/api/v1'
  browser = await chromium.launch({
    executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome',
    headless: true,
    args: [
      '--no-sandbox',
      '--no-proxy-server',
      '--host-resolver-rules=MAP campusos-auth.test 127.0.0.1, MAP *.127.0.0.1.sslip.io 127.0.0.1',
    ],
  })
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
  context.on('page', (tab) => tab.on('pageerror', (error) => report.errors.push(error.message.slice(0, 180))))
  context.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/v1/auth/refresh') report.refreshes++
  })
  page = await context.newPage()
  const loggedIn = (tab) =>
    tab.waitForFunction(async () => {
      const { useUserStore } = await import('/src/modules/identity/store.ts')
      return Boolean(useUserStore().user?.id)
    })
  const login = async (tab, email = 'rss-alice@example.test') => {
    await tab.getByPlaceholder('请输入邮箱').fill(email)
    await tab.getByPlaceholder('请输入密码', { exact: true }).fill('owned-rss-fixture-password')
    await tab.locator('form').getByRole('button', { name: '登录', exact: true }).click()
    await loggedIn(tab)
  }
  const expire = async (tab) => {
    const delay = await tab.evaluate(async () => {
      const { getAccessToken } = await import('/src/modules/identity/session.ts')
      const expires = JSON.parse(atob(getAccessToken().split('.')[1])).exp * 1000
      return Math.max(0, expires - Date.now() + 1100)
    })
    assert.ok(delay < 16000, 'owned fixture must have a short access TTL')
    await tab.waitForTimeout(delay)
  }
  const reads = (tab) =>
    tab.evaluate(async () => {
      const { default: api } = await import('/src/shared/api/client.ts')
      return Promise.all([api.get('/auth/sessions'), api.get('/auth/sessions')]).then((values) =>
        values.map((value) => value.code),
      )
    })
  phase = 'signed RSS package installation'
  const admin = await context.request.post(api + '/admin/identity/login', {
    data: { username: 'admin', password: 'admin' },
  })
  assert.equal(admin.status(), 200)
  const adminToken = (await admin.json()).data.access_token
  const adminCsrf = (await context.cookies()).find((c) => c.name === 'campusos_admin_csrf').value
  const headers = { Authorization: `Bearer ${adminToken}`, 'X-Admin-CSRF-Token': adminCsrf }
  const imported = await context.request.post(api + '/admin/plugin-v5/packages', {
    headers,
    multipart: {
      package: { name: 'rss.tar.gz', mimeType: 'application/gzip', buffer: await readFile(config.package) },
      release: { name: 'rss.release.json', mimeType: 'application/json', buffer: await readFile(config.release) },
      idempotency_key: crypto.randomUUID(),
    },
  })
  assert.equal(imported.status(), 200)
  const family = api + '/admin/plugin-v5/campusos/rss-publisher'
  const command = async (kind, extra = {}) => {
    const response = await context.request.post(family + '/commands', {
      headers,
      data: { kind, version: '1.0.0', idempotency_key: crypto.randomUUID(), ...extra },
    })
    assert.equal(response.status(), 200, `owned plugin command: ${kind}`)
    const detail = await context.request.get(family + '/1.0.0', { headers })
    assert.equal(detail.status(), 200)
    return (await detail.json()).data
  }
  let entry = await command('install', { expected_generation: '0' })
  entry = await command('grant', {
    approved: true,
    expected_revision: entry.grant?.revision || '0',
    consumes: entry.release.manifest.consumes,
  })
  await command('enable', { expected_generation: entry.installation.generation })

  phase = 'anonymous extension deep link and safe return after login'
  const extension = '/extensions/campusos.rss-publisher/ui'
  await page.goto(base + extension)
  await page.waitForURL((url) => url.pathname === '/login' && url.searchParams.get('redirect') === extension)
  await login(page)
  await page.waitForURL((url) => url.pathname === extension)
  await page.getByText('插件界面暂不可用', { exact: true }).waitFor()
  report.anonymousDeepLinkAndReturn = true
  assert.equal(await page.evaluate(() => window.isSecureContext), false)
  assert.equal(await page.evaluate(() => typeof navigator.locks), 'undefined')
  report.realInsecureHTTPWithoutWebLocks = true

  phase = 'user RSS installation'
  await page.goto(base + '/plugins')
  await page
    .getByRole('navigation', { name: '可用插件' })
    .locator('[data-coordinate="campusos/rss-publisher@1.0.0"]')
    .click()
  await page.getByRole('button', { name: '安装到我的账号', exact: true }).click()
  await page.getByLabel('我已了解以上用途，同意此版本在宿主权限内处理我的数据').check()
  await page.getByRole('button', { name: '同意此版本用途', exact: true }).click()
  await page.getByLabel('插件当前状态').filter({ hasText: '等待本人启用' }).waitFor()
  await page.getByRole('button', { name: '启用', exact: true }).click()
  await page.evaluate(async () => {
    const { useUIRuntimeStore } = await import('/src/modules/plugin-runtime/store.ts')
    await useUIRuntimeStore().sync()
  })
  await page.goto(base + extension)
  const rssReady = (tab) =>
    tab
      .frameLocator('iframe')
      .getByText('订阅列表已就绪。添加地址或选择订阅后读取内容。', { exact: true })
      .waitFor({ timeout: 20000 })
  await rssReady(page)
  report.installedRSSDeepLinkLoads = true

  phase = 'authenticated login URL after document reload'
  await page.goto(base + '/login')
  await page.waitForURL((url) => url.pathname === '/')
  await loggedIn(page)
  report.authenticatedLoginRedirectsHome = true

  phase = 'same-origin second tab adopts a verified token without cookie rotation'
  await page.goto(base + '/threads/create')
  await page.getByPlaceholder('请输入文章标题').fill('续期后保留的未提交草稿')
  const beforePeer = report.refreshes
  const second = await context.newPage()
  await second.goto(base + '/account/security')
  await loggedIn(second)
  await second.getByRole('button', { name: '退出全部设备', exact: true }).waitFor()
  assert.equal(new URL(second.url()).pathname, '/account/security')
  assert.equal(report.refreshes, beforePeer)
  assert.deepEqual(await reads(page), [0, 0])
  report.secondTabDoesNotRevokeFirst = true

  phase = 'simultaneous expiry in two HTTP tabs renews once and retains a draft'
  // Stop only the test fixture's periodic manifest sync so expiry is triggered
  // by the two explicit reads at the same instant rather than a background poll.
  for (const tab of [page, second])
    await tab.evaluate(() => {
      const ceiling = window.setInterval(() => {}, 10000)
      for (let id = 1; id <= ceiling; id++) window.clearInterval(id)
    })
  await expire(page)
  const beforeExpiry = report.refreshes
  let navigations = 0
  const countNavigation = (request) => {
    if (request.isNavigationRequest()) navigations++
  }
  context.on('request', countNavigation)
  assert.deepEqual(await Promise.all([reads(page), reads(second)]), [
    [0, 0],
    [0, 0],
  ])
  context.off('request', countNavigation)
  assert.equal(report.refreshes - beforeExpiry, 1)
  assert.equal(navigations, 0)
  assert.equal(await page.getByPlaceholder('请输入文章标题').inputValue(), '续期后保留的未提交草稿')
  assert.equal(new URL(page.url()).pathname, '/threads/create')
  assert.equal(new URL(second.url()).pathname, '/account/security')
  report.concurrentExpiryOneRefreshNoNavigationDraftRetained = true

  phase = 'plugin reopens securely after real session rotation'
  await page.goto(base + extension)
  await rssReady(page)
  for (const tab of [page, second])
    await tab.evaluate(() => {
      const ceiling = window.setInterval(() => {}, 10000)
      for (let id = 1; id <= ceiling; id++) window.clearInterval(id)
    })
  await expire(page)
  const beforePluginRenewal = report.refreshes
  assert.deepEqual(await reads(page), [0, 0])
  await rssReady(page)
  assert.equal(report.refreshes - beforePluginRenewal, 1)
  assert.equal(await page.getByText('插件因权限或版本变化已停止', { exact: true }).count(), 0)
  report.pluginSessionRotationReinitializes = true

  phase = 'logout is effective in every tab and deep link resumes after login'
  await page.locator('.nav-avatar').click()
  await page.getByRole('menuitem', { name: '退出登录', exact: true }).click()
  await Promise.all([page, second].map((tab) => tab.waitForURL((url) => url.pathname === '/login')))
  await page.reload()
  await page.getByPlaceholder('请输入邮箱').waitFor()
  assert.equal(new URL(page.url()).pathname, '/login')
  report.logoutNotSilentlyRestored = true
  await page.goto(base + extension)
  await page.waitForURL((url) => url.pathname === '/login' && url.searchParams.get('redirect') === extension)
  await login(page)
  await page.waitForURL((url) => url.pathname === extension)
  await rssReady(page)
  report.launcherLoginReturnsToInstalledRSS = true

  phase = 'revoked refresh session really requires a new login'
  await page.evaluate(async () => {
    const { default: api } = await import('/src/shared/api/client.ts')
    await api.post('/auth/logout-all')
    try {
      await api.get('/auth/sessions')
    } catch {}
  })
  await page.waitForURL((url) => url.pathname === '/login')
  report.revocationStillEnforced = true

  phase = 'credentials remain out of persistent storage'
  const stored = await page.evaluate(async () => {
    const request = indexedDB.open('campusos-user-auth-coordination', 1)
    const db = await new Promise((resolve, reject) => {
      request.onsuccess = () => resolve(request.result)
      request.onerror = () => reject(request.error)
    })
    const tx = db.transaction('leases', 'readonly'),
      query = tx.objectStore('leases').getAll()
    const leases = await new Promise((resolve, reject) => {
      query.onsuccess = () => resolve(query.result)
      query.onerror = () => reject(query.error)
    })
    db.close()
    return {
      local: Object.keys(localStorage),
      session: Object.keys(sessionStorage),
      leaseKeys: leases.map((lease) => Object.keys(lease).sort()),
    }
  })
  assert.ok(!stored.local.some((key) => /token|session/i.test(key)))
  assert.ok(!stored.session.some((key) => /token|session/i.test(key)))
  for (const keys of stored.leaseKeys) assert.deepEqual(keys, ['expires', 'owner'])
  report.noPersistedCredentials = true
  assert.deepEqual(report.errors, [])
  report.passed = true
} catch (error) {
  report.failure = { phase, type: error.name, message: String(error.message).slice(0, 700) }
  if (page)
    await page.screenshot({ path: resolve(config.output, 'browser-failure.png'), fullPage: true }).catch(() => {})
  process.exitCode = 1
} finally {
  await writeFile(resolve(config.output, 'browser-report.json'), JSON.stringify(report, null, 2) + '\n')
  console.log(JSON.stringify(report))
  await browser?.close()
  await server?.close()
  if (cache) await rm(cache, { recursive: true, force: true })
}
