<template>
  <section class="plugin-center" aria-labelledby="plugin-center-title">
    <div class="page-heading">
      <div>
        <h1 id="plugin-center-title">插件中心</h1>
        <p>管理已安装 v2 外部插件的目录可见性、用户请求和发布记录。</p>
      </div>
      <el-button :loading="loading" @click="load">
        <el-icon><Refresh /></el-icon>
        刷新
      </el-button>
    </div>

    <el-alert
      title="目录只展示通过受管数据合同的外部插件。用户授权、文件和记录均由宿主保存，插件不能直接访问数据库。"
      type="info"
      show-icon
      :closable="false"
    />

    <div class="metric-grid" aria-label="插件中心统计">
      <div class="metric">
        <span>目录插件</span><strong>{{ items.length }}</strong>
      </div>
      <div class="metric">
        <span>用户授权</span><strong>{{ totalUsers }}</strong>
      </div>
      <div class="metric">
        <span>受管记录</span><strong>{{ totalRecords }}</strong>
      </div>
      <div class="metric">
        <span>受管文件</span><strong>{{ totalFiles }}</strong>
      </div>
    </div>

    <section class="workspace-section" aria-labelledby="catalog-title">
      <div class="section-heading">
        <div>
          <h2 id="catalog-title">目录与数据概览</h2>
          <p>发布后，用户可在前台查看并按声明授权。</p>
        </div>
      </div>
      <el-table v-loading="loading" :data="items" class="desktop-table" stripe>
        <el-table-column label="插件" min-width="190">
          <template #default="{ row }"
            ><strong>{{
              row.catalog.display_name || row.catalog.plugin_name
            }}</strong
            ><small>{{ row.catalog.plugin_name }}</small></template
          >
        </el-table-column>
        <el-table-column label="版本 / 运行时" min-width="155"
          ><template #default="{ row }"
            >v{{ row.catalog.version }} · {{ row.catalog.runtime
            }}<small>{{ runtimeLabel(row.runtime_state) }}</small></template
          ></el-table-column
        >
        <el-table-column label="可见性" width="130"
          ><template #default="{ row }"
            ><el-tag
              :type="visibilityType(row.catalog.visibility)"
              effect="plain"
              >{{ visibilityLabel(row.catalog.visibility) }}</el-tag
            ></template
          ></el-table-column
        >
        <el-table-column label="用户 / 记录 / 文件" min-width="185"
          ><template #default="{ row }"
            >{{ row.metrics.user_count }} / {{ row.metrics.record_count }} /
            {{ row.metrics.file_count
            }}<small
              >{{ formatBytes(row.metrics.file_bytes || 0) }} ·
              {{ row.system_permissions?.length || 0 }} 项系统权限</small
            ></template
          ></el-table-column
        >
        <el-table-column label="操作" width="280" fixed="right"
          ><template #default="{ row }">
            <el-button
              size="small"
              @click="setVisibility(row.catalog.plugin_name, 'published')"
              >发布</el-button
            >
            <el-button
              size="small"
              @click="setVisibility(row.catalog.plugin_name, 'draft')"
              >草稿</el-button
            >
            <el-button
              size="small"
              type="warning"
              plain
              @click="setVisibility(row.catalog.plugin_name, 'hidden')"
              >隐藏</el-button
            >
            <el-button
              size="small"
              text
              @click="showReleases(row.catalog.plugin_name)"
              >发布记录</el-button
            >
          </template></el-table-column
        >
      </el-table>
      <div v-if="!loading" class="mobile-list">
        <article
          v-for="row in items"
          :key="row.catalog.plugin_name"
          class="plugin-row"
        >
          <div class="row-summary">
            <strong>{{
              row.catalog.display_name || row.catalog.plugin_name
            }}</strong
            ><el-tag
              :type="visibilityType(row.catalog.visibility)"
              size="small"
              effect="plain"
              >{{ visibilityLabel(row.catalog.visibility) }}</el-tag
            >
          </div>
          <p>{{ row.catalog.description || "未提供说明" }}</p>
          <dl>
            <div>
              <dt>版本</dt>
              <dd>v{{ row.catalog.version }}</dd>
            </div>
            <div>
              <dt>运行状态</dt>
              <dd>{{ runtimeLabel(row.runtime_state) }}</dd>
            </div>
            <div>
              <dt>数据</dt>
              <dd>
                {{ row.metrics.user_count }} 用户 ·
                {{ row.metrics.record_count }} 记录 ·
                {{ row.metrics.file_count }} 文件 ·
                {{ formatBytes(row.metrics.file_bytes || 0) }}
              </dd>
            </div>
            <div>
              <dt>系统权限</dt>
              <dd>{{ row.system_permissions?.length || 0 }} 项</dd>
            </div>
          </dl>
          <div class="row-actions">
            <el-button
              size="small"
              @click="setVisibility(row.catalog.plugin_name, 'published')"
              >发布</el-button
            ><el-button
              size="small"
              @click="setVisibility(row.catalog.plugin_name, 'draft')"
              >草稿</el-button
            ><el-button
              size="small"
              text
              @click="showReleases(row.catalog.plugin_name)"
              >发布记录</el-button
            >
          </div>
        </article>
      </div>
      <el-empty
        v-if="!loading && !items.length"
        description="没有可管理的 v2 外部插件"
      />
    </section>

    <section class="workspace-section" aria-labelledby="audits-title">
      <div class="section-heading">
        <div>
          <h2 id="audits-title">近期治理审计</h2>
          <p>记录目录发布、用户授权、数据操作、导入发布和越权结果。</p>
        </div>
      </div>
      <el-table
        v-loading="auditsLoading"
        :data="audits"
        class="desktop-table"
        stripe
      >
        <el-table-column
          prop="plugin_name"
          label="插件"
          min-width="150"
        /><el-table-column
          prop="action"
          label="操作"
          min-width="170"
        /><el-table-column
          prop="actor_id"
          label="操作者"
          min-width="130"
        /><el-table-column
          prop="outcome"
          label="结果"
          width="100"
        /><el-table-column label="时间" min-width="170"
          ><template #default="{ row }">{{
            formatTime(row.created_at)
          }}</template></el-table-column
        >
      </el-table>
      <div v-if="!auditsLoading" class="mobile-list">
        <article v-for="audit in audits" :key="audit.id" class="plugin-row">
          <div class="row-summary">
            <strong>{{ audit.plugin_name }}</strong
            ><el-tag size="small" effect="plain">{{ audit.outcome }}</el-tag>
          </div>
          <p>{{ audit.action }}</p>
          <small
            >{{ audit.actor_id || "system" }} ·
            {{ formatTime(audit.created_at) }}</small
          >
        </article>
      </div>
      <el-empty
        v-if="!auditsLoading && !audits.length"
        description="暂无市场治理审计"
      />
    </section>

    <section class="workspace-section" aria-labelledby="requests-title">
      <div class="section-heading">
        <div>
          <h2 id="requests-title">用户安装请求</h2>
          <p>请求获批不会自动安装包，只记录管理员的目录审核结果。</p>
        </div>
      </div>
      <el-table
        v-loading="requestsLoading"
        :data="requests"
        class="desktop-table"
        stripe
      >
        <el-table-column
          prop="plugin_name"
          label="插件"
          min-width="150"
        /><el-table-column
          prop="user_id"
          label="用户"
          min-width="130"
        /><el-table-column
          prop="message"
          label="说明"
          min-width="220"
          show-overflow-tooltip
        /><el-table-column prop="status" label="状态" width="100" />
        <el-table-column label="处理" width="180"
          ><template #default="{ row }"
            ><el-button
              size="small"
              type="success"
              :disabled="row.status !== 'pending'"
              @click="review(row.id, 'approved')"
              >批准</el-button
            ><el-button
              size="small"
              type="danger"
              plain
              :disabled="row.status !== 'pending'"
              @click="review(row.id, 'rejected')"
              >拒绝</el-button
            ></template
          ></el-table-column
        >
      </el-table>
      <div v-if="!requestsLoading" class="mobile-list">
        <article
          v-for="request in requests"
          :key="request.id"
          class="plugin-row"
        >
          <div class="row-summary">
            <strong>{{ request.plugin_name }}</strong
            ><el-tag size="small" effect="plain">{{ request.status }}</el-tag>
          </div>
          <p>{{ request.message || "未填写说明" }}</p>
          <small>用户 {{ request.user_id }}</small>
          <div class="row-actions">
            <el-button
              size="small"
              type="success"
              :disabled="request.status !== 'pending'"
              @click="review(request.id, 'approved')"
              >批准</el-button
            ><el-button
              size="small"
              type="danger"
              plain
              :disabled="request.status !== 'pending'"
              @click="review(request.id, 'rejected')"
              >拒绝</el-button
            >
          </div>
        </article>
      </div>
      <el-empty
        v-if="!requestsLoading && !requests.length"
        description="暂无用户安装请求"
      />
    </section>

    <el-dialog
      v-model="releaseDialog"
      :title="`${releasePluginName} 发布记录`"
      width="min(720px, calc(100vw - 24px))"
    >
      <el-table :data="releases" max-height="360"
        ><el-table-column prop="version" label="版本" /><el-table-column
          prop="channel"
          label="通道" /><el-table-column
          prop="signature_state"
          label="签名状态" /><el-table-column
          prop="rollout_state"
          label="发布状态" /><el-table-column
          prop="checksum"
          label="校验和"
          show-overflow-tooltip
      /></el-table>
      <el-form :model="releaseForm" label-position="top" class="release-form"
        ><el-form-item label="版本"
          ><el-input v-model="releaseForm.version" /></el-form-item
        ><el-form-item label="校验和"
          ><el-input v-model="releaseForm.checksum" /></el-form-item
        ><el-form-item label="签名状态"
          ><el-select v-model="releaseForm.signature_state"
            ><el-option label="未签名" value="unsigned" /><el-option
              label="待验签"
              value="pending" /></el-select
          ><small
            >“已验证”仅能由宿主导入时的实际签名校验写入。</small
          ></el-form-item
        ><el-form-item label="通道"
          ><el-select v-model="releaseForm.channel"
            ><el-option label="稳定" value="stable" /><el-option
              label="测试"
              value="beta" /></el-select></el-form-item
        ><el-form-item label="发布状态"
          ><el-select v-model="releaseForm.rollout_state"
            ><el-option label="待发布" value="pending" /><el-option
              label="已发布"
              value="published" /><el-option
              label="已暂停"
              value="paused" /></el-select></el-form-item
      ></el-form>
      <template #footer
        ><el-button @click="releaseDialog = false">关闭</el-button
        ><el-button type="primary" :loading="releaseSaving" @click="saveRelease"
          >记录发布</el-button
        ></template
      >
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { ElMessage } from "element-plus";
import { Refresh } from "@element-plus/icons-vue";
import { pluginApi } from "../api";

type OverviewRow = {
  catalog: {
    plugin_name: string;
    display_name: string;
    description: string;
    version: string;
    runtime: string;
    visibility: string;
  };
  metrics: {
    user_count: number;
    record_count: number;
    file_count: number;
    file_bytes: number;
  };
  runtime_state: { status: string; health: string };
  system_permissions: Array<{ resource: string; actions: string[] }>;
};
type Request = {
  id: number;
  plugin_name: string;
  user_id: string;
  message: string;
  status: string;
};
type Release = {
  version: string;
  checksum: string;
  signature_state: string;
  channel: string;
  rollout_state: string;
};
type Audit = {
  id: number;
  plugin_name: string;
  actor_id: string;
  action: string;
  outcome: string;
  created_at: string;
};
const items = ref<OverviewRow[]>([]),
  requests = ref<Request[]>([]),
  releases = ref<Release[]>([]),
  audits = ref<Audit[]>([]);
const loading = ref(false),
  requestsLoading = ref(false),
  auditsLoading = ref(false),
  releaseDialog = ref(false),
  releaseSaving = ref(false),
  releasePluginName = ref("");
const releaseForm = ref<Release>({
  version: "",
  checksum: "",
  signature_state: "pending",
  channel: "stable",
  rollout_state: "pending",
});
const totalUsers = computed(() =>
  items.value.reduce(
    (sum, item) => sum + Number(item.metrics.user_count || 0),
    0,
  ),
);
const totalRecords = computed(() =>
  items.value.reduce(
    (sum, item) => sum + Number(item.metrics.record_count || 0),
    0,
  ),
);
const totalFiles = computed(() =>
  items.value.reduce(
    (sum, item) => sum + Number(item.metrics.file_count || 0),
    0,
  ),
);
const unwrap = (value: any) => value?.data || value || {};
const load = async () => {
  loading.value = true;
  try {
    items.value = unwrap(await pluginApi.marketOverview()).items || [];
    await Promise.all([loadRequests(), loadAudits()]);
  } catch (error: any) {
    ElMessage.error(error?.message || "加载插件中心失败");
  } finally {
    loading.value = false;
  }
};
const loadRequests = async () => {
  requestsLoading.value = true;
  try {
    requests.value = unwrap(await pluginApi.marketRequests()).items || [];
  } finally {
    requestsLoading.value = false;
  }
};
const loadAudits = async () => {
  auditsLoading.value = true;
  try {
    audits.value = unwrap(await pluginApi.marketAudits()).items || [];
  } finally {
    auditsLoading.value = false;
  }
};
const setVisibility = async (
  name: string,
  visibility: "draft" | "published" | "hidden",
) => {
  try {
    await pluginApi.setMarketVisibility(name, visibility);
    ElMessage.success("目录状态已更新");
    await load();
  } catch (error: any) {
    ElMessage.error(error?.message || "更新失败");
  }
};
const review = async (id: number, status: "approved" | "rejected") => {
  try {
    await pluginApi.reviewMarketRequest(id, status);
    ElMessage.success("请求已处理");
    await loadRequests();
  } catch (error: any) {
    ElMessage.error(error?.message || "处理失败");
  }
};
const showReleases = async (name: string) => {
  releasePluginName.value = name;
  releaseForm.value = {
    version: "",
    checksum: "",
    signature_state: "pending",
    channel: "stable",
    rollout_state: "pending",
  };
  releaseDialog.value = true;
  try {
    releases.value = unwrap(await pluginApi.marketReleases(name)).items || [];
  } catch (error: any) {
    ElMessage.error(error?.message || "加载发布记录失败");
  }
};
const saveRelease = async () => {
  if (!releaseForm.value.version || !releaseForm.value.checksum) {
    ElMessage.warning("请填写版本和校验和");
    return;
  }
  releaseSaving.value = true;
  try {
    await pluginApi.saveMarketRelease(
      releasePluginName.value,
      releaseForm.value,
    );
    ElMessage.success("发布记录已保存");
    releases.value =
      unwrap(await pluginApi.marketReleases(releasePluginName.value)).items ||
      [];
    releaseForm.value = {
      version: "",
      checksum: "",
      signature_state: "pending",
      channel: "stable",
      rollout_state: "pending",
    };
  } catch (error: any) {
    ElMessage.error(error?.message || "保存失败");
  } finally {
    releaseSaving.value = false;
  }
};
const visibilityLabel = (value: string) =>
  ({ published: "已发布", hidden: "已隐藏", draft: "草稿" })[value] || value;
const visibilityType = (value: string) =>
  value === "published" ? "success" : value === "hidden" ? "warning" : "info";
const runtimeLabel = (state?: { status?: string; health?: string }) =>
  state
    ? `${state.status || "unknown"} / ${state.health || "unknown"}`
    : "unknown";
const formatBytes = (value: number) =>
  value < 1024
    ? `${value} B`
    : value < 1024 * 1024
      ? `${(value / 1024).toFixed(1)} KB`
      : `${(value / 1024 / 1024).toFixed(1)} MB`;
const formatTime = (value: string) =>
  value ? new Date(value).toLocaleString() : "-";
onMounted(() => {
  void load();
});
</script>

<style scoped>
.plugin-center {
  max-width: 1440px;
  margin: 0 auto;
  display: grid;
  gap: 20px;
  color: #253244;
}
.page-heading,
.section-heading,
.row-summary,
.row-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
h1,
h2,
p {
  margin: 0;
}
h1 {
  font-size: 22px;
}
h2 {
  font-size: 17px;
}
.page-heading p,
.section-heading p {
  margin-top: 5px;
  color: #687385;
  font-size: 14px;
}
.metric-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  border: 1px solid #dfe6ee;
  background: #fff;
}
.metric {
  min-width: 0;
  padding: 16px;
  border-right: 1px solid #dfe6ee;
  display: grid;
  gap: 5px;
}
.metric:last-child {
  border-right: 0;
}
.metric span,
small {
  color: #687385;
  font-size: 12px;
}
.metric strong {
  font-size: 22px;
}
.workspace-section {
  display: grid;
  gap: 12px;
  padding: 18px;
  background: #fff;
  border: 1px solid #dfe6ee;
}
.desktop-table small {
  display: block;
  margin-top: 3px;
}
.mobile-list {
  display: none;
}
.plugin-row {
  border-top: 1px solid #e5e9ef;
  padding: 14px 0;
  display: grid;
  gap: 9px;
}
.plugin-row p {
  color: #536174;
  font-size: 14px;
}
dl {
  display: flex;
  flex-wrap: wrap;
  gap: 18px;
  margin: 0;
}
dl div {
  display: grid;
  gap: 2px;
}
dt {
  color: #687385;
  font-size: 12px;
}
dd {
  margin: 0;
  font-size: 13px;
}
.release-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 12px;
  margin-top: 18px;
}
@media (max-width: 900px) {
  .metric-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .metric:nth-child(2) {
    border-right: 0;
  }
  .metric:nth-child(-n + 2) {
    border-bottom: 1px solid #dfe6ee;
  }
  .desktop-table {
    display: none;
  }
  .mobile-list {
    display: block;
  }
}
@media (max-width: 540px) {
  .plugin-center {
    gap: 14px;
  }
  .page-heading {
    align-items: flex-start;
  }
  .metric {
    padding: 13px;
  }
  .workspace-section {
    padding: 14px;
  }
  .row-actions {
    justify-content: flex-start;
    flex-wrap: wrap;
  }
  .release-form {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
