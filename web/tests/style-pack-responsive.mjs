import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { chromium } from 'playwright-core'

const chrome = process.env.CHROME_BIN
const webURL = (process.env.WEB_URL || 'http://localhost:3000').replace(/\/$/, '')
const email = process.env.CAMPUSOS_ADMIN_EMAIL || 'admin@campusos.local'
const password = process.env.CAMPUSOS_ADMIN_PASSWORD || 'Admin@123456'
const outputDir = process.env.STYLE_PACK_SCREENSHOT_DIR || path.join(os.tmpdir(), 'campusos-style-pack-smoke')
const systemPacks = ['aurora-campus', 'campus-canvas']
const spacePacks = ['clean-blog', 'kinetic-journal']
const profiles = [
  { id: 'desktop', width: 1440, height: 1000 },
  { id: 'mobile', width: 390, height: 844 },
  { id: 'bridge', width: 768, height: 1024 },
]

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

async function checkViewport(page, evidence) {
  const diagnostics = await page.evaluate(() => {
    const overflow = Math.max(0, document.documentElement.scrollWidth - window.innerWidth)
    const fixed = [...document.querySelectorAll('button, input, select, textarea, [role="button"]')]
      .filter((element) => {
        const rect = element.getBoundingClientRect()
        const style = getComputedStyle(element)
        return rect.width > 0 && rect.height > 0 && style.visibility !== 'hidden' && style.display !== 'none'
      })
      .map((element) => {
        const rect = element.getBoundingClientRect()
        return {
          tag: element.tagName,
          label: (element.getAttribute('aria-label') || element.textContent || '').trim().slice(0, 80),
          width: Math.round(rect.width),
          height: Math.round(rect.height),
          clipped: rect.right > window.innerWidth + 1 || rect.left < -1,
        }
      })
    return { overflow, clipped: fixed.filter((item) => item.clipped), controls: fixed.length }
  })
  if (diagnostics.overflow > 1 || diagnostics.clipped.length) {
    throw new Error(
      `${evidence.profile}/${evidence.pack}/${evidence.page} layout violation: ${JSON.stringify(diagnostics)}`,
    )
  }
}

async function screenshot(page, evidence) {
  const name = [evidence.profile, evidence.pack, evidence.page, evidence.motion].join('--')
  await page.screenshot({ path: path.join(outputDir, `${name}.png`), fullPage: true })
}

async function withEvidence(page, evidence, operation) {
  const failures = []
  const listener = (response) => {
    if (response.status() >= 400) failures.push({ url: response.url(), status: response.status() })
  }
  page.on('response', listener)
  try {
    await operation()
    await checkViewport(page, evidence)
    await screenshot(page, evidence)
  } catch (error) {
    const report = { ...evidence, failures, error: String(error), url: page.url() }
    await page
      .screenshot({
        path: path.join(outputDir, `${evidence.profile}--${evidence.pack}--${evidence.page}--failure.png`),
        fullPage: true,
      })
      .catch(() => {})
    fs.writeFileSync(
      path.join(outputDir, `${evidence.profile}--${evidence.pack}--${evidence.page}--failure.json`),
      `${JSON.stringify(report, null, 2)}\n`,
    )
    throw error
  } finally {
    page.off('response', listener)
  }
}

async function applySystemPack(page, name) {
  await page.goto(`${webURL}/appearance`, { waitUntil: 'domcontentloaded' })
  const item = page.locator(`[data-style-pack-name="${name}"]`)
  await item.waitFor()
  const apply = item.getByRole('button', { name: '应用风格', exact: true })
  if (await apply.isEnabled()) await apply.click()
  await page.locator(`[data-web-theme="${name}"]`).waitFor()
}

async function openSpaceSettings(page) {
  const avatar = page.locator('.nav-avatar')
  await avatar.waitFor()
  await avatar.click()
  const personalSpaceEntry = page.getByRole('menuitem', { name: '个人空间', exact: true })
  await personalSpaceEntry.waitFor()
  await personalSpaceEntry.click()
  await page.waitForURL(/\/space\/settings$/)
  await page.getByText('拓展风格包', { exact: true }).waitFor()
}

async function applySpacePack(page, name) {
  await openSpaceSettings(page)
  const item = page.locator(`[data-source-style-pack-name="${name}"]`)
  await item.waitFor()
  await item.click()
  await page.getByRole('button', { name: '应用源码目录', exact: true }).click()
  await page.getByText('源码目录风格包已应用', { exact: true }).waitFor()
}

async function currentPublicProfile(page) {
  const link = page.getByRole('link', { name: '个人主页', exact: true })
  const href = await link.getAttribute('href')
  if (!href?.startsWith('/u/')) throw new Error(`personal profile route is unavailable: ${href || 'empty'}`)
  return `${webURL}${href}`
}

try {
  for (const profile of profiles) {
    const context = await browser.newContext({
      viewport: { width: profile.width, height: profile.height },
      reducedMotion: 'reduce',
    })
    const page = await context.newPage()
    await login(page)
    const profileURL = await currentPublicProfile(page)

    for (const pack of systemPacks) {
      await withEvidence(page, { profile: profile.id, pack, page: 'system-theme', motion: 'reduced' }, async () => {
        await applySystemPack(page, pack)
        await page.goto(`${webURL}/threads`, { waitUntil: 'domcontentloaded' })
        await page.locator(`[data-web-theme="${pack}"]`).waitFor()
      })
    }

    for (const pack of spacePacks) {
      await withEvidence(page, { profile: profile.id, pack, page: 'space-owner', motion: 'reduced' }, async () => {
        await applySpacePack(page, pack)
        await page.goto(profileURL, { waitUntil: 'domcontentloaded' })
        await page.getByText('个人主页').first().waitFor()
      })

      const visitor = await browser.newContext({
        viewport: { width: profile.width, height: profile.height },
        reducedMotion: 'reduce',
      })
      const visitorPage = await visitor.newPage()
      await withEvidence(
        visitorPage,
        { profile: profile.id, pack, page: 'space-visitor', motion: 'reduced' },
        async () => {
          await visitorPage.goto(profileURL, { waitUntil: 'domcontentloaded' })
          await visitorPage.getByText('个人主页').first().waitFor()
        },
      )
      await visitor.close()
    }
    await context.close()
  }
  console.log(`style pack responsive matrix passed: ${outputDir}`)
} finally {
  await browser.close()
}
