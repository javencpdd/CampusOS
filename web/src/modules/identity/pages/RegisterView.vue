<template>
  <div class="register-page">
    <el-card class="register-card">
      <template #header>
        <h2>注册</h2>
      </template>
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        @submit.prevent="handleRegister"
        label-position="top"
        status-icon
      >
        <el-form-item label="用户名" prop="username">
          <el-input
            v-model="form.username"
            placeholder="3-32位，仅支持字母、数字、下划线"
            maxlength="32"
            show-word-limit
          />
          <div class="field-hint">用于登录，注册后不可修改</div>
        </el-form-item>

        <el-form-item label="昵称" prop="nickname">
          <el-input v-model="form.nickname" placeholder="2-64位，将作为显示名称" maxlength="64" show-word-limit />
          <div class="field-hint">其他用户看到的名称，可随时修改</div>
        </el-form-item>

        <el-form-item label="邮箱" prop="email">
          <div class="verification-row">
            <el-input
              v-model="form.email"
              :disabled="Boolean(challengeId)"
              placeholder="example@email.com"
              maxlength="255"
              @input="clearChallenge"
            />
            <el-button v-if="challengeId" plain @click="clearChallenge">更换邮箱</el-button>
            <el-button
              v-else
              type="primary"
              :loading="sendingCode"
              :disabled="cooldown > 0"
              @click="sendVerificationCode"
            >
              {{ cooldown > 0 ? `${cooldown}s` : '发送验证码' }}
            </el-button>
          </div>
          <div class="field-hint">验证完成后才会创建账号</div>
        </el-form-item>

        <el-form-item label="邮箱验证码" prop="code">
          <el-input
            v-model="form.code"
            :disabled="!challengeId"
            inputmode="numeric"
            maxlength="6"
            placeholder="请输入 6 位验证码"
          />
        </el-form-item>

        <el-form-item label="密码" prop="password">
          <el-input
            v-model="form.password"
            type="password"
            placeholder="6-32位，建议包含字母和数字"
            maxlength="32"
            show-password
          />
          <div class="field-hint">至少6个字符，建议使用字母+数字组合</div>
        </el-form-item>

        <el-form-item label="确认密码" prop="confirmPassword">
          <el-input
            v-model="form.confirmPassword"
            type="password"
            placeholder="请再次输入密码"
            maxlength="32"
            show-password
          />
        </el-form-item>

        <el-form-item>
          <el-button type="primary" native-type="submit" :loading="loading" style="width: 100%">注册</el-button>
        </el-form-item>
      </el-form>
      <p class="tip">已有账号？<router-link to="/login">立即登录</router-link></p>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { useUserStore } from '@/modules/identity/store'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage } from 'element-plus'
import { onBeforeUnmount, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const userStore = useUserStore()
const loading = ref(false)
const sendingCode = ref(false)
const formRef = ref<FormInstance>()
const challengeId = ref('')
const registrationTicket = ref('')
const cooldown = ref(0)
let countdownTimer: ReturnType<typeof setInterval> | undefined

const form = reactive({
  username: '',
  nickname: '',
  email: '',
  code: '',
  password: '',
  confirmPassword: '',
})

const validateConfirmPassword = (_rule: unknown, value: string, callback: (error?: Error) => void) => {
  if (value === '') {
    callback(new Error('请再次输入密码'))
  } else if (value !== form.password) {
    callback(new Error('两次输入的密码不一致'))
  } else {
    callback()
  }
}

const rules: FormRules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    { min: 3, max: 32, message: '用户名长度需在 3-32 个字符之间', trigger: 'blur' },
    { pattern: /^[a-zA-Z0-9_]+$/, message: '用户名仅支持字母、数字和下划线', trigger: 'blur' },
  ],
  nickname: [
    { required: true, message: '请输入昵称', trigger: 'blur' },
    { min: 2, max: 64, message: '昵称长度需在 2-64 个字符之间', trigger: 'blur' },
  ],
  email: [
    { required: true, message: '请输入邮箱地址', trigger: 'blur' },
    { type: 'email', message: '请输入正确的邮箱格式，如 example@email.com', trigger: 'blur' },
  ],
  code: [
    { required: true, message: '请输入邮箱验证码', trigger: 'blur' },
    { pattern: /^\d{6}$/, message: '验证码为 6 位数字', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { min: 6, max: 32, message: '密码长度需在 6-32 个字符之间', trigger: 'blur' },
  ],
  confirmPassword: [
    { required: true, message: '请再次输入密码', trigger: 'blur' },
    { validator: validateConfirmPassword, trigger: 'blur' },
  ],
}

const clearChallenge = () => {
  challengeId.value = ''
  registrationTicket.value = ''
  form.code = ''
}

const startCooldown = () => {
  cooldown.value = 60
  if (countdownTimer) window.clearInterval(countdownTimer)
  countdownTimer = window.setInterval(() => {
    cooldown.value = Math.max(cooldown.value - 1, 0)
    if (cooldown.value === 0 && countdownTimer) {
      window.clearInterval(countdownTimer)
      countdownTimer = undefined
    }
  }, 1000)
}

const sendVerificationCode = async () => {
  if (!formRef.value) return
  try {
    await formRef.value.validateField('email')
  } catch {
    return
  }

  sendingCode.value = true
  try {
    const res: any = await userStore.requestRegistrationChallenge(form.email)
    if (res.code !== 0 || !res.data?.challenge_id) {
      ElMessage.error(res.msg || '验证码发送请求未被接受')
      return
    }
    challengeId.value = res.data.challenge_id
    registrationTicket.value = ''
    form.code = ''
    startCooldown()
    ElMessage.success('验证码发送请求已提交，请查收邮箱')
  } catch (e: any) {
    ElMessage.error(e?.error?.message || e?.msg || '验证码发送请求未被接受')
  } finally {
    sendingCode.value = false
  }
}

const handleRegister = async () => {
  if (!formRef.value) return
  try {
    await formRef.value.validate()
  } catch {
    return
  }
  if (!challengeId.value) {
    ElMessage.warning('请先发送邮箱验证码')
    return
  }

  loading.value = true
  try {
    if (!registrationTicket.value) {
      const verified: any = await userStore.verifyRegistrationChallenge(challengeId.value, form.code)
      if (verified.code !== 0 || !verified.data?.ticket) {
        ElMessage.error(verified.msg || '验证码无效或已过期')
        return
      }
      registrationTicket.value = verified.data.ticket
    }
    const res: any = await userStore.register({
      username: form.username,
      nickname: form.nickname,
      email: form.email,
      password: form.password,
      challenge_id: challengeId.value,
      ticket: registrationTicket.value,
    })
    if (res.code === 0) {
      ElMessage.success('注册成功，请登录')
      router.push('/login')
    }
  } catch (e: any) {
    ElMessage.error(e?.error?.message || e?.msg || '注册失败，请检查输入信息')
  } finally {
    loading.value = false
  }
}

onBeforeUnmount(() => {
  if (countdownTimer) window.clearInterval(countdownTimer)
})
</script>

<style scoped>
.register-page {
  display: flex;
  justify-content: center;
  padding-top: 60px;
}

.register-card {
  width: min(450px, calc(100vw - 32px));
}

.verification-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  width: 100%;
}

.tip {
  text-align: center;
  color: #909399;
}

.field-hint {
  font-size: 12px;
  color: #909399;
  line-height: 1.5;
  margin-top: 4px;
}

@media (max-width: 480px) {
  .register-page {
    padding-top: 24px;
  }

  .verification-row {
    grid-template-columns: 1fr;
  }

  .verification-row .el-button {
    width: 100%;
  }
}
</style>
