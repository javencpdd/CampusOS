<template>
  <main class="plugin-center">
    <header>
      <div>
        <h1>插件中心</h1>
        <p>安装到本人账号、确认用途，再启用并使用。</p>
      </div>
      <div class="toolbar">
        <button :disabled="busy" @click="load(false)">重新加载</button
        ><router-link to="/plugin-config">本人配置与密钥引用</router-link>
      </div>
    </header>
    <p v-if="error && !selected" role="alert">{{ error }}</p>
    <p v-if="message" role="status">{{ message }}</p>
    <div class="columns">
      <nav aria-label="可用插件" class="plugin-list">
        <button
          v-for="item in items"
          :key="coordinateKey(item.release)"
          :data-coordinate="coordinateKey(item.release)"
          :aria-pressed="!!selected && coordinateKey(selected.release) === coordinateKey(item.release)"
          :disabled="busy"
          @click="choose(item.release)"
        >
          <strong>{{ pluginDisplayName(item) }}</strong
          ><small>版本 {{ item.release.version }}</small
          ><span class="status-chip">{{ userProgress(item, now).label }}</span></button
        ><button v-if="cursor" :disabled="busy" @click="load(true)">加载更多</button>
        <p v-if="!items.length">暂无获管理员批准的可用插件。需要管理员先安装并审批。</p>
      </nav>
      <section v-if="selected" aria-label="本人插件安装" class="plugin-detail">
        <header>
          <h2>{{ pluginDisplayName(selected) }}</h2>
          <span>版本 {{ selected.release.version }}</span>
        </header>
        <p v-if="error" role="alert">{{ error }}</p>
        <p aria-label="插件当前状态">
          <span class="status-chip">{{ progress?.label }}</span>
        </p>
        <ol aria-label="本人插件使用步骤" class="plugin-steps">
          <li :class="{ done: selected.installation?.installed }">
            <strong>1. 安装到本人账号</strong
            ><span>{{ selected.installation?.installed ? '已安装' : '尚未安装' }}</span>
          </li>
          <li :class="{ done: consentCurrent(selected) }">
            <strong>2. 确认用途</strong
            ><span>{{
              !consentRequired(selected)
                ? '无需额外同意'
                : consentCurrent(selected)
                  ? '当前用途已同意'
                  : selected.consent?.accepted
                    ? '需要重新同意'
                    : '尚未同意'
            }}</span>
          </li>
          <li :class="{ done: progress?.stage === 'ready' }">
            <strong>3. 启用并使用</strong
            ><span>{{
              progress?.stage === 'ready'
                ? '已启用'
                : selected.installation?.desired === 'enabled'
                  ? '请求已提交'
                  : '尚未启用'
            }}</span>
          </li>
        </ol>
        <aside class="next-step" aria-label="下一步">
          <strong>下一步</strong>
          <p>{{ needsReload ? '当前状态尚未确认，请先刷新当前状态。' : progress?.detail }}</p>
          <button
            v-if="needsReload || progress?.action === 'refresh'"
            :disabled="busy"
            @click="choose(selected.release, true)"
          >
            刷新当前状态</button
          ><button v-if="progress?.action === 'consent' && !needsReload" :disabled="busy" @click="showConsent">
            查看并确认用途
          </button>
          <button v-if="progress?.stage === 'ready' && hasUserPage" :disabled="busy || needsReload" @click="openPlugin">
            打开插件
          </button>
        </aside>
        <fieldset :disabled="busy || needsReload">
          <legend>本人安装与启用</legend>
          <button
            v-if="!selected.installation?.installed"
            :disabled="progress?.action !== 'install'"
            @click="command('install')"
          >
            安装到我的账号</button
          ><template v-else
            ><p>已安装到本人账号。</p>
            <p v-if="selected.installation.version !== selected.release.version">
              当前版本仍为 {{ selected.installation.version }}。切换准备完成后，需明确启用此版本。
            </p>
            <button
              v-if="
                selected.installation.version !== selected.release.version &&
                selected.installation.desired_version !== selected.release.version
              "
              :disabled="progress?.action !== 'upgrade'"
              @click="command('upgrade')"
            >
              切换到此版本</button
            ><button
              v-if="
                selected.installation.desired !== 'enabled' ||
                selected.installation.version !== selected.release.version
              "
              :disabled="progress?.action !== 'enable'"
              @click="command('enable')"
            >
              {{ selected.installation.version !== selected.release.version ? '启用此版本' : '启用' }}</button
            ><button v-if="selected.installation.desired === 'enabled'" @click="command('disable')">
              停用
            </button></template
          >
        </fieldset>
        <details
          v-if="consentRequired(selected)"
          ref="consentPanel"
          class="consent-panel"
          :open="consentOpen"
          @toggle="consentOpen = ($event.currentTarget as HTMLDetailsElement).open"
        >
          <summary>
            本人数据用途
            <span class="status-chip">{{
              consentCurrent(selected) ? '已同意当前用途' : selected.consent?.accepted ? '需要重新同意' : '尚未同意'
            }}</span>
          </summary>
          <form aria-label="本人数据用途同意" @submit.prevent="consent(true)">
            <fieldset :disabled="busy || needsReload || progress?.stage !== 'consent'">
              <legend>当前获批的数据用途</legend>
              <p>插件只可使用管理员已批准、且仍在宿主权限范围内的能力。</p>
              <ul class="purpose-list">
                <li v-for="(item, index) in approvedCapabilities(selected)" :key="index">
                  {{ capabilityTitle(item.capability) }}
                  <details class="inline-details">
                    <summary>查看完整用途与权限范围</summary>
                    <p>{{ item.purpose }}</p>
                    <small>{{ item.capability }} · {{ item.scope.kind }}</small
                    ><small v-if="item.scope.collection_ids?.length"
                      >限定公开集合：{{ item.scope.collection_ids.join('、') }}</small
                    >
                  </details>
                </li>
              </ul>
              <p v-if="!approvedCapabilities(selected).length">当前没有获批的业务能力。</p>
              <p>授权截止：{{ expiryText(selected.grant?.expires_at_ms) }}</p>
              <p>
                当前同意：{{ consentCurrent(selected) ? '已同意' : '未同意'
                }}{{ selected.consent?.accepted && !consentCurrent(selected) ? '（审批已变化，请重新确认）' : '' }}
              </p>
              <label
                ><input
                  type="checkbox"
                  v-model="confirmed"
                />我已了解以上用途，同意此版本在宿主权限内处理我的数据</label
              >
              <p v-if="confirmed && !consentCurrent(selected)" class="draft-notice">
                已勾选，尚未提交。点击“同意此版本用途”后才会保存。
              </p>
              <button type="submit" :disabled="!confirmed">同意此版本用途</button>
            </fieldset>
          </form>
        </details>
        <div v-if="progress?.stage === 'ready'" class="usage-panel">
          <p v-if="!hasUserPage">{{ progress.detail }}</p>
          <router-link to="/plugin-config">管理本人配置</router-link>
        </div>
        <details class="secondary-panel" aria-label="技术详情">
          <summary>技术详情</summary>
          <dl>
            <dt>插件标识</dt>
            <dd>{{ coordinateKey(selected.release) }}</dd>
            <dt>安装代次 / 审批修订</dt>
            <dd>{{ selected.installation?.generation || '0' }} / {{ selected.grant?.revision || '0' }}</dd>
            <dt>期望 / 观测状态</dt>
            <dd>
              {{ selected.installation?.desired || '未安装' }} / {{ selected.installation?.observed || 'stopped' }}
            </dd>
            <dt>原始错误码</dt>
            <dd>{{ selected.installation?.error_code || selected.site_state?.error_code || '无' }}</dd>
            <dt>包摘要</dt>
            <dd>{{ selected.release.package_digest }}</dd>
          </dl>
        </details>
        <details v-if="selected.installation?.installed || selected.consent?.accepted" class="secondary-panel">
          <summary>撤回同意与卸载</summary>
          <p>卸载默认保留本人插件配置和数据。</p>
          <button v-if="selected.consent?.accepted" :disabled="busy || needsReload" @click="consent(false)">
            撤回同意</button
          ><label v-if="selected.installation?.installed"
            ><input type="checkbox" v-model="purge" />卸载时清理插件自身数据</label
          ><button
            v-if="selected.installation?.installed"
            :disabled="busy || needsReload"
            @click="command('uninstall')"
          >
            卸载
          </button>
        </details>
      </section>
      <p v-else>选择左侧插件，查看安装进度和下一步操作。</p>
    </div>
  </main>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { getAccessToken } from '../../identity/session'
import { useUserStore } from '../../identity/store'
import { pluginV5Api } from '../v5-api'
import {
  coordinateKey,
  type Coordinate,
  type ConfigEntry,
} from '../../../../../sdk/typescript/src/v5-configuration-form'
import {
  pluginDisplayName,
  capabilityTitle,
  approvedCapabilities,
  consentRequired,
  consentCurrent,
  requiresSelectedResource,
  userProgress,
} from '../../../../../sdk/typescript/src/v5-lifecycle-presentation'
const user = useUserStore(),
  router = useRouter(),
  items = ref<ConfigEntry[]>([]),
  cursor = ref(''),
  selected = ref<ConfigEntry | null>(null),
  busy = ref(false),
  error = ref(''),
  message = ref(''),
  confirmed = ref(false),
  purge = ref(false),
  needsReload = ref(false),
  consentOpen = ref(false),
  consentPanel = ref<HTMLDetailsElement | null>(null),
  now = ref(Date.now())
const progress = computed(() => (selected.value ? userProgress(selected.value, now.value) : null))
const hasUserPage = computed(
  () =>
    !!selected.value &&
    !requiresSelectedResource(selected.value) &&
    (selected.value.release.manifest.ui || []).some((ui) => ui.audience === 'user'),
)
const expiryText = (value: string | undefined) =>
  value && value !== '0' ? new Date(Number(value)).toLocaleString() : '未设置'
let epoch = 0,
  disposed = false,
  timer: ReturnType<typeof setInterval>
let statusTimer: ReturnType<typeof setTimeout> | undefined
function followPendingStatus(c: Coordinate, turn: number, attempt = 0) {
  clearTimeout(statusTimer)
  const state = selected.value
  const personalPending =
    state?.installation?.desired === 'enabled' &&
    state.installation.observed === 'pending' &&
    !state.installation.error_code
  const sitePending =
    state?.site_state?.enabled &&
    !state.site_state.ready &&
    state.site_state.observed === 'pending' &&
    !state.site_state.error_code
  if (attempt >= 10 || (!personalPending && !sitePending)) return
  statusTimer = setTimeout(async () => {
    if (!current() || turn !== epoch || busy.value || needsReload.value) return
    try {
      const result = await pluginV5Api.detail(c)
      if (!current() || turn !== epoch) return
      if (result.code !== 0 || coordinateKey(result.data.release) !== coordinateKey(c)) return
      remember(result.data)
      followPendingStatus(c, turn, attempt + 1)
    } catch (e: any) {
      if (!current() || turn !== epoch) return
      if ([401, 403, 404].includes(e.status)) {
        selected.value = null
        items.value = []
        error.value = '当前版本的读取权限或可见性已变化，请重新加载。'
      }
    }
  }, 1200)
}
const stamp = () => JSON.stringify([getAccessToken(), user.user?.id])
let identity = stamp()
function current() {
  if (disposed) return false
  if (stamp() === identity && getAccessToken()) return true
  identity = stamp()
  clearTimeout(statusTimer)
  epoch++
  items.value = []
  cursor.value = ''
  selected.value = null
  confirmed.value = false
  busy.value = false
  needsReload.value = false
  consentOpen.value = false
  message.value = ''
  error.value = '用户身份已变化，请重新加载。'
  return false
}
function remember(item: ConfigEntry) {
  selected.value = item
  items.value = items.value.map((old) => (coordinateKey(old.release) === coordinateKey(item.release) ? item : old))
}
function showConsent() {
  consentOpen.value = true
  consentPanel.value?.scrollIntoView?.({ behavior: 'smooth', block: 'start' })
}
async function load(more: boolean) {
  current()
  if (busy.value || !getAccessToken()) return
  const turn = ++epoch
  clearTimeout(statusTimer)
  busy.value = true
  selected.value = null
  error.value = ''
  if (!more) {
    items.value = []
    cursor.value = ''
  }
  try {
    const r = await pluginV5Api.list(cursor.value)
    if (!current() || turn !== epoch) return
    if (r.code !== 0 || !Array.isArray(r.data?.items)) throw Error('插件清单不可用。')
    const seen = new Set(items.value.map((item) => coordinateKey(item.release)))
    items.value.push(...r.data.items.filter((item) => !seen.has(coordinateKey(item.release))))
    cursor.value = r.data.next_cursor || ''
  } catch (e: any) {
    if (current() && turn === epoch) error.value = e.message || '读取失败。'
  } finally {
    if (turn === epoch) busy.value = false
  }
}
async function choose(c: Coordinate, preserveContext = false) {
  if (!current() || busy.value) return
  const turn = ++epoch
  clearTimeout(statusTimer)
  busy.value = true
  needsReload.value = true
  error.value = ''
  if (!preserveContext) {
    selected.value = null
    confirmed.value = false
    purge.value = false
  }
  try {
    const r = await pluginV5Api.detail(c)
    if (!current() || turn !== epoch) return
    if (r.code !== 0 || coordinateKey(r.data.release) !== coordinateKey(c)) throw Error('版本已变化，请重新加载。')
    remember(r.data)
    confirmed.value = false
    purge.value = false
    needsReload.value = false
    consentOpen.value = userProgress(r.data).stage === 'consent'
    followPendingStatus(c, turn)
  } catch (e: any) {
    if (current() && turn === epoch) {
      if ([401, 403, 404].includes(e.status)) selected.value = null
      error.value = e.message || '详情尚未确认，请刷新当前状态。'
    }
  } finally {
    if (turn === epoch) busy.value = false
  }
}
async function write(intent: Record<string, unknown>) {
  if (!current() || busy.value || needsReload.value || !selected.value) return
  const turn = epoch,
    c = { ...selected.value.release }
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const r = await pluginV5Api.command(c, intent)
    if (!current() || turn !== epoch) return
    if (r.code !== 0 || !r.data?.id) throw Error('操作结果未确认。')
    busy.value = false
    await choose(c, true)
    if (current())
      message.value = needsReload.value ? '操作已提交，请刷新确认当前状态。' : '已读取最新状态，请按“下一步”继续。'
  } catch (e: any) {
    if (!current() || turn !== epoch) return
    needsReload.value = true
    try {
      const r = await pluginV5Api.detail(c)
      if (!current() || turn !== epoch) return
      if (r.code !== 0 || coordinateKey(r.data.release) !== coordinateKey(c)) throw Error('状态尚未确认。')
      remember(r.data)
      needsReload.value = false
    } catch (readError: any) {
      if (!current() || turn !== epoch) return
      if ([401, 403, 404].includes(readError.status)) {
        selected.value = null
        items.value = []
      }
    }
    if (current() && turn === epoch)
      error.value = `${e.status === 409 ? '状态已变化。' : e.message || '操作未完成。'} ${needsReload.value ? '请刷新确认当前状态。' : '已重新读取当前状态，请核对下一步。'} 未自动重试。`
  } finally {
    if (turn === epoch) busy.value = false
  }
}
function command(kind: string) {
  if (!current() || !selected.value) return
  const item = selected.value,
    state = userProgress(item)
  if (['install', 'enable', 'upgrade'].includes(kind) && state.action !== kind) {
    error.value = state.detail
    return
  }
  void write({
    kind,
    expected_generation: item.installation?.generation || '0',
    ...(kind === 'uninstall' ? { purge_data: purge.value } : {}),
  })
}
function consent(accepted: boolean) {
  if (
    !current() ||
    !selected.value ||
    (accepted && (!confirmed.value || userProgress(selected.value).action !== 'consent'))
  )
    return
  void write({
    kind: 'consent',
    accepted,
    expected_revision: selected.value.consent?.revision || '0',
    purpose_hash: selected.value.purpose_hash,
  })
}
async function openPlugin() {
  if (
    !current() ||
    busy.value ||
    needsReload.value ||
    !selected.value ||
    userProgress(selected.value).stage !== 'ready' ||
    !hasUserPage.value
  )
    return
  const turn = epoch,
    name = selected.value.release.publisher + '.' + selected.value.release.plugin_key,
    version = selected.value.release.version
  busy.value = true
  error.value = ''
  try {
    const { useUIRuntimeStore } = await import('../../plugin-runtime/store')
    if (!current() || turn !== epoch) return
    const runtime = useUIRuntimeStore()
    await runtime.sync()
    if (!current() || turn !== epoch) return
    const surface = Array.from(runtime.surfaces.values()).find(
      (item) => item.plugin === name && item.plugin_version === version && item.type !== 'document-preview',
    )
    const navigation =
      surface && runtime.navigation.find((item) => item.plugin === name && item.route_id === surface.id + '.route')
    if (!navigation) throw Error('插件界面尚未就绪。请刷新状态；持续失败时请联系管理员检查界面网关。')
    await router.push(navigation.path)
  } catch (e: any) {
    if (current() && turn === epoch) error.value = e.message || '插件界面暂不可用。'
  } finally {
    if (turn === epoch) busy.value = false
  }
}
onMounted(() => {
  void load(false)
  timer = setInterval(() => {
    current()
    now.value = Math.floor(Date.now() / 1000) * 1000
  }, 200)
})
onBeforeUnmount(() => {
  disposed = true
  epoch++
  clearInterval(timer)
  clearTimeout(statusTimer)
})
</script>
<style scoped>
.plugin-center {
  max-width: 1100px;
  margin: auto;
  padding: 16px;
}
header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
}
.columns {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) minmax(0, 3fr);
  gap: 16px;
}
nav {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
section {
  min-width: 0;
}
fieldset {
  margin: 14px 0;
  padding: 16px;
  border: 1px solid #cbd5e1;
}
label {
  display: block;
  margin: 12px 0;
}
button {
  font: inherit;
  padding: 9px;
  margin: 4px;
}
p,
small,
li {
  overflow-wrap: anywhere;
}
small {
  display: block;
}
[role='alert'] {
  color: #a61b1b;
}
@media (max-width: 700px) {
  .columns {
    grid-template-columns: minmax(0, 1fr);
  }
}

header h1 {
  margin: 0;
}
header p {
  color: #637083;
}
.toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}
.plugin-list button {
  text-align: left;
  border: 1px solid #dbe3e8;
  background: #fff;
  border-radius: 8px;
  padding: 14px;
}
.plugin-list button[aria-pressed='true'] {
  border-color: #15805d;
  background: #effaf5;
}
.plugin-detail > header {
  justify-content: space-between;
}
.status-chip {
  display: inline-block;
  font-size: 13px;
  border-radius: 5px;
  padding: 4px 8px;
  margin: 3px;
  background: #eef3f6;
  color: #33465b;
}
.plugin-steps {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  padding: 0;
  gap: 10px;
  list-style: none;
}
.plugin-steps li {
  padding: 12px;
  border: 1px solid #dbe3e8;
  border-radius: 8px;
  background: #f8fafb;
}
.plugin-steps li.done {
  border-color: #92c9af;
  background: #effaf4;
}
.plugin-steps span {
  display: block;
  font-size: 14px;
  margin-top: 7px;
}
.next-step {
  padding: 14px 18px;
  border-left: 4px solid #15805d;
  background: #eff7f3;
  border-radius: 6px;
}
.next-step p {
  margin: 6px 0;
}
.secondary-panel,
.consent-panel {
  border: 1px solid #dbe3e8;
  border-radius: 8px;
  padding: 12px 16px;
  margin: 14px 0;
}
summary {
  cursor: pointer;
  padding: 6px 0;
  font-weight: 600;
}
.inline-details {
  font-size: 13px;
  color: #637083;
}
.inline-details summary {
  font-weight: 400;
}
.purpose-list {
  padding-left: 22px;
}
.purpose-list > li {
  margin: 12px 0;
}
.draft-notice {
  background: #fff5df;
  color: #8c5500;
  padding: 10px;
  border-radius: 5px;
}
dd {
  margin: 4px 0 12px;
  overflow-wrap: anywhere;
  color: #637083;
}
@media (max-width: 700px) {
  .plugin-steps {
    grid-template-columns: 1fr;
  }
}
</style>
