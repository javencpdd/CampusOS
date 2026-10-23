<template>
  <main class="plugin-center">
    <header>
      <h1>插件中心</h1>
      <button :disabled="busy" @click="load(false)">重新加载</button
      ><router-link to="/plugin-config">本人配置与密钥引用</router-link>
    </header>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="message" role="status">{{ message }}</p>
    <div class="columns">
      <nav aria-label="可用插件">
        <button v-for="item in items" :key="coordinateKey(item.release)" :disabled="busy" @click="choose(item.release)">
          {{ item.release.manifest.display_name || item.release.plugin_key
          }}<small>{{ coordinateKey(item.release) }}</small></button
        ><button v-if="cursor" :disabled="busy" @click="load(true)">加载更多</button>
        <p v-if="!items.length">暂无获管理员批准的可用插件。</p>
      </nav>
      <section v-if="selected" aria-label="本人插件安装">
        <h2>{{ coordinateKey(selected.release) }}</h2>
        <p>
          期望：{{ selected.installation?.desired || '未安装' }}；后端观测：{{
            selected.installation?.observed || 'stopped'
          }}；{{ selected.installation?.error_code }}
        </p>
        <fieldset :disabled="busy">
          <legend>本人安装</legend>
          <button v-if="!selected.installation?.installed" @click="command('install')">安装到我的账号</button
          ><template v-else
            ><button v-if="selected.installation.version !== selected.release.version" @click="command('upgrade')">
              切换到此版本</button
            ><button
              v-if="
                selected.installation.version !== selected.release.version &&
                selected.installation.desired_version === selected.release.version &&
                selected.installation.desired === 'disabled'
              "
              @click="command('enable')"
            >
              启用此版本</button
            ><template v-else
              ><button @click="command('enable')">启用</button><button @click="command('disable')">停用</button
              ><label><input type="checkbox" v-model="purge" />卸载时清理插件自身数据</label
              ><button @click="command('uninstall')">卸载</button></template
            ></template
          >
        </fieldset>
        <p
          v-if="
            selected.installation?.desired === 'disabled' &&
            selected.installation.desired_version !== selected.installation.version
          "
        >
          已准备切换，当前版本仍为
          {{ selected.installation.version }}。显式启用时会重新核对当前批准和本人同意，实际就绪后才切换版本。
        </p>
        <form aria-label="本人数据用途同意" @submit.prevent="consent(true)">
          <fieldset :disabled="busy">
            <legend>此版本的数据用途</legend>
            <p>版本：{{ selected.release.version }}；当前批准修订：{{ selected.grant?.revision }}</p>
            <p>
              授权截止：{{
                selected.grant?.expires_at_ms && selected.grant.expires_at_ms !== '0'
                  ? new Date(Number(selected.grant.expires_at_ms)).toLocaleString()
                  : '未设置'
              }}；已过期或撤销的安装可停用、卸载。
            </p>
            <ul>
              <li v-for="(item, index) in selected.release.manifest.consumes || []" :key="index">
                {{ item.capability }} · {{ item.scope.kind }}
                <p v-if="item.scope.collection_ids?.length">公开集合：{{ item.scope.collection_ids.join('、') }}</p>
                <p v-if="item.scope.keys?.length">限定键：{{ item.scope.keys.join('、') }}</p>
                <p>{{ item.purpose }}</p>
              </li>
            </ul>
            <p>当前同意：{{ selected.consent?.accepted ? '已同意' : '未同意' }}</p>
            <label
              ><input type="checkbox" v-model="confirmed" />我已了解以上用途，同意此版本在宿主权限内处理我的数据</label
            ><button type="submit" :disabled="!confirmed">同意此版本用途</button
            ><button v-if="selected.consent?.accepted" type="button" @click="consent(false)">撤回同意</button>
          </fieldset>
        </form>
      </section>
      <p v-else>选择插件查看用途和安装状态。</p>
    </div>
  </main>
</template>
<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { getAccessToken } from '../../identity/session'
import { useUserStore } from '../../identity/store'
import { pluginV5Api } from '../v5-api'
import {
  coordinateKey,
  type Coordinate,
  type ConfigEntry,
} from '../../../../../sdk/typescript/src/v5-configuration-form'
const user = useUserStore(),
  items = ref<ConfigEntry[]>([]),
  cursor = ref(''),
  selected = ref<ConfigEntry | null>(null),
  busy = ref(false),
  error = ref(''),
  message = ref(''),
  confirmed = ref(false),
  purge = ref(false)
let epoch = 0,
  disposed = false,
  timer: ReturnType<typeof setInterval>
const stamp = () => JSON.stringify([getAccessToken(), user.user?.id])
let identity = stamp()
function current() {
  if (disposed) return false
  if (stamp() === identity && getAccessToken()) return true
  identity = stamp()
  epoch++
  items.value = []
  cursor.value = ''
  selected.value = null
  confirmed.value = false
  busy.value = false
  message.value = ''
  error.value = '用户身份已变化，请重新加载。'
  return false
}
async function load(more: boolean) {
  current()
  if (busy.value || !getAccessToken()) return
  const turn = ++epoch
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
    const seen = new Set(items.value.map((i) => coordinateKey(i.release)))
    items.value.push(...r.data.items.filter((i) => !seen.has(coordinateKey(i.release))))
    cursor.value = r.data.next_cursor || ''
  } catch (e: any) {
    if (current() && turn === epoch) error.value = e.message || '读取失败。'
  } finally {
    if (turn === epoch) busy.value = false
  }
}
async function choose(c: Coordinate) {
  if (!current() || busy.value) return
  const turn = ++epoch
  busy.value = true
  selected.value = null
  confirmed.value = false
  purge.value = false
  error.value = ''
  try {
    const r = await pluginV5Api.detail(c)
    if (!current() || turn !== epoch) return
    if (r.code !== 0 || coordinateKey(r.data.release) !== coordinateKey(c)) throw Error('版本已变化，请重新加载。')
    selected.value = r.data
  } catch (e: any) {
    if (current() && turn === epoch) error.value = e.message || '详情不可用。'
  } finally {
    if (turn === epoch) busy.value = false
  }
}
async function write(intent: Record<string, unknown>) {
  if (!current() || busy.value || !selected.value) return
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
    await choose(c)
    if (current()) message.value = '操作已提交，状态以当前服务器结果为准。'
  } catch (e: any) {
    if (current() && turn === epoch) {
      selected.value = null
      confirmed.value = false
      error.value =
        e.status === 409 ? '状态已变化，请重新加载。' : `${e.message || '操作结果未确认。'} 请重新加载确认后再操作。`
    }
  } finally {
    if (turn === epoch) busy.value = false
  }
}
function command(kind: string) {
  if (selected.value)
    void write({
      kind,
      expected_generation: selected.value.installation?.generation || '0',
      ...(kind === 'uninstall' ? { purge_data: purge.value } : {}),
    })
}
function consent(accepted: boolean) {
  if (!selected.value || (accepted && !confirmed.value)) return
  void write({
    kind: 'consent',
    accepted,
    expected_revision: selected.value.consent?.revision || '0',
    purpose_hash: selected.value.purpose_hash,
  })
}
onMounted(() => {
  void load(false)
  timer = setInterval(current, 200)
})
onBeforeUnmount(() => {
  disposed = true
  epoch++
  clearInterval(timer)
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
</style>
