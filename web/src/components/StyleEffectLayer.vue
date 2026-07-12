<template>
  <div v-if="enabled" ref="layer" class="style-effect-layer" aria-hidden="true">
    <iframe ref="frame" class="style-effect-frame" sandbox="allow-scripts" tabindex="-1" title="" :srcdoc="srcdoc" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const props = defineProps<{
  script?: string
  capabilities?: string[]
  resolveQuery?: (method: string, params: Record<string, unknown>) => Promise<unknown>
}>()

const layer = ref<HTMLElement | null>(null)
const frame = ref<HTMLIFrameElement | null>(null)
const reducedMotion = ref(false)
let motionQuery: MediaQueryList | null = null
const queryChannel = `campusos-style-${Math.random().toString(36).slice(2)}`

const encodeScript = (value: string) => {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary)
}

const enabled = computed(() => Boolean(props.script?.trim()) && !reducedMotion.value)
const srcdoc = computed(() => {
  if (!enabled.value) return ''
  const encoded = encodeScript(props.script || '')
  return `<!doctype html>
<html><head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline' blob:; worker-src blob:; connect-src 'none'; img-src 'none'; style-src 'unsafe-inline'">
<style>html,body,canvas{width:100%;height:100%;margin:0;overflow:hidden;background:transparent}canvas{display:block}</style>
</head><body><canvas id="effect"></canvas><script>
(() => {
  const encoded = '${encoded}'
  const bytes = Uint8Array.from(atob(encoded), value => value.charCodeAt(0))
  const effectSource = new TextDecoder().decode(bytes)
  const canvas = document.getElementById('effect')
  if (!canvas || !canvas.transferControlToOffscreen || typeof Worker === 'undefined') return

  const workerSource = \`
let hooks = {}
let canvas
let ctx
let width = 1
let height = 1
let dpr = 1
let pointer = { x: 0.5, y: 0.5, active: false }
let lastTime = 0
let timer = 0
let queryID = 0
const queries = new Map()
const CampusEffect = Object.freeze({
  register(next) {
    if (next && typeof next === 'object') hooks = next
  },
  request(method, params = {}) {
    return new Promise((resolve, reject) => {
      const id = ++queryID
      queries.set(id, { resolve, reject })
      self.postMessage({ type: 'query', id, method, params })
    })
  }
})
\` + effectSource + \`
function frame() {
  const time = performance.now()
  const delta = lastTime ? Math.min(64, time - lastTime) : 16
  lastTime = time
  const api = {
    canvas, ctx, width, height, dpr, time, delta, pointer,
    clear() { ctx.clearRect(0, 0, width * dpr, height * dpr) }
  }
  try { if (typeof hooks.frame === 'function') hooks.frame(api) } catch (_) {}
  self.postMessage({ type: 'heartbeat' })
  timer = setTimeout(frame, 32)
}
self.onmessage = event => {
  const data = event.data || {}
  if (data.type === 'query-result') {
    const pending = queries.get(data.id)
    if (!pending) return
    queries.delete(data.id)
    if (data.ok) pending.resolve(data.data)
    else pending.reject(new Error(data.error || 'CampusStyleSDK request failed'))
  } else if (data.type === 'init') {
    canvas = data.canvas
    ctx = canvas.getContext('2d')
    width = data.width
    height = data.height
    dpr = data.dpr
    canvas.width = Math.max(1, Math.floor(width * dpr))
    canvas.height = Math.max(1, Math.floor(height * dpr))
    try { if (typeof hooks.start === 'function') hooks.start({ canvas, ctx, width, height, dpr }) } catch (_) {}
    frame()
  } else if (data.type === 'resize' && canvas) {
    width = data.width
    height = data.height
    dpr = data.dpr
    canvas.width = Math.max(1, Math.floor(width * dpr))
    canvas.height = Math.max(1, Math.floor(height * dpr))
    try { if (typeof hooks.resize === 'function') hooks.resize({ canvas, ctx, width, height, dpr }) } catch (_) {}
  } else if (data.type === 'pointer') {
    pointer = { x: data.x, y: data.y, active: data.active }
    try { if (typeof hooks.pointer === 'function') hooks.pointer(pointer) } catch (_) {}
  } else if (data.type === 'destroy') {
    clearTimeout(timer)
    try { if (typeof hooks.destroy === 'function') hooks.destroy() } catch (_) {}
    close()
  }
}
\`
  const workerURL = URL.createObjectURL(new Blob([workerSource], { type: 'text/javascript' }))
  const worker = new Worker(workerURL)
  URL.revokeObjectURL(workerURL)
  let lastHeartbeat = performance.now()
  worker.onmessage = event => {
    const data = event.data || {}
    if (data.type === 'heartbeat') lastHeartbeat = performance.now()
    if (data.type === 'query') parent.postMessage({ channel: '${queryChannel}', ...data }, '*')
  }
  const offscreen = canvas.transferControlToOffscreen()
  const sendSize = type => {
    const rect = canvas.getBoundingClientRect()
    worker.postMessage({ type, width: rect.width, height: rect.height, dpr: Math.min(2, devicePixelRatio || 1), canvas: type === 'init' ? offscreen : undefined }, type === 'init' ? [offscreen] : [])
  }
  sendSize('init')
  new ResizeObserver(() => sendSize('resize')).observe(canvas)
  addEventListener('message', event => {
    const data = event.data || {}
    if (data.type === 'pointer') worker.postMessage(data)
    if (data.type === 'destroy') worker.postMessage(data)
    if (data.channel === '${queryChannel}' && data.type === 'query-result') worker.postMessage(data)
  })
  setInterval(() => {
    if (performance.now() - lastHeartbeat > 3000) worker.terminate()
  }, 1000)
})()
<\/script></body></html>`
})

const sendPointer = (event: PointerEvent) => {
  const rect = layer.value?.getBoundingClientRect()
  if (!rect || rect.width <= 0 || rect.height <= 0) return
  frame.value?.contentWindow?.postMessage(
    {
      type: 'pointer',
      x: Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)),
      y: Math.max(0, Math.min(1, (event.clientY - rect.top) / rect.height)),
      active:
        event.clientX >= rect.left &&
        event.clientX <= rect.right &&
        event.clientY >= rect.top &&
        event.clientY <= rect.bottom,
    },
    '*',
  )
}

const updateMotion = () => {
  reducedMotion.value = Boolean(motionQuery?.matches)
}

const handleQuery = async (event: MessageEvent) => {
  if (event.source !== frame.value?.contentWindow) return
  const data = event.data || {}
  if (data.channel !== queryChannel || data.type !== 'query') return
  const method = String(data.method || '')
  const allowed = props.capabilities?.includes(method) ?? false
  let response: Record<string, unknown>
  if (!allowed || !props.resolveQuery) {
    response = { channel: queryChannel, type: 'query-result', id: data.id, ok: false, error: 'capability denied' }
  } else {
    try {
      const payload = await props.resolveQuery(method, data.params || {})
      response = { channel: queryChannel, type: 'query-result', id: data.id, ok: true, data: payload }
    } catch (error: any) {
      response = {
        channel: queryChannel,
        type: 'query-result',
        id: data.id,
        ok: false,
        error: error?.message || 'request failed',
      }
    }
  }
  frame.value?.contentWindow?.postMessage(response, '*')
}

onMounted(() => {
  motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
  updateMotion()
  motionQuery.addEventListener('change', updateMotion)
  window.addEventListener('pointermove', sendPointer, { passive: true })
  window.addEventListener('message', handleQuery)
})

onBeforeUnmount(() => {
  frame.value?.contentWindow?.postMessage({ type: 'destroy' }, '*')
  motionQuery?.removeEventListener('change', updateMotion)
  window.removeEventListener('pointermove', sendPointer)
  window.removeEventListener('message', handleQuery)
})
</script>

<style scoped>
.style-effect-layer,
.style-effect-frame {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  border: 0;
  pointer-events: none;
}

.style-effect-layer {
  z-index: 0;
  overflow: hidden;
}
</style>
