import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import path from 'node:path'
import { chromium } from 'playwright-core'

const origin = new URL(process.env.DOCS_WEBSITE_URL || 'http://127.0.0.1:3002').origin
const output = path.resolve(process.env.DOCS_WEBSITE_SCREENSHOT_DIR || '.cache/docs-website-browser')
const chrome = process.env.CHROME_BIN || '/usr/bin/google-chrome'
await fs.mkdir(output, { recursive: true })
const browser = await chromium.launch({ executablePath: chrome, headless: true, args: ['--no-sandbox'] })
const checks = []
const errors = []
const failedResponses = []
const apiRequests = []
const screenshots = []
const record = (name) => checks.push(name)
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, colorScheme: 'light' })
const page = await context.newPage()
page.on('pageerror', (error) => errors.push(error.message))
page.on('console', (message) => {
  if (message.type() === 'error') errors.push(message.text())
})
page.on('response', (response) => {
  if (response.status() >= 400) failedResponses.push({ status: response.status(), url: response.url() })
})
page.on('request', (request) => {
  if (new URL(request.url()).pathname.startsWith('/api/')) apiRequests.push(request.url())
})

async function noOverflow(label) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)
  assert.ok(overflow <= 1, `${label}: ${overflow}px horizontal overflow`)
}
async function screenshot(name, fullPage = false) {
  const file = path.join(output, `${name}.png`)
  await page.screenshot({ path: file, fullPage, animations: 'disabled' })
  screenshots.push(file)
}
async function visit(route) {
  const response = await page.goto(origin + route, { waitUntil: 'networkidle' })
  assert.equal(response.status(), 200, route)
  await page.getByRole('heading', { level: 1 }).waitFor()
}

try {
  await visit('/')
  assert.equal(await page.getByRole('heading', { level: 1 }).count(), 1)
  assert.match(await page.title(), /CampusOS/)
  assert.match(await page.locator('.home-progress').innerText(), /v1\.2-dev/)
  assert.match(await page.locator('.home-progress').innerText(), /v1\.1\.0-dev/)
  await page.getByText('产品场景示意，非线上业务数据', { exact: true }).waitFor()
  record('website title, one H1, separate runtime/development status, labelled illustrative content')

  const links = await page.locator('.campus-home a').evaluateAll((elements) => elements.map((a) => a.href))
  for (const href of new Set(links)) {
    const url = new URL(href)
    if (url.origin !== origin) continue
    const response = await page.request.get(url.href)
    assert.equal(response.status(), 200, `broken website link: ${url.pathname}`)
    if (url.hash && url.pathname === '/') assert.equal(await page.locator(url.hash).count(), 1)
  }
  record('all website internal destinations and homepage anchors resolve')

  for (const width of [320, 390, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: width < 600 ? 844 : 1000 })
    for (const name of ['社区交流', '知识检索', '开放扩展']) {
      await page.getByRole('tab', { name, exact: true }).click()
      await page.getByRole('tabpanel', { name, exact: true }).waitFor()
      assert.equal(await page.getByRole('tabpanel').count(), 1)
      assert.equal(await page.getByRole('tab', { name, exact: true }).getAttribute('aria-selected'), 'true')
      await noOverflow(`${width} ${name}`)
      if (width < 600) {
        const box = await page.getByRole('tab', { name, exact: true }).boundingBox()
        assert.ok(box.height >= 44, 'mobile scene tab must remain easy to touch')
      }
      await screenshot(`home-${width}-${['社区交流', '知识检索', '开放扩展'].indexOf(name)}`)
      record(`real scene interaction and responsive layout: ${width}px ${name}`)
    }
  }

  const first = page.getByRole('tab', { name: '社区交流', exact: true })
  await first.click()
  await first.press('ArrowRight')
  await page.getByRole('tabpanel', { name: '知识检索', exact: true }).waitFor()
  assert.equal(
    await page.getByRole('tab', { name: '知识检索', exact: true }).evaluate((el) => el === document.activeElement),
    true,
  )
  await page.keyboard.press('End')
  await page.getByRole('tabpanel', { name: '开放扩展', exact: true }).waitFor()
  await page.keyboard.press('Home')
  await page.getByRole('tabpanel', { name: '社区交流', exact: true }).waitFor()
  record('keyboard arrows/Home/End update selected tab, visible panel and focus together')

  await screenshot('home-desktop-full', true)
  await page.getByRole('link', { name: '探索产品', exact: true }).click()
  assert.equal(new URL(page.url()).hash, '#experience')
  await page.getByRole('link', { name: '开始使用', exact: true }).click()
  await page.waitForURL('**/guide/getting-started')
  await page.getByRole('heading', { level: 1, name: /完整入门/ }).waitFor()
  record('product anchor and primary get-started CTA navigate to their actual targets')

  for (const route of [
    '/guide/introduction',
    '/project/current-roadmap',
    '/plugins/overview',
    '/guide/public-knowledge-search',
    '/guide/private-knowledge',
    '/guide/cited-rag',
    '/plugins/pdf-viewer-tutorial',
  ]) {
    await page.setViewportSize({ width: 390, height: 844 })
    await visit(route)
    await noOverflow(route)
    record(`existing or updated documentation still reads at 390px: ${route}`)
  }

  await page.setViewportSize({ width: 1440, height: 1000 })
  await visit('/guide/introduction')
  await page.getByRole('button', { name: '搜索文档', exact: true }).click()
  await page.locator('#localsearch-input').fill('私人知识')
  await page.locator('.VPLocalSearchBox .results a').first().waitFor()
  await page.keyboard.press('Escape')
  await page.locator('.VPLocalSearchBox').waitFor({ state: 'hidden' })
  record('local documentation search returns results and closes by keyboard')

  await visit('/')
  await page.locator('.VPNavBarAppearance .VPSwitchAppearance').click()
  await page.waitForFunction(() => document.documentElement.classList.contains('dark'))
  await noOverflow('dark desktop')
  await screenshot('home-dark-desktop', true)
  await page.setViewportSize({ width: 390, height: 844 })
  await noOverflow('dark mobile')
  await screenshot('home-dark-mobile')
  record('actual theme switch, dark desktop and mobile remain readable without overflow')

  const noScript = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 1280, height: 900 } })
  const staticPage = await noScript.newPage()
  await staticPage.goto(origin, { waitUntil: 'load' })
  await staticPage.getByRole('heading', { level: 1 }).waitFor()
  assert.match(await staticPage.locator('.home-progress').innerText(), /v1\.2-dev/)
  await noScript.close()
  record('server-rendered homepage and version status exist without JavaScript')

  assert.deepEqual(errors, [], 'unexpected browser console/page errors')
  assert.deepEqual(failedResponses, [], 'missing website or documentation resources')
  assert.deepEqual(apiRequests, [], 'website must not require the CampusOS API')
  await fs.writeFile(
    path.join(output, 'report.json'),
    JSON.stringify({ status: 'passed', origin, checks, screenshots, errors, failedResponses, apiRequests }, null, 2) +
      '\n',
  )
  console.log(JSON.stringify({ status: 'passed', checks: checks.length, screenshots: screenshots.length, output }))
} catch (error) {
  await fs.writeFile(
    path.join(output, 'failure.json'),
    JSON.stringify(
      { status: 'failed', message: error.message, url: page.url(), checks, errors, failedResponses },
      null,
      2,
    ) + '\n',
  )
  await page.screenshot({ path: path.join(output, 'failure.png'), fullPage: true }).catch(() => {})
  throw error
} finally {
  await browser.close()
}
