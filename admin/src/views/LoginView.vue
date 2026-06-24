<template>
  <div class="login-container">
    <el-card class="login-card">
      <template #header>
        <div class="login-header">
          <h2>🔧 CampusOS 管理后台</h2>
          <p>请使用管理员账号登录</p>
        </div>
      </template>
      <el-form :model="form" @submit.prevent="handleLogin" label-position="top">
        <el-form-item label="邮箱">
          <el-input v-model="form.email" placeholder="请输入管理员邮箱" prefix-icon="Message" size="large" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.password" type="password" placeholder="请输入密码" prefix-icon="Lock" size="large" show-password @keyup.enter="handleLogin" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" size="large" style="width: 100%" :loading="loading" @click="handleLogin">
            登录
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAdminStore } from '@/stores/admin'

const router = useRouter()
const route = useRoute()
const adminStore = useAdminStore()
const loading = ref(false)

const form = reactive({
  email: '',
  password: '',
})

const handleLogin = async () => {
  if (!form.email || !form.password) {
    ElMessage.warning('请输入邮箱和密码')
    return
  }
  loading.value = true
  try {
    const res = await adminStore.login(form.email, form.password)
    if (res.code === 0) {
      ElMessage.success('登录成功')
      const redirect = (route.query.redirect as string) || '/'
      router.push(redirect)
    } else {
      ElMessage.error(res.msg || '登录失败')
    }
  } catch (err: any) {
    ElMessage.error(err?.msg || '登录失败，请检查账号密码')
  }
  loading.value = false
}
</script>

<style scoped>
.login-container {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 100vh;
  background: linear-gradient(135deg, #304156 0%, #1a2332 100%);
}
.login-card {
  width: 420px;
}
.login-header {
  text-align: center;
}
.login-header h2 {
  margin: 0 0 8px 0;
  color: #303133;
}
.login-header p {
  margin: 0;
  color: #909399;
  font-size: 14px;
}
</style>
