// Built Admin/Web assets proxy every API request to the actual cmd/server.
// Route delay below holds a real fetched response to exercise identity races.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFile, writeFile } from 'node:fs/promises'
import { createServer, request as proxyRequest } from 'node:http'
import { createHmac } from 'node:crypto'
import { extname, resolve, sep } from 'node:path'
const cfg = JSON.parse(await readFile(process.argv[2], 'utf8'))
const require = createRequire(resolve('web/package.json'))
const { chromium } = require('playwright-core')
async function staticProxy(folder) {
 const root = resolve(folder)
 const server = createServer(async (req, res) => {
  if (req.url.startsWith('/api/')) {
   const upstream = proxyRequest(new URL(req.url, cfg.backend), { method: req.method, headers: req.headers }, response => { res.writeHead(response.statusCode, response.headers); response.pipe(res) })
   res.on('close', () => upstream.destroy())
   upstream.on('error', () => { if (!res.headersSent) res.writeHead(502); res.end() }); req.pipe(upstream); return
  }
  try {
   const path = resolve(root, '.' + new URL(req.url, 'http://localhost').pathname)
   if (path !== root && !path.startsWith(root + sep)) { res.writeHead(404).end(); return }
   const file = extname(path) ? path : resolve(root, 'index.html')
   const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png', '.ico': 'image/x-icon' }
   const data = await readFile(file)
   res.writeHead(200, { 'Content-Type': types[extname(file)] || 'application/octet-stream' }).end(data)
  } catch { if (!res.headersSent) res.writeHead(404); res.end() }
 })
 await new Promise(r => server.listen(0, '127.0.0.1', r))
 return { server, origin: `http://127.0.0.1:${server.address().port}` }
}
function totp(secret, step) {
 const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'; let bits = 0, acc = 0; const bytes = []
 for (const c of secret.replace(/=+$/, '')) { const value = alphabet.indexOf(c); assert.ok(value >= 0); acc = (acc << 5) | value; bits += 5; if (bits >= 8) { bits -= 8; bytes.push((acc >> bits) & 255) } }
 const message = Buffer.alloc(8); message.writeBigUInt64BE(BigInt(step)); const digest = createHmac('sha1', Buffer.from(bytes)).update(message).digest(); const offset = digest.at(-1) & 15
 return String((digest.readUInt32BE(offset) & 0x7fffffff) % 1000000).padStart(6, '0')
}
// Pinia's production injection key is a Symbol, so inspect the actual app's
// providers rather than assuming a development-only global or replacing a store.
const findStore = (name) => {
 const provides = document.querySelector('#app')?.__vue_app__?._context.provides
 const pinia = provides && Reflect.ownKeys(provides).map(k => provides[k]).find(value => value?._s instanceof Map)
 return pinia?._s.get(name)
}
const admin = await staticProxy('admin/dist'), web = await staticProxy('web/dist')
const errors = [], checks = [], apiPaths = []
let browser
let releaseHeldResponse = () => {}
try {
 browser = await chromium.launch({ executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox'] })
 const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
 context.setDefaultTimeout(15000)
 const page = await context.newPage(); page.on('pageerror', err => errors.push(err.message))
 page.on('request', req => { const path = new URL(req.url()).pathname; if (path.startsWith('/api/v1/')) apiPaths.push(path) })
 await page.goto(admin.origin + '/login?redirect=/threads')
 await page.locator('input[autocomplete="username"]').fill(cfg.adminUsername)
 await page.locator('input[autocomplete="current-password"]').fill(cfg.password)
 const loginResponse = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/admin/identity/login')
 await page.getByRole('button', { name: '登录', exact: true }).click()
 const loginData = await (await loginResponse).json(); assert.ok(loginData.code === 0 && !Object.hasOwn(loginData.data, 'refresh_token'))
 await page.waitForURL(admin.origin + '/threads')
 const row = page.locator('.thread-table .el-table__body-wrapper tr').filter({ hasText: cfg.thread }).first()
 await row.waitFor(); assert.ok(!(await page.locator('.thread-table').innerText()).includes('PRIVATE_BROWSER_CONTENT')); assert.ok(!(await page.locator('.thread-table').innerText()).includes('PUBLIC_BROWSER_CONTENT'))
 checks.push('independent Admin email login and opaque current profile', 'metadata-only actual thread inventory at password strength')
 const cookies = await context.cookies()
 const refreshCookie = cookies.find(c => c.name === 'campusos_admin_refresh'), csrfCookie = cookies.find(c => c.name === 'campusos_admin_csrf')
 assert.ok(refreshCookie?.httpOnly && refreshCookie.path === '/api/v1/admin/identity' && refreshCookie.sameSite === 'Lax' && !csrfCookie?.httpOnly && csrfCookie?.path === '/')
 assert.ok(await page.evaluate(() => !document.cookie.includes('campusos_admin_refresh=')))
 assert.ok(await page.evaluate(() => [localStorage, sessionStorage].every(storage => !Object.keys(storage).some(key => /token|refresh|password|session/i.test(key)))))
 checks.push('Admin HttpOnly refresh cookie and separate readable CSRF', 'no browser storage credentials')
 await page.waitForFunction(findStore => { const p = eval('(' + findStore + ')')('admin')?.profile; return p?.domain === 'admin' && p.account.id === 'adm.browser.current.opaque' && p.roles.some(r => r.name === '内容盘点员') && p.authentication_strength === 'password' }, findStore.toString())
 const deniedWrite = await page.evaluate(async ({ source, id }) => { const s = eval('(' + source + ')')('admin'); const r = await fetch('/api/v1/threads/' + id + '/pin', { method: 'POST', headers: { Authorization: 'Bearer ' + s.token, 'X-CampusOS-Client': 'browser' } }); return { status: r.status, code: (await r.json()).error?.code } }, { source: findStore.toString(), id: cfg.thread })
 assert.ok(deniedWrite.status === 403)
 checks.push('actual password write rejected by current MFA policy')
 const refreshResponse = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/admin/identity/refresh')
 await page.reload(); await row.waitFor(); const refreshed = await (await refreshResponse).json(); assert.ok(refreshed.code === 0 && !Object.hasOwn(refreshed.data, 'refresh_token'))
 checks.push('real page reload rotates Admin refresh then re-reads current identity')
 await page.getByRole('button', { name: '退出', exact: true }).click(); await page.waitForURL(/\/login(?:\?|$)/)
 assert.ok(!(await context.cookies()).some(cookie => cookie.name === 'campusos_admin_refresh' || cookie.name === 'campusos_admin_csrf'))
 await page.goto(admin.origin + '/login?redirect=/threads')
 await page.locator('input[autocomplete="username"]').fill(cfg.adminUsername); await page.locator('input[autocomplete="current-password"]').fill(cfg.password)
 await page.getByRole('button', { name: '登录', exact: true }).click(); await page.waitForURL(admin.origin + '/threads'); await row.waitFor()
 checks.push('actual Admin UI logout revokes the own session and clears both Admin cookies before re-login')

 await page.getByRole('button', { name: '管理员认证', exact: true }).click()
 const dialog = page.getByRole('dialog', { name: '确认管理员身份' })
 await dialog.getByRole('button', { name: '开始配置管理员认证器', exact: true }).click()
 const secretInput = dialog.locator('input[readonly]'); await secretInput.waitFor(); const secret = await secretInput.inputValue(); assert.ok(secret.length >= 16)
 const codeInput = dialog.locator('input[autocomplete="one-time-code"]'); const step = Math.floor(Date.now() / 30000)
 await codeInput.fill(totp(secret, step)); await dialog.getByRole('button', { name: '完成配置', exact: true }).click()
 await dialog.getByText('配置完成。请等待认证器显示下一组验证码，再确认管理员身份。', { exact: true }).waitFor()
 assert.ok(await secretInput.count() === 0)
 await codeInput.fill(totp(secret, step + 1)); await dialog.getByRole('button', { name: '确认身份', exact: true }).click(); await dialog.waitFor({ state: 'hidden' })
 await page.waitForFunction(source => eval('(' + source + ')')('admin')?.profile?.authentication_strength === 'mfa', findStore.toString())
 checks.push('real own Admin enrollment and confirmation clear the one-time secret', 'password session steps up via fresh TOTP and current self without a new JWT')
 await page.getByRole('button', { name: /刷新/ }).first().click()
 await page.waitForFunction(id => { const row = [...document.querySelectorAll('.thread-table .el-table__body-wrapper tr')].find(r => r.textContent.includes(id)); return row?.querySelector('.row-actions button')?.disabled === false }, cfg.thread)
 await row.locator('.row-actions button').first().click(); await row.getByText('置顶', { exact: true }).waitFor()
 checks.push('actual MFA Admin pin commits and current checked metadata reflects it')
 const userPage = await context.newPage(); userPage.on('pageerror', err => errors.push(err.message))
 await userPage.goto(web.origin + '/login')
 await userPage.waitForFunction(source => !!eval('(' + source + ')')('user'), findStore.toString())
 const aliceLogin = await userPage.evaluate(async ({ source, email, password }) => { const s = eval('(' + source + ')')('user'); const result = await s.login(email, password); return result?.code === 0 && s.user?.id === '6200001' }, { source: findStore.toString(), email: cfg.alice, password: cfg.password }); assert.ok(aliceLogin)
 const domainRejection = await userPage.evaluate(async source => { const s = eval('(' + source + ')')('user'); const r = await fetch('/api/v1/admin/identity/me', { headers: { Authorization: 'Bearer ' + s.token } }); return r.status }, findStore.toString()); assert.ok(domainRejection === 401)
 const bothCookies = await context.cookies(); assert.ok(bothCookies.some(c => c.name === 'campusos_csrf') && bothCookies.some(c => c.name === 'campusos_admin_csrf'))
 checks.push('real User JWT cannot enter Admin domain while both cookie families coexist')
 let releaseResponse, fetchedResponse, finishHeld
 const heldCompleted = new Promise(resolve => { finishHeld = resolve })
 const fetched = new Promise(r => { fetchedResponse = r }), release = new Promise(r => { releaseResponse = r })
 releaseHeldResponse = releaseResponse
 let held = false
 await userPage.route('**/api/v1/spaces/me/contents**', async route => {
  if (held) { await route.continue(); return }; held = true
  const response = await route.fetch(); const body = await response.json(); assert.ok(response.status() === 200 && JSON.stringify(body).includes('PRIVATE_BROWSER_CONTENT'))
  fetchedResponse(); await release; await route.fulfill({ response }); finishHeld()
 })
 await userPage.goto(web.origin + '/u/spa_alice', { waitUntil: 'domcontentloaded' }); await Promise.race([fetched, new Promise((_, reject) => { const timer = setTimeout(() => reject(new Error('actual owner response was not fetched')), 15000); timer.unref() })])
 const bobLogin = await userPage.evaluate(async ({ source, email, password }) => { const s = eval('(' + source + ')')('user'); const result = await s.login(email, password); return result?.code === 0 && s.user?.id === '6200002' }, { source: findStore.toString(), email: cfg.bob, password: cfg.password }); assert.ok(bobLogin)
 assert.ok(!(await userPage.locator('.public-space').innerText()).includes('PRIVATE_BROWSER_CONTENT'))
 await userPage.getByRole('link', { name: 'PUBLIC_BROWSER_CONTENT', exact: true }).waitFor(); releaseResponse(); await heldCompleted
 await userPage.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
 assert.ok(!(await userPage.locator('.public-space').innerText()).includes('PRIVATE_BROWSER_CONTENT'))
 assert.ok(await userPage.locator('.public-space .content-title').count() === 1)
 checks.push('actual late owner private response is discarded after switching User identity', 'current public Space still renders only public Core content')
 const control = async path => { const r = await fetch(cfg.control + path, { method: 'POST', headers: { Authorization: 'Bearer ' + cfg.controlKey } }); assert.ok(r.status === 204) }
 await control('/revoke-read'); await page.getByRole('button', { name: /刷新/ }).first().click()
 await page.waitForFunction(() => document.querySelectorAll('.thread-table .el-table__body-wrapper tr.el-table__row').length === 0)
 assert.ok(!(await page.locator('.thread-table').innerText()).includes(cfg.thread))
 checks.push('committed role permission revocation denies actual inventory and clears cached rows')
 await control('/suspend-admin')
 await page.getByRole('menuitem', { name: '用户管理', exact: true }).click()
 await page.waitForURL(/\/login(?:\?|$)/)
 assert.ok(await page.locator('input[autocomplete="username"]').count() === 1)
 checks.push('committed Admin suspension fails current navigation and clears local identity')
 assert.deepEqual(errors, [])
 assert.ok(!apiPaths.some(path => path === '/auth/me' || path === '/api/v1/auth/me' || path.startsWith('/api/v1/users/adm.')))
 checks.push('Admin SPA uses no User identity endpoint', 'no uncaught browser errors')
 const result = { status: 'passed', mocked_api: false, actual_cmd_server: true, browser: await browser.version(), checks }
 await writeFile(cfg.result, JSON.stringify(result, null, 2) + '\n'); console.log(JSON.stringify({ status: result.status, mocked_api: false, checks: checks.length, browser: result.browser }))
} finally {
 releaseHeldResponse()
 if (browser) await browser.close()
 for (const { server } of [admin, web]) server.closeAllConnections()
 await Promise.all([admin, web].map(({ server }) => new Promise(resolve => server.close(resolve))))
}
