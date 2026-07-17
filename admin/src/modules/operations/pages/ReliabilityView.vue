<template>
  <div class="reliability-view">
    <section class="page-header">
      <div>
        <p class="eyebrow">受权限保护的运行状态</p>
        <h2>可靠任务</h2>
        <p>
          查看持久事件、Worker、补偿操作和保留策略预演；重放只处理已明确进入失败队列的任务。
        </p>
      </div>
      <div class="header-actions">
        <el-tooltip content="立即刷新所有只读状态">
          <el-button
            circle
            :loading="loading"
            aria-label="刷新可靠任务状态"
            @click="loadAll"
          >
            <el-icon><Refresh /></el-icon>
          </el-button>
        </el-tooltip>
      </div>
    </section>

    <section class="summary-grid" aria-label="可靠任务概览">
      <article
        v-for="item in summaryCards"
        :key="item.key"
        class="summary-item"
        :class="`summary-${item.key}`"
      >
        <span>{{ item.label }}</span>
        <strong>{{ item.value }}</strong>
      </article>
      <article class="summary-item summary-age">
        <span>最早待处理</span>
        <strong class="summary-date">{{
          formatDate(summary.oldest_pending_at)
        }}</strong>
      </article>
      <article class="summary-item summary-email" aria-label="邮件投递状态">
        <span>邮件投递</span>
        <strong>{{ emailDeliveryLabel }}</strong>
        <small v-if="emailDelivery.last_error">{{ emailDelivery.last_error }}</small>
      </article>
    </section>

    <el-tabs v-model="activeTab" class="reliability-tabs">
      <el-tab-pane label="事件队列" name="events">
        <section class="toolbar-band">
          <el-select
            v-model="eventFilters.status"
            clearable
            placeholder="事件状态"
            @change="filterEvents"
          >
            <el-option label="全部状态" value="" />
            <el-option label="待处理" value="pending" />
            <el-option label="处理中" value="processing" />
            <el-option label="重试中" value="retry" />
            <el-option label="已发布" value="published" />
            <el-option label="失败队列" value="dead" />
          </el-select>
          <el-input
            v-model="eventFilters.type"
            clearable
            placeholder="事件类型"
            @keyup.enter="filterEvents"
          />
          <el-tooltip content="按当前筛选重新加载事件">
            <el-button circle aria-label="筛选事件" @click="filterEvents"
              ><el-icon><Search /></el-icon
            ></el-button>
          </el-tooltip>
        </section>
        <el-table
          v-loading="loadingEvents"
          :data="events"
          stripe
          class="data-table"
        >
          <el-table-column
            prop="type"
            label="事件"
            min-width="190"
            show-overflow-tooltip
          />
          <el-table-column prop="aggregate_type" label="聚合" width="120" />
          <el-table-column
            prop="aggregate_id"
            label="对象"
            min-width="130"
            show-overflow-tooltip
          />
          <el-table-column label="状态" width="108">
            <template #default="{ row }"
              ><el-tag :type="eventStatusType(row.status)" size="small">{{
                eventStatusLabel(row.status)
              }}</el-tag></template
            >
          </el-table-column>
          <el-table-column
            prop="attempts"
            label="尝试"
            width="78"
            align="center"
          >
            <template #default="{ row }"
              >{{ row.attempts }}/{{ row.max_attempts }}</template
            >
          </el-table-column>
          <el-table-column
            prop="last_error"
            label="最近错误"
            min-width="180"
            show-overflow-tooltip
          />
          <el-table-column label="更新时间" width="168"
            ><template #default="{ row }">{{
              formatDate(row.updated_at)
            }}</template></el-table-column
          >
          <el-table-column
            label="操作"
            width="104"
            fixed="right"
            align="center"
          >
            <template #default="{ row }">
              <el-tooltip content="查看本事件的消费尝试">
                <el-button
                  circle
                  text
                  type="primary"
                  :aria-label="`查看 ${row.id} 的消费尝试`"
                  @click="openAttempts(row)"
                  ><el-icon><List /></el-icon
                ></el-button>
              </el-tooltip>
              <el-tooltip
                v-if="row.status === 'dead'"
                content="重放失败队列事件"
              >
                <el-button
                  circle
                  text
                  type="warning"
                  :loading="replayingId === row.id"
                  :aria-label="`重放 ${row.id}`"
                  @click="replay(row)"
                  ><el-icon><RefreshRight /></el-icon
                ></el-button>
              </el-tooltip>
            </template>
          </el-table-column>
        </el-table>
        <div class="pagination-wrapper">
          <el-pagination
            v-model:current-page="pages.events.page"
            :page-size="pageSize"
            :total="pages.events.total"
            layout="total, prev, pager, next"
            @current-change="loadEvents"
          />
        </div>
      </el-tab-pane>

      <el-tab-pane label="Worker 与操作" name="workers">
        <div class="two-column">
          <section class="table-section">
            <h3>Worker 心跳</h3>
            <el-table
              v-loading="loadingWorkers"
              :data="workers"
              stripe
              class="data-table compact-table"
            >
              <el-table-column
                prop="worker_id"
                label="Worker"
                min-width="180"
                show-overflow-tooltip
              />
              <el-table-column label="最近心跳" min-width="160"
                ><template #default="{ row }">{{
                  formatDate(row.last_heartbeat_at)
                }}</template></el-table-column
              >
            </el-table>
            <div class="pagination-wrapper compact-pagination">
              <el-pagination
                v-model:current-page="pages.workers.page"
                :page-size="pageSize"
                :total="pages.workers.total"
                layout="total, prev, next"
                @current-change="loadWorkers"
              />
            </div>
          </section>
          <section class="table-section">
            <h3>可恢复操作</h3>
            <el-table
              v-loading="loadingOperations"
              :data="operations"
              stripe
              class="data-table compact-table"
            >
              <el-table-column
                prop="kind"
                label="类型"
                min-width="155"
                show-overflow-tooltip
              />
              <el-table-column
                prop="subject_id"
                label="对象"
                min-width="120"
                show-overflow-tooltip
              />
              <el-table-column label="状态" width="105"
                ><template #default="{ row }"
                  ><el-tag
                    size="small"
                    :type="operationStatusType(row.status)"
                    >{{ row.status }}</el-tag
                  ></template
                ></el-table-column
              >
              <el-table-column label="更新时间" width="155"
                ><template #default="{ row }">{{
                  formatDate(row.updated_at)
                }}</template></el-table-column
              >
            </el-table>
            <div class="pagination-wrapper compact-pagination">
              <el-pagination
                v-model:current-page="pages.operations.page"
                :page-size="pageSize"
                :total="pages.operations.total"
                layout="total, prev, next"
                @current-change="loadOperations"
              />
            </div>
          </section>
        </div>
        <section class="table-section command-audit-section">
          <div class="section-heading">
            <h3>可靠命令审计</h3>
            <span
              >关联命令、操作者、资源、请求和事件；不显示命令内容或密钥。</span
            >
          </div>
          <el-table
            v-loading="loadingCommandAudits"
            :data="commandAudits"
            stripe
            class="data-table compact-table"
          >
            <el-table-column
              prop="command_code"
              label="命令"
              min-width="190"
              show-overflow-tooltip
            />
            <el-table-column
              prop="actor_id"
              label="操作者"
              min-width="120"
              show-overflow-tooltip
            />
            <el-table-column label="资源" min-width="170" show-overflow-tooltip>
              <template #default="{ row }">{{ resourceLabel(row) }}</template>
            </el-table-column>
            <el-table-column
              prop="request_id"
              label="请求"
              min-width="150"
              show-overflow-tooltip
            />
            <el-table-column
              prop="event_id"
              label="事件"
              min-width="150"
              show-overflow-tooltip
            />
            <el-table-column label="时间" width="168"
              ><template #default="{ row }">{{
                formatDate(row.created_at)
              }}</template></el-table-column
            >
          </el-table>
          <div class="pagination-wrapper">
            <el-pagination
              v-model:current-page="pages.commandAudits.page"
              :page-size="pageSize"
              :total="pages.commandAudits.total"
              layout="total, prev, pager, next"
              @current-change="loadCommandAudits"
            />
          </div>
        </section>
      </el-tab-pane>

      <el-tab-pane label="兼容与保留" name="retention">
        <div class="two-column">
          <section class="table-section">
            <div class="section-heading">
              <h3>兼容路径使用</h3>
              <span>保留旧路径前的实际调用证据</span>
            </div>
            <el-table
              v-loading="loadingCompatibility"
              :data="compatibility"
              stripe
              class="data-table compact-table"
            >
              <el-table-column
                prop="kind"
                label="类型"
                min-width="155"
                show-overflow-tooltip
              />
              <el-table-column
                prop="key"
                label="路径"
                min-width="200"
                show-overflow-tooltip
              />
              <el-table-column
                prop="count"
                label="次数"
                width="78"
                align="center"
              />
              <el-table-column label="最近调用" width="155"
                ><template #default="{ row }">{{
                  formatDate(row.last_seen)
                }}</template></el-table-column
              >
            </el-table>
            <div class="pagination-wrapper compact-pagination">
              <el-pagination
                v-model:current-page="pages.compatibility.page"
                :page-size="pageSize"
                :total="pages.compatibility.total"
                layout="total, prev, next"
                @current-change="loadCompatibility"
              />
            </div>
          </section>
          <section class="table-section">
            <div class="section-heading">
              <h3>保留策略预演</h3>
              <span>仅统计候选记录，不会删除历史数据</span>
            </div>
            <div class="retention-controls">
              <el-select v-model="retention.target" aria-label="保留目标">
                <el-option
                  v-for="target in retentionTargets"
                  :key="target.value"
                  :label="target.label"
                  :value="target.value"
                />
              </el-select>
              <el-date-picker
                v-model="retention.before"
                type="datetime"
                value-format="YYYY-MM-DDTHH:mm:ss[Z]"
                aria-label="保留截止时间"
              />
              <el-tooltip content="计算符合条件的历史记录数量">
                <el-button
                  circle
                  aria-label="预演保留策略"
                  :loading="previewingRetention"
                  @click="previewRetention"
                  ><el-icon><Search /></el-icon
                ></el-button>
              </el-tooltip>
              <el-tooltip content="保存本次预演记录，不执行删除">
                <el-button
                  circle
                  type="primary"
                  aria-label="保存保留预演"
                  :loading="startingRetention"
                  @click="startRetentionPreview"
                  ><el-icon><DocumentChecked /></el-icon
                ></el-button>
              </el-tooltip>
            </div>
            <el-descriptions
              v-if="retentionPreview"
              :column="1"
              border
              size="small"
              class="retention-preview"
            >
              <el-descriptions-item label="目标">{{
                retentionPreview.target
              }}</el-descriptions-item>
              <el-descriptions-item label="候选记录">{{
                retentionPreview.eligible_rows
              }}</el-descriptions-item>
              <el-descriptions-item label="允许删除">{{
                retentionPreview.can_delete ? "是" : "否，仅预演"
              }}</el-descriptions-item>
            </el-descriptions>
            <el-table
              v-loading="loadingRetentionRuns"
              :data="retentionRuns"
              stripe
              class="data-table compact-table retention-runs"
            >
              <el-table-column prop="target" label="目标" min-width="135" />
              <el-table-column
                prop="eligible_rows"
                label="候选"
                width="76"
                align="center"
              />
              <el-table-column prop="mode" label="模式" width="92" />
              <el-table-column label="时间" width="155"
                ><template #default="{ row }">{{
                  formatDate(row.created_at)
                }}</template></el-table-column
              >
            </el-table>
            <div class="pagination-wrapper compact-pagination">
              <el-pagination
                v-model:current-page="pages.retentionRuns.page"
                :page-size="pageSize"
                :total="pages.retentionRuns.total"
                layout="total, prev, next"
                @current-change="loadRetentionRuns"
              />
            </div>
          </section>
        </div>
      </el-tab-pane>
    </el-tabs>

    <el-dialog
      v-model="attemptDialog"
      title="消费尝试"
      width="min(860px, calc(100vw - 32px))"
    >
      <el-table
        v-loading="loadingAttempts"
        :data="attempts"
        stripe
        class="data-table"
      >
        <el-table-column
          prop="consumer_name"
          label="消费者"
          min-width="180"
          show-overflow-tooltip
        />
        <el-table-column
          prop="worker_id"
          label="Worker"
          min-width="140"
          show-overflow-tooltip
        />
        <el-table-column
          prop="attempt"
          label="次数"
          width="72"
          align="center"
        />
        <el-table-column label="结果" width="100"
          ><template #default="{ row }"
            ><el-tag size="small" :type="attemptStatusType(row.status)">{{
              row.status
            }}</el-tag></template
          ></el-table-column
        >
        <el-table-column
          prop="error"
          label="错误"
          min-width="170"
          show-overflow-tooltip
        />
        <el-table-column label="开始" width="162"
          ><template #default="{ row }">{{
            formatDate(row.started_at)
          }}</template></el-table-column
        >
      </el-table>
      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="pages.attempts.page"
          :page-size="pageSize"
          :total="pages.attempts.total"
          layout="total, prev, pager, next"
          @current-change="loadAttempts"
        />
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  DocumentChecked,
  List,
  Refresh,
  RefreshRight,
  Search,
} from "@element-plus/icons-vue";
import { reliabilityApi } from "@/modules/operations/api";

const activeTab = ref("events");
const loading = ref(false);
const loadingEvents = ref(false);
const loadingWorkers = ref(false);
const loadingOperations = ref(false);
const loadingCommandAudits = ref(false);
const loadingCompatibility = ref(false);
const loadingRetentionRuns = ref(false);
const loadingAttempts = ref(false);
const previewingRetention = ref(false);
const startingRetention = ref(false);
const replayingId = ref("");
const attemptDialog = ref(false);
const summary = reactive<any>({});
const emailDelivery = reactive<any>({});
const events = ref<any[]>([]);
const workers = ref<any[]>([]);
const operations = ref<any[]>([]);
const commandAudits = ref<any[]>([]);
const compatibility = ref<any[]>([]);
const attempts = ref<any[]>([]);
const retentionRuns = ref<any[]>([]);
const retentionPreview = ref<any | null>(null);
const selectedEventID = ref("");
const eventFilters = reactive({ status: "", type: "" });
const retention = reactive({ target: "outbox", before: "" });
const pageSize = 20;
const pages = reactive({
  events: { page: 1, total: 0 },
  workers: { page: 1, total: 0 },
  operations: { page: 1, total: 0 },
  commandAudits: { page: 1, total: 0 },
  compatibility: { page: 1, total: 0 },
  retentionRuns: { page: 1, total: 0 },
  attempts: { page: 1, total: 0 },
});

const retentionTargets = [
  { label: "持久事件", value: "outbox" },
  { label: "授权审计", value: "authorization-audits" },
  { label: "Webhook 投递", value: "webhook-deliveries" },
  { label: "内容修订", value: "content-revisions" },
  { label: "内容治理操作", value: "content-moderation-actions" },
  { label: "插件日志", value: "plugin-logs" },
];

const summaryCards = computed(() => [
  { key: "pending", label: "待处理", value: summary.pending || 0 },
  { key: "processing", label: "处理中", value: summary.processing || 0 },
  { key: "retry", label: "重试中", value: summary.retry || 0 },
  { key: "dead", label: "失败队列", value: summary.dead || 0 },
  { key: "published", label: "已发布", value: summary.published || 0 },
]);
const emailDeliveryLabel = computed(() => {
  const provider = emailDelivery.provider || "未加载";
  const state = emailDelivery.state || "unknown";
  const stateLabel = state === "healthy" ? "正常" : state === "degraded" ? "降级" : "未知";
  return `${provider} · ${stateLabel}`;
});

const payload = (result: any) => result?.data ?? result;
const pageItems = (result: any, state: { total: number }) => {
  const data = payload(result) || {};
  state.total = Number(data?.pagination?.total ?? data?.total ?? 0);
  return data?.items || [];
};

const loadSummary = async () => {
  Object.assign(summary, payload(await reliabilityApi.summary()) || {});
};
const loadEmailDelivery = async () => {
  Object.assign(emailDelivery, payload(await reliabilityApi.emailDeliveryStatus()) || {});
};
const loadEvents = async () => {
  loadingEvents.value = true;
  try {
    const result = await reliabilityApi.events({
      ...eventFilters,
      page: pages.events.page,
      page_size: pageSize,
    });
    events.value = pageItems(result, pages.events);
  } finally {
    loadingEvents.value = false;
  }
};
const filterEvents = () => {
  pages.events.page = 1;
  return loadEvents();
};
const loadWorkers = async () => {
  loadingWorkers.value = true;
  try {
    workers.value = pageItems(
      await reliabilityApi.workers({
        page: pages.workers.page,
        page_size: pageSize,
      }),
      pages.workers,
    );
  } finally {
    loadingWorkers.value = false;
  }
};
const loadOperations = async () => {
  loadingOperations.value = true;
  try {
    operations.value = pageItems(
      await reliabilityApi.operations({
        page: pages.operations.page,
        page_size: pageSize,
      }),
      pages.operations,
    );
  } finally {
    loadingOperations.value = false;
  }
};
const loadCommandAudits = async () => {
  loadingCommandAudits.value = true;
  try {
    commandAudits.value = pageItems(
      await reliabilityApi.commandAudits({
        page: pages.commandAudits.page,
        page_size: pageSize,
      }),
      pages.commandAudits,
    );
  } finally {
    loadingCommandAudits.value = false;
  }
};
const loadCompatibility = async () => {
  loadingCompatibility.value = true;
  try {
    compatibility.value = pageItems(
      await reliabilityApi.compatibility({
        page: pages.compatibility.page,
        page_size: pageSize,
      }),
      pages.compatibility,
    );
  } finally {
    loadingCompatibility.value = false;
  }
};
const loadRetentionRuns = async () => {
  loadingRetentionRuns.value = true;
  try {
    retentionRuns.value = pageItems(
      await reliabilityApi.retentionRuns({
        page: pages.retentionRuns.page,
        page_size: pageSize,
      }),
      pages.retentionRuns,
    );
  } finally {
    loadingRetentionRuns.value = false;
  }
};
const loadAll = async () => {
  loading.value = true;
  try {
    await Promise.all([
      loadSummary(),
      loadEmailDelivery(),
      loadEvents(),
      loadWorkers(),
      loadOperations(),
      loadCommandAudits(),
      loadCompatibility(),
      loadRetentionRuns(),
    ]);
  } catch (error: any) {
    ElMessage.error(error?.message || "加载可靠任务状态失败");
  } finally {
    loading.value = false;
  }
};

const openAttempts = async (event: any) => {
  attemptDialog.value = true;
  selectedEventID.value = event.id;
  pages.attempts.page = 1;
  await loadAttempts();
};

const loadAttempts = async () => {
  if (!selectedEventID.value) return;
  loadingAttempts.value = true;
  try {
    const result = await reliabilityApi.attempts({
      event_id: selectedEventID.value,
      page: pages.attempts.page,
      page_size: pageSize,
    });
    attempts.value = pageItems(result, pages.attempts);
  } catch (error: any) {
    ElMessage.error(error?.message || "加载消费尝试失败");
  } finally {
    loadingAttempts.value = false;
  }
};

const replay = async (event: any) => {
  try {
    await ElMessageBox.confirm(
      `重放事件 ${event.type}？接收方必须按事件 ID 保持幂等。`,
      "确认重放",
      { type: "warning", confirmButtonText: "重放", cancelButtonText: "取消" },
    );
    replayingId.value = event.id;
    const idempotencyKey =
      typeof crypto.randomUUID === "function"
        ? crypto.randomUUID()
        : `replay-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    await reliabilityApi.replay(event.id, idempotencyKey);
    ElMessage.success("已重新加入待处理队列");
    await Promise.all([loadSummary(), loadEvents()]);
  } catch (error: any) {
    if (error !== "cancel" && error !== "close")
      ElMessage.error(error?.message || "重放失败");
  } finally {
    replayingId.value = "";
  }
};

const previewRetention = async () => {
  previewingRetention.value = true;
  try {
    retentionPreview.value = payload(
      await reliabilityApi.retentionPreview(retention),
    );
  } catch (error: any) {
    ElMessage.error(error?.message || "保留策略预演失败");
  } finally {
    previewingRetention.value = false;
  }
};

const startRetentionPreview = async () => {
  startingRetention.value = true;
  try {
    await reliabilityApi.startRetentionPreview(retention);
    ElMessage.success("已保存 dry-run 预演记录");
    pages.retentionRuns.page = 1;
    await loadRetentionRuns();
  } catch (error: any) {
    ElMessage.error(error?.message || "保存保留预演失败");
  } finally {
    startingRetention.value = false;
  }
};

const formatDate = (value?: string) => {
  if (!value) return "无";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
};
const resourceLabel = (row: any) =>
  [row.resource_type, row.resource_id].filter(Boolean).join(":") || "平台操作";
const eventStatusLabel = (status: string) =>
  ({
    pending: "待处理",
    processing: "处理中",
    retry: "重试中",
    published: "已发布",
    dead: "失败队列",
  })[status] || status;
const eventStatusType = (status: string) =>
  (({
    pending: "info",
    processing: "primary",
    retry: "warning",
    published: "success",
    dead: "danger",
  })[status] || "info") as any;
const operationStatusType = (status: string) =>
  (({
    succeeded: "success",
    failed: "danger",
    compensating: "warning",
    running: "primary",
  })[status] || "info") as any;
const attemptStatusType = (status: string) =>
  (({
    succeeded: "success",
    skipped: "info",
    retry: "warning",
    dead: "danger",
  })[status] || "primary") as any;

onMounted(loadAll);
</script>

<style scoped>
.reliability-view {
  max-width: 1500px;
  display: grid;
  gap: 18px;
}
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}
.eyebrow {
  margin: 0 0 6px;
  color: #64748b;
  font-size: 13px;
}
.page-header h2 {
  margin: 0;
  font-size: 24px;
  color: #1f2937;
}
.page-header p:not(.eyebrow) {
  margin: 8px 0 0;
  color: #64748b;
  max-width: 760px;
}
.header-actions,
.toolbar-band,
.retention-controls {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.summary-grid {
  display: grid;
  grid-template-columns: repeat(7, minmax(120px, 1fr));
  border: 1px solid #dfe7f1;
  background: #fff;
}
.summary-item {
  min-height: 78px;
  padding: 14px;
  border-right: 1px solid #dfe7f1;
  display: grid;
  align-content: center;
  gap: 4px;
}
.summary-item:last-child {
  border-right: 0;
}
.summary-item span {
  color: #64748b;
  font-size: 13px;
}
.summary-item strong {
  color: #1f2937;
  font-size: 24px;
  font-variant-numeric: tabular-nums;
}
.summary-item.summary-dead strong {
  color: #c2410c;
}
.summary-item.summary-retry strong {
  color: #a16207;
}
.summary-date {
  font-size: 14px !important;
  line-height: 1.35;
}
.summary-email strong {
  font-size: 15px;
  line-height: 1.35;
  overflow-wrap: anywhere;
}
.summary-email small {
  color: #b45309;
  font-size: 12px;
  line-height: 1.3;
}
.reliability-tabs {
  background: #fff;
  border: 1px solid #dfe7f1;
  padding: 0 16px 16px;
}
.toolbar-band {
  padding: 10px 0 16px;
}
.toolbar-band :deep(.el-input),
.toolbar-band :deep(.el-select) {
  width: min(220px, 100%);
}
.data-table {
  width: 100%;
}
.two-column {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 18px;
}
.table-section {
  min-width: 0;
  border: 1px solid #e5e7eb;
  padding: 14px;
}
.table-section h3 {
  margin: 0 0 12px;
  color: #334155;
  font-size: 16px;
}
.section-heading {
  display: grid;
  gap: 3px;
  margin-bottom: 12px;
}
.section-heading span {
  color: #64748b;
  font-size: 13px;
}
.retention-controls {
  margin-bottom: 12px;
}
.retention-controls :deep(.el-select) {
  width: 160px;
}
.retention-preview {
  margin-bottom: 12px;
}
.retention-runs {
  margin-top: 12px;
}
.command-audit-section {
  margin-top: 18px;
}
.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
  overflow-x: auto;
}
.compact-pagination {
  justify-content: center;
}
@media (max-width: 1100px) {
  .summary-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
  .summary-item {
    border-bottom: 1px solid #dfe7f1;
  }
  .summary-item:nth-child(4n) {
    border-right: 0;
  }
  .summary-item:nth-child(n + 5) {
    border-bottom: 0;
  }
  .summary-item:last-child {
    border-right: 0;
  }
  .two-column {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 640px) {
  .page-header {
    align-items: stretch;
    flex-direction: column;
  }
  .header-actions {
    justify-content: flex-end;
  }
  .summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .summary-item {
    border-bottom: 1px solid #dfe7f1;
  }
  .summary-item:nth-child(2n) {
    border-right: 0;
  }
  .summary-item:last-child {
    border-bottom: 0;
  }
  .reliability-tabs {
    padding: 0 10px 12px;
  }
  .toolbar-band :deep(.el-input),
  .toolbar-band :deep(.el-select),
  .retention-controls :deep(.el-select),
  .retention-controls :deep(.el-date-editor) {
    width: 100%;
  }
  .pagination-wrapper {
    justify-content: center;
  }
}
</style>
