import { createReadStream, existsSync, statSync } from 'node:fs'
import { createServer } from 'node:http'
import { extname, join, normalize, resolve, sep } from 'node:path'

const root = resolve(process.env.CAMPUSOS_PLUGIN_V4_DIR || '/workspace/plugins', '.installed')
const port = Number(process.env.CAMPUSOS_PLUGIN_UI_PORT || '3003')
const frameAncestors = process.env.CAMPUSOS_PLUGIN_UI_FRAME_ANCESTORS || '*'
const mimeTypes = new Map([
  ['.html', 'text/html; charset=utf-8'],
  ['.js', 'text/javascript; charset=utf-8'],
  ['.mjs', 'text/javascript; charset=utf-8'],
  ['.css', 'text/css; charset=utf-8'],
  ['.json', 'application/json; charset=utf-8'],
  ['.svg', 'image/svg+xml'],
  ['.png', 'image/png'],
  ['.jpg', 'image/jpeg'],
  ['.jpeg', 'image/jpeg'],
  ['.webp', 'image/webp'],
  ['.woff2', 'font/woff2'],
])

function csp() {
  return [
    "default-src 'none'",
    "script-src 'self'",
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: blob:",
    "font-src 'self'",
    "connect-src 'none'",
    "worker-src 'self' blob:",
    "object-src 'none'",
    "base-uri 'none'",
    "form-action 'none'",
    `frame-ancestors ${frameAncestors}`,
  ].join('; ')
}

function send(response, status, message) {
  response.writeHead(status, {
    'Content-Type': 'text/plain; charset=utf-8',
    'Cache-Control': 'no-store',
    'Content-Security-Policy': csp(),
    'X-Content-Type-Options': 'nosniff',
  })
  response.end(message)
}

createServer((request, response) => {
  if (request.url === '/health') return send(response, 200, 'ok')
  if (request.headers['sec-fetch-dest'] === 'serviceworker') return send(response, 403, 'service worker is not allowed')
  const pathname = new URL(request.url || '/', 'http://plugin-ui.invalid').pathname
  if (!pathname.startsWith('/plugins/')) return send(response, 404, 'not found')
  const relative = normalize(pathname.slice('/plugins/'.length)).replace(/^([/\\])+/, '')
  const target = resolve(root, relative)
  if (!target.startsWith(`${root}${sep}`) || !existsSync(target)) return send(response, 404, 'not found')
  let stat
  try {
    stat = statSync(target)
  } catch {
    return send(response, 404, 'not found')
  }
  if (!stat.isFile() || !mimeTypes.has(extname(target).toLowerCase())) return send(response, 404, 'not found')
  response.writeHead(200, {
    'Content-Type': mimeTypes.get(extname(target).toLowerCase()),
    'Content-Length': stat.size,
    'Cache-Control': extname(target).toLowerCase() === '.html' ? 'no-store' : 'public, max-age=31536000, immutable',
    'Content-Security-Policy': csp(),
    'Permissions-Policy': 'camera=(), microphone=(), geolocation=(), payment=(), usb=()',
    'Referrer-Policy': 'no-referrer',
    'X-Content-Type-Options': 'nosniff',
  })
  createReadStream(target).pipe(response)
}).listen(port, '0.0.0.0', () => console.log(`CampusOS plugin UI gateway listening on ${port}`))
