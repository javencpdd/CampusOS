<template>
  <div class="login-container">
    <el-card class="login-card">
      <template #header>
        <div class="login-header">
          <h2>CampusOS 管理后台</h2>
          <p>{{ mfaTicket ? '请完成第二步验证' : '请使用管理员账号登录' }}</p>
        </div>
      </template>
      <el-form v-if="!mfaTicket" :model="form" @submit.prevent="handleLogin" label-position="top">
        <el-form-item label="邮箱">
          <el-input v-model.trim="form.email" type="email" autocomplete="email" placeholder="请输入管理员邮箱" size="large" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.password" type="password" autocomplete="current-password" placeholder="请输入密码" size="large" show-password @keyup.enter="handleLogin" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" size="large" native-type="submit" style="width: 100%" :loading="loading">登录</el-button>
        </el-form-item>
      </el-form>
      <el-form v-else :model="mfaForm" @submit.prevent="completeMFA" label-position="top">
        <p class="mfa-note">请输入认证器中显示的 6 位动态验证码。本次 MFA Ticket 不会写入本地存储。</p>
        <el-form-item label="动态验证码">
          <el-input v-model.trim="mfaForm.code" inputmode="numeric" autocomplete="one-time-code" maxlength="6" placeholder="000000" size="large" @keyup.enter="completeMFA" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" size="large" native-type="submit" style="width: 100%" :loading="loading">完成登录</el-button>
        </el-form-item>
        <el-button text @click="cancelMFA">返回账号密码登录</el-button>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAdminStore } from '@/modules/identity/store'

const router = useRouter()
const route = useRoute()
const adminStore = useAdminStore()
const loading = ref(false)
const form = reactive({ email: '', password: '' })
const mfaForm = reactive({ code: '' })
const mfaTicket = ref('')
const messageOf = (error: any, fallback: string) => error?.msg || error?.message || fallback

const finishLogin = () => {
  ElMessage.success('登录成功')
  router.push((route.query.redirect as string) || '/')
}

const handleLogin = async () => {
  if (!form.email || !form.password) return ElMessage.warning('请输入邮箱和密码')
  loading.value = true
  try {
    const response: any = await adminStore.login(form.email, form.password)
    if (response?.data?.mfa_required) {
      if (!response.data.mfa_ticket) throw new Error('本次多因素验证请求不可用，请重新登录')
      mfaTicket.value = response.data.mfa_ticket
      mfaForm.code = ''
      return
    }
    if (response?.code !== 0 || !adminStore.isLoggedIn) throw new Error(response?.msg || '登录失败')
    finishLogin()
  } catch (error: any) {
    if (error?.error?.code === 'identity.mfa.enrollment_required') {
      ElMessage.warning('此管理员尚未完成 MFA 注册。请先在普通用户端的“账号安全”中启用认证器。')
      return
    }
    ElMessage.error(messageOf(error, '登录失败，请检查账号密码'))
  } finally {
    loading.value = false
  }
}

const completeMFA = async () => {
  if (!/^\d{6}$/.test(mfaForm.code)) return ElMessage.warning('请输入 6 位动态验证码')
  loading.value = true
  try {
    const response: any = await adminStore.completeMFA(mfaTicket.value, mfaForm.code)
    if (response?.code !== 0 || !adminStore.isLoggedIn) throw new Error(response?.msg || '动态验证码无效')
    finishLogin()
  } catch (error: any) {
    ElMessage.error(messageOf(error, '动态验证码无效或已过期'))
  } finally {
    loading.value = false
  }
}

const cancelMFA = () => {
  mfaTicket.value = ''
  mfaForm.code = ''
}
</script>

<style scoped>
.login-container { display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 16px; background: #23313f; }
.login-card { width: min(100%, 420px); }
.login-header { text-align: center; }
.login-header h2 { margin: 0 0 8px; color: #303133; }
.login-header p { margin: 0; color: #909399; font-size: 14px; }
.mfa-note { margin: 0 0 18px; color: #606266; font-size: 14px; line-height: 1.6; }
</style>
