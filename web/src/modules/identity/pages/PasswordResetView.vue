<template>
  <main class="reset-page">
    <section class="reset-surface" aria-labelledby="reset-title">
      <header class="page-heading">
        <h1 id="reset-title">重置密码</h1>
        <el-segmented v-model="mode" :options="modeOptions" />
      </header>

      <el-steps :active="step" finish-status="success" simple class="reset-steps">
        <el-step title="验证" />
        <el-step title="新密码" />
      </el-steps>

      <el-form v-if="step === 0" label-position="top" @submit.prevent="verifyCode">
        <el-form-item v-if="mode === 'password'" label="邮箱">
          <el-input v-model.trim="email" type="email" autocomplete="email" />
        </el-form-item>
        <el-form-item label="请求编号">
          <el-input v-model.trim="challengeID" autocomplete="off" />
        </el-form-item>
        <el-form-item label="验证码">
          <el-input v-model.trim="code" inputmode="numeric" maxlength="6" autocomplete="one-time-code" />
        </el-form-item>
        <div class="form-actions">
          <el-button v-if="mode === 'password'" plain :loading="sending" @click="requestCode">发送验证码</el-button>
          <el-button type="primary" native-type="submit" :loading="verifying">继续</el-button>
        </div>
      </el-form>

      <el-form v-else label-position="top" @submit.prevent="complete">
        <el-form-item label="新密码">
          <el-input v-model="password" type="password" show-password autocomplete="new-password" />
        </el-form-item>
        <el-form-item label="确认新密码">
          <el-input v-model="passwordAgain" type="password" show-password autocomplete="new-password" />
        </el-form-item>
        <div class="form-actions">
          <el-button plain @click="step = 0">上一步</el-button>
          <el-button type="primary" native-type="submit" :loading="saving">重置并重新登录</el-button>
        </div>
      </el-form>

      <footer><router-link to="/login">返回登录</router-link></footer>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useRouter } from 'vue-router'
import { authApi } from '@/modules/identity/api'

const router = useRouter()
const mode = ref<'password' | 'assisted'>('password')
const modeOptions = [
  { label: '邮箱找回', value: 'password' },
  { label: '辅助恢复', value: 'assisted' },
]
const email = ref('')
const challengeID = ref('')
const code = ref('')
const ticket = ref('')
const password = ref('')
const passwordAgain = ref('')
const step = ref(0)
const sending = ref(false)
const verifying = ref(false)
const saving = ref(false)

const canVerify = computed(
  () => challengeID.value && code.value.length === 6 && (mode.value === 'assisted' || email.value),
)

watch(mode, () => {
  challengeID.value = ''
  code.value = ''
  ticket.value = ''
  step.value = 0
})

const messageOf = (error: any, fallback: string) => error?.msg || error?.message || fallback

const requestCode = async () => {
  if (!email.value) return ElMessage.warning('请输入邮箱')
  sending.value = true
  try {
    const response: any = await authApi.requestPasswordReset({ email: email.value })
    if (response?.data?.challenge_id) challengeID.value = response.data.challenge_id
    ElMessage.success('如账号符合条件，验证码将发送至邮箱')
  } catch (error: any) {
    ElMessage.error(messageOf(error, '验证码请求暂时不可用'))
  } finally {
    sending.value = false
  }
}

const verifyCode = async () => {
  if (!canVerify.value) return ElMessage.warning('请填写完整的验证信息')
  verifying.value = true
  try {
    const response: any = await authApi.verifyPasswordReset({ challenge_id: challengeID.value, code: code.value })
    if (!response?.data?.ticket) throw new Error('验证未通过')
    ticket.value = response.data.ticket
    step.value = 1
  } catch (error: any) {
    ElMessage.error(messageOf(error, '验证码无效或已过期'))
  } finally {
    verifying.value = false
  }
}

const complete = async () => {
  if (password.value.length < 6 || password.value !== passwordAgain.value) {
    return ElMessage.warning('请确认两次密码一致，且不少于 6 位')
  }
  saving.value = true
  try {
    if (mode.value === 'assisted') {
      await authApi.completeAdminRecovery({
        challenge_id: challengeID.value,
        ticket: ticket.value,
        password: password.value,
      })
    } else {
      await authApi.completePasswordReset({
        email: email.value,
        challenge_id: challengeID.value,
        ticket: ticket.value,
        password: password.value,
      })
    }
    ElMessage.success('密码已更新')
    router.replace('/login')
  } catch (error: any) {
    ElMessage.error(messageOf(error, '重置请求无效或已过期'))
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.reset-page {
  display: grid;
  min-height: calc(100vh - 156px);
  place-items: start center;
  padding: 36px 16px;
}
.reset-surface {
  width: min(100%, 520px);
  padding: 24px;
  border: 1px solid var(--el-border-color-light);
  background: var(--el-bg-color);
}
.page-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 24px;
}
.page-heading h1 {
  margin: 0;
  color: var(--el-text-color-primary);
  font-size: 22px;
}
.reset-steps {
  margin: 0 0 24px;
}
.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
footer {
  margin-top: 24px;
  text-align: center;
}
@media (max-width: 600px) {
  .reset-page {
    padding: 16px 12px;
  }
  .reset-surface {
    padding: 18px;
  }
  .page-heading {
    align-items: flex-start;
    flex-direction: column;
  }
  .page-heading :deep(.el-segmented) {
    width: 100%;
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
