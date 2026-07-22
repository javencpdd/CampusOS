<template>
  <div class="admin-admission" v-loading="loading">
    <header class="page-heading">
      <div>
        <p class="eyebrow">Management Plane</p>
        <h2>管理员准入</h2>
      </div>
      <div class="heading-actions">
        <el-select v-model="statusFilter" aria-label="筛选准入状态" @change="reload">
          <el-option label="全部状态" value="" />
          <el-option label="可进入后台" value="active" />
          <el-option label="已暂停" value="suspended" />
          <el-option label="已撤销" value="revoked" />
        </el-select>
        <el-tooltip content="刷新管理员准入列表" placement="top">
          <el-button :icon="Refresh" circle aria-label="刷新管理员准入列表" @click="load" />
        </el-tooltip>
        <el-button plain :icon="Document" @click="openAudit">变更审计</el-button>
      </div>
    </header>

    <section class="admission-table-section" aria-labelledby="admission-list-title">
      <div class="section-title">
        <h3 id="admission-list-title">管理平面账号</h3>
        <span>{{ total }} 条</span>
      </div>
      <el-table :data="records" class="admission-table" empty-text="暂无管理员准入账号">
        <el-table-column label="管理员" min-width="170">
          <template #default="{ row }">
            <div class="identity-cell">
              <strong>{{ row.nickname || row.username || row.account.user_id }}</strong>
              <span>{{ row.username || row.account.user_id }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="email" label="邮箱" min-width="190" show-overflow-tooltip />
        <el-table-column label="准入状态" width="130">
          <template #default="{ row }"><el-tag :type="statusType(row.account.status)">{{ statusLabel(row.account.status) }}</el-tag></template>
        </el-table-column>
        <el-table-column label="最近认证" min-width="168">
          <template #default="{ row }">{{ formatTime(row.account.last_authenticated_at) }}</template>
        </el-table-column>
        <el-table-column label="最近变更" min-width="220">
          <template #default="{ row }">
            <div class="change-cell">
              <span>{{ formatTime(row.account.status_changed_at) }}</span>
              <small>{{ row.account.status_reason || '—' }}</small>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="版本" width="84" align="center"><template #default="{ row }">{{ row.account.version }}</template></el-table-column>
        <el-table-column label="操作" width="130" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.account.status === 'active'" type="danger" text @click="openTransition(row, 'suspend')">暂停</el-button>
            <el-button v-else-if="row.account.status === 'suspended'" type="success" text @click="openTransition(row, 'restore')">恢复</el-button>
            <el-tooltip v-else content="撤销状态由管理员角色变更决定" placement="top"><span><el-button text disabled>已撤销</el-button></span></el-tooltip>
          </template>
        </el-table-column>
      </el-table>
      <footer class="pagination-bar">
        <el-pagination
          v-model:current-page="page"
          :page-size="pageSize"
          :total="total"
          layout="total, prev, pager, next"
          @current-change="load"
        />
      </footer>
    </section>

    <el-dialog v-model="transitionOpen" :title="transitionTitle" width="min(480px, calc(100% - 24px))" destroy-on-close @closed="resetTransition">
      <el-form label-position="top" @submit.prevent="submitTransition">
        <el-form-item label="管理员"><el-input :model-value="transitionTargetLabel" disabled /></el-form-item>
        <el-form-item label="变更原因" required>
          <el-input v-model.trim="transitionReason" type="textarea" :rows="4" maxlength="500" show-word-limit />
        </el-form-item>
        <div class="dialog-actions">
          <el-button @click="transitionOpen = false">取消</el-button>
          <el-button :type="transitionKind === 'suspend' ? 'danger' : 'success'" native-type="submit" :loading="saving" :disabled="!transitionReason">
            {{ transitionKind === 'suspend' ? '确认暂停' : '确认恢复' }}
          </el-button>
        </div>
      </el-form>
    </el-dialog>

    <el-drawer v-model="auditOpen" title="管理员准入变更审计" size="min(720px, 100%)" @open="loadAudits">
      <el-table :data="audits" v-loading="auditLoading" empty-text="暂无准入变更审计">
        <el-table-column prop="created_at" label="时间" min-width="160"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column>
        <el-table-column prop="actor_id" label="操作者" min-width="120"><template #default="{ row }">{{ row.actor_id || '本机恢复' }}</template></el-table-column>
        <el-table-column prop="permission_code" label="操作" min-width="190" show-overflow-tooltip />
        <el-table-column prop="resource_id" label="目标用户" min-width="120" />
        <el-table-column prop="reason" label="原因" min-width="180" show-overflow-tooltip />
      </el-table>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Document, Refresh } from '@element-plus/icons-vue'
import { adminAdmissionApi, type AdminAdmissionRecord } from '@/modules/identity/api'

const loading = ref(false)
const saving = ref(false)
const records = ref<AdminAdmissionRecord[]>([])
const statusFilter = ref('')
const page = ref(1)
const pageSize = 50
const total = ref(0)
const transitionOpen = ref(false)
const transitionKind = ref<'suspend' | 'restore'>('suspend')
const transitionTarget = ref<AdminAdmissionRecord | null>(null)
const transitionReason = ref('')
const auditOpen = ref(false)
const auditLoading = ref(false)
const audits = ref<any[]>([])

const messageOf = (error: any, fallback: string) => error?.msg || error?.message || fallback
const formatTime = (value?: string) => value ? new Date(value).toLocaleString() : '—'
const statusLabel = (status: string) => ({ active: '可进入后台', suspended: '已暂停', revoked: '已撤销' }[status] || status)
const statusType = (status: string) => ({ active: 'success', suspended: 'warning', revoked: 'info' }[status] || 'info')
const transitionTitle = computed(() => transitionKind.value === 'suspend' ? '暂停管理员准入' : '恢复管理员准入')
const transitionTargetLabel = computed(() => {
  const target = transitionTarget.value
  return target ? `${target.nickname || target.username || target.account.user_id} (${target.account.user_id})` : ''
})

const load = async () => {
  loading.value = true
  try {
    const result: any = await adminAdmissionApi.list({ status: statusFilter.value || undefined, page: page.value, page_size: pageSize })
    records.value = result?.data?.items || []
    total.value = result?.data?.pagination?.total || 0
  } catch (error: any) {
    ElMessage.error(messageOf(error, '管理员准入列表加载失败'))
  } finally {
    loading.value = false
  }
}

const reload = () => { page.value = 1; void load() }
const openTransition = (record: AdminAdmissionRecord, kind: 'suspend' | 'restore') => {
  transitionTarget.value = record
  transitionKind.value = kind
  transitionReason.value = ''
  transitionOpen.value = true
}
const resetTransition = () => {
  transitionTarget.value = null
  transitionReason.value = ''
}
const submitTransition = async () => {
  const target = transitionTarget.value
  if (!target || !transitionReason.value) return
  saving.value = true
  try {
    const payload = { expected_version: target.account.version, reason: transitionReason.value }
    const result: any = transitionKind.value === 'suspend'
      ? await adminAdmissionApi.suspend(target.account.user_id, payload)
      : await adminAdmissionApi.restore(target.account.user_id, payload)
    const updated = result?.data as AdminAdmissionRecord | undefined
    if (!updated?.account?.user_id) throw new Error('准入状态更新结果不可用')
    ElMessage.success(transitionKind.value === 'suspend' ? '管理员准入已暂停，会话已撤销' : '管理员准入已恢复')
    transitionOpen.value = false
    await load()
  } catch (error: any) {
    ElMessage.error(messageOf(error, '管理员准入状态更新失败'))
    await load()
  } finally {
    saving.value = false
  }
}
const openAudit = () => { auditOpen.value = true }
const loadAudits = async () => {
  auditLoading.value = true
  try {
    const result: any = await adminAdmissionApi.audits()
    audits.value = result?.data?.items || []
  } catch (error: any) {
    ElMessage.error(messageOf(error, '管理员准入审计加载失败'))
  } finally {
    auditLoading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.admin-admission { max-width: 1440px; margin: 0 auto; padding-bottom: 24px; }
.page-heading,.heading-actions,.section-title,.dialog-actions,.pagination-bar { display: flex; align-items: center; }
.page-heading { justify-content: space-between; gap: 16px; margin-bottom: 22px; }
.heading-actions { gap: 10px; flex-wrap: wrap; justify-content: flex-end; }
.heading-actions .el-select { width: 148px; }
.page-heading h2,.section-title h3 { margin: 0; }
.eyebrow { margin: 0 0 4px; color: var(--el-color-primary); font-size: 12px; font-weight: 700; text-transform: uppercase; }
.admission-table-section { border-top: 1px solid var(--el-border-color-lighter); padding-top: 18px; }
.section-title { justify-content: space-between; margin-bottom: 12px; }
.section-title span { color: var(--el-text-color-secondary); font-size: 13px; }
.admission-table { width: 100%; }
.identity-cell,.change-cell { display: grid; gap: 3px; min-width: 0; }
.identity-cell span,.change-cell small { color: var(--el-text-color-secondary); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pagination-bar { justify-content: flex-end; padding-top: 16px; }
.dialog-actions { justify-content: flex-end; gap: 10px; }
@media (max-width: 720px) { .page-heading { align-items: flex-start; flex-direction: column; }.heading-actions { width: 100%; justify-content: stretch; }.heading-actions .el-select { flex: 1; }.heading-actions .el-button:last-child { flex: 1; }.pagination-bar { justify-content: center; }.admin-admission { padding-bottom: 12px; } }
</style>
