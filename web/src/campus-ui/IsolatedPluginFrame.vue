<template>
  <section class="isolated-plugin-frame" :aria-busy="state === 'connecting'">
    <iframe
      v-if="state !== 'blocked'"
      ref="frame"
      :key="frameKey"
      :src="src"
      :title="title"
      sandbox="allow-scripts allow-same-origin"
      referrerpolicy="no-referrer"
      @load="onLoad"
    />
    <el-alert v-if="message" :title="message" :type="state === 'ready' ? 'success' : 'warning'" :closable="false" />
  </section>
</template>

<script setup lang="ts">
import { getAccessToken } from '@/modules/identity/session'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  PLUGIN_BRIDGE_VERSION,
  dispatchPluginBridgeRequest,
  createPluginBridgeSession,
  isMatchingHandshake,
  isMatchingReady,
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
const state = ref<'connecting' | 'ready' | 'blocked'>(
  validFrameIdentity(props.identity, window.location.origin) && sourceMatchesIdentity() ? 'connecting' : 'blocked',
)
const frameKey = computed(() => JSON.stringify([props.identity, props.src]))
const message = computed(() => {
  if (state.value === 'blocked') return '插件界面未通过隔离连接校验，已阻止加载。'
  if (state.value === 'ready') return ''
  return loaded.value ? '正在建立受限插件连接…' : '正在加载隔离插件界面…'
})

let session: PluginBridgeSession | undefined
let identityToken = getAccessToken()
let identityTimer: ReturnType<typeof setInterval>
let requestIDs = new Set<string>()
let requests = { count: 0 }
let timeout: ReturnType<typeof setTimeout> | undefined

function closeSession() {
  if (timeout) clearTimeout(timeout)
  timeout = undefined
  session?.close()
  session = undefined
  requestIDs = new Set<string>()
  requests = { count: 0 }
}

function reject() {
  closeSession()
  state.value = 'blocked'
}

function newChallenge(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  if (typeof crypto === 'undefined' || typeof crypto.getRandomValues !== 'function')
    throw new Error('安全随机数不可用。')
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
  if (session || state.value === 'blocked') return
  const channel = new MessageChannel()
  let challenge: string
  try {
    challenge = newChallenge()
  } catch {
    channel.port1.close()
    channel.port2.close()
    reject()
    return
  }
  session = createPluginBridgeSession(props.identity, challenge, channel)
  const connected = session
  const fingerprint = frameKey.value
  const token = identityToken
  const seen = requestIDs,
    budget = requests
  const handler = props.request
  connected.port.onmessage = (portEvent) =>
    handlePortMessage(
      connected,
      portEvent,
      () =>
        session === connected &&
        frameKey.value === fingerprint &&
        getAccessToken() === token &&
        state.value !== 'blocked',
      handler,
      seen,
      budget,
    )
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

async function handlePortMessage(
  connected: PluginBridgeSession,
  event: MessageEvent<unknown>,
  current: () => boolean,
  handler: (request: PluginBridgeRequest) => Promise<unknown>,
  seen: Set<string>,
  budget: { count: number },
) {
  if (!current()) return
  if (!connected.active) {
    if (!isMatchingHandshake(event.data, connected.challenge)) {
      reject()
      return
    }
    connected.active = true
    if (timeout) clearTimeout(timeout)
    state.value = 'ready'
    return
  }
  await dispatchPluginBridgeRequest(connected, event.data, current, handler, seen, budget)
}
function onLoad() {
  if (loaded.value) reject()
  loaded.value = true
}
watch(
  frameKey,
  () => {
    closeSession()
    loaded.value = false
    state.value =
      validFrameIdentity(props.identity, window.location.origin) && sourceMatchesIdentity() ? 'connecting' : 'blocked'
  },
  { flush: 'sync' },
)

onMounted(() => {
  if (!validFrameIdentity(props.identity, window.location.origin) || !sourceMatchesIdentity()) reject()
  else window.addEventListener('message', onWindowMessage)
  identityTimer = setInterval(() => {
    if (getAccessToken() !== identityToken || (session && state.value === 'ready' && !session.active)) reject()
  }, 100)
})
onBeforeUnmount(() => {
  clearInterval(identityTimer)
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
