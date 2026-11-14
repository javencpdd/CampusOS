import { expect, it, vi } from 'vitest'
import { randomRequestID } from '../../sdk/typescript/src/random-request-id'
it('uses secure native UUIDs when available', () => {
  const values = vi.fn()
  expect(randomRequestID({ randomUUID: () => '12345678-1234-4234-8234-123456789012', getRandomValues: values })).toBe(
    '12345678-1234-4234-8234-123456789012',
  )
  expect(values).not.toHaveBeenCalled()
})
it('generates version 4 request IDs through secure random values on LAN HTTP', () => {
  let count = 0
  const source = {
    getRandomValues<T extends ArrayBufferView | null>(value: T): T {
      ;(value as Uint8Array).fill(++count)
      return value
    },
  }
  const first = randomRequestID(source),
    second = randomRequestID(source)
  expect(first).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
  expect(second).not.toBe(first)
  expect(() => randomRequestID({} as Crypto)).toThrow('安全随机数不可用')
})
