<template>
  <main class="security-page">
    <header class="page-heading"><h1>账号安全</h1></header>
    <el-tabs v-model="tab" class="security-tabs">
      <el-tab-pane label="登录设备" name="sessions">
        <section class="section-toolbar">
          <el-button :icon="Refresh" circle aria-label="刷新登录设备" @click="loadSessions" />
          <el-button type="danger" plain :loading="loggingOut" @click="logoutAll">退出全部设备</el-button>
        </section>
        <el-table :data="sessions" v-loading="loadingSessions" class="session-table">
          <el-table-column prop="device_name" label="设备" min-width="150" />
          <el-table-column prop="device_type" label="类型" width="110" />
          <el-table-column label="最近活动" min-width="170"
            ><template #default="{ row }">{{ formatTime(row.last_active_at) }}</template></el-table-column
          >
          <el-table-column label="状态" width="100"
            ><template #default="{ row }"
              ><el-tag :type="row.revoked_at ? 'info' : row.current ? 'success' : 'primary'">{{
                row.revoked_at ? '已退出' : row.current ? '当前' : '有效'
              }}</el-tag></template
            ></el-table-column
          >
          <el-table-column label="操作" width="96" fixed="right"
            ><template #default="{ row }"
              ><el-button v-if="!row.revoked_at" text type="danger" @click="revokeSession(row.id)"
                >退出</el-button
              ></template
            ></el-table-column
          >
        </el-table>
      </el-tab-pane>
      <el-tab-pane label="绑定邮箱" name="email">
        <section class="binding-form">
          <el-steps :active="bindingStep" finish-status="success" simple
            ><el-step title="邮箱" /><el-step title="验证"
          /></el-steps>
          <el-form v-if="bindingStep === 0" label-position="top" @submit.prevent="requestBinding">
            <el-form-item label="新邮箱"
              ><el-input v-model.trim="bindingEmail" type="email" autocomplete="email"
            /></el-form-item>
            <div class="form-actions">
              <el-button type="primary" native-type="submit" :loading="bindingLoading">发送验证码</el-button>
            </div>
          </el-form>
          <el-form v-else label-position="top" @submit.prevent="completeBinding">
            <el-form-item label="验证码"
              ><el-input v-model.trim="bindingCode" inputmode="numeric" maxlength="6" autocomplete="one-time-code"
            /></el-form-item>
            <div class="form-actions">
              <el-button plain @click="bindingStep = 0">上一步</el-button
              ><el-button type="primary" native-type="submit" :loading="bindingLoading">绑定并重新登录</el-button>
            </div>
          </el-form>
        </section>
      </el-tab-pane>
    </el-tabs>
  </main>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { useRouter } from 'vue-router'
import { authApi } from '@/modules/identity/api'
import { useUserStore } from '@/modules/identity/store'

const router = useRouter()
const userStore = useUserStore()
const tab = ref('sessions')
const sessions = ref<any[]>([])
const loadingSessions = ref(false)
const loggingOut = ref(false)
const bindingEmail = ref('')
const bindingCode = ref('')
const bindingChallengeID = ref('')
const bindingTicket = ref('')
const bindingStep = ref(0)
const bindingLoading = ref(false)
const messageOf = (error: any, fallback: string) => error?.msg || error?.message || fallback
const formatTime = (value?: string) => (value ? new Date(value).toLocaleString() : '—')

const loadSessions = async () => {
  loadingSessions.value = true
  try {
    const response: any = await authApi.sessions()
    sessions.value = response?.data?.items || []
  } catch (error: any) {
    ElMessage.error(messageOf(error, '登录设备加载失败'))
  } finally {
    loadingSessions.value = false
  }
}
const revokeSession = async (id: string) => {
  try {
    await authApi.revokeSession(id)
    await loadSessions()
    ElMessage.success('设备已退出')
  } catch (error: any) {
    ElMessage.error(messageOf(error, '退出失败'))
  }
}
const logoutAll = async () => {
  try {
    await ElMessageBox.confirm('这会退出所有设备。', '退出全部设备', { type: 'warning' })
    loggingOut.value = true
    await authApi.logoutAll()
    await userStore.logout()
    router.replace('/login')
  } catch (error: any) {
    if (error !== 'cancel') ElMessage.error(messageOf(error, '退出失败'))
  } finally {
    loggingOut.value = false
  }
}
const requestBinding = async () => {
  if (!bindingEmail.value) return ElMessage.warning('请输入新邮箱')
  bindingLoading.value = true
  try {
    const response: any = await authApi.requestEmailBinding({ email: bindingEmail.value })
    if (!response?.data?.challenge_id) throw new Error('验证码请求未创建')
    bindingChallengeID.value = response.data.challenge_id
    bindingStep.value = 1
    ElMessage.success('验证码已发送')
  } catch (error: any) {
    ElMessage.error(messageOf(error, '邮箱不可绑定'))
  } finally {
    bindingLoading.value = false
  }
}
const completeBinding = async () => {
  if (bindingCode.value.length !== 6) return ElMessage.warning('请输入 6 位验证码')
  bindingLoading.value = true
  try {
    const verified: any = await authApi.verifyEmailBinding({
      challenge_id: bindingChallengeID.value,
      code: bindingCode.value,
    })
    bindingTicket.value = verified?.data?.ticket || ''
    if (!bindingTicket.value) throw new Error('验证码无效')
    await authApi.completeEmailBinding({
      email: bindingEmail.value,
      challenge_id: bindingChallengeID.value,
      ticket: bindingTicket.value,
    })
    ElMessage.success('邮箱已绑定，请重新登录')
    await userStore.logout()
    router.replace('/login')
  } catch (error: any) {
    ElMessage.error(messageOf(error, '邮箱验证失败'))
  } finally {
    bindingLoading.value = false
  }
}
onMounted(loadSessions)
</script>

<style scoped>
.security-page {
  width: min(100%, 980px);
  margin: 0 auto;
  padding: 24px 16px 40px;
  box-sizing: border-box;
}
.page-heading {
  margin-bottom: 12px;
}
.page-heading h1 {
  margin: 0;
  font-size: 24px;
}
.section-toolbar {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin: 0 0 14px;
}
.binding-form {
  width: min(100%, 520px);
  padding-top: 10px;
}
.binding-form .el-steps {
  margin: 0 0 24px;
}
.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
.session-table {
  width: 100%;
}
@media (max-width: 640px) {
  .security-page {
    padding: 16px 12px 32px;
  }
  .section-toolbar {
    justify-content: space-between;
  }
  .form-actions {
    flex-direction: column-reverse;
  }
  .form-actions .el-button {
    width: 100%;
    margin: 0;
  }
}
</style>
