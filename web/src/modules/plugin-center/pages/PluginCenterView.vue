<template>
  <section class="plugin-center-page" aria-labelledby="plugin-center-title">
    <div class="page-heading">
      <div>
        <h1 id="plugin-center-title">插件中心</h1>
        <p>查看管理员发布的插件，按用途授权，并随时撤销或导出自己的数据。</p>
      </div>
      <el-button :loading="loading" @click="load"
        ><el-icon><Refresh /></el-icon>刷新</el-button
      >
    </div>

    <el-alert
      title="插件只能获得你明确授予的用户级能力。系统级 Host API 权限由管理员在安装时审核，不会在这里授予。"
      type="info"
      show-icon
      :closable="false"
    />

    <section class="request-panel" aria-labelledby="request-plugin-title">
      <div>
        <h2 id="request-plugin-title">推荐或申请安装插件</h2>
        <p>填写稳定插件 ID；申请只进入管理员审核，不会由浏览器直接安装代码。</p>
      </div>
      <div class="request-fields">
        <el-input v-model="requestName" placeholder="例如 calendar-assistant" aria-label="插件 ID" />
        <el-input v-model="requestMessage" placeholder="用途或来源说明（可选）" aria-label="申请说明" />
        <el-button :loading="requesting" @click="requestInstall()">提交申请</el-button>
      </div>
    </section>

    <div v-if="!loading" class="plugin-list">
      <el-empty v-if="!catalog.length" class="catalog-empty" :description="catalogEmptyReason">
        <template #image
          ><el-icon class="empty-icon"><Box /></el-icon
        ></template>
        <template #default>
          <p class="empty-title">暂时没有可用的外部插件</p>
          <p class="empty-copy">{{ catalogEmptyReason }}</p>
          <el-button type="primary" @click="focusRequest">申请安装插件</el-button>
        </template>
      </el-empty>
      <article v-for="entry in catalog" :key="entry.plugin_name" class="plugin-item">
        <div class="plugin-heading">
          <div>
            <h2>{{ entry.display_name || entry.plugin_name }}</h2>
            <small>{{ entry.plugin_name }} · v{{ entry.version }} · {{ entry.runtime }}</small>
          </div>
          <el-tag :type="grantType(entry.plugin_name)" effect="plain">{{ grantLabel(entry.plugin_name) }}</el-tag>
        </div>
        <p>{{ entry.description || '该插件未提供详细介绍。' }}</p>
        <dl class="experience-list">
          <div v-if="entry.experience?.use_cases?.length">
            <dt>适合场景</dt>
            <dd>{{ entry.experience.use_cases.join('、') }}</dd>
          </div>
          <div>
            <dt>数据使用</dt>
            <dd>{{ entry.experience?.data_use || '仅使用你明确授权的数据。' }}</dd>
          </div>
          <div>
            <dt>风险与控制</dt>
            <dd>{{ entry.experience?.risk_summary || '可随时撤销授权。' }}</dd>
          </div>
          <div>
            <dt>关闭后</dt>
            <dd>{{ entry.experience?.disabled_behavior || '不会自动删除个人数据。' }}</dd>
          </div>
          <div v-if="entry.experience?.maintainer || entry.experience?.documentation_url">
            <dt>维护与帮助</dt>
            <dd>
              <span v-if="entry.experience?.maintainer">{{ entry.experience.maintainer }}</span>
              <a
                v-if="entry.experience?.documentation_url"
                :href="entry.experience.documentation_url"
                target="_blank"
                rel="noopener noreferrer"
                >查看使用说明</a
              >
            </dd>
          </div>
        </dl>
        <div class="capability-list">
          <span v-for="capability in entry.data_capabilities" :key="capability" class="capability">{{
            capabilityLabel(capability)
          }}</span
          ><span v-if="!entry.data_capabilities?.length" class="capability">不保存用户数据</span>
        </div>
        <dl v-if="usageFor(entry.plugin_name)" class="usage-list" aria-label="个人插件数据占用">
          <div>
            <dt>记录</dt>
            <dd>{{ usageFor(entry.plugin_name)?.record_count || 0 }} 条</dd>
          </div>
          <div>
            <dt>文件</dt>
            <dd>{{ usageFor(entry.plugin_name)?.file_count || 0 }} 个</dd>
          </div>
          <div>
            <dt>文件占用</dt>
            <dd>{{ formatBytes(usageFor(entry.plugin_name)?.file_bytes || 0) }}</dd>
          </div>
          <div>
            <dt>系统检索</dt>
            <dd>{{ usageFor(entry.plugin_name)?.search_enabled ? '已授权' : '未授权' }}</dd>
          </div>
        </dl>
        <dl v-if="entry.user_permissions?.length" class="permission-list">
          <div v-for="permission in entry.user_permissions" :key="permissionKey(permission)">
            <dt>{{ permissionPurpose(permission) }}</dt>
            <dd>
              {{ permission.resource }} / {{ permission.actions.join(', ')
              }}<span v-if="permission.risk"> · 风险：{{ permission.risk }}</span>
            </dd>
          </div>
        </dl>
        <div class="plugin-actions">
          <template v-if="isEnabled(entry.plugin_name)"
            ><el-button @click="exportData(entry.plugin_name)"
              ><el-icon><Download /></el-icon>导出数据</el-button
            ><el-popconfirm
              title="将撤销授权并删除此插件保存的个人记录和文件。"
              confirm-button-text="删除并撤销"
              cancel-button-text="取消"
              @confirm="deleteData(entry.plugin_name)"
              ><template #reference
                ><el-button type="danger" plain
                  ><el-icon><Delete /></el-icon>删除数据</el-button
                ></template
              ></el-popconfirm
            ><el-button type="warning" plain @click="revoke(entry.plugin_name)">撤销授权</el-button></template
          >
          <template v-else
            ><el-button type="primary" @click="openConsent(entry)">查看并授权</el-button
            ><el-button text @click="requestInstall(entry.plugin_name)">请求安装</el-button></template
          >
        </div>
      </article>
    </div>
    <div v-else class="loading-state"><el-skeleton :rows="5" animated /></div>

    <el-dialog v-model="consentDialog" title="确认插件授权" width="min(620px, calc(100vw - 24px))">
      <template v-if="selected"
        ><p class="consent-intro">{{ selected.display_name || selected.plugin_name }} 将按以下声明使用你的数据：</p>
        <el-checkbox-group v-model="selectedPermissions" class="consent-list"
          ><el-checkbox
            v-for="permission in selected.user_permissions"
            :key="permissionKey(permission)"
            :label="permissionKey(permission)"
            ><strong>{{ permissionPurpose(permission) }}</strong
            ><small
              >{{ permission.resource }} / {{ permission.actions.join(', ')
              }}{{ permission.risk ? ` · 风险：${permission.risk}` : '' }}</small
            ></el-checkbox
          ></el-checkbox-group
        ><el-alert
          v-if="!selected.user_permissions?.length"
          title="该插件未声明用户数据权限；启用后仅可使用其公开功能。"
          type="success"
          :closable="false"
      /></template>
      <template #footer
        ><el-button @click="consentDialog = false">取消</el-button
        ><el-button type="primary" :loading="granting" @click="grant">确认授权</el-button></template
      >
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Box, Delete, Download, Refresh } from '@element-plus/icons-vue'
import { pluginCenterApi } from '../api'

type Permission = { resource: string; actions: string[]; purpose: string; risk?: string; revocable: boolean }
type CatalogEntry = {
  plugin_name: string
  display_name: string
  description: string
  version: string
  runtime: string
  data_capabilities: string[]
  user_permissions: Permission[]
  experience?: {
    use_cases?: string[]
    data_use?: string
    risk_summary?: string
    disabled_behavior?: string
    maintainer?: string
    documentation_url?: string
  }
}
type Grant = { plugin_name: string; status: string; permissions: string[] }
type Usage = {
  plugin_name: string
  record_count: number
  file_count: number
  file_bytes: number
  search_enabled: boolean
}
const catalog = ref<CatalogEntry[]>([]),
  grants = ref<Grant[]>([]),
  usages = ref<Usage[]>([]),
  loading = ref(false),
  granting = ref(false),
  requesting = ref(false),
  consentDialog = ref(false),
  selected = ref<CatalogEntry | null>(null),
  selectedPermissions = ref<string[]>([]),
  requestName = ref(''),
  requestMessage = ref(''),
  catalogState = ref('ready'),
  catalogEmptyReason = ref('管理员暂未发布可供用户授权的外部插件。内置功能不在插件中心安装或授权。')
const unwrap = (value: any) => value?.data || value || {}
const enabledGrants = computed(
  () => new Map(grants.value.filter((grant) => grant.status === 'enabled').map((grant) => [grant.plugin_name, grant])),
)
const load = async () => {
  loading.value = true
  try {
    const [catalogResponse, grantResponse, usageResponse] = await Promise.all([
      pluginCenterApi.catalog(),
      pluginCenterApi.myGrants(),
      pluginCenterApi.myUsage(),
    ])
    const catalogData = unwrap(catalogResponse)
    catalog.value = catalogData.items || []
    catalogState.value = catalogData.catalog_state || (catalog.value.length ? 'ready' : 'empty')
    catalogEmptyReason.value =
      catalogData.empty_reason || '管理员暂未发布可供用户授权的外部插件。内置功能不在插件中心安装或授权。'
    grants.value = unwrap(grantResponse).items || []
    usages.value = unwrap(usageResponse).items || []
  } catch (error: any) {
    ElMessage.error(error?.message || '加载插件中心失败')
  } finally {
    loading.value = false
  }
}
const isEnabled = (name: string) => enabledGrants.value.has(name)
const grantLabel = (name: string) => (isEnabled(name) ? '已授权' : '未授权')
const grantType = (name: string) => (isEnabled(name) ? 'success' : 'info')
const usageFor = (name: string) => usages.value.find((item) => item.plugin_name === name)
const formatBytes = (value: number) => {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / 1024 / 1024).toFixed(1)} MB`
}
const permissionKey = (permission: Permission) => `${permission.resource}:${permission.actions.join(',')}`
const permissionPurpose = (permission: Permission) => permission.purpose || '未说明用途'
const capabilityLabel = (capability: string) =>
  ({ 'managed-data': '受管数据', 'user-files': '个人文件', 'user-consent': '需用户授权' })[capability] || capability
const focusRequest = () => {
  const target = document.querySelector<HTMLInputElement>('.request-fields input')
  target?.focus()
}
const openConsent = (entry: CatalogEntry) => {
  selected.value = entry
  selectedPermissions.value = entry.user_permissions?.map(permissionKey) || []
  consentDialog.value = true
}
const grant = async () => {
  if (!selected.value) return
  const permissions = selected.value.user_permissions
    .filter((permission) => selectedPermissions.value.includes(permissionKey(permission)))
    .flatMap((permission) => permission.actions.map((action) => `${permission.resource}:${action}`))
  granting.value = true
  try {
    await pluginCenterApi.enable(selected.value.plugin_name, permissions)
    ElMessage.success('插件授权已保存')
    consentDialog.value = false
    await load()
  } catch (error: any) {
    ElMessage.error(error?.message || '授权失败')
  } finally {
    granting.value = false
  }
}
const revoke = async (name: string) => {
  try {
    await pluginCenterApi.revoke(name)
    ElMessage.success('已撤销插件授权')
    await load()
  } catch (error: any) {
    ElMessage.error(error?.message || '撤销失败')
  }
}
const requestInstall = async (name = requestName.value) => {
  name = name.trim()
  if (!name) {
    ElMessage.error('请填写插件 ID')
    return
  }
  requesting.value = true
  try {
    await pluginCenterApi.request(name, name === requestName.value.trim() ? requestMessage.value : '')
    ElMessage.success('已提交管理员审核请求')
    requestName.value = ''
    requestMessage.value = ''
  } catch (error: any) {
    ElMessage.error(error?.message || '请求提交失败')
  } finally {
    requesting.value = false
  }
}
const exportData = async (name: string) => {
  try {
    const content = JSON.stringify(unwrap(await pluginCenterApi.exportData(name)), null, 2)
    const blob = new Blob([content], { type: 'application/json' })
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = `${name}-my-data.json`
    link.click()
    URL.revokeObjectURL(link.href)
    ElMessage.success('数据导出已生成')
  } catch (error: any) {
    ElMessage.error(error?.message || '导出失败')
  }
}
const deleteData = async (name: string) => {
  try {
    await pluginCenterApi.deleteData(name)
    ElMessage.success('插件数据已删除')
    await load()
  } catch (error: any) {
    ElMessage.error(error?.message || '删除失败')
  }
}
onMounted(() => {
  void load()
})
</script>

<style scoped>
.plugin-center-page {
  width: min(960px, 100%);
  margin: 0 auto;
  display: grid;
  gap: 18px;
}
.page-heading,
.plugin-heading,
.plugin-actions {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 14px;
}
h1,
h2,
p {
  margin: 0;
}
h1 {
  font-size: 23px;
}
h2 {
  font-size: 17px;
}
.page-heading p {
  margin-top: 5px;
  color: #687385;
  font-size: 14px;
}
.plugin-list {
  display: grid;
  gap: 12px;
}
.request-panel {
  display: grid;
  gap: 12px;
  padding: 16px;
  border: 1px solid var(--campus-border-color, #dfe3e8);
  background: var(--campus-surface-color, #fff);
}
.request-panel p {
  margin-top: 4px;
  color: var(--campus-muted-color, #687385);
  font-size: 13px;
}
.request-fields {
  display: grid;
  grid-template-columns: minmax(150px, 0.8fr) minmax(220px, 1.4fr) auto;
  gap: 8px;
}
.plugin-item {
  padding: 18px;
  border: 1px solid var(--campus-border-color, #dfe3e8);
  background: var(--campus-surface-color, #fff);
  border-radius: 6px;
  display: grid;
  gap: 12px;
}
.plugin-heading small,
.plugin-item p,
.permission-list dd {
  color: var(--campus-muted-color, #687385);
}
.plugin-item p {
  font-size: 14px;
  line-height: 1.6;
}
.capability-list {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
}
.capability {
  padding: 3px 7px;
  background: #edf4ff;
  color: #245b9b;
  font-size: 12px;
  border-radius: 4px;
}
.permission-list {
  display: grid;
  gap: 8px;
  margin: 0;
}
.experience-list {
  display: grid;
  gap: 7px;
  margin: 0;
  padding: 11px 12px;
  border-left: 3px solid #4a85c5;
  background: color-mix(in srgb, var(--campus-page-background, #f4f6f8) 84%, #d9ecff);
}
.experience-list div {
  display: grid;
  grid-template-columns: 84px minmax(0, 1fr);
  gap: 8px;
}
.experience-list dt {
  color: var(--campus-muted-color, #687385);
  font-size: 12px;
}
.experience-list dd {
  margin: 0;
  color: var(--campus-text-color, #1f2937);
  font-size: 13px;
  line-height: 1.55;
}
.experience-list a {
  margin-left: 10px;
}
.catalog-empty {
  padding: 34px 18px;
  border: 1px solid var(--campus-border-color, #dfe3e8);
  background: var(--campus-surface-color, #fff);
}
.empty-icon {
  font-size: 44px;
  color: #4a85c5;
}
.empty-title {
  margin-top: 10px;
  color: var(--campus-text-color, #1f2937);
  font-weight: 650;
}
.empty-copy {
  max-width: 480px;
  margin: 7px auto 14px;
  color: var(--campus-muted-color, #687385);
  font-size: 13px;
  line-height: 1.6;
}
.usage-list {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  margin: 0;
  padding: 10px 12px;
  background: var(--campus-page-background, #f4f6f8);
}
.usage-list div {
  min-width: 0;
}
.usage-list dt {
  color: var(--campus-muted-color, #687385);
  font-size: 12px;
}
.usage-list dd {
  margin: 3px 0 0;
  font-size: 13px;
}
.permission-list div {
  display: grid;
  gap: 3px;
}
.permission-list dt {
  font-weight: 650;
  font-size: 13px;
}
.permission-list dd {
  margin: 0;
  font-size: 12px;
}
.plugin-actions {
  justify-content: flex-start;
  flex-wrap: wrap;
}
.consent-intro {
  margin-bottom: 14px;
}
.consent-list {
  display: grid;
  gap: 12px;
}
.consent-list :deep(.el-checkbox) {
  height: auto;
  align-items: flex-start;
  white-space: normal;
}
.consent-list strong,
.consent-list small {
  display: block;
}
.consent-list small {
  margin-top: 4px;
  color: #687385;
  line-height: 1.5;
}
.loading-state {
  padding: 18px;
  border: 1px solid var(--campus-border-color, #dfe3e8);
  background: var(--campus-surface-color, #fff);
}
@media (max-width: 600px) {
  .page-heading,
  .plugin-heading {
    align-items: flex-start;
  }
  .page-heading {
    flex-direction: column;
  }
  .plugin-item {
    padding: 14px;
  }
  .plugin-actions :deep(.el-button) {
    margin-left: 0;
  }
  .plugin-actions {
    gap: 8px;
  }
  .usage-list {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .request-fields {
    grid-template-columns: minmax(0, 1fr);
  }
  .experience-list div {
    grid-template-columns: minmax(0, 1fr);
    gap: 2px;
  }
}
</style>
