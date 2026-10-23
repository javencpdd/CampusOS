import { expect, it } from 'vitest'
import { memoryPreviewHandoffs } from '../src/modules/identity/previewSessionHandoff'
it('is one-use, bounded, expires and cannot transfer a previous account token', () => {
  let token: string | null = 'current-host-token',
    now = 1000,
    sequence = 0
  const broker = memoryPreviewHandoffs(
    () => token,
    () => now,
    () => String(++sequence).padStart(48, '0'),
  )
  const first = broker.issue()
  expect(broker.claim(first)).toBe(token)
  expect(broker.claim(first)).toBeUndefined()
  const old = broker.issue()
  token = 'other-account'
  expect(broker.claim(old)).toBeUndefined()
  const expired = broker.issue()
  now += 10000
  expect(broker.claim(expired)).toBeUndefined()
  for (let i = 0; i < 32; i++) broker.issue()
  expect(() => broker.issue()).toThrow()
  token = null
  expect(() => broker.issue()).toThrow()
})
