import assert from 'node:assert/strict'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { chromium } from '../../web/node_modules/playwright-core/index.mjs'

const base = process.env.CAMPUSOS_CHEN_BROWSER_BASE || 'http://127.0.0.1:3001'
const output = resolve(process.env.CAMPUSOS_CHEN_BROWSER_OUTPUT || '.cache/chen-er-20261030')
await mkdir(output, { recursive: true })
const browser = await chromium.launch({ executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox', '--no-proxy-server'] })
const page = await browser.newPage({ viewport: { width: 1600, height: 1080 } })
const report = {}; let phase = 'login'
try {
  await page.goto(`${base}/login`)
  await page.getByPlaceholder('请输入管理员用户名或邮箱').fill(process.env.CAMPUSOS_CHEN_BROWSER_USERNAME || 'admin')
  await page.getByPlaceholder('请输入密码', { exact: true }).fill(process.env.CAMPUSOS_CHEN_BROWSER_PASSWORD || 'admin')
  await page.locator('form').getByRole('button', { name: '登录', exact: true }).click()
  await page.waitForURL(u => u.pathname === '/')
  phase = 'Chen diagram'
  await page.getByRole('menuitem', { name: '数据架构', exact: true }).click()
  const graph = page.locator('[data-chen-diagram]')
  await graph.waitFor()
  assert.equal(await page.locator('.relation-lane').count(), 0)
  assert.equal(await graph.locator('[data-entity="users"]').count(), 1)
  assert.ok(await graph.locator('[data-relationship]').count() >= 1)
  assert.equal(await graph.locator('[data-attribute="users:id"] text').getAttribute('text-decoration'), 'underline')
  assert.equal(await graph.locator('marker, [marker-end], [marker-start]').count(), 0)
  report.rectanglesOvalsDiamondsAndKeys = true
  await page.getByRole('button', { name: '适应宽度', exact: true }).click()
  await page.locator('.chen-view').screenshot({ path: resolve(output, 'chen-community.png') })
  phase = 'multivalued attributes'
  await page.getByRole('button', { name: '标签多值属性', exact: true }).click()
  assert.equal(await graph.locator('[data-attribute="threads:tags"] ellipse').count(), 2)
  report.multivaluedDoubleOval = true
  phase = 'weak entity'
  await page.getByRole('button', { name: '执行事件弱实体', exact: true }).click()
  assert.equal(await graph.locator('[data-entity="ai_model_run_events"] rect').count(), 2)
  assert.equal(await graph.locator('[data-identifying="true"] polygon').count(), 2)
  assert.ok(await graph.locator('[data-participation="total"] line').count() >= 2)
  report.weakEntityAndIdentifyingRelationship = true
  await page.locator('.chen-view').screenshot({ path: resolve(output, 'chen-weak.png') })
  phase = 'export'
  const [download] = await Promise.all([page.waitForEvent('download'), page.getByRole('button', { name: '导出 SVG', exact: true }).click()])
  const svgPath = resolve(output, 'chen-weak.svg'); await download.saveAs(svgPath)
  const svg = await readFile(svgPath, 'utf8')
  assert.ok(svg.includes('xmlns="http://www.w3.org/2000/svg"'))
  assert.ok(svg.includes('ai_model_run_events'))
  assert.ok(!svg.includes('marker-end'))
  const exported = await browser.newPage({ viewport: { width: 1660, height: 900 } })
  await exported.goto(`data:image/svg+xml;base64,${Buffer.from(svg).toString('base64')}`)
  assert.equal(await exported.locator('[data-identifying="true"] polygon').count(), 2)
  await exported.goto('about:blank')
  await exported.setContent(`<html><body style="margin:0">${svg}</body></html>`)
  await exported.screenshot({ path: resolve(output, 'chen-export-preview.png'), fullPage: true })
  await exported.close()
  report.standaloneSVGExport = true
  phase = 'entity selection and identifier details'
  await graph.locator('[data-entity="ai_model_run_events"]').focus()
  await page.keyboard.press('Enter')
  await page.waitForFunction(() => document.querySelector('#chen-focus')?.value === 'ai_model_run_events')
  assert.ok((await page.locator('.table-inspector').innerText()).includes('run_id + ordinal'))
  report.keyboardSelection = true
  phase = 'domain filter and empty relationship'
  await page.getByRole('radiogroup', { name: '数据库领域筛选' }).getByText('空间与内容', { exact: true }).click()
  await page.getByRole('button', { name: '执行事件弱实体', exact: true }).click()
  assert.equal(await page.locator('#chen-focus').inputValue(), 'ai_model_runs')
  await page.locator('#chen-focus').selectOption('schema_migrations')
  assert.equal(await graph.locator('[data-entity]').count(), 1)
  assert.equal(await graph.locator('[data-attribute="schema_migrations:version"]').count(), 1)
  report.filterAndStandaloneEntity = true
  phase = 'mobile'
  await page.setViewportSize({ width: 375, height: 812 })
  await page.getByRole('button', { name: '用户与帖子', exact: true }).click()
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
  await page.locator('.chen-view').screenshot({ path: resolve(output, 'chen-mobile.png') })
  await page.getByRole('button', { name: '适应宽度', exact: true }).click()
  assert.equal(await page.locator('.chen-viewport').evaluate(el => el.scrollWidth <= el.clientWidth + 1), true)
  report.mobileNoPageOverflow = true
  report.mobileFit = true
  report.passed = true
} catch (error) {
  report.failure = { phase, type: error.name, message: String(error.message).slice(0, 800) }
  await page.screenshot({ path: resolve(output, 'failure.png'), fullPage: true }).catch(() => {})
  process.exitCode = 1
} finally {
  await page.evaluate(async () => { const { useAdminStore } = await import('/src/modules/identity/store.ts'); await useAdminStore().logout() }).catch(() => {})
  await writeFile(resolve(output, 'browser-report.json'), JSON.stringify(report, null, 2) + '\n')
  console.log(JSON.stringify(report))
  await browser.close()
}
