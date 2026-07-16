<template>
  <div
    ref="hubRoot"
    class="extension-hub"
    :data-layout-mode="layoutMode"
    v-loading="loading"
  >
    <header class="page-header">
      <div>
        <p class="eyebrow">Extension inventory</p>
        <h2>扩展与集成</h2>
        <p>
          统一发现外部插件、内置功能、资源包和集成入口；它们仍由各自的生命周期服务管理，不会因为同屏展示而混为一种插件。
        </p>
      </div>
      <el-button
        :icon="Refresh"
        circle
        title="刷新扩展清单"
        aria-label="刷新扩展清单"
        @click="load"
      />
    </header>

    <section class="extension-summary" aria-label="扩展分类说明">
      <article v-for="item in kinds" :key="item.title" class="summary-item">
        <div class="summary-top">
          <strong>{{ item.title }}</strong>
          <el-tag :type="item.type" size="small" effect="plain">{{
            item.lifecycle
          }}</el-tag>
        </div>
        <p>{{ item.description }}</p>
      </article>
    </section>

    <section class="extension-panel">
      <el-tabs v-model="activeTab">
        <el-tab-pane label="外部插件" name="plugins">
          <div class="tab-heading">
            <div>
              <h3>已安装外部插件</h3>
              <p>
                可导入、升级、启停和卸载。插件只能通过 Host API、Gateway
                或公开接口访问平台能力。
              </p>
            </div>
            <el-button plain @click="go('/plugins')">管理外部插件</el-button>
          </div>

          <div
            v-if="isCompact"
            class="compact-entity-list"
            aria-label="外部插件列表"
          >
            <article
              v-for="plugin in externalPlugins"
              :key="plugin.name"
              class="compact-entity"
            >
              <div class="compact-entity-heading">
                <strong>{{ plugin.name }}</strong>
                <el-tag size="small" effect="plain">{{
                  plugin.status || "unknown"
                }}</el-tag>
              </div>
              <dl>
                <div>
                  <dt>版本</dt>
                  <dd>{{ plugin.version || "-" }}</dd>
                </div>
                <div>
                  <dt>运行时</dt>
                  <dd>{{ plugin.runtime || "-" }}</dd>
                </div>
              </dl>
              <p>{{ plugin.description || "未填写插件说明。" }}</p>
            </article>
          </div>
          <el-table
            v-else
            :data="externalPlugins"
            border
            stripe
            class="desktop-table"
          >
            <el-table-column prop="name" label="插件" min-width="180" />
            <el-table-column prop="version" label="版本" width="120" />
            <el-table-column prop="runtime" label="运行时" width="130" />
            <el-table-column prop="status" label="状态" width="120" />
            <el-table-column
              prop="description"
              label="说明"
              min-width="260"
              show-overflow-tooltip
            />
          </el-table>
          <el-empty
            v-if="!externalPlugins.length"
            description="没有已安装的外部插件"
          />
        </el-tab-pane>

        <el-tab-pane label="内置功能" name="features">
          <div class="tab-heading">
            <div>
              <h3>Built-in Feature</h3>
              <p>
                随主程序发布，可按策略启停和配置，但不会进入普通安装或卸载流程，停用不删除数据。
              </p>
            </div>
            <el-button plain @click="go('/features')">配置内置功能</el-button>
          </div>

          <el-alert
            v-if="builtinCompatibilityPlugins.length"
            class="compatibility-note"
            type="info"
            :closable="false"
            show-icon
            :title="`兼容 Manifest：${builtinCompatibilityPlugins.map((item) => item.name).join('、')}`"
            description="这些 runtime: builtin 项保留用于旧包兼容；它们不是可导入或卸载的 External Plugin。"
          />

          <div
            v-if="isCompact"
            class="compact-entity-list"
            aria-label="内置功能列表"
          >
            <article
              v-for="feature in features"
              :key="feature.id"
              class="compact-entity"
            >
              <div class="compact-entity-heading">
                <strong>{{ feature.label }}</strong>
                <el-tag
                  :type="
                    feature.state?.status === 'running' ? 'success' : 'info'
                  "
                  size="small"
                  effect="plain"
                >
                  {{
                    feature.state?.status === "running" ? "已启用" : "已停用"
                  }}
                </el-tag>
              </div>
              <code>{{ feature.id }}</code>
              <p>{{ feature.description || "未填写功能说明。" }}</p>
            </article>
          </div>
          <el-table v-else :data="features" border stripe class="desktop-table">
            <el-table-column prop="label" label="功能" min-width="210">
              <template #default="{ row }"
                ><strong>{{ row.label }}</strong
                ><small>{{ row.id }}</small></template
              >
            </el-table-column>
            <el-table-column prop="description" label="职责" min-width="290" />
            <el-table-column label="状态" width="160">
              <template #default="{ row }">
                <el-tag
                  :type="row.state?.status === 'running' ? 'success' : 'info'"
                >
                  {{ row.state?.status === "running" ? "已启用" : "已停用" }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="资源包" name="resources">
          <div class="tab-heading">
            <div>
              <h3>Resource Package</h3>
              <p>
                主题、首页包、个人主页风格包、Skills、Prompt 和 Persona
                只包含受校验的数据资源，不注册业务路由、后台进程或数据库迁移。
              </p>
            </div>
            <el-button plain @click="go('/appearance')"
              >查看外观资源入口</el-button
            >
          </div>
          <div class="resource-grid">
            <article
              v-for="resource in resources"
              :key="resource.kind"
              class="resource-row"
            >
              <strong>{{ resource.label }}</strong>
              <code>{{ resource.path }}</code>
              <span>{{ resource.rule }}</span>
            </article>
          </div>
          <el-alert
            type="info"
            :closable="false"
            show-icon
            title="当前资源目录的校验、导入和应用由 Appearance 与资源仓库负责；Legacy Style Pack 仍从 data/plugin_data 只读兼容。"
          />
        </el-tab-pane>

        <el-tab-pane label="用户目录" name="market">
          <div class="tab-heading">
            <div>
              <h3>插件目录与用户请求</h3>
              <p>
                目录发布不会自动安装插件；用户授权和插件受管数据由宿主保存。
              </p>
            </div>
            <el-button plain @click="go('/plugin-center')"
              >打开插件中心</el-button
            >
          </div>
          <div class="market-metrics">
            <div>
              <span>目录插件</span
              ><strong>{{ market.catalog_count ?? "-" }}</strong>
            </div>
            <div>
              <span>待处理请求</span
              ><strong>{{ market.pending_request_count ?? "-" }}</strong>
            </div>
            <div>
              <span>已授权用户</span
              ><strong>{{ market.user_count ?? "-" }}</strong>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { Refresh } from "@element-plus/icons-vue";
import { featureApi } from "@/modules/features/api";
import { mapBuiltinFeatures } from "@/modules/features/catalog";
import { pluginApi } from "@/modules/plugins/api";
import { useLayoutCapability } from "@/shared/layout/useLayoutCapability";

const router = useRouter();
const hubRoot = ref<HTMLElement | null>(null);
const { mode: layoutMode, isCompact } = useLayoutCapability(hubRoot);
const activeTab = ref("plugins");
const loading = ref(false);
const plugins = ref<any[]>([]);
const features = ref<any[]>([]);
const market = ref<Record<string, any>>({});

const kinds = [
  {
    title: "External Plugin",
    lifecycle: "可安装 / 卸载",
    type: "success",
    description: "独立运行时与受管数据，使用最小权限访问 Host API。",
  },
  {
    title: "Built-in Feature",
    lifecycle: "可启停",
    type: "warning",
    description: "编译在主程序内，停用时保留数据并隐藏对应能力。",
  },
  {
    title: "Resource Package",
    lifecycle: "校验 / 应用",
    type: "info",
    description: "只包含样式、模板、Skill、Prompt 等资源，没有业务 Runtime。",
  },
  {
    title: "Integration Adapter",
    lifecycle: "按能力配置",
    type: "primary",
    description:
      "Webhook、MCP-like 和 Message Local 在集成中心标注成熟度与边界。",
  },
];
const resources = [
  {
    kind: "themes",
    label: "系统主题",
    path: "data/resources/themes/",
    rule: "用户可选择后端提供的已校验版本。",
  },
  {
    kind: "homepage",
    label: "首页风格包",
    path: "data/resources/homepage-packs/",
    rule: "系统管理员校验、应用与回滚。",
  },
  {
    kind: "space",
    label: "个人主页风格包",
    path: "data/resources/space-style-packs/",
    rule: "主页所有者使用自己的已校验包。",
  },
  {
    kind: "agent",
    label: "Skills / Prompt / Persona",
    path: "data/resources/{skills,prompts,personas}/",
    rule: "资源包不含 Token、数据库连接或代码执行能力。",
  },
];

const dataOf = (value: any) => value?.data ?? value;
const itemsOf = (value: any): any[] => {
  const data = dataOf(value);
  return Array.isArray(data) ? data : data?.items || [];
};
const externalPlugins = computed(() =>
  plugins.value.filter(
    (plugin) => String(plugin.runtime || "").toLowerCase() !== "builtin",
  ),
);
const builtinCompatibilityPlugins = computed(() =>
  plugins.value.filter(
    (plugin) => String(plugin.runtime || "").toLowerCase() === "builtin",
  ),
);
const go = (path: string) => router.push(path);

const load = async () => {
  loading.value = true;
  try {
    const [pluginResponse, featureResponse, marketResponse] = await Promise.all(
      [pluginApi.list(), featureApi.list(), pluginApi.marketOverview()],
    );
    plugins.value = itemsOf(pluginResponse);
    features.value = mapBuiltinFeatures(itemsOf(featureResponse));
    market.value = dataOf(marketResponse) || {};
  } finally {
    loading.value = false;
  }
};

onMounted(load);
</script>

<style scoped>
.extension-hub {
  display: grid;
  gap: 16px;
  max-width: 1500px;
}
.page-header,
.extension-panel {
  border: 1px solid #e4e7ed;
  border-radius: 6px;
  background: #fff;
}
.page-header,
.summary-top,
.tab-heading,
.compact-entity-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.page-header {
  padding: 20px;
}
.page-header h2,
.tab-heading h3 {
  margin: 0;
}
.page-header p:last-child,
.tab-heading p {
  margin: 7px 0 0;
  color: #606266;
  line-height: 1.6;
}
.eyebrow {
  margin: 0 0 6px;
  color: #1d4ed8;
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
}
.extension-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.summary-item,
.compact-entity {
  padding: 14px;
  border: 1px solid #e4e7ed;
  border-radius: 6px;
  background: #fff;
}
.summary-item p,
.compact-entity p {
  margin: 8px 0 0;
  color: #606266;
  font-size: 13px;
  line-height: 1.55;
}
.extension-panel {
  padding: 18px;
}
.tab-heading {
  margin: 0 0 14px;
}
.desktop-table {
  width: 100%;
}
.desktop-table small {
  display: block;
  margin-top: 4px;
  color: #909399;
}
.compatibility-note {
  margin-bottom: 14px;
}
.compact-entity-list {
  display: grid;
  gap: 10px;
}
.compact-entity {
  display: grid;
  gap: 8px;
}
.compact-entity dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
}
.compact-entity dl > div {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.compact-entity dt {
  color: #909399;
  font-size: 12px;
}
.compact-entity dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.compact-entity code,
.resource-row code {
  overflow-wrap: anywhere;
  color: #1d4ed8;
}
.resource-grid {
  display: grid;
  gap: 10px;
  margin-bottom: 14px;
}
.resource-row {
  display: grid;
  grid-template-columns: minmax(150px, 0.7fr) minmax(230px, 1fr) minmax(
      0,
      1.3fr
    );
  gap: 12px;
  align-items: center;
  padding: 13px;
  border: 1px solid #ebeef5;
  border-radius: 6px;
}
.resource-row span {
  color: #606266;
  line-height: 1.5;
}
.market-metrics {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}
.market-metrics > div {
  display: grid;
  gap: 6px;
  padding: 18px;
  border: 1px solid #ebeef5;
  border-radius: 6px;
}
.market-metrics span {
  color: #606266;
  font-size: 13px;
}
.market-metrics strong {
  font-size: 24px;
}
@media (max-width: 900px) {
  .extension-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .resource-row {
    grid-template-columns: 1fr;
    gap: 6px;
  }
}
@media (max-width: 620px) {
  .page-header,
  .tab-heading {
    flex-direction: column;
  }
  .page-header,
  .extension-panel {
    padding: 14px;
  }
  .extension-summary,
  .market-metrics {
    grid-template-columns: 1fr;
  }
}
</style>
