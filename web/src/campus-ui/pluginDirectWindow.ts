export interface DirectWindowFile {
  filename: string
  content: string
}

// Only repair embedded-IP development domains for the current private IPv4
// host. Keep the real plugin URL/origin, TLS checks and browser sandbox intact.
export function windowsDirectWindowFile(
  parentOrigin: string,
  pluginOrigin: string,
  pathname: string,
): DirectWindowFile | null {
  try {
    const parent = new URL(parentOrigin)
    const plugin = new URL(pluginOrigin)
    if (
      !['http:', 'https:'].includes(parent.protocol) ||
      !['http:', 'https:'].includes(plugin.protocol) ||
      parent.origin !== parentOrigin ||
      plugin.origin !== pluginOrigin
    )
      return null
    const ip = parent.hostname
    const octets = ip.split('.').map(Number)
    if (
      !/^\d+\.\d+\.\d+\.\d+$/.test(ip) ||
      octets.some((value) => !Number.isInteger(value) || value < 0 || value > 255) ||
      !(
        octets[0] === 10 ||
        octets[0] === 127 ||
        (octets[0] === 172 && octets[1] >= 16 && octets[1] <= 31) ||
        (octets[0] === 192 && octets[1] === 168)
      )
    )
      return null
    const labels = plugin.hostname.match(/^[a-f0-9]{32}\.(.+)$/)
    if (!labels) return null
    const domain = labels[1]
    const allowedDomains = [ip, ip.replace(/\./g, '-')].flatMap((value) => [value + '.sslip.io', value + '.nip.io'])
    if (!allowedDomains.includes(domain)) return null
    const route = /^\/extensions\/[a-z][a-z0-9.-]+\/ui$/.test(pathname) ? pathname : '/'
    const site = parent.origin + route
    const port = parent.port || (parent.protocol === 'https:' ? '443' : '80')
    const profile = `${parent.protocol.slice(0, -1)}-${ip.replace(/\./g, '_')}-${port}`
    const candidates = [
      '%ProgramFiles(x86)%\\Microsoft\\Edge\\Application\\msedge.exe',
      '%ProgramFiles%\\Microsoft\\Edge\\Application\\msedge.exe',
      '%LOCALAPPDATA%\\Microsoft\\Edge\\Application\\msedge.exe',
      '%ProgramFiles%\\Google\\Chrome\\Application\\chrome.exe',
      '%ProgramFiles(x86)%\\Google\\Chrome\\Application\\chrome.exe',
      '%LOCALAPPDATA%\\Google\\Chrome\\Application\\chrome.exe',
    ]
    const lines = [
      '@echo off',
      'setlocal DisableDelayedExpansion',
      'rem CampusOS direct LAN browser. No system DNS, hosts or proxy settings are changed.',
      'rem Only this dedicated browser profile connects directly and maps the plugin domain.',
      'set "CAMPUSOS_BROWSER="',
      ...candidates.map((path) => `if not defined CAMPUSOS_BROWSER if exist "${path}" set "CAMPUSOS_BROWSER=${path}"`),
      'if not defined CAMPUSOS_BROWSER goto missing_browser',
      `echo Opening CampusOS: ${site}`,
      'echo Sign in again in this dedicated browser window.',
      `start "" "%CAMPUSOS_BROWSER%" "--user-data-dir=%LOCALAPPDATA%\\CampusOS\\DirectBrowser\\${profile}" "--host-resolver-rules=MAP *.${domain} ${ip}" --no-proxy-server --no-first-run --no-default-browser-check --new-window "${site}"`,
      'exit /b',
      ':missing_browser',
      'echo Microsoft Edge or Google Chrome was not found in the standard installation folders.',
      'pause',
      'exit /b 1',
    ]
    return { filename: 'campusos-direct-window.cmd', content: lines.join('\r\n') + '\r\n' }
  } catch {
    return null
  }
}
