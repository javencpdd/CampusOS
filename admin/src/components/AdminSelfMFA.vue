<template>
  <el-dialog
    :model-value="modelValue"
    title="确认管理员身份"
    width="min(440px, calc(100% - 24px))"
    :close-on-click-modal="false"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <p v-if="!admin.profile?.mfa_enrolled">
      为当前管理员配置认证器后，即可确认需要 MFA 的管理操作。
    </p>
    <template v-if="enrollmentURL">
      <p>
        <a :href="enrollmentURL">在认证器中添加当前管理员</a>，或手动输入密钥。
      </p>
      <el-input
        :model-value="enrollmentSecret"
        type="password"
        show-password
        readonly
        aria-label="管理员认证器密钥"
      />
      <p>输入认证器的验证码以完成配置。</p>
    </template>
    <el-alert v-if="notice" :title="notice" type="info" :closable="false" />
    <el-form
      v-if="admin.profile?.mfa_enrolled || enrollmentURL"
      label-position="top"
      @submit.prevent="submit"
    >
      <el-form-item label="认证器验证码">
        <el-input
          v-model.trim="code"
          inputmode="numeric"
          autocomplete="one-time-code"
          maxlength="6"
          placeholder="000000"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button
        v-if="!admin.profile?.mfa_enrolled && !enrollmentURL"
        type="primary"
        :loading="loading"
        @click="enroll"
        >开始配置管理员认证器</el-button
      >
      <el-button v-else type="primary" :loading="loading" @click="submit">{{
        enrollmentURL ? "完成配置" : "确认身份"
      }}</el-button>
    </template>
  </el-dialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { authApi } from "@/modules/identity/api";
import { useAdminStore } from "@/modules/identity/store";
const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ "update:modelValue": [boolean]; confirmed: [] }>();
const admin = useAdminStore();
const code = ref(""),
  enrollmentURL = ref(""),
  notice = ref(""),
  loading = ref(false);
let generation = 0;
const reset = () => {
  generation++;
  code.value = "";
  enrollmentURL.value = "";
  notice.value = "";
  loading.value = false;
};
watch(() => props.modelValue, reset, { flush: "sync" });
watch(() => admin.user?.id, reset, { flush: "sync" });
watch(() => admin.token, reset, { flush: "sync" });
onBeforeUnmount(reset);
const enrollmentSecret = computed(() => {
  try {
    return new URL(enrollmentURL.value).searchParams.get("secret") || "";
  } catch {
    return "";
  }
});
const context = () => ({
  epoch: generation,
  token: admin.token,
  id: admin.user?.id,
});
const current = (c: ReturnType<typeof context>) =>
  props.modelValue &&
  c.epoch === generation &&
  c.token === admin.token &&
  c.id === admin.user?.id;
async function enroll() {
  if (loading.value || !admin.isLoggedIn) return;
  const c = context();
  loading.value = true;
  try {
    const result: any = await authApi.enrollMFA();
    if (!current(c)) return;
    const uri = new URL(result?.data?.otpauth_url);
    if (
      result?.code !== 0 ||
      uri.protocol !== "otpauth:" ||
      uri.hostname !== "totp" ||
      !uri.searchParams.get("secret")
    )
      throw new Error("认证器配置结果不可用");
    enrollmentURL.value = uri.toString();
  } catch (error: any) {
    if (current(c))
      ElMessage.error(error?.msg || error?.message || "认证器配置失败");
  } finally {
    if (current(c)) loading.value = false;
  }
}
async function submit() {
  if (loading.value || !admin.isLoggedIn) return;
  if (!/^\d{6}$/.test(code.value)) {
    ElMessage.warning("请输入 6 位认证器验证码");
    return;
  }
  const c = context();
  loading.value = true;
  try {
    if (enrollmentURL.value) {
      const result: any = await authApi.confirmMFA({ code: code.value });
      if (!current(c)) return;
      if (
        result?.code !== 0 ||
        result.data?.status !== "active" ||
        !(await admin.restore()) ||
        !admin.profile?.mfa_enrolled
      )
        throw new Error("当前认证器配置状态不可用");
      if (!current(c)) return;
      enrollmentURL.value = "";
      code.value = "";
      notice.value =
        "配置完成。请等待认证器显示下一组验证码，再确认管理员身份。";
    } else {
      const result: any = await authApi.stepUpMFA({ code: code.value });
      if (!current(c)) return;
      if (
        result?.code !== 0 ||
        result.data?.authentication_strength !== "mfa" ||
        !(await admin.restore()) ||
        admin.profile?.authentication_strength !== "mfa"
      )
        throw new Error("当前管理员身份确认未完成");
      if (!current(c)) return;
      ElMessage.success("身份确认完成，请重试操作");
      emit("confirmed");
      emit("update:modelValue", false);
    }
  } catch (error: any) {
    if (current(c))
      ElMessage.error(error?.msg || error?.message || "验证码无效或已使用");
  } finally {
    if (current(c)) {
      code.value = "";
      loading.value = false;
    }
  }
}
</script>
