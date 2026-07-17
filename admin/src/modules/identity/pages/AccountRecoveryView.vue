<template>
  <div class="account-recovery" v-loading="loading">
    <header class="page-heading">
      <div><p class="eyebrow">Identity Security</p><h2>账号恢复</h2></div>
      <el-button :icon="Refresh" circle aria-label="刷新账号恢复列表" @click="load" />
    </header>

    <section class="case-create" aria-labelledby="create-case-title">
      <h3 id="create-case-title">创建辅助恢复</h3>
      <el-form :model="createForm" label-position="top" class="case-form" @submit.prevent="createCase">
        <el-form-item label="用户 ID"><el-input v-model.trim="createForm.user_id" inputmode="numeric" /></el-form-item>
        <el-form-item label="新邮箱"><el-input v-model.trim="createForm.email" type="email" /></el-form-item>
        <el-form-item label="线下核验编号"><el-input v-model.trim="createForm.proof_reference" /></el-form-item>
        <div class="form-actions"><el-button type="primary" native-type="submit" :loading="creating">创建并发送验证</el-button></div>
      </el-form>
    </section>

    <section class="case-table-section" aria-labelledby="case-list-title">
      <div class="section-title"><h3 id="case-list-title">恢复记录</h3><span>{{ cases.length }} 条</span></div>
      <el-table :data="cases" class="case-table" empty-text="暂无恢复记录">
        <el-table-column prop="id" label="恢复 ID" min-width="180" show-overflow-tooltip />
        <el-table-column prop="user_id" label="用户 ID" width="150" />
        <el-table-column prop="target_email_masked" label="目标邮箱" min-width="160" />
        <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
        <el-table-column label="到期时间" min-width="170"><template #default="{ row }">{{ formatTime(row.expires_at) }}</template></el-table-column>
        <el-table-column label="操作" width="190" fixed="right"><template #default="{ row }"><el-button text @click="openSessions(row.user_id)">会话</el-button><el-popconfirm v-if="row.status === 'pending'" title="取消此恢复请求？" @confirm="cancelCase(row.id)"><template #reference><el-button text type="danger">取消</el-button></template></el-popconfirm></template></el-table-column>
      </el-table>
    </section>

    <el-drawer v-model="sessionDrawer" :title="`用户 ${sessionUserID} 的会话`" size="min(480px, 100%)">
      <div class="drawer-actions"><el-button :icon="Refresh" circle aria-label="刷新用户会话" @click="openSessions(sessionUserID)" /><el-popconfirm title="撤销该用户的全部会话？" @confirm="revokeAll"><template #reference><el-button type="danger" plain :loading="revoking">撤销全部会话</el-button></template></el-popconfirm></div>
      <el-table :data="sessions" v-loading="loadingSessions" empty-text="暂无会话">
        <el-table-column prop="device_name" label="设备" min-width="140" />
        <el-table-column label="最近活动" min-width="150"><template #default="{ row }">{{ formatTime(row.last_active_at) }}</template></el-table-column>
        <el-table-column label="状态" width="90"><template #default="{ row }"><el-tag :type="row.revoked_at ? 'info' : 'success'">{{ row.revoked_at ? '已撤销' : '有效' }}</el-tag></template></el-table-column>
      </el-table>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { identityRecoveryApi } from '@/modules/identity/api'

const loading = ref(false)
const creating = ref(false)
const cases = ref<any[]>([])
const createForm = reactive({ user_id: '', email: '', proof_reference: '' })
const sessionDrawer = ref(false)
const sessionUserID = ref('')
const sessions = ref<any[]>([])
const loadingSessions = ref(false)
const revoking = ref(false)
const messageOf = (error: any, fallback: string) => error?.msg || error?.message || fallback
const formatTime = (value?: string) => value ? new Date(value).toLocaleString() : '—'
const statusLabel = (status: string) => ({ pending: '待验证', completed: '已完成', cancelled: '已取消', expired: '已过期' }[status] || status)
const statusType = (status: string) => ({ pending: 'warning', completed: 'success', cancelled: 'info', expired: 'danger' }[status] || 'info')

const load = async () => {
  loading.value = true
  try { const result: any = await identityRecoveryApi.cases(); cases.value = result?.data?.items || [] } catch (error: any) { ElMessage.error(messageOf(error, '恢复记录加载失败')) } finally { loading.value = false }
}
const createCase = async () => {
  if (!createForm.user_id || !createForm.email || !createForm.proof_reference) return ElMessage.warning('请填写完整的恢复信息')
  creating.value = true
  try {
    await identityRecoveryApi.createCase(createForm)
    createForm.user_id = ''; createForm.email = ''; createForm.proof_reference = ''
    ElMessage.success('恢复请求已创建')
    await load()
  } catch (error: any) { ElMessage.error(messageOf(error, '恢复请求无法创建')) } finally { creating.value = false }
}
const cancelCase = async (id: string) => {
  try { await identityRecoveryApi.cancelCase(id); ElMessage.success('恢复请求已取消'); await load() } catch (error: any) { ElMessage.error(messageOf(error, '恢复请求无法取消')) }
}
const openSessions = async (userID: string) => {
  if (!userID) return
  sessionUserID.value = userID; sessionDrawer.value = true; loadingSessions.value = true
  try { const result: any = await identityRecoveryApi.sessions(userID); sessions.value = result?.data?.items || [] } catch (error: any) { ElMessage.error(messageOf(error, '会话加载失败')) } finally { loadingSessions.value = false }
}
const revokeAll = async () => {
  if (!sessionUserID.value) return
  revoking.value = true
  try { await identityRecoveryApi.revokeAllSessions(sessionUserID.value); ElMessage.success('会话已撤销'); await openSessions(sessionUserID.value) } catch (error: any) { ElMessage.error(messageOf(error, '会话无法撤销')) } finally { revoking.value = false }
}
onMounted(load)
</script>

<style scoped>
.account-recovery { max-width: 1280px; margin: 0 auto; }.page-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 22px; }.page-heading h2,.page-heading h3,.case-create h3,.section-title h3 { margin: 0; }.eyebrow { margin: 0 0 4px; color: var(--el-color-primary); font-size: 12px; font-weight: 700; text-transform: uppercase; }.case-create { padding: 18px 0 24px; border-bottom: 1px solid var(--el-border-color-lighter); }.case-form { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); align-items: end; gap: 12px; margin-top: 16px; }.case-form .el-form-item { margin-bottom: 0; }.form-actions { display: flex; align-items: end; height: 100%; }.case-table-section { padding-top: 22px; }.section-title { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 12px; }.section-title span { color: var(--el-text-color-secondary); font-size: 13px; }.case-table { width: 100%; }.drawer-actions { display: flex; justify-content: space-between; gap: 10px; margin-bottom: 16px; }
@media (max-width: 840px) { .case-form { grid-template-columns: 1fr; }.form-actions,.form-actions .el-button { width: 100%; }.account-recovery { padding-bottom: 20px; } }
</style>
