// Built Web assets, normal Server/router and owned PostgreSQL. Transport faults
// only abort/delay a connection; no application response data is fabricated.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFile, writeFile } from 'node:fs/promises'
import { createServer, request as proxyRequest } from 'node:http'
import { extname, resolve, sep } from 'node:path'
import { runKnowledgeAdmin } from './v12-12-admin-browser.mjs'
const cfg = JSON.parse(await readFile(process.argv[2], 'utf8'))
const require = createRequire(resolve('web/package.json'))
const { chromium } = require('playwright-core')
const checks = [], errors = [], knowledgeRequests = []
let abortSearch = false, delaySource = false, releaseSource, sourceHeld
const root = resolve(cfg.webDist)
const web = createServer(async (req, res) => {
  if (req.url.startsWith('/api/')) {
    const path = new URL(req.url, cfg.backend).pathname
    if (path.startsWith('/api/v1/knowledge/')) knowledgeRequests.push({ path, query: new URL(req.url, cfg.backend).search, cookie: Boolean(req.headers.cookie), authorization: Boolean(req.headers.authorization) })
    if (abortSearch && path.endsWith('/search')) { req.socket.destroy(); return }
    const upstream = proxyRequest(new URL(req.url, cfg.backend), { method: req.method, headers: req.headers }, async response => {
      if (delaySource && path.includes('/sources/')) {
        response.pause()
        await new Promise(r => { releaseSource = r; sourceHeld?.() })
      }
      if (res.destroyed) { response.destroy(); return }
      res.writeHead(response.statusCode, response.headers); response.pipe(res)
    })
    res.on('close', () => upstream.destroy())
    upstream.on('error', () => { if (!res.headersSent) res.writeHead(502); res.end() })
    req.pipe(upstream); return
  }
  try {
    const path = resolve(root, '.' + new URL(req.url, 'http://localhost').pathname)
    if (path !== root && !path.startsWith(root + sep)) { res.writeHead(404).end(); return }
    const file = extname(path) ? path : resolve(root, 'index.html')
    const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png', '.ico': 'image/x-icon' }
    const bytes = await readFile(file); res.writeHead(200, { 'Content-Type': types[extname(file)] || 'application/octet-stream' }).end(bytes)
  } catch { if (!res.headersSent) res.writeHead(404); res.end() }
})
await new Promise(r => web.listen(0, '127.0.0.1', r))
const origin = `http://127.0.0.1:${web.address().port}`
const control = async (path, body) => {
  const response = await fetch(cfg.control + path, { method: 'POST', headers: { Authorization: 'Bearer ' + cfg.controlKey, 'Content-Type': 'application/json' }, ...(body ? { body: JSON.stringify(body) } : {}) })
  assert.equal(response.status, 200, 'owned fixture control failed'); return response.json()
}
const check = text => checks.push(text)
const waitSearch = page => page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/search') && r.request().method() === 'GET')
async function search(page, text = '图书馆') {
  await page.getByLabel('搜索词', { exact: true }).fill(text)
  const response = waitSearch(page)
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  const res = await response; assert.equal(res.status(), 200); const data = (await res.json()).data
  await page.getByRole('status').filter({ hasText: `找到 ${data.total} 条公开资料` }).waitFor()
  assert.equal(await page.locator('.knowledge-hit').count(), data.items.length)
  return data
}
async function open(page, data, index = data.items.findIndex(h => h.citation.source.kind === 'community.thread')) {
  assert.ok(index >= 0)
  const citation = data.items[index].citation
  const response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/sources/' + citation.member_id))
  await page.locator('.knowledge-hit').nth(index).getByRole('button', { name: '查看当前引用' }).click()
  const res = await response; assert.equal(res.status(), 200); const current = (await res.json()).data
  assert.equal(current.revision, citation.visibility_revision)
  assert.equal(new URL(res.url()).searchParams.get('revision'), citation.visibility_revision)
  await page.locator('pre.source-content').waitFor()
  assert.equal(await page.locator('pre.source-content').textContent(), current.source.content)
  return { current, citation, index }
}
let browser
try {
  browser = await chromium.launch({ executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox'] })
  const desktop = await browser.newContext({ viewport: { width: 1280, height: 900 } }); desktop.setDefaultTimeout(15000)
  await desktop.addCookies([{ name: 'owned12_dummy_identity', value: 'not-public-authority', url: origin }])
  const page = await desktop.newPage(); page.on('pageerror', e => errors.push(e.message))
  let unsafeFetches = 0
  page.on('request', r => { if (r.url().includes('never-fetch-knowledge-image')) unsafeFetches++ })
  await page.goto(origin + '/')
  await page.getByRole('link', { name: '知识检索', exact: true }).click()
  await page.getByText('知识检索暂未启用，请联系管理员。', { exact: true }).waitFor()
  assert.equal(await page.locator('form[aria-label="公开知识检索"]').count(), 0)
  check('desktop anonymous navigation reaches actual disabled Knowledge state')
  await control('/enable'); await page.getByRole('button', { name: '重新加载集合' }).click()
  await page.getByText('尚未配置公开知识集合，请联系管理员添加资料。', { exact: true }).waitFor()
  check('enabled but genuinely unconfigured database has no search action')
  const seeded = await control('/seed')
  await page.getByRole('button', { name: '重新加载集合' }).click()
  await page.getByLabel('公开集合', { exact: true }).waitFor()
  assert.equal(await page.getByLabel('公开集合', { exact: true }).inputValue(), '')
  assert.equal(await page.getByRole('button', { name: '搜索', exact: true }).isDisabled(), true)
  await page.getByLabel('公开集合', { exact: true }).selectOption(seeded.primary)
  let data = await search(page)
  assert.equal(data.total, 24); assert.equal(data.items.length, 20)
  assert.ok(!JSON.stringify(data).includes('PRIVATE_'))
  check('explicit public collection selection and normal resident-indexer search show current 24-item corpus and page20')
  let response = waitSearch(page)
  await page.getByRole('button', { name: '下一页', exact: true }).click(); let res = await response
  assert.equal(res.status(), 200); let second = (await res.json()).data
  await page.getByText('第 2 页', { exact: true }).waitFor(); assert.equal(second.items.length, 4)
  assert.equal(await page.getByRole('button', { name: '下一页', exact: true }).isDisabled(), true)
  assert.ok(second.items.every(h => !data.items.some(old => old.citation.member_id === h.citation.member_id)))
  check('desktop paging reaches four remaining rows with no repeated citation')
  data = await search(page)
  let opened = await open(page, data)
  assert.equal(await page.getByRole('link', { name: '打开原帖', exact: true }).getAttribute('href'), '/threads/' + opened.citation.source.id)
  assert.equal(await page.locator('pre img, .knowledge-hit img, pre script, .knowledge-hit script').count(), 0)
  assert.equal(await page.evaluate(() => window.knowledgeInjected), undefined); assert.equal(unsafeFetches, 0)
  await page.screenshot({ path: resolve(cfg.evidence, 'desktop.png'), fullPage: true })
  check('current revision reopens through real Source API; raw markup stays text and verified original link is local')
  abortSearch = true
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByText('检索暂不可用，请稍后重试。', { exact: true }).waitFor()
  assert.equal(await page.locator('pre, .knowledge-hit').count(), 0)
  abortSearch = false; response = waitSearch(page)
  await page.getByRole('button', { name: '重试检索', exact: true }).click(); res = await response
  assert.equal(res.status(), 200); data = (await res.json()).data
  await page.locator('.knowledge-hit').first().waitFor()
  check('transport connection failure clears all output; user retry reaches actual successful backend')
  delaySource = true
  const held = new Promise(r => { sourceHeld = r })
  const firstThread = data.items.findIndex(h => h.citation.source.kind === 'community.thread')
  await page.locator('.knowledge-hit').nth(firstThread).getByRole('button', { name: '查看当前引用' }).click()
  await held; await page.getByText('正在核验来源…', { exact: true }).waitFor()
  await page.getByLabel('公开集合', { exact: true }).selectOption(seeded.secondary)
  delaySource = false; releaseSource(); sourceHeld = undefined
  await page.waitForTimeout(150)
  assert.equal(await page.locator('pre, .knowledge-hit').count(), 0)
  const official = await search(page)
  assert.equal(official.total, 1); await open(page, official, 0)
  assert.equal(await page.getByRole('link', { name: '打开原帖', exact: true }).count(), 0)
  check('collection switch aborts delayed source bytes; official text has no fabricated original-page link')
  await page.getByLabel('公开集合', { exact: true }).selectOption(seeded.primary)
  data = await search(page); opened = await open(page, data)
  await control('/revise', { id: opened.citation.source.id })
  response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/sources/' + opened.citation.member_id))
  await page.locator('.knowledge-hit').nth(opened.index).getByRole('button', { name: '查看当前引用' }).click()
  assert.equal((await response).status(), 409)
  await page.getByText('来源已更新，请重新搜索获取当前引用。', { exact: true }).waitFor()
  assert.equal(await page.locator('pre, .knowledge-hit').count(), 0)
  response = waitSearch(page); await page.getByRole('button', { name: '重新搜索', exact: true }).click()
  assert.equal((await response).status(), 200); await page.locator('.knowledge-hit').first().waitFor()
  check('actual origin revision update rejects old citation409, removes old full text/snippets and offers working new search')
  data = await search(page); opened = await open(page, data)
  await control('/private', { id: opened.citation.source.id })
  response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/sources/' + opened.citation.member_id))
  await page.locator('.knowledge-hit').nth(opened.index).getByRole('button', { name: '查看当前引用' }).click()
  assert.equal((await response).status(), 404)
  await page.getByText('引用已不可读或已移除，请重新搜索。', { exact: true }).waitFor()
  assert.equal(await page.locator('pre, .knowledge-hit').count(), 0)
  assert.ok(!(await page.locator('main.knowledge-search').innerText()).includes('PRIVATE_'))
  check('actual private origin rejects old citation404; no cached source, stale snippet or private body remains')
  data = await search(page, '完全不匹配xyz')
  assert.equal(data.total, 0); await page.getByText('没有匹配的公开资料，请调整搜索词或选择其他集合。', { exact: true }).waitFor()
  check('genuine empty query result is distinct from unconfigured/disabled/failed states')
  await search(page); await control('/disable')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByText('知识检索暂未启用，请联系管理员。', { exact: true }).waitFor()
  assert.equal(await page.locator('form[aria-label="公开知识检索"], pre, .knowledge-hit').count(), 0)
  await control('/enable'); await page.getByRole('button', { name: '重新加载集合' }).click()
  await page.getByLabel('公开集合', { exact: true }).waitFor()
  check('real owning Feature hot-disable removes search/citation output; explicit reload recovers after enable')
  const mobile = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true }); mobile.setDefaultTimeout(15000)
  const mp = await mobile.newPage(); mp.on('pageerror', e => errors.push(e.message))
  await mp.goto(origin + '/'); await mp.getByRole('button', { name: '打开主导航', exact: true }).click()
  await mp.getByRole('navigation', { name: '移动端站点导航' }).getByRole('link', { name: '知识检索', exact: true }).click()
  await mp.getByLabel('公开集合', { exact: true }).selectOption(seeded.primary)
  const mobileResponse = waitSearch(mp)
  await mp.getByLabel('搜索词', { exact: true }).fill('图书馆'); await mp.getByLabel('搜索词', { exact: true }).press('Enter')
  res = await mobileResponse; assert.equal(res.status(), 200); const mobileData = (await res.json()).data
  await mp.locator('.knowledge-hit').first().waitFor(); await open(mp, mobileData)
  assert.ok(await mp.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
  assert.ok(await mp.locator('.knowledge-search button').evaluateAll(nodes => nodes.filter(n => n.getBoundingClientRect().height > 0).every(n => n.getBoundingClientRect().height >= 44)))
  await mp.screenshot({ path: resolve(cfg.evidence, 'mobile.png'), fullPage: true })
  check('390px touch viewport uses actual navigation, keyboard search/current citation,44px controls and no horizontal overflow')
  assert.ok(!new URL(mp.url()).search)
  assert.ok(await mp.evaluate(() => !Object.values(localStorage).some(value => /PUBLIC_CAMPUS_BODY|OFFICIAL_PUBLIC_12|图书馆/.test(value))))
  assert.ok(knowledgeRequests.length >= 15 && knowledgeRequests.every(r => !r.cookie && !r.authorization))
  assert.deepEqual(errors, [])
  check('public requests omit identity/cookies even with a browser cookie; query/body not persisted; no page script errors')
  const adminChecks = await runKnowledgeAdmin(browser, {...cfg, publicThread:mobileData.items.find(h=>h.citation.source.kind === "community.thread").citation.source.id}, control)
  checks.push(...adminChecks)
  await writeFile(cfg.result, JSON.stringify({ status: 'passed', checks, check_count: checks.length, actual_built_web: true, actual_server_composition: true, actual_production_router: true, actual_postgresql: true, actual_cmd_server_process: false, actual_built_admin: true, admin_check_count: adminChecks.length, mocked_api_data: false, transport_faults: ['search connection abort', 'delay actual source response'], knowledge_http_requests: knowledgeRequests.length, desktop: { width: 1280, height: 900 }, mobile: { width: 390, height: 844 }, browser_errors: errors, public_identity_header_count: 0, windows: false, standalone_server_deployment: false }, null, 2) + '\n')
} finally {
  releaseSource?.()
  await browser?.close()
  await new Promise(r => { web.close(r); web.closeAllConnections() })
}
