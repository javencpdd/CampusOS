import { chromium } from 'playwright-core'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'

const chrome = process.env.CHROME_BIN
const webURL = (process.env.WEB_URL || 'http://localhost:3000').replace(/\/$/, '')
const screenshotDir = process.env.GROUPED_NAV_SCREENSHOT_DIR || path.join(os.tmpdir(), 'campusos-grouped-navigation')
const viewports = [
  { name: 'desktop', width: 1366, height: 768 },
  { name: 'phone', width: 360, height: 800 },
]

if (!chrome) throw new Error('CHROME_BIN is required')
fs.mkdirSync(screenshotDir, { recursive: true })

const browser = await chromium.launch({ executablePath: chrome, headless: true, args: ['--no-sandbox'] })
try {
  for (const viewport of viewports) {
    const page = await browser.newPage({ viewport })
    await page.goto(webURL, { waitUntil: 'domcontentloaded' })

    const group = page.getByRole('button', { name: '打开分组测试下的板块' })
    await group.waitFor()
    await group.click()

    const menu = page.locator('.el-dropdown-menu:visible')
    await menu.getByText('测试板块', { exact: true }).waitFor()
    await menu.getByText('版主测试', { exact: true }).waitFor()
    await page.screenshot({ path: path.join(screenshotDir, `${viewport.name}.png`) })

    await menu.getByText('测试板块', { exact: true }).click()
    await page.waitForURL((url) => url.pathname === '/threads' && Boolean(url.searchParams.get('category_id')))
    await page.close()
  }
  console.log(`grouped navigation browser check passed: ${screenshotDir}`)
} finally {
  await browser.close()
}
