<template>
  <div class="register-page">
    <el-card class="register-card">
      <template #header><h2>注册</h2></template>
      <el-form :model="form" @submit.prevent="handleRegister" label-position="top">
        <el-form-item label="用户名">
          <el-input v-model="form.username" placeholder="3-32位字符" />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="form.nickname" placeholder="显示名称" />
        </el-form-item>
        <el-form-item label="邮箱">
          <el-input v-model="form.email" placeholder="请输入邮箱" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.password" type="password" placeholder="至少6位" show-password />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleRegister" :loading="loading" style="width:100%">注册</el-button>
        </el-form-item>
      </el-form>
      <p class="tip">已有账号？<router-link to="/login">立即登录</router-link></p>
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
const form = reactive({ username: '', nickname: '', email: '', password: '' })

const handleRegister = async () => {
  if (!form.username || !form.nickname || !form.email || !form.password)
    return ElMessage.warning('请填写完整信息')
  loading.value = true
  try {
    const res: any = await userStore.register(form)
    if (res.code === 0) {
      ElMessage.success('注册成功，请登录')
      router.push('/login')
    }
  } catch (e: any) {
    ElMessage.error(e?.msg || '注册失败')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.register-page { display: flex; justify-content: center; padding-top: 60px; }
.register-card { width: 400px; }
.tip { text-align: center; color: #909399; }
</style>