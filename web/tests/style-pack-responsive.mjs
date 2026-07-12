import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { chromium } from 'playwright-core'

const chrome = process.env.CHROME_BIN
const webURL = (process.env.WEB_URL || 'http://localhost:3000').replace(/\/$/, '')
const email = process.env.CAMPUSOS_ADMIN_EMAIL || 'admin@campusos.local'
const password = process.env.CAMPUSOS_ADMIN_PASSWORD || 'Admin@123456'
const outputDir = process.env.STYLE_PACK_SCREENSHOT_DIR || path.join(os.tmpdir(), 'campusos-style-pack-smoke')

if (!chrome) throw new Error('CHROME_BIN is required')
fs.mkdirSync(outputDir, { recursive: true })

const browser = await chromium.launch({ executablePath: chrome, headless: true, args: ['--no-sandbox'] })

async function login(page) {
  await page.goto(`${webURL}/login`, { waitUntil: 'domcontentloaded' })
  await page.getByPlaceholder('请输入邮箱').fill(email)
  await page.getByPlaceholder('请输入密码').fill(password)
  await Promise.all([
    page.waitForURL((url) => url.pathname === '/', { timeout: 15_000 }),
    page.locator('form').getByRole('button', { name: '登录', exact: true }).click(),
  ])
}

async function verifyViewport(page, name) {
  await page.goto(`${webURL}/appearance`, { waitUntil: 'domcontentloaded' })
  await page.getByRole('heading', { name: '界面风格' }).waitFor()
  const aurora = page.locator('.theme-item').filter({ hasText: 'Aurora Campus' })
  await aurora.waitFor()
  const apply = aurora.getByRole('button', { name: '应用风格', exact: true })
  if (await apply.isEnabled()) await apply.click()
  await page.locator('[data-web-theme="aurora-campus"]').waitFor()
  const settings = aurora.getByRole('button', { name: '个性设置', exact: true })
  await settings.waitFor()
  await settings.click()
  await page.getByText('主要文字颜色', { exact: true }).waitFor()
  await page.getByText('页面背景图', { exact: true }).waitFor()
  await page.getByRole('button', { name: '应用设置', exact: true }).waitFor()
  await page.keyboard.press('Escape')
  await page.waitForTimeout(700)
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  if (overflow > 1) throw new Error(`${name} viewport has ${overflow}px horizontal overflow`)
  await page.screenshot({ path: path.join(outputDir, `${name}.png`), fullPage: true })
}

try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
  const page = await context.newPage()
  await login(page)
  await verifyViewport(page, 'desktop')
  await page.setViewportSize({ width: 390, height: 844 })
  await verifyViewport(page, 'mobile')
  await context.close()
  console.log(`style pack responsive smoke passed: ${outputDir}`)
} finally {
  await browser.close()
}
