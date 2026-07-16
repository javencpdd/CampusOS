<template>
  <div class="permission-manage" v-loading="loading">
    <header class="page-header">
      <div>
        <p class="eyebrow">Identity & Authorization</p>
        <h2>角色与权限</h2>
        <p>系统角色用于平台基础能力；自定义角色可复用一组稳定权限 Code。实际板块范围仍在版主管理中配置。</p>
      </div>
      <div class="header-actions">
        <el-button :icon="Refresh" circle aria-label="刷新权限数据" title="刷新" @click="loadAll" />
        <el-button type="primary" :icon="Plus" @click="createDialogVisible = true">新建自定义角色</el-button>
      </div>
    </header>

    <section class="permission-workspace" aria-label="角色权限矩阵">
      <aside class="role-panel">
        <div class="panel-heading">
          <div>
            <h3>角色</h3>
            <p>{{ roles.length }} 个可用角色</p>
          </div>
        </div>
        <el-scrollbar class="role-list">
          <button
            v-for="role in roles"
            :key="role.id"
            type="button"
            class="role-row"
            :class="{ active: selectedRole?.id === role.id }"
            @click="selectRole(role)"
          >
            <span class="role-row-copy">
              <strong>{{ roleLabel(role.name) }}</strong>
              <small>{{ role.description || '未填写说明' }}</small>
            </span>
            <el-tag :type="role.is_system ? 'info' : 'success'" size="small" effect="plain">
              {{ role.is_system ? '系统' : '自定义' }}
            </el-tag>
          </button>
        </el-scrollbar>
      </aside>

      <section class="matrix-panel">
        <template v-if="selectedRole">
          <div class="matrix-heading">
            <div>
              <div class="title-line">
                <h3>{{ roleLabel(selectedRole.name) }}</h3>
                <el-tag :type="selectedRole.is_system ? 'info' : 'success'" effect="plain">
                  {{ selectedRole.is_system ? '系统角色，权限只读' : '自定义角色' }}
                </el-tag>
              </div>
              <p>{{ selectedRole.description || '未填写角色说明' }}</p>
            </div>
            <el-button
              v-if="!selectedRole.is_system"
              type="primary"
              :loading="saving"
              :disabled="!hasChanges"
              @click="savePermissions"
            >
              保存权限变更
            </el-button>
          </div>

          <el-alert
            v-if="selectedRole.is_system"
            type="info"
            :closable="false"
            show-icon
            title="系统角色的权限由平台种子和专用功能配置保护。请创建自定义角色来组合权限。"
          />
          <el-alert
            v-else
            type="warning"
            :closable="false"
            show-icon
            title="只能授予当前管理员自己已拥有的权限。高风险权限会记录授权审计，保存前请核对变更。"
          />

          <div class="matrix-summary" aria-live="polite">
            <span>已选择 {{ selectedCodes.length }} 项</span>
            <span v-if="addedCodes.length" class="summary-add">新增 {{ addedCodes.length }} 项</span>
            <span v-if="removedCodes.length" class="summary-remove">移除 {{ removedCodes.length }} 项</span>
          </div>

          <div class="permission-groups">
            <section v-for="group in permissionGroups" :key="group.domain" class="permission-group">
              <header>
                <div>
                  <h4>{{ domainLabel(group.domain) }}</h4>
                  <p>{{ group.items.length }} 项权限</p>
                </div>
              </header>
              <el-checkbox-group v-model="selectedCodes" :disabled="selectedRole.is_system" class="permission-list">
                <el-checkbox v-for="item in group.items" :key="item.code" :label="item.code" class="permission-option">
                  <span class="permission-copy">
                    <span class="permission-line">
                      <strong>{{ item.code }}</strong>
                      <el-tag :type="riskType(item.risk_level)" size="small" effect="plain">{{ riskLabel(item.risk_level) }}</el-tag>
                      <el-tag v-if="item.audit_level === 'required'" type="warning" size="small" effect="plain">强审计</el-tag>
                    </span>
                    <small>{{ item.description || `${item.resource}.${item.action}` }} · {{ scopeLabel(item.allowed_scope_types) }}</small>
                  </span>
                </el-checkbox>
              </el-checkbox-group>
            </section>
          </div>
        </template>
        <el-empty v-else description="选择一个角色后查看权限矩阵" />
      </section>
    </section>

    <section class="audit-panel">
      <div class="audit-heading">
        <div>
          <h3>授权记录</h3>
          <p>记录路由授权和角色权限调整。记录不包含任何 Token 或 Secret。</p>
        </div>
        <el-button text :icon="Refresh" @click="loadAudits">刷新记录</el-button>
      </div>
      <el-table :data="audits" size="small" border stripe class="audit-table">
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column prop="actor_id" label="操作者" width="120" show-overflow-tooltip />
        <el-table-column prop="permission_code" label="权限" min-width="210" show-overflow-tooltip />
        <el-table-column prop="operation_code" label="操作" min-width="240" show-overflow-tooltip />
        <el-table-column label="结果" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="row.outcome === 'allow' ? 'success' : row.outcome === 'deny' ? 'danger' : 'warning'" size="small">
              {{ outcomeLabel(row.outcome) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="reason" label="原因" min-width="180" show-overflow-tooltip />
      </el-table>
      <el-empty v-if="!auditLoading && audits.length === 0" description="暂无可见授权记录" :image-size="58" />
    </section>

    <el-dialog v-model="createDialogVisible" title="新建自定义角色" width="min(560px, calc(100vw - 24px))" destroy-on-close>
      <el-form label-position="top" :model="createForm">
        <el-form-item label="角色标识" required>
          <el-input v-model="createForm.name" maxlength="64" placeholder="例如 content_reviewer" />
          <div class="field-hint">使用小写字母、数字和下划线；创建后标识不可修改。</div>
        </el-form-item>
        <el-form-item label="说明">
          <el-input v-model="createForm.description" type="textarea" :rows="3" maxlength="300" show-word-limit />
        </el-form-item>
        <el-form-item label="初始权限">
          <el-select v-model="createForm.permission_codes" multiple filterable class="field-full" placeholder="可在创建后继续编辑">
            <el-option v-for="item in definitions" :key="item.code" :label="item.code" :value="item.code">
              <span>{{ item.code }}</span>
              <span class="option-description">{{ item.description }}</span>
            </el-option>
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="createRole">创建角色</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { roleApi } from '@/modules/identity/api'

type Role = { id: number; name: string; description?: string; is_system: boolean }
type PermissionDefinition = {
  code: string
  domain: string
  resource: string
  action: string
  description?: string
  risk_level?: string
  allowed_scope_types?: string[]
  audit_level?: string
}

const loading = ref(false)
const auditLoading = ref(false)
const saving = ref(false)
const creating = ref(false)
const roles = ref<Role[]>([])
const definitions = ref<PermissionDefinition[]>([])
const audits = ref<any[]>([])
const selectedRole = ref<Role | null>(null)
const selectedCodes = ref<string[]>([])
const originalCodes = ref<string[]>([])
const createDialogVisible = ref(false)
const createForm = ref({ name: '', description: '', permission_codes: [] as string[] })

const dataOf = (value: any) => value?.data ?? value
const itemsOf = (value: any): any[] => {
  const data = dataOf(value)
  return Array.isArray(data) ? data : Array.isArray(data?.items) ? data.items : []
}

const permissionGroups = computed(() => {
  const groups = new Map<string, PermissionDefinition[]>()
  for (const definition of definitions.value) {
    const domain = definition.domain || definition.code.split('.')[0] || 'platform'
    const items = groups.get(domain) || []
    items.push(definition)
    groups.set(domain, items)
  }
  return Array.from(groups.entries())
    .map(([domain, items]) => ({ domain, items: items.sort((left, right) => left.code.localeCompare(right.code)) }))
    .sort((left, right) => left.domain.localeCompare(right.domain))
})

const addedCodes = computed(() => selectedCodes.value.filter((code) => !originalCodes.value.includes(code)))
const removedCodes = computed(() => originalCodes.value.filter((code) => !selectedCodes.value.includes(code)))
const hasChanges = computed(() => addedCodes.value.length > 0 || removedCodes.value.length > 0)

const roleLabel = (name: string) => ({ admin: '管理员', moderator: '版主', member: '普通会员', guest: '访客' }[name] || name)
const domainLabel = (domain: string) => ({ identity: '身份与权限', community: '社区内容', plugin: '插件平台', integration: '集成中心', appearance: '外观', personal_space: '个人空间', platform: '平台运维', ai: 'AI 网关' }[domain] || domain)
const riskLabel = (risk?: string) => ({ high: '高风险', medium: '中风险', low: '低风险' }[risk || 'low'] || '低风险')
const riskType = (risk?: string) => ({ high: 'danger', medium: 'warning', low: 'info' }[risk || 'low'] || 'info')
const scopeLabel = (scopes?: string[]) => (scopes || ['global']).map((scope) => (scope === 'category' ? '版块范围' : '全局范围')).join('、')
const outcomeLabel = (value: string) => ({ allow: '允许', deny: '拒绝', error: '错误' }[value] || value || '-')
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN') : '-'
const messageOf = (error: any, fallback: string) => error?.msg || error?.message || fallback

const loadRoles = async () => {
  roles.value = itemsOf(await roleApi.list())
  if (!selectedRole.value && roles.value.length) await selectRole(roles.value[0])
}

const loadDefinitions = async () => {
  definitions.value = itemsOf(await roleApi.permissions())
}

const loadAudits = async () => {
  auditLoading.value = true
  try {
    audits.value = itemsOf(await roleApi.authorizationAudits())
  } catch (error: any) {
    audits.value = []
    ElMessage.warning(messageOf(error, '暂时无法读取授权记录'))
  } finally {
    auditLoading.value = false
  }
}

const selectRole = async (role: Role) => {
  selectedRole.value = role
  selectedCodes.value = []
  originalCodes.value = []
  try {
    const items = itemsOf(await roleApi.getPermissions(role.id))
    const codes = items.map((item) => item.permission?.code || item.code).filter(Boolean)
    selectedCodes.value = [...codes]
    originalCodes.value = [...codes]
  } catch (error: any) {
    ElMessage.error(messageOf(error, '加载角色权限失败'))
  }
}

const loadAll = async () => {
  loading.value = true
  try {
    await Promise.all([loadDefinitions(), loadRoles(), loadAudits()])
    if (selectedRole.value) await selectRole(selectedRole.value)
  } catch (error: any) {
    ElMessage.error(messageOf(error, '加载权限管理数据失败'))
  } finally {
    loading.value = false
  }
}

const savePermissions = async () => {
  if (!selectedRole.value || selectedRole.value.is_system || !hasChanges.value) return
  const additions = addedCodes.value.join('、') || '无'
  const removals = removedCodes.value.join('、') || '无'
  try {
    await ElMessageBox.confirm(`新增：${additions}\n移除：${removals}`, '确认权限变更', {
      type: 'warning',
      confirmButtonText: '确认保存',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  saving.value = true
  try {
    await roleApi.updatePermissions(selectedRole.value.id, selectedCodes.value)
    originalCodes.value = [...selectedCodes.value]
    ElMessage.success('角色权限已保存并写入授权审计')
    await loadAudits()
  } catch (error: any) {
    ElMessage.error(messageOf(error, '保存角色权限失败'))
  } finally {
    saving.value = false
  }
}

const createRole = async () => {
  const name = createForm.value.name.trim().toLowerCase()
  if (!/^[a-z][a-z0-9_]{1,63}$/.test(name)) {
    ElMessage.warning('角色标识需要以小写字母开头，只能使用小写字母、数字和下划线')
    return
  }
  creating.value = true
  try {
    const created: any = dataOf(await roleApi.createCustom({ ...createForm.value, name }))
    createDialogVisible.value = false
    createForm.value = { name: '', description: '', permission_codes: [] }
    await loadRoles()
    const role = roles.value.find((item) => item.id === created?.id || item.name === name)
    if (role) await selectRole(role)
    await loadAudits()
    ElMessage.success('自定义角色已创建')
  } catch (error: any) {
    ElMessage.error(messageOf(error, '创建自定义角色失败'))
  } finally {
    creating.value = false
  }
}

onMounted(loadAll)
</script>

<style scoped>
.permission-manage { display: grid; gap: 16px; max-width: 1500px; }
.page-header, .permission-workspace, .audit-panel { border: 1px solid #e4e7ed; border-radius: 6px; background: #fff; }
.page-header, .matrix-heading, .audit-heading, .panel-heading, .permission-group > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.page-header { padding: 20px; }
.page-header h2, .panel-heading h3, .matrix-heading h3, .audit-heading h3, .permission-group h4 { margin: 0; }
.page-header p:last-child, .panel-heading p, .matrix-heading p, .audit-heading p, .permission-group p { margin: 6px 0 0; color: #606266; line-height: 1.6; }
.eyebrow { margin: 0 0 6px; color: #166534; font-size: 12px; font-weight: 700; text-transform: uppercase; }
.header-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.permission-workspace { display: grid; grid-template-columns: minmax(220px, 280px) minmax(0, 1fr); min-height: 620px; overflow: hidden; }
.role-panel { border-right: 1px solid #ebeef5; min-width: 0; }
.panel-heading { padding: 16px; border-bottom: 1px solid #ebeef5; }
.role-list { height: 545px; }
.role-row { width: 100%; display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 13px 16px; border: 0; border-bottom: 1px solid #f0f2f5; background: transparent; color: #303133; cursor: pointer; text-align: left; }
.role-row:hover, .role-row.active { background: #eff6ff; }
.role-row.active { box-shadow: inset 3px 0 0 #2563eb; }
.role-row-copy { display: grid; min-width: 0; gap: 3px; }
.role-row-copy strong, .role-row-copy small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.role-row-copy small { color: #606266; }
.matrix-panel { display: grid; align-content: start; gap: 14px; min-width: 0; padding: 20px; }
.title-line { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.matrix-summary { display: flex; gap: 12px; flex-wrap: wrap; color: #606266; font-size: 13px; }
.summary-add { color: #15803d; }.summary-remove { color: #b42318; }
.permission-groups { display: grid; gap: 14px; }
.permission-group { border: 1px solid #ebeef5; border-radius: 6px; overflow: hidden; }
.permission-group > header { padding: 12px 14px; border-bottom: 1px solid #ebeef5; background: #fafafa; }
.permission-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
.permission-option { display: flex; align-items: flex-start; min-width: 0; min-height: 76px; margin: 0; padding: 12px 14px; border-right: 1px solid #f0f2f5; border-bottom: 1px solid #f0f2f5; white-space: normal; }
.permission-copy { display: grid; min-width: 0; gap: 5px; }
.permission-line { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.permission-line strong { overflow-wrap: anywhere; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
.permission-copy small { color: #606266; line-height: 1.45; }
.audit-panel { padding: 20px; }
.audit-heading { margin-bottom: 14px; }
.audit-table { width: 100%; }
.field-full { width: 100%; }.field-hint { margin-top: 5px; color: #909399; font-size: 12px; line-height: 1.5; }.option-description { float: right; max-width: 45%; overflow: hidden; color: #909399; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
@media (max-width: 920px) { .permission-workspace { grid-template-columns: 1fr; } .role-panel { border-right: 0; border-bottom: 1px solid #ebeef5; } .role-list { height: auto; max-height: 230px; } .permission-list { grid-template-columns: 1fr; } }
@media (max-width: 620px) { .page-header, .matrix-heading, .audit-heading { flex-direction: column; } .page-header, .matrix-panel, .audit-panel { padding: 14px; } .header-actions { width: 100%; } .header-actions .el-button:last-child { flex: 1; } .permission-option { min-height: 82px; padding: 11px; } .audit-panel { overflow-x: auto; } .audit-table { min-width: 780px; } }
</style>
