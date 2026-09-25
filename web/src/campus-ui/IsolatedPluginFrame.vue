<template>
  <section class="isolated-plugin-frame" :aria-busy="state === 'connecting'">
    <iframe
      v-if="state !== 'blocked'"
      ref="frame"
      :src="src"
      :title="title"
      sandbox="allow-scripts allow-same-origin"
      referrerpolicy="no-referrer"
      @load="loaded = true"
    />
    <el-alert v-if="message" :title="message" :type="state === 'ready' ? 'success' : 'warning'" :closable="false" />
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  PLUGIN_BRIDGE_VERSION,
  createPluginBridgeSession,
  isMatchingHandshake,
  isMatchingReady,
  pluginBridgeError,
  pluginBridgeResult,
  pluginBridgeTransferables,
  validatePluginBridgeRequest,
  type PluginBridgeRequest,
  type PluginBridgeSession,
  type PluginFrameIdentity,
  validFrameIdentity,
} from './isolatedPluginBridge'

const props = defineProps<{
  identity: PluginFrameIdentity
  src: string
  title: string
  request: (request: PluginBridgeRequest) => Promise<unknown>
}>()

const frame = ref<HTMLIFrameElement>()
const loaded = ref(false)
const state = ref<'connecting' | 'ready' | 'blocked'>('connecting')
const message = computed(() => {
  if (state.value === 'blocked') return '插件界面未通过隔离连接校验，已阻止加载。'
  if (state.value === 'ready') return ''
  return loaded.value ? '正在建立受限插件连接…' : '正在加载隔离插件界面…'
})

let session: PluginBridgeSession | undefined
let timeout: ReturnType<typeof setTimeout> | undefined

function closeSession() {
  if (timeout) clearTimeout(timeout)
  timeout = undefined
  session?.close()
  session = undefined
}

function reject() {
  closeSession()
  state.value = 'blocked'
}

function newChallenge(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  if (typeof crypto === 'undefined' || typeof crypto.getRandomValues !== 'function')
    return `${Date.now()}-${Math.random()}`
  const values = new Uint32Array(4)
  crypto.getRandomValues(values)
  return Array.from(values, (value) => value.toString(16)).join('-')
}

function sourceMatchesIdentity(): boolean {
  try {
    return new URL(props.src).origin === props.identity.origin
  } catch {
    return false
  }
}

function onWindowMessage(event: MessageEvent<unknown>) {
  const iframeWindow = frame.value?.contentWindow
  if (
    !iframeWindow ||
    event.source !== iframeWindow ||
    event.origin !== props.identity.origin ||
    !validFrameIdentity(props.identity, window.location.origin) ||
    !sourceMatchesIdentity() ||
    !isMatchingReady(event.data, props.identity)
  ) {
    return
  }
  closeSession()
  const channel = new MessageChannel()
  const challenge = newChallenge()
  session = createPluginBridgeSession(props.identity, challenge, channel)
  session.port.onmessage = (portEvent) => handlePortMessage(portEvent)
  iframeWindow.postMessage(
    {
      type: 'campusos.bridge.connect',
      version: PLUGIN_BRIDGE_VERSION,
      challenge,
      plugin_key: props.identity.pluginKey,
      plugin_version: props.identity.pluginVersion,
      surface_id: props.identity.surfaceID,
      audience: props.identity.audience,
    },
    props.identity.origin,
    [channel.port2],
  )
  timeout = setTimeout(() => {
    if (!session?.active) reject()
  }, 10_000)
}

async function handlePortMessage(event: MessageEvent<unknown>) {
  if (!session) return
  if (!session.active) {
    if (!isMatchingHandshake(event.data, session.challenge)) {
      reject()
      return
    }
    session.active = true
    if (timeout) clearTimeout(timeout)
    state.value = 'ready'
    return
  }
  const request = validatePluginBridgeRequest(event.data)
  if (!request) {
    session.port.postMessage(pluginBridgeError('invalid', 'bridge.invalid_request', '插件请求格式无效，已被宿主拒绝。'))
    return
  }
  try {
    const result = await props.request(request)
    session.port.postMessage(pluginBridgeResult(request.request_id, result), pluginBridgeTransferables(result))
  } catch {
    session.port.postMessage(pluginBridgeError(request.request_id, 'bridge.denied', '当前操作未获授权或资源已不可用。'))
  }
}

onMounted(() => {
  if (!validFrameIdentity(props.identity, window.location.origin) || !sourceMatchesIdentity()) reject()
  else window.addEventListener('message', onWindowMessage)
})
onBeforeUnmount(() => {
  window.removeEventListener('message', onWindowMessage)
  closeSession()
})
</script>

<style scoped>
.isolated-plugin-frame {
  display: grid;
  gap: 10px;
  min-height: 320px;
}

iframe {
  width: 100%;
  min-height: 620px;
  border: 0;
  border-radius: 8px;
  background: #fff;
}
</style>
