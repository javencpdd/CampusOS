<template>
  <div class="challenge-policy" v-loading="loading">
    <header class="page-heading">
      <div>
        <p class="eyebrow">Identity Security</p>
        <h2>验证码策略</h2>
      </div>
      <el-button
        :icon="Refresh"
        circle
        aria-label="刷新验证码策略"
        @click="load"
      />
    </header>

    <el-form label-position="top" class="policy-form" @submit.prevent="save">
      <section aria-labelledby="email-policy-title">
        <h3 id="email-policy-title">邮箱限制</h3>
        <div class="field-grid">
          <el-form-item>
            <template #label>
              <span class="field-label"
                >统计窗口（分钟）<el-tooltip
                  content="任意连续时间窗口内累计发送次数"
                  placement="top"
                  ><el-icon><QuestionFilled /></el-icon></el-tooltip
              ></span>
            </template>
            <el-input-number
              v-model="form.email_window_minutes"
              :min="1"
              :max="1440"
              controls-position="right"
            />
          </el-form-item>
          <el-form-item>
            <template #label>
              <span class="field-label"
                >最多发送（次）<el-tooltip
                  content="同一邮箱在统计窗口内允许创建的验证码数量"
                  placement="top"
                  ><el-icon><QuestionFilled /></el-icon></el-tooltip
              ></span>
            </template>
            <el-input-number
              v-model="form.email_max_requests"
              :min="1"
              :max="100"
              controls-position="right"
            />
          </el-form-item>
        </div>
      </section>

      <section aria-labelledby="ip-policy-title">
        <h3 id="ip-policy-title">IP 限制</h3>
        <div class="field-grid">
          <el-form-item>
            <template #label>
              <span class="field-label"
                >统计窗口（分钟）<el-tooltip
                  content="同一来源 IP 的连续统计窗口"
                  placement="top"
                  ><el-icon><QuestionFilled /></el-icon></el-tooltip
              ></span>
            </template>
            <el-input-number
              v-model="form.ip_window_minutes"
              :min="1"
              :max="1440"
              controls-position="right"
            />
          </el-form-item>
          <el-form-item>
            <template #label>
              <span class="field-label"
                >最多发送（次）<el-tooltip
                  content="同一来源 IP 在统计窗口内允许创建的验证码总数"
                  placement="top"
                  ><el-icon><QuestionFilled /></el-icon></el-tooltip
              ></span>
            </template>
            <el-input-number
              v-model="form.ip_max_requests"
              :min="1"
              :max="1000"
              controls-position="right"
            />
          </el-form-item>
        </div>
      </section>

      <footer class="policy-actions">
        <span v-if="policy" class="policy-meta"
          >版本 {{ policy.version }} · {{ formatTime(policy.updated_at) }}</span
        >
        <el-button
          type="primary"
          native-type="submit"
          :icon="Check"
          :loading="saving"
          :disabled="!policy"
          >应用策略</el-button
        >
      </footer>
    </el-form>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { ElMessage } from "element-plus";
import { Check, QuestionFilled, Refresh } from "@element-plus/icons-vue";
import {
  challengePolicyApi,
  type ChallengePolicy,
} from "@/modules/identity/api";

const loading = ref(false);
const saving = ref(false);
const policy = ref<ChallengePolicy | null>(null);
const form = reactive({
  email_window_minutes: 10,
  email_max_requests: 5,
  ip_window_minutes: 60,
  ip_max_requests: 10,
});

const messageOf = (error: any, fallback: string) =>
  error?.msg || error?.message || fallback;
const formatTime = (value?: string) =>
  value ? new Date(value).toLocaleString() : "未更新";

const applyPolicy = (value: ChallengePolicy) => {
  policy.value = value;
  form.email_window_minutes = value.email_window_minutes;
  form.email_max_requests = value.email_max_requests;
  form.ip_window_minutes = value.ip_window_minutes;
  form.ip_max_requests = value.ip_max_requests;
};

const load = async () => {
  loading.value = true;
  try {
    const response: any = await challengePolicyApi.get();
    if (!response?.data?.id) throw new Error("策略数据不可用");
    applyPolicy(response.data);
  } catch (error: any) {
    ElMessage.error(messageOf(error, "验证码策略加载失败"));
  } finally {
    loading.value = false;
  }
};

const save = async () => {
  if (!policy.value) return;
  saving.value = true;
  try {
    const response: any = await challengePolicyApi.update({
      ...form,
      expected_version: policy.value.version,
    });
    if (!response?.data?.id) throw new Error("策略更新结果不可用");
    applyPolicy(response.data);
    ElMessage.success("验证码策略已应用");
  } catch (error: any) {
    ElMessage.error(messageOf(error, "验证码策略更新失败"));
    await load();
  } finally {
    saving.value = false;
  }
};

onMounted(load);
</script>

<style scoped>
.challenge-policy {
  max-width: 960px;
  margin: 0 auto;
}
.page-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 24px;
}
.page-heading h2,
.policy-form h3 {
  margin: 0;
}
.eyebrow {
  margin: 0 0 4px;
  color: var(--el-color-primary);
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
}
.policy-form section {
  padding: 20px 0 8px;
  border-top: 1px solid var(--el-border-color-lighter);
}
.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 18px;
  margin-top: 18px;
}
.field-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.field-label .el-icon {
  color: var(--el-text-color-secondary);
  cursor: help;
}
.field-grid :deep(.el-input-number) {
  width: 100%;
}
.policy-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding-top: 20px;
  border-top: 1px solid var(--el-border-color-lighter);
}
.policy-meta {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
@media (max-width: 720px) {
  .field-grid {
    grid-template-columns: 1fr;
    gap: 0;
  }
  .policy-actions {
    align-items: stretch;
    flex-direction: column;
  }
  .policy-actions .el-button {
    width: 100%;
    margin: 0;
  }
}
</style>
