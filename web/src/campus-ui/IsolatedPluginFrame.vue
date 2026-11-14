<template>
  <section class="isolated-plugin-frame" :aria-busy="state === 'connecting'">
    <iframe
      v-if="state === 'connecting' || state === 'ready'"
      ref="frame"
      :key="frameKey"
      :src="src"
      :title="title"
      :sandbox="identity.opaque ? 'allow-scripts' : 'allow-scripts allow-same-origin'"
      referrerpolicy="no-referrer"
      @load="onLoad"
      @error="onError"
    />
    <el-alert v-if="message" :title="message" :type="state === 'ready' ? 'success' : 'warning'" :closable="false" />
    <div v-if="state === 'unavailable'" class="frame-recovery" role="status">
      <p>重新加载后可继续操作。若仍失败，请检查这台电脑的网络或代理能否访问插件界面地址：</p>
      <code>{{ identity.origin }}</code>
      <el-button type="primary" @click="retry">重新加载插件界面</el-button>
      <template v-if="directWindow">
        <p>Windows 若能访问本站IP、但打不开上方域名，可下载并双击直连启动器。在独立窗口登录后重新打开插件。</p>
        <el-button @click="downloadDirectWindow">下载 Windows 直连启动器</el-button>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { getAccessToken } from '@/modules/identity/session'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { windowsDirectWindowFile } from './pluginDirectWindow'
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
  request: (request: PluginBridgeRequest, active?: () => boolean) => Promise<unknown>
}>()

const frame = ref<HTMLIFrameElement>()
const loaded = ref(false)
const attempt = ref(0)
const failure = ref('')
const state = ref<'connecting' | 'ready' | 'blocked' | 'unavailable'>(
  validFrameIdentity(props.identity, window.location.origin) && sourceMatchesIdentity() ? 'connecting' : 'blocked',
)
const frameKey = computed(() => JSON.stringify([props.identity, props.src, attempt.value]))
const directWindow = computed(() =>
  props.identity.opaque
    ? undefined
    : windowsDirectWindowFile(window.location.origin, props.identity.origin, window.location.pathname),
)
const message = computed(() => {
  if (state.value === 'blocked') return '插件界面未通过隔离连接校验，已阻止加载。'
  if (state.value === 'unavailable') return failure.value
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

function unavailable(message: string) {
  closeSession()
  failure.value = message
  state.value = 'unavailable'
}

function startLoadDeadline() {
  if (state.value !== 'connecting') return
  const fingerprint = frameKey.value
  timeout = setTimeout(() => {
    if (frameKey.value === fingerprint && state.value === 'connecting')
      unavailable('插件界面加载超时，请重新加载后继续。')
  }, 10_000)
}

function retry() {
  if (getAccessToken() !== identityToken) return reject()
  attempt.value++
}

function downloadDirectWindow() {
  if (!directWindow.value) return
  const url = URL.createObjectURL(new Blob([directWindow.value.content], { type: 'application/octet-stream' }))
  const link = document.createElement('a')
  link.href = url
  link.download = directWindow.value.filename
  link.click()
  URL.revokeObjectURL(url)
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
    const url = new URL(props.src)
    return (
      url.origin === props.identity.origin &&
      (!props.identity.opaque || /^\/api\/v1\/plugin-v5\/ui-assets\/[a-f0-9]{32}\/dist\//.test(url.pathname))
    )
  } catch {
    return false
  }
}

function onWindowMessage(event: MessageEvent<unknown>) {
  const iframeWindow = frame.value?.contentWindow
  if (
    !iframeWindow ||
    event.source !== iframeWindow ||
    event.origin !== (props.identity.opaque ? 'null' : props.identity.origin) ||
    !validFrameIdentity(props.identity, window.location.origin) ||
    !sourceMatchesIdentity() ||
    !isMatchingReady(event.data, props.identity)
  ) {
    return
  }
  if (session || state.value !== 'connecting') return
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
    props.identity.opaque ? '*' : props.identity.origin,
    [channel.port2],
  )
}

async function handlePortMessage(
  connected: PluginBridgeSession,
  event: MessageEvent<unknown>,
  current: () => boolean,
  handler: (request: PluginBridgeRequest, active?: () => boolean) => Promise<unknown>,
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
  await dispatchPluginBridgeRequest(
    connected,
    event.data,
    current,
    (request) => handler(request, () => connected.active && current()),
    seen,
    budget,
  )
}
function onLoad(event: Event) {
  if (event.target !== frame.value) return
  if (loaded.value) reject()
  loaded.value = true
}
function onError(event: Event) {
  if (event.target !== frame.value) return
  unavailable('未能加载插件界面，请重新加载后继续。')
}
watch(
  frameKey,
  () => {
    closeSession()
    loaded.value = false
    failure.value = ''
    state.value =
      validFrameIdentity(props.identity, window.location.origin) && sourceMatchesIdentity() ? 'connecting' : 'blocked'
    if (getAccessToken() !== identityToken) reject()
    else startLoadDeadline()
  },
  { flush: 'sync' },
)

onMounted(() => {
  if (!validFrameIdentity(props.identity, window.location.origin) || !sourceMatchesIdentity()) reject()
  window.addEventListener('message', onWindowMessage)
  startLoadDeadline()
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

.frame-recovery {
  display: grid;
  gap: 12px;
  justify-items: start;
  overflow-wrap: anywhere;
}

iframe {
  width: 100%;
  min-height: 620px;
  border: 0;
  border-radius: 8px;
  background: #fff;
}
</style>
