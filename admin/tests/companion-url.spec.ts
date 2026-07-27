import { describe, expect, it } from 'vitest'
import { resolveCompanionUrl } from '../src/shared/runtime/companionUrl'

describe('resolveCompanionUrl', () => {
  it('uses an explicitly configured public URL', () => {
    expect(resolveCompanionUrl('https://docs.example.edu/', 3002, undefined)).toBe('https://docs.example.edu')
  })

  it('keeps the browser host when deriving a Docker companion service', () => {
    expect(
      resolveCompanionUrl('', 3000, {
        origin: 'http://192.0.2.50:3001',
        protocol: 'http:',
      }),
    ).toBe('http://192.0.2.50:3000')
  })
})

