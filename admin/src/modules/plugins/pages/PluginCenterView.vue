<template>
  <section class="plugin-center" aria-labelledby="plugin-center-title">
    <div class="page-heading">
      <div>
        <h1 id="plugin-center-title">用户目录与授权</h1>
        <p>
          管理已安装外部插件在用户端的目录发布、用户请求、授权概览和发布审计。
        </p>
      </div>
      <el-button :loading="loading" @click="load">
        <el-icon><Refresh /></el-icon>
        刷新
      </el-button>
    </div>

    <el-alert
      title="“用户目录”只投影当前已安装的外部插件；“可信插件市场”只提供待审核候选，二者不是第二套安装记录。市场申请获批不会下载、安装或启动代码；实际导入、验签、启停和管理员能力授权仍在“外部插件运行管理”完成。"
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

    <section class="workspace-section" aria-labelledby="market-sources-title">
      <div class="section-heading">
        <div>
          <h2 id="market-sources-title">可信插件市场白名单</h2>
          <p>
            只有这里启用的 HTTPS 市场、且其目录响应通过所填 Ed25519
            公钥验签后，用户才可以检索并申请其中的插件。
          </p>
        </div>
        <el-button type="primary" @click="openSourceDialog()"
          >添加可信市场</el-button
        >
      </div>
      <el-alert
        title="市场公钥是验证证书，不是 Secret。请仅配置由平台信任方提供的目录地址和公钥；未配置任何已启用市场时，用户端不能提交外部市场插件申请。"
        type="warning"
        :closable="false"
        show-icon
      />
      <el-table
        v-loading="sourcesLoading"
        :data="marketplaceSources"
        class="desktop-table"
        stripe
      >
        <el-table-column label="市场" min-width="180">
          <template #default="{ row }"
            ><strong>{{ row.display_name }}</strong
            ><small>{{ row.id }}</small></template
          >
        </el-table-column>
        <el-table-column
          prop="catalog_url"
          label="签名目录地址"
          min-width="280"
          show-overflow-tooltip
        />
        <el-table-column label="证书指纹" min-width="160"
          ><template #default="{ row }">{{
            row.key_fingerprint || "-"
          }}</template></el-table-column
        >
        <el-table-column label="状态" width="100"
          ><template #default="{ row }"
            ><el-tag
              :type="row.status === 'enabled' ? 'success' : 'info'"
              effect="plain"
              >{{ row.status === "enabled" ? "已启用" : "已停用" }}</el-tag
            ></template
          ></el-table-column
        >
        <el-table-column label="操作" width="190" fixed="right">
          <template #default="{ row }"
            ><el-button size="small" @click="openSourceDialog(row)"
              >编辑</el-button
            ><el-button
              size="small"
              type="warning"
              plain
              @click="toggleSource(row)"
              >{{ row.status === "enabled" ? "停用" : "启用" }}</el-button
            ><el-button
              size="small"
              type="danger"
              link
              @click="removeSource(row)"
              >删除</el-button
            ></template
          >
        </el-table-column>
      </el-table>
      <el-empty
        v-if="!sourcesLoading && !marketplaceSources.length"
        description="尚未配置可信插件市场；用户端外部市场申请已关闭。"
      />
    </section>

    <section class="workspace-section" aria-labelledby="catalog-title">
      <div class="section-heading">
        <div>
          <h2 id="catalog-title">用户目录发布与受管数据概览</h2>
          <p>
            每一行对应一个当前已安装的外部插件；发布后用户可在前台查看并添加，再按声明完成用户授权。
          </p>
        </div>
      </div>
      <el-table v-loading="loading" :data="items" class="desktop-table" stripe>
        <el-table-column label="插件" min-width="190">
          <template #default="{ row }"
            ><strong>{{
              row.catalog.display_name || row.catalog.plugin_name
            }}</strong
            ><small>{{ row.catalog.plugin_name }}</small
            ><el-tag
              v-if="row.catalog.trusted_builtin"
              type="success"
              size="small"
              effect="plain"
              >第一方受管</el-tag
            ></template
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
        description="没有可管理的 v2 受管插件"
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
          <p>
            每个申请都绑定可信市场、签名目录解析出的插件 ID
            与链接快照。批准仅表示同意进入安装评估；请在外部插件运行管理中重新验签并导入包。
          </p>
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
        /><el-table-column label="可信来源" min-width="190"
          ><template #default="{ row }"
            ><strong>{{ row.market_source_id || "历史本地请求" }}</strong
            ><small v-if="row.market_plugin_id"
              >{{ row.market_plugin_id }} · v{{
                row.market_version || "-"
              }}</small
            ><a
              v-if="row.market_listing_url"
              :href="row.market_listing_url"
              target="_blank"
              rel="noopener noreferrer"
              >查看市场条目</a
            ></template
          ></el-table-column
        ><el-table-column
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
          <small v-if="request.market_source_id"
            >{{ request.market_source_id }} · {{ request.market_plugin_id }} ·
            v{{ request.market_version || "-" }}</small
          >
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

    <el-dialog
      v-model="sourceDialog"
      :title="
        sourceForm.id ? `编辑可信市场：${sourceForm.id}` : '添加可信插件市场'
      "
      width="min(680px, calc(100vw - 24px))"
    >
      <el-form :model="sourceForm" label-position="top">
        <el-form-item label="市场 ID"
          ><el-input
            v-model="sourceForm.id"
            :disabled="Boolean(sourceForm.id)"
            placeholder="例如 campusos-official-market"
        /></el-form-item>
        <el-form-item label="显示名称"
          ><el-input v-model="sourceForm.display_name" maxlength="120"
        /></el-form-item>
        <el-form-item label="签名目录地址"
          ><el-input
            v-model="sourceForm.catalog_url"
            placeholder="https://market.example/campusos/catalog"
        /></el-form-item>
        <el-form-item label="Ed25519 公钥（Base64）"
          ><el-input
            v-model="sourceForm.public_key"
            type="textarea"
            :rows="3"
            placeholder="由可信市场提供的 32 字节 Ed25519 公钥 Base64 值"
        /></el-form-item>
        <el-form-item label="状态"
          ><el-select v-model="sourceForm.status"
            ><el-option label="启用" value="enabled" /><el-option
              label="停用"
              value="disabled" /></el-select
        ></el-form-item>
      </el-form>
      <template #footer
        ><el-button @click="sourceDialog = false">取消</el-button
        ><el-button type="primary" :loading="sourceSaving" @click="saveSource"
          >保存</el-button
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
  market_source_id?: string;
  market_plugin_id?: string;
  market_listing_url?: string;
  market_package_url?: string;
  market_version?: string;
  market_publisher?: string;
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
type MarketplaceSource = {
  id: string;
  display_name: string;
  catalog_url: string;
  public_key: string;
  key_fingerprint: string;
  status: "enabled" | "disabled";
};
const items = ref<OverviewRow[]>([]),
  requests = ref<Request[]>([]),
  releases = ref<Release[]>([]),
  audits = ref<Audit[]>([]),
  marketplaceSources = ref<MarketplaceSource[]>([]);
const loading = ref(false),
  requestsLoading = ref(false),
  auditsLoading = ref(false),
  sourcesLoading = ref(false),
  releaseDialog = ref(false),
  releaseSaving = ref(false),
  sourceDialog = ref(false),
  sourceSaving = ref(false),
  releasePluginName = ref("");
const releaseForm = ref<Release>({
  version: "",
  checksum: "",
  signature_state: "pending",
  channel: "stable",
  rollout_state: "pending",
});
const emptySource = (): MarketplaceSource => ({
  id: "",
  display_name: "",
  catalog_url: "",
  public_key: "",
  key_fingerprint: "",
  status: "enabled",
});
const sourceForm = ref<MarketplaceSource>(emptySource());
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
    await Promise.all([loadRequests(), loadAudits(), loadMarketplaceSources()]);
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
const loadMarketplaceSources = async () => {
  sourcesLoading.value = true;
  try {
    marketplaceSources.value =
      unwrap(await pluginApi.marketplaceSources()).items || [];
  } finally {
    sourcesLoading.value = false;
  }
};
const openSourceDialog = (source?: MarketplaceSource) => {
  sourceForm.value = source ? { ...source } : emptySource();
  sourceDialog.value = true;
};
const saveSource = async () => {
  const source = sourceForm.value;
  if (!/^[a-z][a-z0-9_.-]{1,127}$/.test(source.id)) {
    ElMessage.warning(
      "市场 ID 必须以小写字母开头，只能包含小写字母、数字、点、下划线和连字符",
    );
    return;
  }
  if (
    !source.display_name.trim() ||
    !source.catalog_url.trim() ||
    !source.public_key.trim()
  ) {
    ElMessage.warning("请填写市场名称、HTTPS 签名目录地址和 Ed25519 公钥");
    return;
  }
  sourceSaving.value = true;
  try {
    await pluginApi.saveMarketplaceSource(source.id, {
      display_name: source.display_name.trim(),
      catalog_url: source.catalog_url.trim(),
      public_key: source.public_key.trim(),
      status: source.status,
    });
    ElMessage.success("可信插件市场已保存");
    sourceDialog.value = false;
    await loadMarketplaceSources();
  } catch (error: any) {
    ElMessage.error(error?.message || "保存可信插件市场失败");
  } finally {
    sourceSaving.value = false;
  }
};
const toggleSource = async (source: MarketplaceSource) => {
  try {
    await pluginApi.saveMarketplaceSource(source.id, {
      display_name: source.display_name,
      catalog_url: source.catalog_url,
      public_key: source.public_key,
      status: source.status === "enabled" ? "disabled" : "enabled",
    });
    ElMessage.success(
      source.status === "enabled" ? "可信市场已停用" : "可信市场已启用",
    );
    await loadMarketplaceSources();
  } catch (error: any) {
    ElMessage.error(error?.message || "更新可信市场状态失败");
  }
};
const removeSource = async (source: MarketplaceSource) => {
  try {
    await pluginApi.deleteMarketplaceSource(source.id);
    ElMessage.success("可信插件市场已删除");
    await loadMarketplaceSources();
  } catch (error: any) {
    ElMessage.error(
      error?.message ||
        "删除失败；已有用户申请的市场应改为停用，以保留审计证据",
    );
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
    ElMessage.success(
      status === "approved"
        ? "申请已批准；请在外部插件运行管理中重新验签并导入包"
        : "申请已拒绝",
    );
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
