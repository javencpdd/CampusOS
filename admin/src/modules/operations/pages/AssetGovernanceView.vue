<template>
  <div class="asset-governance">
    <el-alert
      title="低敏附件治理"
      type="warning"
      :closable="false"
      show-icon
      description="此页面仅展示聚合状态与按附件标识执行的受控操作；不会列出文件名、文件内容、所有者、对象路径或下载地址。清除操作不可恢复。"
    />

    <el-card v-loading="loading" class="summary-card">
      <template #header>
        <div class="card-header">
          <span>资产生命周期概览</span>
          <el-button :loading="loading" @click="loadSummary">刷新</el-button>
        </div>
      </template>
      <el-row :gutter="16">
        <el-col
          v-for="status in summary.statuses"
          :key="status.status"
          :xs="24"
          :sm="12"
          :md="8"
          :lg="6"
        >
          <div class="metric">
            <span>{{ statusLabel(status.status) }}</span>
            <strong>{{ status.count }}</strong>
            <small>{{ formatBytes(status.size_bytes) }}</small>
          </div>
        </el-col>
        <el-col :xs="24" :sm="12" :md="8" :lg="6">
          <div class="metric">
            <span>过期预览调用</span>
            <strong>{{ summary.expired_invocations }}</strong>
            <small>需按既定保留策略清理</small>
          </div>
        </el-col>
      </el-row>
      <el-divider content-position="left">生命周期操作累计</el-divider>
      <el-space wrap>
        <el-tag
          v-for="action in summary.actions"
          :key="action.action"
          effect="plain"
        >
          {{ actionLabel(action.action) }}：{{ action.count }}
        </el-tag>
        <span v-if="summary.actions.length === 0" class="muted"
          >暂无生命周期操作记录</span
        >
      </el-space>
      <p class="generated-at">
        汇总生成时间：{{ formatTime(summary.generated_at) }}
      </p>
    </el-card>

    <el-card class="action-card">
      <template #header><span>按附件标识治理</span></template>
      <el-form label-position="top" @submit.prevent>
        <el-form-item label="附件标识">
          <el-input
            v-model.trim="assetID"
            placeholder="输入系统提供的附件 ID；本页不会搜索或枚举用户文件"
            maxlength="64"
          />
        </el-form-item>
        <el-form-item label="操作原因">
          <el-input
            v-model.trim="reason"
            type="textarea"
            :rows="3"
            maxlength="500"
            show-word-limit
            placeholder="必填，1 至 500 个字符；用于低敏生命周期审计"
          />
        </el-form-item>
        <el-space wrap>
          <el-button
            type="warning"
            :loading="acting === 'quarantine'"
            @click="quarantine"
            >隔离</el-button
          >
          <el-button :loading="acting === 'restore'" @click="restore"
            >恢复隔离附件</el-button
          >
          <el-button
            type="danger"
            plain
            :loading="previewing"
            @click="previewPurge"
            >检查清除条件</el-button
          >
        </el-space>
      </el-form>

      <el-alert
        v-if="preview"
        class="preview"
        :type="preview.eligible ? 'error' : 'info'"
        :closable="false"
        show-icon
      >
        <template #title
          >清除预检：{{
            preview.eligible ? "满足条件" : "暂不可清除"
          }}</template
        >
        <p>
          状态：{{ statusLabel(preview.status) }}；占用：{{
            formatBytes(preview.size_bytes)
          }}；文章引用：{{ preview.reference_count }}
        </p>
        <p>{{ preview.reason }}</p>
      </el-alert>
      <div v-if="preview?.eligible" class="purge-confirm">
        <el-input
          v-model.trim="confirmAssetID"
          placeholder="再次输入上方附件标识以确认不可恢复清除"
          maxlength="64"
        />
        <el-button type="danger" :loading="acting === 'purge'" @click="purge"
          >确认永久清除</el-button
        >
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { assetGovernanceApi } from "@/modules/operations/api";

type Status = { status: string; count: number; size_bytes: number };
type Action = { action: string; count: number };
type Preview = {
  asset_id: string;
  status: string;
  size_bytes: number;
  reference_count: number;
  eligible: boolean;
  reason: string;
};

const loading = ref(false);
const previewing = ref(false);
const acting = ref("");
const assetID = ref("");
const confirmAssetID = ref("");
const reason = ref("");
const preview = ref<Preview | null>(null);
const summary = reactive<{
  statuses: Status[];
  actions: Action[];
  expired_invocations: number;
  generated_at?: string;
}>({
  statuses: [],
  actions: [],
  expired_invocations: 0,
});

const statusNames: Record<string, string> = {
  active: "可用",
  trashed: "回收站",
  quarantined: "已隔离",
  purging: "清除中",
  deleted: "已删除",
};
const actionNames: Record<string, string> = {
  trashed: "移入回收站",
  restored: "恢复",
  quarantined: "隔离",
  purge_started: "开始清除",
  purge_failed: "清除失败",
  purged: "已永久清除",
};
const statusLabel = (value: string) => statusNames[value] || value;
const actionLabel = (value: string) => actionNames[value] || value;
const formatTime = (value?: string) =>
  value ? new Date(value).toLocaleString() : "-";
const formatBytes = (value = 0) =>
  value < 1024
    ? `${value} B`
    : value < 1024 ** 2
      ? `${(value / 1024).toFixed(1)} KiB`
      : `${(value / 1024 ** 2).toFixed(1)} MiB`;

const requestReady = () => {
  if (!assetID.value) {
    ElMessage.warning("请输入附件标识。");
    return false;
  }
  if (!reason.value || reason.value.length > 500) {
    ElMessage.warning("请输入 1 至 500 个字符的操作原因。");
    return false;
  }
  return true;
};

const loadSummary = async () => {
  loading.value = true;
  try {
    const result: any = await assetGovernanceApi.summary();
    Object.assign(summary, result?.data || {});
  } catch (error: any) {
    ElMessage.error(error?.msg || "读取附件治理汇总失败。");
  } finally {
    loading.value = false;
  }
};
const quarantine = async () => {
  if (!requestReady()) return;
  acting.value = "quarantine";
  try {
    await assetGovernanceApi.quarantine(assetID.value, {
      reason: reason.value,
    });
    ElMessage.success("附件已隔离，普通读取将立即拒绝。");
    await loadSummary();
  } catch (error: any) {
    ElMessage.error(error?.msg || "隔离失败。");
  } finally {
    acting.value = "";
  }
};
const restore = async () => {
  if (!requestReady()) return;
  acting.value = "restore";
  try {
    await assetGovernanceApi.restore(assetID.value, { reason: reason.value });
    ElMessage.success("附件已恢复为可用状态。");
    await loadSummary();
  } catch (error: any) {
    ElMessage.error(error?.msg || "恢复失败。");
  } finally {
    acting.value = "";
  }
};
const previewPurge = async () => {
  if (!requestReady()) return;
  previewing.value = true;
  confirmAssetID.value = "";
  try {
    const result: any = await assetGovernanceApi.previewPurge(assetID.value);
    preview.value = result?.data || null;
  } catch (error: any) {
    preview.value = null;
    ElMessage.error(error?.msg || "无法检查清除条件。");
  } finally {
    previewing.value = false;
  }
};
const purge = async () => {
  if (!requestReady() || !preview.value?.eligible) return;
  if (confirmAssetID.value !== assetID.value) {
    ElMessage.warning("请再次输入完全一致的附件标识以确认清除。");
    return;
  }
  try {
    await ElMessageBox.confirm(
      "将物理删除附件并释放空间，无法恢复。是否继续？",
      "确认永久清除",
      {
        type: "error",
        confirmButtonText: "永久清除",
        cancelButtonText: "取消",
      },
    );
  } catch {
    return;
  }
  acting.value = "purge";
  try {
    await assetGovernanceApi.purge(assetID.value, {
      reason: reason.value,
      confirm_asset_id: confirmAssetID.value,
    });
    ElMessage.success("附件已永久清除。");
    preview.value = null;
    confirmAssetID.value = "";
    await loadSummary();
  } catch (error: any) {
    ElMessage.error(
      error?.msg || "清除未完成；附件会保持不可访问状态，请在修复存储后重试。",
    );
  } finally {
    acting.value = "";
  }
};
onMounted(loadSummary);
</script>

<style scoped>
.asset-governance {
  display: grid;
  gap: 16px;
}
.summary-card,
.action-card {
  border-radius: 12px;
}
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.metric {
  min-height: 110px;
  display: grid;
  align-content: center;
  gap: 5px;
  padding: 16px;
  border-radius: 10px;
  background: var(--el-fill-color-light);
}
.metric strong {
  font-size: 28px;
  line-height: 1;
}
.metric small,
.muted,
.generated-at {
  color: var(--el-text-color-secondary);
}
.generated-at {
  margin: 16px 0 0;
  font-size: 13px;
}
.preview {
  margin-top: 20px;
}
.preview p {
  margin: 5px 0 0;
}
.purge-confirm {
  display: flex;
  gap: 12px;
  margin-top: 16px;
}
.purge-confirm .el-input {
  max-width: 440px;
}
@media (max-width: 640px) {
  .purge-confirm {
    flex-direction: column;
  }
  .purge-confirm .el-input {
    max-width: none;
  }
}
</style>
