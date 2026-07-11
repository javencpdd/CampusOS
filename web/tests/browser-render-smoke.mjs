import fs from 'node:fs'
import process from 'node:process'
import { chromium } from 'playwright-core'

const [url, screenshot, name] = process.argv.slice(2)
if (!url || !screenshot || !name || !process.env.CHROME_BIN) process.exit(2)

const browser = await chromium.launch({ executablePath: process.env.CHROME_BIN, headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] })
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
  await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 20_000 })
  if (name === 'web' || name === 'admin') {
    await page.waitForFunction(() => document.querySelector('#app')?.childElementCount > 0, undefined, { timeout: 10_000 })
  } else {
    await page.waitForFunction(() => document.body.innerText.includes('CampusOS'), undefined, { timeout: 10_000 })
  }
  await page.screenshot({ path: screenshot, fullPage: false })
  if (fs.statSync(screenshot).size < 1024) throw new Error('rendered screenshot is unexpectedly small')
} finally {
  await browser.close()
}
