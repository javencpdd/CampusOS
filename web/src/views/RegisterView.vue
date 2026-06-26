<template>
  <div class="register-page">
    <el-card class="register-card">
      <template #header>
        <h2>注册</h2>
      </template>
      <el-form ref="formRef" :model="form" :rules="rules" @submit.prevent="handleRegister" label-position="top"
        status-icon>
        <el-form-item label="用户名" prop="username">
          <el-input v-model="form.username" placeholder="3-32位，仅支持字母、数字、下划线" maxlength="32" show-word-limit />
          <div class="field-hint">用于登录，注册后不可修改</div>
        </el-form-item>

        <el-form-item label="昵称" prop="nickname">
          <el-input v-model="form.nickname" placeholder="2-64位，将作为显示名称" maxlength="64" show-word-limit />
          <div class="field-hint">其他用户看到的名称，可随时修改</div>
        </el-form-item>

        <el-form-item label="邮箱" prop="email">
          <el-input v-model="form.email" placeholder="example@email.com" maxlength="255" />
          <div class="field-hint">用于接收通知和找回密码</div>
        </el-form-item>

        <el-form-item label="密码" prop="password">
          <el-input v-model="form.password" type="password" placeholder="6-32位，建议包含字母和数字" maxlength="32"
            show-password />
          <div class="field-hint">至少6个字符，建议使用字母+数字组合</div>
        </el-form-item>

        <el-form-item label="确认密码" prop="confirmPassword">
          <el-input v-model="form.confirmPassword" type="password" placeholder="请再次输入密码" maxlength="32" show-password />
        </el-form-item>

        <el-form-item>
          <el-button type="primary" native-type="submit" :loading="loading" style="width: 100%">
            注册
          </el-button>
        </el-form-item>
      </el-form>
      <p class="tip">已有账号？<router-link to="/login">立即登录</router-link></p>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { useUserStore } from '@/stores/user'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage } from 'element-plus'
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const userStore = useUserStore()
const loading = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  username: '',
  nickname: '',
  email: '',
  password: '',
  confirmPassword: '',
})

// 自定义校验：确认密码
const validateConfirmPassword = (_rule: any, value: string, callback: any) => {
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
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { min: 6, max: 32, message: '密码长度需在 6-32 个字符之间', trigger: 'blur' },
  ],
  confirmPassword: [
    { required: true, message: '请再次输入密码', trigger: 'blur' },
    { validator: validateConfirmPassword, trigger: 'blur' },
  ],
}

const handleRegister = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async (valid) => {
    if (!valid) return

    loading.value = true
    try {
      const res: any = await userStore.register({
        username: form.username,
        nickname: form.nickname,
        email: form.email,
        password: form.password,
      })
      if (res.code === 0) {
        ElMessage.success('注册成功，请登录')
        router.push('/login')
      }
    } catch (e: any) {
      ElMessage.error(e?.msg || '注册失败，请检查输入信息')
    } finally {
      loading.value = false
    }
  })
}
</script>

<style scoped>
.register-page {
  display: flex;
  justify-content: center;
  padding-top: 60px;
}

.register-card {
  width: 450px;
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
</style>