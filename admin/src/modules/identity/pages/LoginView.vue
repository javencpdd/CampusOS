<template>
  <div class="login-container">
    <el-card class="login-card">
      <template #header
        ><div class="login-header">
          <h2>CampusOS 管理后台</h2>
          <p>使用管理员用户名或邮箱登录</p>
        </div></template
      >
      <el-form :model="form" @submit.prevent="handleLogin" label-position="top">
        <el-form-item label="管理员用户名或邮箱"
          ><el-input
            v-model.trim="form.username"
            autocomplete="username"
            placeholder="请输入管理员用户名或邮箱"
            size="large"
        /></el-form-item>
        <el-form-item label="密码"
          ><el-input
            v-model="form.password"
            type="password"
            autocomplete="current-password"
            placeholder="请输入密码"
            size="large"
            show-password
        /></el-form-item>
        <el-form-item
          ><el-button
            type="primary"
            size="large"
            native-type="submit"
            style="width: 100%"
            :loading="loading"
            >登录</el-button
          ></el-form-item
        >
      </el-form>
    </el-card>
  </div>
</template>
<script setup lang="ts">
import { reactive, ref } from "vue";
import { useRouter, useRoute } from "vue-router";
import { ElMessage } from "element-plus";
import { useAdminStore } from "@/modules/identity/store";
const router = useRouter(),
  route = useRoute(),
  adminStore = useAdminStore();
const loading = ref(false),
  form = reactive({ username: "", password: "" });
async function handleLogin() {
  if (loading.value) return;
  if (!form.username || !form.password) {
    ElMessage.warning("请输入管理员用户名或邮箱和密码");
    return;
  }
  loading.value = true;
  try {
    const result: any = await adminStore.login(form.username, form.password);
    if (result?.code !== 0 || !adminStore.isLoggedIn)
      throw new Error(result?.msg || "当前管理员身份不可用");
    ElMessage.success("登录成功");
    const redirect =
      typeof route.query.redirect === "string" ? route.query.redirect : "/";
    await router.push(
      redirect.startsWith("/") &&
        !redirect.startsWith("//") &&
        !redirect.startsWith("/login")
        ? redirect
        : "/",
    );
  } catch (error: any) {
    ElMessage.error(
      error?.msg || error?.message || "登录失败，请检查管理员账号密码",
    );
  } finally {
    form.password = "";
    loading.value = false;
  }
}
</script>

<style scoped>
.login-container {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  padding: 16px;
  background: #23313f;
}
.login-card {
  width: min(100%, 420px);
}
.login-header {
  text-align: center;
}
.login-header h2 {
  margin: 0 0 8px;
  color: #303133;
}
.login-header p {
  margin: 0;
  color: #909399;
  font-size: 14px;
}
.mfa-note {
  margin: 0 0 18px;
  color: #606266;
  font-size: 14px;
  line-height: 1.6;
}
</style>
