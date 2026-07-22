<template>
  <div class="mfa-policy" v-loading="loading">
    <header class="page-heading">
      <div><p class="eyebrow">Identity Security</p><h2>管理员 MFA 策略</h2></div>
      <el-tooltip content="刷新策略状态" placement="top"><el-button :icon="Refresh" circle aria-label="刷新管理员 MFA 策略" @click="load" /></el-tooltip>
    </header>

    <el-alert v-if="!status.available" title="当前部署未配置 MFA 加密密钥，不能启用或强制管理员 MFA。" type="warning" :closable="false" show-icon />
    <section class="coverage-section" aria-labelledby="coverage-title">
      <h3 id="coverage-title">覆盖率</h3>
      <div class="coverage-grid">
        <div><span>有效管理员</span><strong>{{ status.coverage.active_administrators }}</strong></div>
        <div><span>已注册认证器</span><strong>{{ status.coverage.mfa_enrolled_administrators }}</strong></div>
        <div><span>本机恢复</span><strong>{{ status.coverage.local_recovery_available ? '可用' : '未配置' }}</strong></div>
      </div>
    </section>

    <section class="policy-section" aria-labelledby="policy-title">
      <div class="section-heading"><h3 id="policy-title">强制策略</h3><span>版本 {{ status.policy.version }}</span></div>
      <el-form label-position="top" @submit.prevent="save">
        <el-form-item label="管理员登录策略">
          <el-radio-group v-model="draft.mode">
            <el-radio label="off">不强制</el-radio>
            <el-radio label="enrollment_grace">注册宽限期</el-radio>
            <el-radio label="required">强制 MFA</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="draft.mode === 'enrollment_grace'" label="宽限期结束时间">
          <el-date-picker v-model="draft.graceEndsAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ss[Z]" placeholder="选择结束时间" />
        </el-form-item>
        <el-alert v-if="draft.mode === 'required'" title="只有所有有效管理员已注册 MFA 且本机恢复可用时，服务端才会接受强制策略。" type="warning" :closable="false" show-icon />
        <div class="form-actions"><el-button type="primary" native-type="submit" :loading="saving" :disabled="!status.available">保存策略</el-button></div>
      </el-form>
    </section>

    <el-dialog v-model="stepUpOpen" title="确认管理员身份" width="min(420px, calc(100% - 24px))" @closed="stepUpCode = ''">
      <el-form label-position="top" @submit.prevent="completeStepUp">
        <el-form-item label="认证器验证码"><el-input v-model.trim="stepUpCode" inputmode="numeric" autocomplete="one-time-code" maxlength="6" placeholder="000000" /></el-form-item>
        <div class="form-actions"><el-button @click="stepUpOpen = false">取消</el-button><el-button type="primary" native-type="submit" :loading="stepUpLoading">继续保存</el-button></div>
      </el-form>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { authApi, mfaPolicyApi, type MFAAdminPolicyStatus } from '@/modules/identity/api'
import { useAdminStore } from '@/modules/identity/store'

const loading = ref(false)
const saving = ref(false)
const stepUpOpen = ref(false)
const stepUpLoading = ref(false)
const stepUpCode = ref('')
const adminStore = useAdminStore()
const status = reactive<MFAAdminPolicyStatus>({
  policy: { id: 'admin', mode: 'off', version: 1, updated_at: '' },
  coverage: { active_administrators: 0, mfa_enrolled_administrators: 0, local_recovery_available: false },
  available: false,
})
const draft = reactive<{ mode: MFAAdminPolicyStatus['policy']['mode']; graceEndsAt: string }>({ mode: 'off', graceEndsAt: '' })
const messageOf = (error: any, fallback: string) => error?.msg || error?.message || fallback

const applyStatus = (value?: MFAAdminPolicyStatus) => {
  if (!value) return
  Object.assign(status.policy, value.policy)
  Object.assign(status.coverage, value.coverage)
  status.available = value.available
  draft.mode = value.policy.mode
  draft.graceEndsAt = value.policy.grace_ends_at || ''
}
const load = async () => {
  loading.value = true
  try { const result: any = await mfaPolicyApi.get(); applyStatus(result?.data) }
  catch (error: any) {
    if (error?.machineCode === 'identity.mfa.step_up_required') {
      stepUpOpen.value = true
      return
    }
    ElMessage.error(messageOf(error, '管理员 MFA 策略加载失败'))
  }
  finally { loading.value = false }
}
const save = async (skipConfirmation = false) => {
  if (draft.mode === 'enrollment_grace' && !draft.graceEndsAt) return ElMessage.warning('请选择宽限期结束时间')
  if (draft.mode === 'required' && !skipConfirmation) {
    try { await ElMessageBox.confirm('服务端会再次检查管理员覆盖率和本机恢复状态。', '确认强制 MFA', { type: 'warning' }) }
    catch { return }
  }
  saving.value = true
  try {
    const result: any = await mfaPolicyApi.update({
      mode: draft.mode,
      grace_ends_at: draft.mode === 'enrollment_grace' ? draft.graceEndsAt : undefined,
      expected_version: status.policy.version,
    })
    applyStatus(result?.data)
    ElMessage.success('管理员 MFA 策略已更新')
  } catch (error: any) {
    if (error?.error?.code === 'identity.mfa.step_up_required') {
      stepUpOpen.value = true
      return
    }
    ElMessage.error(messageOf(error, '管理员 MFA 策略更新失败'))
    await load()
  } finally { saving.value = false }
}
const completeStepUp = async () => {
  if (!/^\d{6}$/.test(stepUpCode.value)) return ElMessage.warning('请输入 6 位认证器验证码')
  stepUpLoading.value = true
  try {
    const result: any = await authApi.stepUpMFA({ code: stepUpCode.value })
    if (!result?.data?.access_token) throw new Error('身份确认结果不可用')
    adminStore.updateAccessToken(result.data.access_token)
    stepUpOpen.value = false
    await save(true)
  } catch (error: any) { ElMessage.error(messageOf(error, '认证器验证码无效或已使用')) }
  finally { stepUpLoading.value = false }
}
onMounted(load)
</script>

<style scoped>
.mfa-policy { max-width: 920px; margin: 0 auto; padding-bottom: 28px; }.page-heading,.section-heading,.form-actions { display: flex; align-items: center; }.page-heading { justify-content: space-between; gap: 16px; margin-bottom: 20px; }.page-heading h2,.section-heading h3 { margin: 0; }.eyebrow { margin: 0 0 4px; color: var(--el-color-primary); font-size: 12px; font-weight: 700; text-transform: uppercase; }.coverage-section,.policy-section { padding: 20px 0; border-bottom: 1px solid var(--el-border-color-lighter); }.coverage-section h3 { margin: 0 0 12px; }.coverage-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }.coverage-grid > div { display: grid; gap: 7px; padding: 14px; border: 1px solid var(--el-border-color-lighter); border-radius: 6px; }.coverage-grid span,.section-heading span { color: var(--el-text-color-secondary); font-size: 13px; }.coverage-grid strong { font-size: 20px; }.section-heading { justify-content: space-between; margin-bottom: 18px; }.form-actions { justify-content: flex-end; margin-top: 20px; }@media (max-width: 640px) { .coverage-grid { grid-template-columns: 1fr; }.form-actions .el-button { width: 100%; } }
</style>
