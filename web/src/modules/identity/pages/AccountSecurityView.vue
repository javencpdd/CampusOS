<template>
  <main class="security-page">
    <header class="page-heading"><h1>账号安全</h1></header>
    <el-tabs v-model="tab" class="security-tabs">
      <el-tab-pane label="登录设备" name="sessions">
        <section class="section-toolbar">
          <el-tooltip content="刷新登录设备" placement="top"
            ><el-button :icon="Refresh" circle aria-label="刷新登录设备" @click="loadSessions"
          /></el-tooltip>
          <el-button type="danger" plain :loading="loggingOut" @click="logoutAll">退出全部设备</el-button>
        </section>
        <el-table :data="sessions" v-loading="loadingSessions" class="session-table">
          <el-table-column prop="device_name" label="设备" min-width="150" />
          <el-table-column prop="device_type" label="类型" width="110" />
          <el-table-column label="认证" width="108"
            ><template #default="{ row }"
              ><el-tag :type="row.authentication_strength === 'mfa' ? 'success' : 'info'">{{
                row.authentication_strength === 'mfa' ? '已验证 MFA' : '密码'
              }}</el-tag></template
            ></el-table-column
          >
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

      <el-tab-pane label="多因素认证" name="mfa">
        <section v-loading="loadingMFA" class="mfa-panel">
          <el-alert
            v-if="!mfaStatus.mfa_available"
            title="当前部署未配置 MFA 加密密钥，不能启用认证器。"
            type="warning"
            :closable="false"
            show-icon
          />
          <div class="mfa-summary">
            <div>
              <span>认证器</span><strong>{{ mfaStatus.enabled ? '已启用' : '未启用' }}</strong>
            </div>
            <div>
              <span>恢复码</span
              ><strong>{{ mfaStatus.enabled ? `${mfaStatus.recovery_codes_remaining} 个可用` : '—' }}</strong>
            </div>
            <div>
              <span>管理员策略</span><strong>{{ policyLabel(mfaStatus.policy_mode) }}</strong>
            </div>
          </div>
          <div class="mfa-actions">
            <el-button
              v-if="!mfaStatus.enabled"
              type="primary"
              :disabled="!mfaStatus.mfa_available"
              @click="openEnrollment"
              >启用认证器</el-button
            >
            <template v-else>
              <el-button @click="openRotateRecovery">更新恢复码</el-button>
              <el-button type="danger" plain @click="openDisable">关闭认证器</el-button>
            </template>
            <el-tooltip content="刷新多因素认证状态" placement="top"
              ><el-button :icon="Refresh" circle aria-label="刷新多因素认证状态" @click="loadMFA"
            /></el-tooltip>
          </div>
        </section>
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

    <el-dialog
      v-model="enrollmentOpen"
      title="启用认证器"
      width="min(560px, calc(100% - 24px))"
      :close-on-click-modal="false"
      @closed="resetEnrollment"
    >
      <el-form v-if="!enrollment" label-position="top" @submit.prevent="startEnrollment">
        <el-form-item label="当前密码"
          ><el-input v-model="enrollmentPassword" type="password" autocomplete="current-password" show-password
        /></el-form-item>
        <div class="dialog-actions">
          <el-button @click="enrollmentOpen = false">取消</el-button
          ><el-button type="primary" native-type="submit" :loading="mfaActionLoading">继续</el-button>
        </div>
      </el-form>
      <el-form v-else label-position="top" @submit.prevent="confirmEnrollment">
        <div class="qr-block">
          <img v-if="qrDataURL" :src="qrDataURL" width="220" height="220" alt="认证器配置二维码" /><el-skeleton
            v-else
            :rows="4"
            animated
          />
        </div>
        <el-form-item label="手工密钥"
          ><el-input :model-value="enrollment.manual_key" readonly
            ><template #append><el-button @click="copyManualKey">复制</el-button></template></el-input
          ></el-form-item
        >
        <el-form-item label="认证器验证码"
          ><el-input
            v-model.trim="enrollmentCode"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="6"
            placeholder="000000"
        /></el-form-item>
        <div class="dialog-actions">
          <el-button @click="enrollmentOpen = false">稍后继续</el-button
          ><el-button type="primary" native-type="submit" :loading="mfaActionLoading">确认启用</el-button>
        </div>
      </el-form>
    </el-dialog>

    <el-dialog
      v-model="recoveryOpen"
      title="保存恢复码"
      width="min(560px, calc(100% - 24px))"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
      :show-close="recoveryAcknowledged"
    >
      <p class="recovery-note">这些恢复码只显示这一次。每个恢复码仅可使用一次。</p>
      <div class="recovery-codes">
        <code v-for="code in recoveryCodes" :key="code">{{ code }}</code>
      </div>
      <el-checkbox v-model="recoveryAcknowledged">我已安全保存这些恢复码</el-checkbox>
      <template #footer
        ><el-button type="primary" :disabled="!recoveryAcknowledged" @click="closeRecoveryCodes"
          >完成</el-button
        ></template
      >
    </el-dialog>

    <el-dialog v-model="rotateOpen" title="更新恢复码" width="min(500px, calc(100% - 24px))" @closed="resetFactorProof">
      <el-form label-position="top" @submit.prevent="rotateRecoveryCodes">
        <el-radio-group v-model="factorProofType"
          ><el-radio label="code">认证器验证码</el-radio
          ><el-radio label="recovery">现有恢复码</el-radio></el-radio-group
        >
        <el-form-item :label="factorProofType === 'code' ? '认证器验证码' : '恢复码'" class="proof-input"
          ><el-input
            v-model.trim="factorProof"
            :inputmode="factorProofType === 'code' ? 'numeric' : 'text'"
            autocomplete="one-time-code"
        /></el-form-item>
        <div class="dialog-actions">
          <el-button @click="rotateOpen = false">取消</el-button
          ><el-button type="primary" native-type="submit" :loading="mfaActionLoading">生成新恢复码</el-button>
        </div>
      </el-form>
    </el-dialog>

    <el-dialog v-model="disableOpen" title="关闭认证器" width="min(500px, calc(100% - 24px))" @closed="resetDisable">
      <el-form label-position="top" @submit.prevent="disableMFA">
        <el-form-item label="当前密码"
          ><el-input v-model="disablePassword" type="password" autocomplete="current-password" show-password
        /></el-form-item>
        <el-radio-group v-model="disableProofType"
          ><el-radio label="code">认证器验证码</el-radio><el-radio label="recovery">恢复码</el-radio></el-radio-group
        >
        <el-form-item :label="disableProofType === 'code' ? '认证器验证码' : '恢复码'" class="proof-input"
          ><el-input
            v-model.trim="disableProof"
            :inputmode="disableProofType === 'code' ? 'numeric' : 'text'"
            autocomplete="one-time-code"
        /></el-form-item>
        <div class="dialog-actions">
          <el-button @click="disableOpen = false">取消</el-button
          ><el-button type="danger" native-type="submit" :loading="mfaActionLoading">关闭并退出全部设备</el-button>
        </div>
      </el-form>
    </el-dialog>
  </main>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import QRCode from 'qrcode'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { useRouter } from 'vue-router'
import { authApi, type MFAEnrollment, type MFAStatus } from '@/modules/identity/api'
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
const loadingMFA = ref(false)
const mfaActionLoading = ref(false)
const mfaStatus = reactive<MFAStatus>({
  enabled: false,
  pending_enrollment: false,
  recovery_codes_remaining: 0,
  mfa_available: false,
  policy_mode: 'off',
  step_up_required_after_seconds: 0,
})
const enrollmentOpen = ref(false)
const enrollmentPassword = ref('')
const enrollment = ref<MFAEnrollment | null>(null)
const enrollmentCode = ref('')
const qrDataURL = ref('')
const recoveryOpen = ref(false)
const recoveryCodes = ref<string[]>([])
const recoveryAcknowledged = ref(false)
const rotateOpen = ref(false)
const factorProofType = ref<'code' | 'recovery'>('code')
const factorProof = ref('')
const disableOpen = ref(false)
const disablePassword = ref('')
const disableProofType = ref<'code' | 'recovery'>('code')
const disableProof = ref('')
const messageOf = (error: any, fallback: string) => error?.msg || error?.message || fallback
const formatTime = (value?: string) => (value ? new Date(value).toLocaleString() : '—')
const policyLabel = (mode: string) =>
  ({ off: '未强制', enrollment_grace: '注册宽限期', required: '已强制' })[mode] || mode

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
const loadMFA = async () => {
  loadingMFA.value = true
  try {
    const response: any = await authApi.mfaStatus()
    Object.assign(mfaStatus, response?.data || {})
  } catch (error: any) {
    ElMessage.error(messageOf(error, '多因素认证状态加载失败'))
  } finally {
    loadingMFA.value = false
  }
}
const openEnrollment = () => {
  enrollmentOpen.value = true
}
const startEnrollment = async () => {
  if (!enrollmentPassword.value) return ElMessage.warning('请输入当前密码')
  mfaActionLoading.value = true
  try {
    const response: any = await authApi.startMFAEnrollment({ password: enrollmentPassword.value })
    const value = response?.data as MFAEnrollment | undefined
    if (!value?.manual_key || !value.otpauth_uri) throw new Error('认证器配置不可用')
    enrollment.value = value
    qrDataURL.value = await QRCode.toDataURL(value.otpauth_uri, {
      errorCorrectionLevel: 'M',
      margin: 1,
      width: 220,
      color: { dark: '#141b24', light: '#ffffff' },
    })
    enrollmentPassword.value = ''
  } catch (error: any) {
    ElMessage.error(messageOf(error, '无法开始认证器配置'))
  } finally {
    mfaActionLoading.value = false
  }
}
const copyManualKey = async () => {
  if (!enrollment.value?.manual_key || !navigator.clipboard) return ElMessage.warning('请手工选择并复制密钥')
  try {
    await navigator.clipboard.writeText(enrollment.value.manual_key)
    ElMessage.success('手工密钥已复制')
  } catch {
    ElMessage.warning('请手工选择并复制密钥')
  }
}
const confirmEnrollment = async () => {
  if (!/^\d{6}$/.test(enrollmentCode.value)) return ElMessage.warning('请输入 6 位认证器验证码')
  mfaActionLoading.value = true
  try {
    const response: any = await authApi.confirmMFAEnrollment({ code: enrollmentCode.value })
    if (response?.data?.access_token) userStore.updateAccessToken(response.data.access_token)
    recoveryCodes.value = response?.data?.recovery_codes || []
    if (recoveryCodes.value.length === 0) throw new Error('恢复码未生成')
    enrollmentOpen.value = false
    recoveryAcknowledged.value = false
    recoveryOpen.value = true
    await loadMFA()
  } catch (error: any) {
    ElMessage.error(messageOf(error, '认证器验证码无效或已过期'))
  } finally {
    mfaActionLoading.value = false
  }
}
const resetEnrollment = () => {
  enrollmentPassword.value = ''
  enrollment.value = null
  enrollmentCode.value = ''
  qrDataURL.value = ''
}
const closeRecoveryCodes = () => {
  recoveryOpen.value = false
  recoveryCodes.value = []
  recoveryAcknowledged.value = false
}
const openRotateRecovery = () => {
  factorProofType.value = 'code'
  factorProof.value = ''
  rotateOpen.value = true
}
const resetFactorProof = () => {
  factorProof.value = ''
  factorProofType.value = 'code'
}
const rotateRecoveryCodes = async () => {
  if (!factorProof.value) return ElMessage.warning('请输入认证证明')
  mfaActionLoading.value = true
  try {
    const response: any = await authApi.rotateMFARecoveryCodes(
      factorProofType.value === 'code' ? { code: factorProof.value } : { recovery_code: factorProof.value },
    )
    recoveryCodes.value = response?.data?.recovery_codes || []
    if (recoveryCodes.value.length === 0) throw new Error('恢复码未生成')
    rotateOpen.value = false
    recoveryAcknowledged.value = false
    recoveryOpen.value = true
    await loadMFA()
  } catch (error: any) {
    ElMessage.error(messageOf(error, '认证证明无效'))
  } finally {
    mfaActionLoading.value = false
  }
}
const openDisable = () => {
  disablePassword.value = ''
  disableProof.value = ''
  disableProofType.value = 'code'
  disableOpen.value = true
}
const resetDisable = () => {
  disablePassword.value = ''
  disableProof.value = ''
  disableProofType.value = 'code'
}
const disableMFA = async () => {
  if (!disablePassword.value || !disableProof.value) return ElMessage.warning('请完成当前密码和认证证明')
  mfaActionLoading.value = true
  try {
    await authApi.disableMFA(
      disableProofType.value === 'code'
        ? { password: disablePassword.value, code: disableProof.value }
        : { password: disablePassword.value, recovery_code: disableProof.value },
    )
    disableOpen.value = false
    ElMessage.success('认证器已关闭，请重新登录')
    await userStore.logout()
    router.replace('/login')
  } catch (error: any) {
    ElMessage.error(messageOf(error, '无法关闭认证器'))
  } finally {
    mfaActionLoading.value = false
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
onMounted(() => {
  void loadSessions()
  void loadMFA()
})
</script>

<style scoped>
.security-page {
  box-sizing: border-box;
  width: min(100%, 980px);
  margin: 0 auto;
  padding: 24px 16px 40px;
}
.page-heading {
  margin-bottom: 12px;
}
.page-heading h1 {
  margin: 0;
  font-size: 24px;
}
.section-toolbar,
.mfa-actions,
.form-actions,
.dialog-actions {
  display: flex;
  gap: 10px;
}
.section-toolbar {
  justify-content: flex-end;
  margin: 0 0 14px;
}
.form-actions,
.dialog-actions {
  justify-content: flex-end;
}
.binding-form {
  width: min(100%, 520px);
  padding-top: 10px;
}
.binding-form .el-steps {
  margin: 0 0 24px;
}
.session-table {
  width: 100%;
}
.mfa-panel {
  display: grid;
  gap: 18px;
  width: min(100%, 680px);
  padding: 12px 0;
}
.mfa-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}
.mfa-summary > div {
  display: grid;
  gap: 6px;
  padding: 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
}
.mfa-summary span {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.mfa-summary strong {
  overflow-wrap: anywhere;
}
.mfa-actions {
  flex-wrap: wrap;
}
.qr-block {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 220px;
  margin: 0 0 16px;
}
.qr-block img {
  max-width: 100%;
  border: 1px solid var(--el-border-color-lighter);
}
.recovery-note {
  color: var(--el-text-color-regular);
  line-height: 1.6;
}
.recovery-codes {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 16px 0;
}
.recovery-codes code {
  padding: 9px;
  overflow-wrap: anywhere;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-primary);
  font-size: 14px;
}
.proof-input {
  margin-top: 16px;
}
@media (max-width: 640px) {
  .security-page {
    padding: 16px 12px 32px;
  }
  .section-toolbar {
    justify-content: space-between;
  }
  .form-actions,
  .dialog-actions {
    flex-direction: column-reverse;
  }
  .form-actions .el-button,
  .dialog-actions .el-button {
    width: 100%;
    margin: 0;
  }
  .mfa-summary {
    grid-template-columns: 1fr;
  }
  .recovery-codes {
    grid-template-columns: 1fr;
  }
}
</style>
