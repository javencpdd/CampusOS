import assert from 'node:assert/strict'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { chromium } from '../../web/node_modules/playwright-core/index.mjs'

const base = process.env.CAMPUSOS_KNOWLEDGE_BROWSER_BASE || 'http://127.0.0.1:3001'
const output = resolve(process.env.CAMPUSOS_KNOWLEDGE_BROWSER_OUTPUT || '.cache/knowledge-feature-20261030')
const enable = process.env.CAMPUSOS_KNOWLEDGE_ENABLE === 'true'
await mkdir(output, { recursive: true })
const envBefore = await readFile('deploy/docker/.env.dev.local')
const browser = await chromium.launch({ executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox', '--no-proxy-server'] })
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } })
const report = { mutations: [], browserErrors: [] }; let phase = 'login'
page.on('request', r => { if (r.method() === 'POST' && new URL(r.url()).pathname.includes('/features/')) report.mutations.push(new URL(r.url()).pathname) })
page.on('pageerror', e => report.browserErrors.push(String(e.message).slice(0, 300)))
try {
  await page.goto(`${base}/login`)
  await page.getByPlaceholder('请输入管理员用户名或邮箱').fill(process.env.CAMPUSOS_KNOWLEDGE_BROWSER_USERNAME || 'admin')
  await page.getByPlaceholder('请输入密码', { exact: true }).fill(process.env.CAMPUSOS_KNOWLEDGE_BROWSER_PASSWORD || 'admin')
  await page.locator('form').getByRole('button', { name: '登录', exact: true }).click()
  await page.waitForURL(u => u.pathname === '/')
  phase = 'registry projection'
  const [response] = await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/features'),
    page.getByRole('menuitem', { name: '内置功能', exact: true }).click(),
  ])
  assert.equal(response.status(), 200)
  const before = (await response.json()).data.items
  const registered = before.filter(f => f.kind === 'builtin-feature')
  const rows = page.locator('.el-table__body tbody tr')
  assert.equal(await rows.count(), registered.length)
  for (const feature of registered) assert.equal(await rows.filter({ has: page.getByText(feature.id, { exact: true }) }).count(), 1)
  report.allRegisteredFeaturesVisible = registered.length
  const knowledge = before.find(f => f.id === 'knowledge')
  assert.ok(knowledge)
  const knowledgeRow = rows.filter({ has: page.getByText('knowledge', { exact: true }) })
  assert.ok((await knowledgeRow.innerText()).includes('知识检索'))
  if (!knowledge.enabled) {
    phase = 'disabled management guidance'
    await knowledgeRow.getByRole('button', { name: '知识来源管理', exact: true }).click()
    await page.getByRole('link', { name: '打开内置功能，启用“知识检索”', exact: true }).waitFor()
    report.disabledGuidanceLink = true
    await page.getByRole('link', { name: '打开内置功能，启用“知识检索”', exact: true }).click()
    if (enable) {
      phase = 'explicit Knowledge enable'
      const [changed] = await Promise.all([
        page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/features/knowledge/enable' && r.request().method() === 'POST'),
        knowledgeRow.locator('.el-switch__core').click(),
      ])
      assert.equal(changed.status(), 200)
      const state = (await changed.json()).data.state
      assert.equal(state.enabled, true); assert.equal(state.pending_restart, false)
      report.hotEnable = true
    }
  }
  await page.screenshot({ path: resolve(output, 'features.png'), fullPage: true })
  if (knowledge.enabled || enable) {
    phase = 'enabled workspace'
    await knowledgeRow.getByRole('button', { name: '知识来源管理', exact: true }).click()
    await page.getByRole('textbox', { name: '集合名称', exact: true }).waitFor()
    assert.equal(await page.getByRole('link', { name: '打开内置功能，启用“知识检索”', exact: true }).count(), 0)
    await page.screenshot({ path: resolve(output, 'workspace.png'), fullPage: true })
    report.workspaceAvailable = true
    const policy = await page.evaluate(async () => { const { useAdminStore } = await import('/src/modules/identity/store.ts'); return useAdminStore().profile?.mfa_required })
    if (policy === false) { assert.equal(await page.getByRole('button', { name: '创建集合', exact: true }).isDisabled(), false); report.currentMFAPolicyRespected = true }
    phase = 'other states preserved'
    const token = await page.evaluate(async () => { const { getAdminAccessToken } = await import('/src/modules/identity/session.ts'); return getAdminAccessToken() })
    const after = await page.request.get(`${base}/api/v1/features`, { headers: { Authorization: `Bearer ${token}` } })
    assert.equal(after.status(), 200)
    const current = (await after.json()).data.items
    for (const feature of before.filter(f => f.id !== 'knowledge')) {
      const item = current.find(f => f.id === feature.id)
      assert.equal(item.enabled, feature.enabled); assert.equal(item.desired_enabled, feature.desired_enabled); assert.equal(item.pending_restart, feature.pending_restart)
    }
    report.otherFeatureStatesPreserved = true
    const publicPage = await browser.newPage()
    const webBase = process.env.CAMPUSOS_KNOWLEDGE_WEB_BASE || 'http://127.0.0.1:3000'
    await publicPage.goto(`${webBase}/knowledge`)
    const publicResponse = await publicPage.request.get(`${webBase}/api/v1/knowledge/collections`)
    assert.equal(publicResponse.status(), 200)
    report.publicKnowledgeAvailable = true
    await publicPage.close()
  }
  assert.deepEqual(await readFile('deploy/docker/.env.dev.local'), envBefore)
  report.deploymentConfigPreserved = true; report.passed = true
} catch (error) {
  report.failure = { phase, type: error.name, message: String(error.message).slice(0, 700) }
  await page.screenshot({ path: resolve(output, 'failure.png'), fullPage: true }).catch(() => {})
  process.exitCode = 1
} finally {
  await page.evaluate(async () => { const { useAdminStore } = await import('/src/modules/identity/store.ts'); await useAdminStore().logout() }).catch(() => {})
  await writeFile(resolve(output, 'browser-report.json'), JSON.stringify(report, null, 2) + '\n')
  console.log(JSON.stringify(report))
  await browser.close()
}
