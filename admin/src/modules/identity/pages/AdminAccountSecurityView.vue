<template>
  <section class="account-security">
    <h2>修改管理员密码</h2>
    <p>
      仅修改当前管理员的登录密码。修改成功后，当前管理员的所有会话将退出，其他账号及系统配置保持原样。
    </p>
    <el-form :model="form" label-position="top" @submit.prevent="submit">
      <el-form-item label="当前密码">
        <el-input
          v-model="form.current"
          type="password"
          autocomplete="current-password"
          placeholder="请输入当前密码"
          show-password
        />
      </el-form-item>
      <el-form-item label="新密码">
        <el-input
          v-model="form.next"
          type="password"
          autocomplete="new-password"
          placeholder="请输入6–64字节的新密码"
          show-password
        />
      </el-form-item>
      <el-form-item label="确认新密码">
        <el-input
          v-model="form.repeat"
          type="password"
          autocomplete="new-password"
          placeholder="请再次输入新密码"
          show-password
        />
      </el-form-item>
      <el-button type="primary" native-type="submit" :loading="saving"
        >修改密码并重新登录</el-button
      >
    </el-form>
  </section>
</template>

<script setup lang="ts">
import { reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { authApi } from "@/modules/identity/api";
import { useAdminStore } from "@/modules/identity/store";

const router = useRouter();
const admin = useAdminStore();
const saving = ref(false);
const form = reactive({ current: "", next: "", repeat: "" });

async function submit() {
  if (saving.value) return;
  if (!form.current) return ElMessage.warning("请输入当前密码");
  const bytes = new TextEncoder().encode(form.next).length;
  if (bytes < 6 || bytes > 64 || form.current === form.next)
    return ElMessage.warning("新密码须为6–64字节，且不能与当前密码相同");
  if (form.next !== form.repeat) return ElMessage.warning("两次新密码不一致");
  saving.value = true;
  try {
    const result: any = await authApi.changePassword({
      current_password: form.current,
      new_password: form.next,
    });
    if (result?.code !== 0) throw new Error(result?.msg || "密码修改失败");
    admin.clearSession();
    ElMessage.success("密码已修改，请使用新密码重新登录");
    await router.replace("/login");
  } catch (error: any) {
    ElMessage.error(error?.msg || error?.message || "密码修改失败");
  } finally {
    form.current = form.next = form.repeat = "";
    saving.value = false;
  }
}
</script>

<style scoped>
.account-security {
  max-width: 560px;
}
.account-security p {
  color: #606266;
  line-height: 1.7;
  margin: 16px 0 24px;
}
</style>
