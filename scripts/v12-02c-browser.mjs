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
const admin = await staticProxy('admin/dist'); const checks = [], errors = []
let browser
try {
 browser = await chromium.launch({ executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox'] })
 const context = await browser.newContext({ viewport: { width: 1280, height: 1000 } }); context.setDefaultTimeout(15000)
 const page = await context.newPage(); page.on('pageerror', err => errors.push(err.message))
 await page.goto(admin.origin + '/login?redirect=/integrations/principals')
 await page.locator('input[autocomplete="username"]').fill(cfg.adminUsername); await page.locator('input[autocomplete="current-password"]').fill(cfg.password)
 await page.getByRole('button', { name: '登录', exact: true }).click(); await page.waitForURL(admin.origin + '/integrations/principals')
 await page.getByRole('heading', { name: '机器集成凭据', exact: true }).waitFor()
 checks.push('actual Admin login and current management inventory through built SPA')
 await page.getByLabel('集成名称', { exact: true }).fill('浏览器机器验收')
 let responseWait = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/v1/admin/identity/integrations')
 await page.getByRole('button', { name: '创建集成', exact: true }).click(); const denied = await responseWait; assert.ok(denied.status() === 403)
 checks.push('password-only Admin cannot create machine identity')
 const dialog = page.getByRole('dialog', { name: '确认管理员身份' }); await dialog.waitFor()
 await dialog.getByRole('button', { name: '开始配置管理员认证器', exact: true }).click()
 const secretInput = dialog.locator('input[readonly]'); await secretInput.waitFor(); const mfaSecret = await secretInput.inputValue()
 const codeInput = dialog.locator('input[autocomplete="one-time-code"]'); const step = Math.floor(Date.now()/30000)
 await codeInput.fill(totp(mfaSecret, step)); await dialog.getByRole('button', { name: '完成配置', exact: true }).click()
 await dialog.getByText('配置完成。请等待认证器显示下一组验证码，再确认管理员身份。', { exact: true }).waitFor()
 await codeInput.fill(totp(mfaSecret, step + 1)); await dialog.getByRole('button', { name: '确认身份', exact: true }).click(); await dialog.waitFor({ state: 'hidden' })
 checks.push('actual own Admin TOTP enrollment and recent step-up')
 responseWait = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/v1/admin/identity/integrations')
 await page.getByRole('button', { name: '创建集成', exact: true }).click(); const created = await responseWait; assert.ok(created.status() === 201); const principal = (await created.json()).data
 await page.getByLabel('帖子 ID', { exact: true }).fill(cfg.thread + ',' + cfg.privateThread)
 await page.getByLabel('MCP 读取公开帖子', { exact: true }).check()
 responseWait = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/credentials'))
 await page.getByRole('button', { name: '签发新凭据', exact: true }).click(); const issuance = await responseWait; assert.ok(issuance.status() === 201); const issued = (await issuance.json()).data
 await page.getByRole('dialog', { name: '一次性凭据' }).waitFor(); assert.ok(await page.getByTestId('one-time-token').textContent() === issued.token, 'one-time display mismatch')
 const persisted = await page.evaluate(() => JSON.stringify(localStorage) + JSON.stringify(sessionStorage)); assert.ok(!persisted.includes(issued.token))
 const machine = async (token, path, body, scheme = 'Integration') => { const r = await fetch(cfg.backend + '/api/v1' + path, { method: body ? 'POST' : 'GET', headers: { Authorization: scheme + ' ' + token, ...(body ? { 'Content-Type': 'application/json' } : {}) }, ...(body ? { body: JSON.stringify(body) } : {}) }); return { status: r.status, data: await r.json() } }
 let r = await machine(issued.token, '/integration/threads/' + cfg.thread); assert.ok(r.status === 200 && r.data.data.content.includes('PUBLIC_BROWSER_CONTENT')); assert.ok(!Object.hasOwn(r.data.data, 'moderation_reason'))
 r = await machine(issued.token, '/integration/mcp/tools/public_thread_read/call', { id: cfg.thread }); assert.ok(r.status === 200)
 checks.push('real digest authentication, exact HTTP and MCP whitelist with actual public Community row', 'one-time secret absent from persistent browser storage')
 r = await machine(issued.token, '/integration/threads/' + cfg.privateThread); assert.ok(r.status === 404 && !JSON.stringify(r.data).includes('PRIVATE_BROWSER_CONTENT'))
 r = await machine(issued.token, '/integration/threads/99999999'); assert.ok(r.status === 403)
 r = await machine(issued.token, '/integration/mcp/tools/admin_accounts_list/call', { id: cfg.thread }); assert.ok(r.status === 403)
 for (const path of ['/admin/identity/me','/auth/me','/mcp/tools']) { r = await machine(issued.token,path); assert.ok(r.status === 401) }
 r = await machine(issued.token, '/integration/identity/me', undefined, 'Bearer'); assert.ok(r.status === 401)
 r = await machine(issued.token, '/integration/identity/me?token=' + issued.token); assert.ok(r.status === 401)
 const adminToken = await page.evaluate(source => eval('(' + source + ')')('admin')?.token, findStore.toString()); r = await machine(adminToken, '/integration/identity/me'); assert.ok(r.status === 401)
 checks.push('URL credentials rejected without raw query logging', 'current real Admin JWT cannot become an Integration credential')
 checks.push('private source remains denied despite explicit key whitelist', 'unlisted resource/tool and User/Admin/legacy MCP domains rejected')
 await page.getByRole('button', { name: '我已保存，关闭', exact: true }).click(); assert.ok(await page.getByTestId('one-time-token').count() === 0)
 await page.getByRole('button', { name: new RegExp(issued.credential.prefix) }).click()
 await page.getByLabel('帖子 ID', { exact: true }).fill(cfg.privateThread)
 responseWait = page.waitForResponse(r => r.request().method() === 'PUT' && new URL(r.url()).pathname.includes('/credentials/'))
 await page.getByRole('button', { name: '更新所选范围', exact: true }).click(); assert.ok((await responseWait).status() === 200)
 r = await machine(issued.token, '/integration/threads/' + cfg.thread); assert.ok(r.status === 403)
 checks.push('scope narrowing commits and next real read is denied')
 await page.getByRole('button', { name: new RegExp(issued.credential.prefix) }).click(); await page.getByLabel('帖子 ID', { exact: true }).fill(cfg.thread)
 responseWait = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/rotate'))
 await page.getByRole('button', { name: '轮换所选凭据', exact: true }).click(); const rotation = await responseWait; assert.ok(rotation.status() === 201); const rotated = (await rotation.json()).data
 await page.getByRole('dialog', { name: '一次性凭据' }).waitFor(); assert.ok(rotated.token !== issued.token)
 r = await machine(issued.token, '/integration/identity/me'); assert.ok(r.status === 401)
 r = await machine(rotated.token, '/integration/threads/' + cfg.thread); assert.ok(r.status === 200)
 checks.push('actual UI atomic rotation invalidates old key only after successful commit')
 await page.getByRole('button', { name: '我已保存，关闭', exact: true }).click()
 responseWait = page.waitForResponse(r => r.request().method() === 'PUT' && new URL(r.url()).pathname === '/api/v1/admin/identity/integrations/' + principal.id)
 await page.getByRole('button', { name: '暂停集成', exact: true }).click(); assert.ok((await responseWait).status() === 200)
 r = await machine(rotated.token, '/integration/identity/me'); assert.ok(r.status === 401)
 await page.getByRole('button', { name: '恢复集成', exact: true }).click(); await page.getByRole('button', { name: '暂停集成', exact: true }).waitFor()
 r = await machine(rotated.token, '/integration/identity/me'); assert.ok(r.status === 200 && r.data.data.principal.actor.kind === 'integration')
 checks.push('principal suspension denies all credentials; explicit restore rechecks original expiry and scope')
 let control = await fetch(cfg.control + '/make-private', { method:'POST', headers:{ Authorization:'Bearer ' + cfg.controlKey } }); assert.ok(control.status === 204)
 r = await machine(rotated.token, '/integration/threads/' + cfg.thread); assert.ok(r.status === 404 && !JSON.stringify(r.data).includes('PRIVATE_AFTER_WHITELIST'))
 control = await fetch(cfg.control + '/make-public', { method:'POST', headers:{ Authorization:'Bearer ' + cfg.controlKey } }); assert.ok(control.status === 204)
 checks.push('source made private after issuance cannot be read; no cached permission')
 await page.getByRole('button', { name: new RegExp(rotated.credential.prefix) }).click(); page.once('dialog', dialog => dialog.accept())
 responseWait = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/revoke'))
 await page.getByRole('button', { name:'撤销所选凭据', exact:true }).click(); assert.ok((await responseWait).status() === 200)
 r = await machine(rotated.token, '/integration/identity/me'); assert.ok(r.status === 401)
 checks.push('real UI credential revocation commits and key is immediately rejected')
 // Issue again and permanently revoke its principal to cover both lifecycle roots.
 await page.getByLabel('帖子 ID', { exact: true }).fill(cfg.thread)
 responseWait = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/credentials'))
 await page.getByRole('button', { name:'签发新凭据', exact:true }).click(); const lastIssue = (await (await responseWait).json()).data; await page.getByRole('dialog', { name:'一次性凭据' }).waitFor()
 await page.getByRole('button', { name:'我已保存，关闭', exact:true }).click(); page.once('dialog', dialog => dialog.accept())
 await page.getByRole('button', { name:'撤销集成', exact:true }).click(); await page.getByText('该集成已永久撤销，所有关联凭据失效。', { exact:true }).waitFor()
 r = await machine(lastIssue.token, '/integration/identity/me'); assert.ok(r.status === 401)
 checks.push('permanent principal revocation invalidates remaining current credential')
 await page.setViewportSize({ width:390,height:844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2))
 checks.push('mobile management view has no horizontal overflow')
 await page.reload(); await page.getByRole('heading', { name:'机器集成凭据', exact:true }).waitFor(); assert.ok(await page.getByTestId('one-time-token').count() === 0)
 checks.push('actual reload fetches safe metadata and cannot redisplay any issued secret')
 const logText = await readFile(cfg.serverLog, 'utf8'); for(const secret of [issued.token, rotated.token, lastIssue.token, mfaSecret]) assert.ok(!logText.includes(secret), 'secret appeared in request logs')
 checks.push('actual server logs contain no issued raw credentials or MFA enrollment secret')
 assert.deepEqual(errors, [])
 await writeFile(cfg.result, JSON.stringify({ status:'passed', mocked_api:false, actual_cmd_server:true, checks, browser_errors:errors },null,2)+'\n')
 console.log('Integration actual backend browser passed: ' + checks.length + ' checks')
} finally { if(browser) await browser.close(); await new Promise(r => admin.server.close(r)) }
