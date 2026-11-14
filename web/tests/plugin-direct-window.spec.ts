import { expect, it } from 'vitest'
import { windowsDirectWindowFile } from '../src/campus-ui/pluginDirectWindow'
const parent = 'http://10.63.23.135:3000'
const lease = 'a'.repeat(32)
const origin = `http://${lease}.10.63.23.135.sslip.io:3003`
const route = '/extensions/campusos.rss-publisher/ui'

it('opens the same site and preserves plugin origins while mapping only its embedded-IP domain', () => {
  const file = windowsDirectWindowFile(parent, origin, route)!
  expect(file.filename).toBe('campusos-direct-window.cmd')
  expect(file.content).toContain('"--host-resolver-rules=MAP *.10.63.23.135.sslip.io 10.63.23.135"')
  expect(file.content).toContain('--no-proxy-server')
  expect(file.content).toContain('"http://10.63.23.135:3000/extensions/campusos.rss-publisher/ui"')
  expect(file.content).toContain('--user-data-dir=%LOCALAPPDATA%\\CampusOS\\DirectBrowser\\http-10_63_23_135-3000')
  expect(file.content).not.toContain(lease)
  expect(file.content).not.toMatch(
    /disable-web-security|ignore-certificate-errors|no-sandbox|netsh|Set-Dns|System32\\drivers\\etc/i,
  )
  expect(file.content.replaceAll('\r\n', '')).not.toContain('\n')
})
it('supports the current IP-matching nip.io alias and HTTPS without weakening certificate checks', () => {
  const file = windowsDirectWindowFile('https://192.168.1.20', `https://${lease}.192-168-1-20.nip.io`, route)!
  expect(file.content).toContain('MAP *.192-168-1-20.nip.io 192.168.1.20')
  expect(file.content).toContain('https://192.168.1.20/extensions/campusos.rss-publisher/ui')
  expect(file.content).toContain('https-192_168_1_20-443')
})
it('never carries resource handles, query strings or shell input into the command file', () => {
  for (const path of ['/extensions/plugin/ui?token=secret', '/bad&calc.exe', '/%PATH%', '/bad!x!', '/\r\ncalc.exe']) {
    const file = windowsDirectWindowFile(parent, origin, path)!
    expect(file.content).toContain('--new-window "http://10.63.23.135:3000/"')
    expect(file.content).not.toContain('secret')
    expect(file.content).not.toContain('calc.exe')
  }
})
it('rejects public hosts, unrelated domains, mismatched IPs and non-origin inputs', () => {
  for (const [host, frame] of [
    ['http://8.8.8.8:3000', `http://${lease}.8.8.8.8.sslip.io:3003`],
    ['https://campus.example', origin],
    [parent, `http://${lease}.10.63.23.136.sslip.io:3003`],
    [parent, 'https://unrelated.example'],
    [parent, `http://${lease}.10.63.23.135.sslip.io.evil.example:3003`],
    [parent + '/path', origin],
    ['http://user:password@10.63.23.135:3000', origin],
    [parent, origin + '/path'],
    [parent, 'javascript:alert(1)'],
  ])
    expect(windowsDirectWindowFile(host, frame, route)).toBeNull()
})
