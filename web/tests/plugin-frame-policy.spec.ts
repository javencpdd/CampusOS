// @vitest-environment jsdom
import { afterEach, it, expect } from 'vitest'
import { installPluginFramePolicy } from '../../sdk/typescript/src/plugin-frame-policy'
afterEach(() => document.querySelector('meta[data-campusos-plugin-frames]')?.remove())
it('installs one host frame restriction and rejects policy injection or runtime widening', () => {
  for (const invalid of [
    '*',
    'https://*.evil.test; connect-src *',
    'https://host.test',
    'http://*.localhost:99999',
    'https://*.test/path',
  ])
    expect(() => installPluginFramePolicy(invalid)).toThrow()
  installPluginFramePolicy('http://*.localhost:3003')
  installPluginFramePolicy('http://*.localhost:3003')
  expect(document.querySelectorAll('meta[data-campusos-plugin-frames]')).toHaveLength(1)
  expect((document.querySelector('meta[data-campusos-plugin-frames]') as HTMLMetaElement).content).toBe(
    'frame-src http://*.localhost:3003',
  )
  expect(() => installPluginFramePolicy('https://*.other.test')).toThrow('刷新')
})
