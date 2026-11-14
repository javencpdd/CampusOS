import assert from 'node:assert/strict'
import { readFile, writeFile, mkdtemp, rm } from 'node:fs/promises'
import { resolve } from 'node:path'
import { tmpdir } from 'node:os'
import { chromium } from '../node_modules/playwright-core/index.mjs'
import { createServer } from '../node_modules/vite/dist/node/index.js'
const config = JSON.parse(await readFile(process.env.CAMPUSOS_RSS_BROWSER_CONFIG, 'utf8'))
assert.equal(config.ownedFixture, true)
assert.match(config.database, /^campusos_rss_e2e_\d+$/)
assert.equal(new URL(config.backend).hostname, '127.0.0.1')
const report = { errors: [], externalBrowserRequests: [], consoleErrors: [] }
let browser,
  server,
  page,
  cache,
  phase = 'setup'
try {
  cache = await mkdtemp(resolve(tmpdir(), 'campusos-rss-web-'))
  server = await createServer({
    root: resolve('web'),
    configFile: resolve('web/vite.config.ts'),
    cacheDir: cache,
    server: {
      host: '127.0.0.1',
      port: config.webPort,
      strictPort: true,
      proxy: { '/api': { target: config.backend, changeOrigin: false } },
    },
  })
  await server.listen()
  const base = `http://127.0.0.1:${config.webPort}`,
    api = config.backend + '/api/v1'
  browser = await chromium.launch({
    executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome',
    headless: true,
    args: ['--no-sandbox', '--no-proxy-server', '--host-resolver-rules=MAP *.127.0.0.1.sslip.io ~NOTFOUND'],
  })
  const context = await browser.newContext({ viewport: { width: 1600, height: 1000 } })
  page = await context.newPage()
  page.on('console', (message) => {
    if (message.type() === 'error') report.consoleErrors.push(message.text().slice(0, 300))
  })
  page.on('pageerror', (error) => report.errors.push(error.message.slice(0, 180)))
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (['http:', 'https:'].includes(url.protocol) && url.hostname !== '127.0.0.1')
      report.externalBrowserRequests.push(url.hostname)
  })
  phase = 'signed independent packages and grants'
  const adminLogin = await context.request.post(api + '/admin/identity/login', {
    data: { username: 'admin', password: 'admin' },
  })
  assert.equal(adminLogin.status(), 200)
  const adminToken = (await adminLogin.json()).data.access_token
  const adminCsrf = (await context.cookies()).find((c) => c.name === 'campusos_admin_csrf').value
  const adminHeaders = { Authorization: `Bearer ${adminToken}`, 'X-Admin-CSRF-Token': adminCsrf }
  async function installGlobal(publisher, key, archive, release) {
    const imported = await context.request.post(api + '/admin/plugin-v5/packages', {
      headers: adminHeaders,
      multipart: {
        package: { name: 'plugin.tar.gz', mimeType: 'application/gzip', buffer: await readFile(archive) },
        release: { name: 'release.json', mimeType: 'application/json', buffer: await readFile(release) },
        idempotency_key: crypto.randomUUID(),
      },
    })
    assert.equal(imported.status(), 200)
    const family = api + '/admin/plugin-v5/' + publisher + '/' + key
    async function command(kind, extra = {}) {
      const response = await context.request.post(family + '/commands', {
        headers: adminHeaders,
        data: { kind, version: '2.0.0', idempotency_key: crypto.randomUUID(), ...extra },
      })
      assert.equal(response.status(), 200, `global ${kind}`)
      const detail = await context.request.get(family + '/2.0.0', { headers: adminHeaders })
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
  }
  await installGlobal('campusos', 'rss-publisher', config.package, config.release)
  await installGlobal('example', 'news-reader', config.renamedPackage, config.renamedRelease)
  phase = 'user login and personal installations'
  await page.goto(base + '/login')
  await page.getByPlaceholder('请输入邮箱').fill('rss-alice@example.test')
  await page.getByPlaceholder('请输入密码', { exact: true }).fill('owned-rss-fixture-password')
  await page.locator('form').getByRole('button', { name: '登录', exact: true }).click()
  await page.waitForURL((url) => url.pathname === '/')
  async function installPersonal(publisher, key) {
    await page.goto(base + '/plugins')
    await page
      .getByRole('navigation', { name: '可用插件' })
      .locator(`[data-coordinate="${publisher}/${key}@2.0.0"]`)
      .click()
    await page.getByRole('button', { name: '安装到我的账号', exact: true }).click()
    await page.getByLabel('我已了解以上用途，同意此版本在宿主权限内处理我的数据').check()
    await page.getByRole('button', { name: '同意此版本用途', exact: true }).click()
    await page.getByLabel('插件当前状态').filter({ hasText: '等待本人启用' }).waitFor()
    await page.getByRole('button', { name: '启用', exact: true }).click()
  }
  await installPersonal('campusos', 'rss-publisher')
  await installPersonal('example', 'news-reader')
  async function openPlugin(name) {
    await page.goto(base + '/extensions/' + name + '/ui')
    const frame = page.frameLocator('iframe')
    await frame.getByText('订阅列表已就绪。添加地址或选择订阅后读取内容。', { exact: true }).waitFor({ timeout: 20000 })
    const iframe = page.locator('iframe')
    assert.equal(iframe && new URL(await iframe.getAttribute('src')).origin, base)
    assert.equal(await iframe.getAttribute('sandbox'), 'allow-scripts')
    assert.equal(await page.getByRole('button', { name: '连接本机助手', exact: true }).count(), 0)
    return frame
  }
  phase = 'opaque sandbox without wildcard DNS or a native helper'
  await openPlugin('campusos.rss-publisher')
  const sandbox = await page
    .frames()
    .find((frame) => frame !== page.mainFrame())
    .evaluate(async () => {
      const blocked = (operation) => {
        try {
          operation()
          return false
        } catch {
          return true
        }
      }
      return {
        cookie: blocked(() => document.cookie),
        storage: blocked(() => localStorage.getItem('campusos')),
        parent: blocked(() => parent.document.body),
        opaque: self.origin === 'null',
      }
    })
  assert.deepEqual(sandbox, { cookie: true, storage: true, parent: true, opaque: true })
  report.opaqueSandboxBlocksHostAuthority = true
  report.noWildcardDNSGatewayOrNativeCompanion = true
  phase = 'independent publisher opens on the same host contracts'
  let frame = await openPlugin('example.news-reader')
  report.unrelatedPublisherUsesIdenticalHost = true
  phase = 'server network policy remains administrator-owned'
  assert.equal(await frame.locator('#reader-location').innerText(), '读取位置：CampusOS 服务器')
  assert.equal(await frame.getByRole('combobox', { name: '抓取代理', exact: true }).count(), 0)
  assert.equal(await frame.getByRole('button', { name: '保存读取设置', exact: true }).count(), 0)
  report.serverProxyControlsAbsentFromUserUI = true
  phase = 'plugin-side RSS parsing and brokered public HTTPS'
  await frame.getByLabel('公开订阅地址', { exact: true }).fill('https://www.nasa.gov/news-release/feed/')
  await frame.getByRole('button', { name: '添加订阅', exact: true }).click()
  await page.getByRole('button', { name: '确认保存', exact: true }).click()
  frame = page.frameLocator('iframe')
  await frame
    .getByRole('combobox', { name: '选择订阅', exact: true })
    .getByRole('option', { name: 'https://www.nasa.gov/news-release/feed/', exact: true })
    .waitFor({ state: 'attached', timeout: 20000 })
  await frame.getByRole('button', { name: '读取最新内容', exact: true }).click()
  await frame.locator('.entry').first().waitFor({ timeout: 30000 })
  report.realPublicFeedParsedByPluginJS = true
  let selectedTitle = ''
  for (let index = 0; index < Math.min(await frame.locator('.entry').count(), 5); index++) {
    await frame.locator('.entry').nth(index).getByRole('button', { name: '预览图文', exact: true }).click()
    if (await frame.locator('#load-images').isHidden()) continue
    await frame.getByRole('button', { name: '加载预览图片', exact: true }).click()
    try {
      await frame.locator('#entry-body img[src^="blob:"]').first().waitFor({ state: 'attached', timeout: 15000 })
      selectedTitle = await frame.locator('#entry-title').innerText()
      break
    } catch {}
  }
  assert.ok(selectedTitle, 'public fixture must provide at least one bounded image')
  report.brokeredPreviewImages = true
  phase = 'generic editor import and explicit manual publication'
  const publications = []
  page.on('request', (request) => {
    if (/\/richtext\/articles\/[^/]+\/publish$/.test(new URL(request.url()).pathname)) publications.push(request.url())
  })
  await frame.getByRole('button', { name: '导入图文并编辑发布', exact: true }).click()
  await page.getByRole('button', { name: '导入并编辑', exact: true }).click()
  await page.waitForURL((url) => url.pathname === '/threads/create', { timeout: 180000 })
  await page.waitForFunction(
    (expected) => document.querySelector('input[placeholder="请输入文章标题"]')?.value === expected,
    selectedTitle,
  )
  assert.equal(await page.getByPlaceholder('请输入文章标题').inputValue(), selectedTitle)
  assert.equal(publications.length, 0)
  await page.getByRole('button', { name: '预览', exact: true }).click()
  const preview = page.getByRole('dialog', { name: '文章预览', exact: true })
  await preview.locator('img').first().waitFor({ state: 'attached' })
  assert.ok((await preview.innerText()).includes('来源：'))
  await preview.locator('.el-drawer__close-btn').click()
  const board = page
    .locator('.el-form-item')
    .filter({ has: page.locator('.el-form-item__label').filter({ hasText: '版块' }) })
    .locator('.el-select')
    .first()
  await board.click()
  await page.getByRole('option', { name: /RSS 测试版块/ }).click()
  await page.getByRole('button', { name: '发布文章', exact: true }).click()
  await page.waitForURL((url) => /^\/threads\/\d+$/.test(url.pathname), { timeout: 30000 })
  assert.equal(publications.length, 1)
  await page.waitForFunction(() =>
    [...document.querySelectorAll('img')].some(
      (image) => image.complete && image.naturalWidth > 0 && image.src.startsWith('blob:'),
    ),
  )
  report.genericImportOwnedImagesManualPublish = true
  phase = 'owner isolation and uninstall without changes to the host'
  const userToken = await page.evaluate(async () => (await import('/src/modules/identity/session.ts')).getAccessToken())
  const csrf = (await context.cookies()).find((cookie) => cookie.name === 'campusos_csrf').value
  const userHeaders = { Authorization: `Bearer ${userToken}`, 'X-CSRF-Token': csrf }
  const family = api + '/plugin-v5/example/news-reader'
  const detail = await context.request.get(family + '/2.0.0', { headers: userHeaders })
  assert.equal(detail.status(), 200)
  const proof = await context.request.get(family + '/2.0.0/ui', { headers: userHeaders })
  const aliceProof = (await proof.json()).data.proof
  for (const extra of [
    { profile: 'owned-http' },
    { proxy: 'http://127.0.0.1:7890' },
    { media_profile: 'owned-socks5' },
  ]) {
    const refused = await context.request.post(family + '/2.0.0/ui-actions', {
      headers: userHeaders,
      data: {
        proof: aliceProof,
        action: 'web.read',
        url: 'https://www.nasa.gov/news-release/feed/',
        kind: 'document',
        ...extra,
      },
    })
    assert.equal(refused.status(), 400)
  }
  report.userCannotOverrideServerProxyPolicy = true
  const bob = await browser.newContext()
  const bobLogin = await bob.request.post(api + '/auth/login', {
    data: { email: 'rss-bob@example.test', password: 'owned-rss-fixture-password' },
  })
  const bobToken = (await bobLogin.json()).data.access_token
  const bobCsrf = (await bob.cookies()).find((cookie) => cookie.name === 'campusos_csrf').value
  const denied = await bob.request.post(family + '/2.0.0/ui-actions', {
    headers: { Authorization: `Bearer ${bobToken}`, 'X-CSRF-Token': bobCsrf },
    data: { proof: aliceProof, action: 'web.read', url: 'https://www.nasa.gov/news-release/feed/', kind: 'document' },
  })
  assert.ok([403, 404].includes(denied.status()))
  await bob.close()
  report.otherOwnerCannotReuseUIProof = true
  const own = (await detail.json()).data.installation
  const removed = await context.request.post(family + '/commands', {
    headers: userHeaders,
    data: {
      version: '2.0.0',
      kind: 'uninstall',
      expected_generation: own.generation,
      idempotency_key: crypto.randomUUID(),
    },
  })
  assert.equal(removed.status(), 200)
  await openPlugin('campusos.rss-publisher')
  await page.setViewportSize({ width: 375, height: 812 })
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
  report.uninstallLeavesCoreAndOtherPluginUsable = true
  assert.deepEqual(report.externalBrowserRequests, [])
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
