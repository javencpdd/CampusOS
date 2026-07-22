<template>
  <div class="login-page">
    <el-card class="login-card">
      <template #header
        ><h2>{{ mfaTicket ? '验证身份' : '登录' }}</h2></template
      >
      <el-form v-if="!mfaTicket" :model="form" @submit.prevent="handleLogin" label-position="top">
        <el-form-item label="邮箱">
          <el-input v-model.trim="form.email" type="email" autocomplete="email" placeholder="请输入邮箱" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input
            v-model="form.password"
            type="password"
            autocomplete="current-password"
            placeholder="请输入密码"
            show-password
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" native-type="submit" :loading="loading" style="width: 100%">登录</el-button>
        </el-form-item>
      </el-form>
      <el-form v-else :model="mfaForm" @submit.prevent="completeMFA" label-position="top">
        <p class="mfa-note">请输入认证器中显示的 6 位动态验证码。验证码和本次登录凭据只保留在当前页面内存中。</p>
        <el-form-item label="动态验证码">
          <el-input
            v-model.trim="mfaForm.code"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="6"
            placeholder="000000"
            @keyup.enter="completeMFA"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" native-type="submit" :loading="loading" style="width: 100%">完成登录</el-button>
        </el-form-item>
        <el-button text @click="cancelMFA">返回账号密码登录</el-button>
      </el-form>
      <p v-if="!mfaTicket" class="tip">
        <router-link to="/reset-password">找回密码</router-link> · 还没有账号？<router-link to="/register"
          >立即注册</router-link
        >
      </p>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useUserStore } from '@/modules/identity/store'
import { ElMessage } from 'element-plus'

const router = useRouter()
const userStore = useUserStore()
const loading = ref(false)
const form = reactive({ email: '', password: '' })
const mfaForm = reactive({ code: '' })
const mfaTicket = ref('')

const messageOf = (error: any, fallback: string) => error?.msg || error?.message || fallback

const handleLogin = async () => {
  if (!form.email || !form.password) return ElMessage.warning('请填写完整信息')
  loading.value = true
  try {
    const response: any = await userStore.login(form.email, form.password)
    if (response?.data?.mfa_required) {
      if (!response.data.mfa_ticket) throw new Error('本次多因素验证请求不可用，请重新登录')
      mfaTicket.value = response.data.mfa_ticket
      mfaForm.code = ''
      return
    }
    if (!userStore.isLoggedIn) throw new Error(response?.msg || '登录结果不可用')
    ElMessage.success('登录成功')
    router.push('/')
  } catch (error: any) {
    if (error?.error?.code === 'identity.mfa.enrollment_required') {
      ElMessage.warning('管理端已要求多因素认证。请先在普通用户端的“账号安全”中启用认证器。')
      return
    }
    ElMessage.error(messageOf(error, '登录失败'))
  } finally {
    loading.value = false
  }
}

const completeMFA = async () => {
  if (!/^\d{6}$/.test(mfaForm.code)) return ElMessage.warning('请输入 6 位动态验证码')
  loading.value = true
  try {
    const response: any = await userStore.completeMFA(mfaTicket.value, mfaForm.code)
    if (!userStore.isLoggedIn) throw new Error(response?.msg || '多因素验证结果不可用')
    ElMessage.success('登录成功')
    router.push('/')
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
.login-page {
  display: flex;
  justify-content: center;
  padding: 80px 16px 24px;
}
.login-card {
  width: min(100%, 400px);
}
.tip {
  color: #909399;
  text-align: center;
}
.mfa-note {
  margin: 0 0 18px;
  color: #606266;
  font-size: 14px;
  line-height: 1.6;
}
</style>
