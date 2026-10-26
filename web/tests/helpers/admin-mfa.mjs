import { createHmac } from 'node:crypto'

function totp(secret, step) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = 0
  let acc = 0
  const bytes = []
  for (const c of secret.replace(/=+$/, '')) {
    const value = alphabet.indexOf(c)
    if (value < 0) throw new Error('Invalid authenticator secret')
    acc = (acc << 5) | value
    bits += 5
    if (bits >= 8) {
      bits -= 8
      bytes.push((acc >> bits) & 255)
    }
  }
  const message = Buffer.alloc(8)
  message.writeBigUInt64BE(BigInt(step))
  const digest = createHmac('sha1', Buffer.from(bytes)).update(message).digest()
  return String((digest.readUInt32BE(digest.at(-1) & 15) & 0x7fffffff) % 1000000).padStart(6, '0')
}

// The owned release target starts with no enrolled Admin authenticator. Use
// the actual UI flow and a later TOTP step, never a synthetic MFA session.
export async function enrollOwnedAdminMFA(page) {
  await page.getByRole('button', { name: '管理员认证', exact: true }).click()
  await page.getByRole('button', { name: '开始配置管理员认证器', exact: true }).click()
  const input = page.getByRole('textbox', { name: '管理员认证器密钥', exact: true })
  await input.waitFor()
  let secret = await input.inputValue()
  const step = Math.floor(Date.now() / 30000)
  await page.locator('input[autocomplete="one-time-code"]').fill(totp(secret, step))
  await page.getByRole('button', { name: '完成配置', exact: true }).click()
  await page.getByText('配置完成。请等待认证器显示下一组验证码，再确认管理员身份。', { exact: true }).waitFor()
  await page.waitForTimeout(Math.max(0, (step + 1) * 30000 - Date.now() + 300))
  await page.locator('input[autocomplete="one-time-code"]').fill(totp(secret, Math.floor(Date.now() / 30000)))
  secret = ''
  await page.getByRole('button', { name: '确认身份', exact: true }).click()
  await page.getByRole('dialog', { name: '确认管理员身份', exact: true }).waitFor({ state: 'hidden' })
}
