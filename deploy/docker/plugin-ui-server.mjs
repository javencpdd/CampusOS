import { createServer, request as upstreamRequest } from 'node:http'

const port = Number(process.env.CAMPUSOS_PLUGIN_UI_PORT || '3003')
const host = process.env.CAMPUSOS_PLUGIN_UI_HOST || '0.0.0.0'
const upstream = new URL(process.env.CAMPUSOS_PLUGIN_UI_UPSTREAM || 'http://api:3004')
if (upstream.protocol !== 'http:' || upstream.username || upstream.password || upstream.pathname !== '/' || upstream.search || upstream.hash) throw new Error('Invalid dedicated UI upstream')
function fail(response, status) {
  response.writeHead(status, { 'Content-Type':'text/plain; charset=utf-8', 'Cache-Control':'no-store', 'Content-Security-Policy':"default-src 'none'; frame-ancestors 'none'", 'X-Content-Type-Options':'nosniff' })
  response.end(status === 200 ? 'ok' : 'unavailable')
}
// Finite asset gateway: no API proxy, filesystem reads, redirects, credentials
// or source-directory publication. Authorization is rechecked by the UI host.
createServer((request,response) => {
  if(request.url === '/health' && request.method === 'GET') return fail(response,200)
  if(!['GET','HEAD'].includes(request.method) || !/^\/ui\/[0-9a-f]{32}\//.test(request.url || '') || request.headers['sec-fetch-dest'] === 'serviceworker' || request.headers['sec-fetch-dest'] === 'document') return fail(response,404)
  const headers = { host:request.headers.host, 'sec-fetch-dest':request.headers['sec-fetch-dest'] || '' }
  const proxy = upstreamRequest(upstream,{ method:request.method,path:request.url,headers,timeout:10000 }, incoming => {
    if(incoming.statusCode !== 200){ incoming.resume();return fail(response,404) }
    const safeHeaders = Object.fromEntries(['content-type','content-security-policy','cache-control','x-content-type-options','referrer-policy','permissions-policy','origin-agent-cluster'].filter(key=>incoming.headers[key]).map(key=>[key,incoming.headers[key]]))
    response.writeHead(200,safeHeaders);incoming.pipe(response)
  })
  proxy.on('timeout',()=>proxy.destroy());proxy.on('error',()=>{if(!response.headersSent)fail(response,502);else response.destroy()})
  response.on('close',()=>proxy.destroy());proxy.end()
}).listen(port,host,()=>console.log(`CampusOS v5 UI gateway listening on ${port}`))
