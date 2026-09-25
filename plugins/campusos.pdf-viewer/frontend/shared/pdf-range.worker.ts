// A deliberately trivial Worker probe. The real PDF.js worker stays within
// this package at build time; this probe verifies that an isolated plugin
// origin can create a module Worker without reading host state.
self.addEventListener('message', (event: MessageEvent<{ type: 'ping' }>) => {
  if (event.data?.type === 'ping') self.postMessage({ type: 'ready' })
})
