import { createHmac } from 'node:crypto'
import { chromium } from 'playwright-core'

const chrome = process.env.CHROME_BIN
const webURL = (process.env.WEB_URL || 'http://localhost:3000').replace(/\/$/, '')
const adminURL = (process.env.ADMIN_URL || 'http://localhost:3001').replace(/\/$/, '')
const email = process.env.CAMPUSOS_MFA_TEST_EMAIL || ''
const password = process.env.CAMPUSOS_MFA_TEST_PASSWORD || ''

if (!chrome) throw new Error('CHROME_BIN is required')
if (!email || !password || process.env.CAMPUSOS_MFA_TEST_ALLOW_STATE_CHANGE !== 'yes') {
  throw new Error(
    'MFA browser drill requires CAMPUSOS_MFA_TEST_EMAIL, CAMPUSOS_MFA_TEST_PASSWORD, and CAMPUSOS_MFA_TEST_ALLOW_STATE_CHANGE=yes',
  )
}

function decodeBase32(raw) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  const normalized = raw.replace(/=+$/g, '').toUpperCase()
  let bits = 0
  let value = 0
  const bytes = []
  for (const character of normalized) {
    const index = alphabet.indexOf(character)
    if (index < 0) throw new Error('manual TOTP key is not valid base32')
    value = (value << 5) | index
    bits += 5
    while (bits >= 8) {
      bytes.push((value >>> (bits - 8)) & 0xff)
      bits -= 8
    }
  }
  return Buffer.from(bytes)
}

function totpCode(secret, step) {
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(step))
  const digest = createHmac('sha1', decodeBase32(secret)).update(counter).digest()
  const offset = digest[digest.length - 1] & 0x0f
  const value =
    ((digest[offset] & 0x7f) << 24) | (digest[offset + 1] << 16) | (digest[offset + 2] << 8) | digest[offset + 3]
  return String(value % 1_000_000).padStart(6, '0')
}

async function nextTOTP(secret, state) {
  let step = Math.floor(Date.now() / 1000 / 30)
  if (step <= state.step) {
    await new Promise((resolve) => setTimeout(resolve, (state.step + 1) * 30_000 - Date.now() + 250))
    step = Math.floor(Date.now() / 1000 / 30)
  }
  state.step = step
  return totpCode(secret, step)
}

async function dialog(page, title) {
  const value = page.locator('.el-dialog:visible').filter({ hasText: title }).first()
  await value.waitFor()
  return value
}

async function openMFASettings(page) {
  await page.goto(`${webURL}/account/security`, { waitUntil: 'domcontentloaded' })
  await page.getByText('多因素认证', { exact: true }).click()
  await page.getByText('认证器', { exact: true }).waitFor()
}

async function beginEnrollment(page) {
  await page.getByRole('button', { name: '启用认证器', exact: true }).click()
  const enrollment = await dialog(page, '启用认证器')
  await enrollment.locator('input').first().fill(password)
  await enrollment.getByRole('button', { name: '继续', exact: true }).click()
  await enrollment.locator('input').nth(1).waitFor()
  return enrollment
}

async function finishWebMFALogin(page, secret, state) {
  await page.goto(`${webURL}/login`, { waitUntil: 'domcontentloaded' })
  await page.getByPlaceholder('请输入邮箱').fill(email)
  await page.getByPlaceholder('请输入密码').fill(password)
  await page.locator('form').getByRole('button', { name: '登录', exact: true }).click()
  await page.getByPlaceholder('000000').waitFor()
  await page.getByPlaceholder('000000').fill(await nextTOTP(secret, state))
  await Promise.all([
    page.waitForURL((url) => url.pathname === '/', { timeout: 20_000 }),
    page.locator('form').getByRole('button', { name: '完成登录', exact: true }).click(),
  ])
}

async function finishAdminMFALogin(page, secret, state) {
  await page.goto(`${adminURL}/login`, { waitUntil: 'domcontentloaded' })
  await page.getByPlaceholder('请输入管理员邮箱').fill(email)
  await page.getByPlaceholder('请输入密码').fill(password)
  await page.locator('form').getByRole('button', { name: '登录', exact: true }).click()
  await page.getByPlaceholder('000000').waitFor()
  await page.getByPlaceholder('000000').fill(await nextTOTP(secret, state))
  await Promise.all([
    page.waitForURL((url) => url.pathname === '/', { timeout: 20_000 }),
    page.locator('form').getByRole('button', { name: '完成登录', exact: true }).click(),
  ])
  await page.getByText('用户总数', { exact: true }).waitFor()
}

async function rotateRecoveryCodes(page, currentCode) {
  await openMFASettings(page)
  await page.getByRole('button', { name: '更新恢复码', exact: true }).click()
  const rotate = await dialog(page, '更新恢复码')
  await rotate.getByText('现有恢复码', { exact: true }).click()
  await rotate.locator('input:not([type="radio"])').first().fill(currentCode)
  await rotate.getByRole('button', { name: '生成新恢复码', exact: true }).click()
  const recovery = await dialog(page, '保存恢复码')
  const codes = (await recovery.locator('.recovery-codes code').allTextContents())
    .map((value) => value.trim())
    .filter(Boolean)
  if (codes.length < 2) throw new Error('recovery code rotation did not display a replacement set')
  await recovery.getByText('我已安全保存这些恢复码', { exact: true }).click()
  await recovery.getByRole('button', { name: '完成', exact: true }).click()
  return codes
}

async function disableMFA(page, recoveryCode) {
  await openMFASettings(page)
  await page.getByRole('button', { name: '关闭认证器', exact: true }).click()
  const disable = await dialog(page, '关闭认证器')
  await disable.getByText('恢复码', { exact: true }).click()
  const fields = disable.locator('input:not([type="radio"])')
  await fields.nth(0).fill(password)
  await fields.nth(1).fill(recoveryCode)
  await Promise.all([
    page.waitForURL((url) => url.pathname === '/login', { timeout: 20_000 }),
    disable.getByRole('button', { name: '关闭并退出全部设备', exact: true }).click(),
  ])
}

async function verifyAdminPasswordOnlyLogin(page) {
  await page.goto(`${adminURL}/login`, { waitUntil: 'domcontentloaded' })
  await page.getByPlaceholder('请输入管理员邮箱').fill(email)
  await page.getByPlaceholder('请输入密码').fill(password)
  await Promise.all([
    page.waitForURL((url) => url.pathname === '/', { timeout: 20_000 }),
    page.locator('form').getByRole('button', { name: '登录', exact: true }).click(),
  ])
}

const browser = await chromium.launch({ executablePath: chrome, headless: true, args: ['--no-sandbox'] })
let recoveryCode = ''
let cleanupPage = null
let mfaEnabled = false

try {
  const ownerContext = await browser.newContext()
  const ownerPage = await ownerContext.newPage()
  cleanupPage = ownerPage
  await ownerPage.goto(`${webURL}/login`, { waitUntil: 'domcontentloaded' })
  await ownerPage.getByPlaceholder('请输入邮箱').fill(email)
  await ownerPage.getByPlaceholder('请输入密码').fill(password)
  await Promise.all([
    ownerPage.waitForURL((url) => url.pathname === '/', { timeout: 20_000 }),
    ownerPage.locator('form').getByRole('button', { name: '登录', exact: true }).click(),
  ])
  await openMFASettings(ownerPage)
  if (!(await ownerPage.getByRole('button', { name: '启用认证器', exact: true }).isEnabled())) {
    throw new Error('MFA browser drill account must start with an available, disabled authenticator')
  }

  const enrollment = await beginEnrollment(ownerPage)
  const manualKey = await enrollment.locator('input').first().inputValue()
  if (!manualKey) throw new Error('MFA enrollment did not expose the one-time manual key')
  const state = { step: -1 }
  await enrollment
    .locator('input')
    .nth(1)
    .fill(await nextTOTP(manualKey, state))
  await enrollment.getByRole('button', { name: '确认启用', exact: true }).click()
  const initialRecovery = await dialog(ownerPage, '保存恢复码')
  const initialCodes = (await initialRecovery.locator('.recovery-codes code').allTextContents())
    .map((value) => value.trim())
    .filter(Boolean)
  if (initialCodes.length < 2) throw new Error('MFA enrollment did not display recovery codes once')
  recoveryCode = initialCodes[0]
  mfaEnabled = true
  await initialRecovery.getByText('我已安全保存这些恢复码', { exact: true }).click()
  await initialRecovery.getByRole('button', { name: '完成', exact: true }).click()

  const webMFAContext = await browser.newContext()
  const webMFAPage = await webMFAContext.newPage()
  cleanupPage = webMFAPage
  await finishWebMFALogin(webMFAPage, manualKey, state)
  await webMFAPage.goto(`${webURL}/account/security`, { waitUntil: 'domcontentloaded' })
  await webMFAPage.getByText('已验证 MFA', { exact: true }).first().waitFor()

  const adminMFAContext = await browser.newContext()
  const adminMFAPage = await adminMFAContext.newPage()
  await finishAdminMFALogin(adminMFAPage, manualKey, state)
  await adminMFAContext.close()

  const replacementCodes = await rotateRecoveryCodes(webMFAPage, recoveryCode)
  recoveryCode = replacementCodes[0]
  await disableMFA(webMFAPage, recoveryCode)
  mfaEnabled = false
  recoveryCode = ''
  await webMFAContext.close()
  await ownerContext.close()

  const finalAdminContext = await browser.newContext()
  const finalAdminPage = await finalAdminContext.newPage()
  await verifyAdminPasswordOnlyLogin(finalAdminPage)
  await finalAdminContext.close()
  console.log(
    'MFA browser workflow passed: enrollment, web/admin second step, recovery-code rotation, session revocation, and password-only recovery after disable',
  )
} finally {
  if (mfaEnabled && cleanupPage && recoveryCode) {
    await disableMFA(cleanupPage, recoveryCode).catch((error) => {
      throw new Error(`MFA browser drill cleanup failed; authenticator may still be enabled: ${error.message}`)
    })
  }
  await browser.close()
}
