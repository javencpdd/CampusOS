<template>
  <div class="login-page">
    <el-card class="login-card">
      <template #header><h2>登录</h2></template>
      <el-form :model="form" @submit.prevent="handleLogin" label-position="top">
        <el-form-item label="邮箱">
          <el-input v-model="form.email" placeholder="请输入邮箱" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.password" type="password" placeholder="请输入密码" show-password />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleLogin" :loading="loading" style="width:100%">登录</el-button>
        </el-form-item>
      </el-form>
      <p class="tip">还没有账号？<router-link to="/register">立即注册</router-link></p>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { ElMessage } from 'element-plus'

const router = useRouter()
const userStore = useUserStore()
const loading = ref(false)
const form = reactive({ email: '', password: '' })

const handleLogin = async () => {
  if (!form.email || !form.password) return ElMessage.warning('请填写完整信息')
  loading.value = true
  try {
    await userStore.login(form.email, form.password)
    ElMessage.success('登录成功')
    router.push('/')
  } catch (e: any) {
    ElMessage.error(e?.msg || '登录失败')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-page { display: flex; justify-content: center; padding-top: 80px; }
.login-card { width: 400px; }
.tip { text-align: center; color: #909399; }
</style>