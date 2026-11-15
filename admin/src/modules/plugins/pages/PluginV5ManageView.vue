<template>
  <main class="v5-management">
    <header class="page-header">
      <div>
        <h2>插件管理</h2>
        <p>按安装、审批和启用三个步骤，将插件提供给本站用户。</p>
      </div>
      <div class="toolbar">
        <button :disabled="busy" @click="load(false)">重新加载</button
        ><router-link to="/plugin-config">系统配置与密钥引用</router-link>
      </div>
    </header>
    <p v-if="error && !selected" role="alert">{{ error }}</p>
    <p v-if="message" role="status">{{ message }}</p>
    <details v-if="allowed('install')" class="secondary-panel">
      <summary>导入新插件</summary>
      <form aria-label="导入签名插件包" @submit.prevent="importPackage">
        <fieldset :disabled="busy">
          <legend>导入签名插件包</legend>
          <label
            >包文件<input
              type="file"
              required
              @change="archive = file($event)" /></label
          ><label
            >签名发布文件<input
              type="file"
              required
              @change="releaseFile = file($event)" /></label
          ><button type="submit">验证并导入</button>
        </fieldset>
      </form>
    </details>
    <div class="columns">
      <nav aria-label="插件版本" class="plugin-list">
        <button
          v-for="item in visibleItems"
          :key="coordinateKey(item.release)"
          :data-coordinate="coordinateKey(item.release)"
          :aria-pressed="
            !!selected &&
            coordinateKey(selected.release) === coordinateKey(item.release)
          "
          :disabled="busy"
          @click="choose(item.release)"
        >
          <strong>{{ pluginDisplayName(item) }}</strong
          ><small>版本 {{ item.release.version }}</small
          ><span class="status-chip">{{ approvalState(item, now).label }}</span
          ><small>{{ runtimeLabel(item) }}</small>
        </button>
        <button v-if="cursor" :disabled="busy" @click="load(true)">
          加载更多
        </button>
        <p v-if="!visibleItems.length">暂无可用的发布包，可展开“导入新插件”添加。</p>
      </nav>
      <section v-if="selected" aria-label="插件生命周期" class="plugin-detail">
        <header>
          <h3>{{ pluginDisplayName(selected) }}</h3>
          <span class="version">版本 {{ selected.release.version }}</span>
        </header>
        <p v-if="error" role="alert">{{ error }}</p>
        <p aria-label="当前审批状态">
          <span class="status-chip">{{
            approvalState(selected, now).label
          }}</span
          ><span class="status-chip">{{ runtimeLabel(selected) }}</span>
        </p>
        <ol class="plugin-steps" aria-label="插件启用步骤">
          <li :class="{ done: selected.installation?.installed }">
            <strong>1. 安装</strong
            ><span>{{
              selected.installation?.installed ? "已安装到本站" : "尚未安装"
            }}</span>
          </li>
          <li :class="{ done: approvalState(selected, now).ready }">
            <strong>2. 审批</strong
            ><span
              >{{ approvalState(selected, now).label
              }}{{ approvalDraftDirty ? " · 有未保存修改" : "" }}</span
            >
          </li>
          <li :class="{ done: adminReady }">
            <strong>3. 启用</strong><span>{{ runtimeLabel(selected) }}</span>
          </li>
        </ol>
        <aside class="next-step" aria-label="下一步">
          <strong>下一步</strong>
          <p>{{ nextGuidance }}</p>
          <button
            v-if="needsReload"
            :disabled="busy"
            @click="choose(selected.release, true)"
          >
            刷新当前状态
          </button>
          <button
            v-else-if="
              !approvalState(selected, now).ready || approvalDraftDirty
            "
            :disabled="busy || !allowed('configure')"
            @click="showApproval"
          >
            前往审批设置
          </button>
          <button
            v-else-if="
              selected.installation?.desired === 'enabled' && !adminReady
            "
            :disabled="busy"
            @click="choose(selected.release, true)"
          >
            刷新当前状态
          </button>
        </aside>
        <fieldset :disabled="busy || needsReload" class="install-actions">
          <legend>1. 安装到本站</legend>
          <p v-if="selected.installation?.installed">
            已安装。安装不会自动批准权限或启动插件。
          </p>
          <button
            v-if="allowed('install') && !selected.installation?.installed"
            @click="command('install')"
          >
            安装
          </button>
          <button
            v-if="
              allowed('install') &&
              selected.installation?.installed &&
              selected.installation.version !== selected.release.version
            "
            @click="command('upgrade')"
          >
            切换到此版本（升级或回退）
          </button>
          <p
            v-if="
              selected.installation?.version &&
              selected.installation.version !== selected.release.version
            "
          >
            当前版本仍为
            {{
              selected.installation.version
            }}。切换准备完成后需显式启用此版本。
          </p>
        </fieldset>
        <details
          ref="approvalPanel"
          class="approval-panel"
          :open="approvalOpen"
          @toggle="
            approvalOpen = ($event.currentTarget as HTMLDetailsElement).open
          "
        >
          <summary>
            2. 审批设置
            <span class="status-chip">{{
              approvalState(selected, now).label
            }}</span
            ><span v-if="approvalDraftDirty" class="draft-tag"
              >有未保存修改</span
            >
          </summary>
          <form aria-label="版本授权审批" @submit.prevent="approve">
            <p class="help-text">
              “已保存”来自服务端；勾选框是本次草稿。保存审批会停用当前插件，之后需单独启用。
            </p>
            <p v-if="approvalDraftDirty" role="status" class="draft-notice">
              当前有未保存的勾选或修改。保存后才会生效，也可恢复已保存的设置。
            </p>
            <fieldset :disabled="busy || needsReload || !allowed('configure')">
              <legend>业务能力</legend>
              <div
                v-for="(consume, index) in selected.release.manifest.consumes ||
                []"
                :key="index"
                class="permission-row"
              >
                <label
                  ><input
                    type="checkbox"
                    v-model="capabilities[index]"
                    :aria-label="capabilityTitle(consume.capability)"
                  /><span
                    >{{ capabilityTitle(consume.capability) }}
                    <small>{{
                      consume.required ? "启用必需" : "可选"
                    }}</small></span
                  ><span class="saved-tag">{{
                    savedCapability(selected, consume)
                      ? "已保存：批准"
                      : "已保存：未批准"
                  }}</span
                  ><span
                    v-if="
                      !!capabilities[index] !==
                      savedCapability(selected, consume)
                    "
                    class="draft-tag"
                    >待保存</span
                  ></label
                >
                <details class="inline-details">
                  <summary>用途与权限详情</summary>
                  <p>{{ consume.purpose }}</p>
                  <small
                    >能力：{{ consume.capability }}；范围：{{
                      consume.scope.kind
                    }}</small
                  ><small v-if="consume.scope.collection_ids?.length"
                    >限定公开集合：{{
                      consume.scope.collection_ids.join("、")
                    }}</small
                  >
                </details>
              </div>
              <p v-if="!selected.release.manifest.consumes?.length">
                此版本不申请业务能力，仍需保存运行资源审批。
              </p>
            </fieldset>
            <fieldset :disabled="busy || needsReload || !allowed('configure')">
              <legend>运行资源</legend>
              <template v-if="selected.release.manifest.host_resources"
                ><label
                  ><input
                    type="checkbox"
                    v-model="resourcesApproved"
                  />批准插件后端所需运行资源
                  <span class="saved-tag">{{
                    selected.grant?.host_resources && selected.grant?.approved
                      ? "已保存：批准"
                      : "已保存：未批准"
                  }}</span></label
                >
                <p class="help-text">
                  需要后端程序的插件必须批准运行资源。额度默认取插件声明的上限，可展开调整。
                </p>
                <details>
                  <summary>查看或调整资源额度</summary>
                  <label v-for="key in resourceKeys" :key="key"
                    >{{ resourceLabels[key]
                    }}<input
                      :aria-label="resourceLabels[key]"
                      type="text"
                      inputmode="numeric"
                      v-model="limits[key]" /></label
                  ><label
                    v-for="target in selected.release.manifest.host_resources
                      .network_targets || []"
                    :key="target"
                    ><input
                      type="checkbox"
                      v-model="targets"
                      :value="target"
                    />网络目标：{{ target }}</label
                  >
                </details>
              </template>
              <p v-else>仅需插件界面，无须批准后端运行资源。</p>
              <label
                >授权有效期（最长 30 天）<input
                  aria-label="授权有效期"
                  type="datetime-local"
                  required
                  v-model="expiry"
              /></label>
            </fieldset>
            <label v-if="allowed('configure')"
              >审批原因<input
                aria-label="审批原因"
                required
                maxlength="512"
                v-model="reason"
                placeholder="说明批准这些能力和资源的用途"
            /></label>
            <div class="toolbar">
              <button
                v-if="allowed('configure')"
                :disabled="busy || needsReload"
                type="submit"
              >
                保存此版本审批</button
              ><button
                v-if="approvalDraftDirty"
                :disabled="busy"
                type="button"
                @click="restoreApprovalDraft"
              >
                恢复已保存的设置
              </button>
            </div>
          </form>
        </details>
        <fieldset :disabled="busy || needsReload" class="enable-actions">
          <legend>3. 启用插件</legend>
          <p v-if="enableBlocked" id="plugin-enable-requirement">
            {{ enableBlocked }}
          </p>
          <button
            v-if="allowed('lifecycle') && selected.installation?.installed"
            :disabled="!!enableBlocked"
            :aria-describedby="
              enableBlocked ? 'plugin-enable-requirement' : undefined
            "
            @click="command('enable')"
          >
            {{
              selected.installation?.version !== selected.release.version
                ? "启用此版本"
                : "启用"
            }}
          </button>
          <button
            v-if="
              allowed('lifecycle') &&
              selected.installation?.installed &&
              selected.installation.desired === 'enabled'
            "
            @click="command('disable')"
          >
            停用
          </button>
          <p
            v-if="
              selected.installation?.error_code === 'plugin.runtime_unavailable'
            "
          >
            插件尚未运行，需要部署者配置执行器；重复启用不会完成部署。
          </p>
          <details v-if="selected.release.manifest.runtime !== 'none'">
            <summary>执行器是什么？</summary>
            <p>
              执行器负责启动受限的插件后端、管理 CPU
              和内存，并在停用时回收程序。审批和启用请求完成后，仍需要部署环境提供执行器。
            </p>
          </details>
        </fieldset>
        <div class="usage-panel">
          <p aria-label="插件界面说明">{{ managementUI?.message }}</p>
          <button
            v-if="managementUI?.declared"
            :disabled="busy || needsReload || !managementUI.canOpen"
            @click="openUI"
          >
            打开插件管理界面</button
          ><button v-if="frame" @click="frame = null">关闭插件管理界面</button
          ><PluginV5Frame
            v-if="frame"
            :identity="frame.identity"
            :src="frame.src"
            title="插件隔离界面"
            :request="bridgeRequest"
          />
        </div>
        <details class="secondary-panel" aria-label="技术详情">
          <summary>技术详情</summary>
          <dl>
            <dt>插件标识</dt>
            <dd>{{ coordinateKey(selected.release) }}</dd>
            <dt>期望 / 观测状态</dt>
            <dd>
              {{ selected.installation?.desired || "未安装" }} /
              {{ selected.installation?.observed || "stopped" }}
            </dd>
            <dt>安装代次 / 授权修订</dt>
            <dd>
              {{ selected.installation?.generation || "0" }} /
              {{ selected.grant?.revision || "0" }}
            </dd>
            <dt>原始错误码</dt>
            <dd>{{ selected.installation?.error_code || "无" }}</dd>
            <dt>包摘要</dt>
            <dd>{{ selected.release.package_digest }}</dd>
            <dt>最近审批记录</dt>
            <dd>
              {{ selected.grant?.approved_by || "无" }}
              {{ selected.grant?.reason }}
            </dd>
          </dl>
          <button v-if="profile?.mfa_required" @click="confirmMFA">
            管理员二次验证（MFA）
          </button>
        </details>
        <details class="secondary-panel danger-zone">
          <summary>撤销授权与卸载</summary>
          <p>卸载默认保留插件自己的配置和数据。</p>
          <label v-if="allowed('configure')"
            >撤销原因<input
              aria-label="撤销原因"
              maxlength="512"
              v-model="reason" /></label
          ><button
            v-if="allowed('configure')"
            :disabled="busy || needsReload"
            @click="command('revoke')"
          >
            撤销此版本授权</button
          ><label v-if="allowed('uninstall')"
            ><input
              type="checkbox"
              v-model="purge"
            />卸载时清理插件自身数据</label
          ><button
            v-if="allowed('uninstall') && selected.installation?.installed"
            :disabled="busy || needsReload"
            @click="command('uninstall')"
          >
            卸载
          </button>
        </details>
      </section>
      <p v-else>选择左侧插件，查看已保存状态和下一步操作。</p>
    </div>
    <details v-if="operationID" class="secondary-panel">
      <summary>最近操作与排查</summary>
      <p>操作编号：{{ operationID }}</p>
      <button :disabled="busy" @click="readOperation">查询操作状态</button>
      <p>{{ operationState }}</p>
      <button
        v-if="
          operationFact &&
          ['waiting', 'failed'].includes(operationFact.status) &&
          allowed('lifecycle')
        "
        :disabled="busy || needsReload"
        @click="retryOperation"
      >
        重试此操作
      </button>
    </details>
  </main>
</template>
<script setup lang="ts">
import PluginV5Frame from "../components/PluginV5Frame.vue";
import type {
  PluginBridgeRequest,
  PluginFrameIdentity,
} from "../../../../../sdk/typescript/src/isolated-plugin-bridge";
import { stringifyExactJSON } from "../../../../../sdk/typescript/src/exact-json";
import { installPluginFramePolicy } from "../../../../../sdk/typescript/src/plugin-frame-policy";
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  getAdminAccessToken,
  getCurrentAdminProfile,
} from "../../identity/session";
import { pluginV5Api } from "../v5-api";
import { coordinateKey, type ConfigEntry, type Coordinate } from "../v5-config";
import {
  pluginDisplayName,
  capabilityTitle,
  savedCapability,
} from "../../../../../sdk/typescript/src/v5-lifecycle-presentation";
import {
  approvalState,
  enableBlockReason,
  managementUIState,
  runtimeLabel,
  sameConsumption,
} from "../v5-lifecycle";
import {
  compareExactNumbers,
  exactNumberText,
  ExactJSONNumber,
} from "../../../../../sdk/typescript/src/exact-json";
const items = ref<ConfigEntry[]>([]),
  cursor = ref(""),
  selected = ref<ConfigEntry | null>(null),
  busy = ref(false),
  error = ref(""),
  message = ref(""),
  purge = ref(false);
const visibleItems = computed(() =>
  items.value.filter((item) => item.release.storage !== "purged"),
);
const needsReload = ref(false),
  now = ref(Date.now()),
  enableBlocked = computed(() =>
    selected.value
      ? approvalDraftDirty.value
        ? "请先保存审批草稿，或恢复已保存的设置，再启用。"
        : enableBlockReason(selected.value, now.value)
      : "",
  ),
  managementUI = computed(() =>
    selected.value ? managementUIState(selected.value, now.value) : null,
  );
const reason = ref("");
const frame = ref<{
  identity: PluginFrameIdentity;
  src: string;
  proof: string;
  coordinate: Coordinate;
} | null>(null);
const capabilities = ref<boolean[]>([]),
  resourcesApproved = ref(false),
  limits = ref<Record<string, string>>({}),
  targets = ref<string[]>([]);
const resourceKeys = [
  "cpu_millis",
  "memory_mb",
  "storage_bytes",
  "max_concurrency",
  "timeout_ms",
];
const resourceLabels: Record<string, string> = {
  cpu_millis: "CPU 毫秒",
  memory_mb: "内存 MB",
  storage_bytes: "存储字节",
  max_concurrency: "并发上限",
  timeout_ms: "超时毫秒",
};
const approvalOpen = ref(false),
  approvalPanel = ref<HTMLDetailsElement | null>(null),
  draftBaseline = ref("");
function draftSnapshot() {
  return JSON.stringify({
    capabilities: capabilities.value,
    resources: resourcesApproved.value,
    limits: limits.value,
    targets: [...targets.value].sort(),
    expiry: expiry.value,
    reason: reason.value,
  });
}
function serverDraftSnapshot(item: ConfigEntry) {
  const m = item.release.manifest,
    g = item.grant;
  return JSON.stringify({
    capabilities: (m.consumes || []).map(
      (consume) =>
        g?.consumes?.some((approved: any) =>
          sameConsumption(approved, consume),
        ) || false,
    ),
    resources: !!g?.host_resources,
    limits: Object.fromEntries(
      resourceKeys.map((key) => [
        key,
        m.host_resources
          ? exactNumberText(g?.host_resources?.[key] ?? m.host_resources[key])
          : "",
      ]),
    ),
    targets: [...(g?.host_resources?.network_targets || [])].sort(),
    expiry:
      g?.expires_at_ms && g.expires_at_ms !== "0"
        ? localDateTime(Number(g.expires_at_ms))
        : expiry.value,
    reason: "",
  });
}
const approvalDraftDirty = computed(
  () =>
    !!selected.value &&
    !!draftBaseline.value &&
    draftSnapshot() !== draftBaseline.value,
);
const adminReady = computed(
  () =>
    !!selected.value &&
    approvalState(selected.value, now.value).ready &&
    selected.value.installation?.version === selected.value.release.version &&
    (selected.value.installation.desired_version ||
      selected.value.installation.version) === selected.value.release.version &&
    selected.value.installation?.desired === "enabled" &&
    !selected.value.installation.error_code &&
    (selected.value.installation.observed === "ready" ||
      (selected.value.release.manifest.runtime === "none" &&
        selected.value.installation.observed === "stopped")),
);
const nextGuidance = computed(() => {
  const item = selected.value;
  if (!item) return "请选择一个插件。";
  if (needsReload.value) return "当前状态尚未确认，请先刷新当前状态。";
  if (!item.installation?.installed)
    return "点击安装，将此插件加入本站。安装后再审批权限和启用。";
  if (approvalDraftDirty.value)
    return "当前有未保存的修改。请保存审批草稿，或恢复已保存的设置，再启用。";
  if (!approvalState(item, now.value).ready)
    return `${approvalState(item, now.value).label}。展开审批设置，选择必需能力及运行资源，填写原因并保存。`;
  if (
    item.installation.version !== item.release.version &&
    !(
      item.installation.desired_version === item.release.version &&
      item.installation.desired === "disabled"
    )
  )
    return "审批已保存。请先点击“切换到此版本”，准备完成后再启用。";
  if (item.installation.desired !== "enabled")
    return "审批已保存。点击下方“启用”，让宿主开始加载此插件。";
  if (item.installation.error_code === "plugin.runtime_unavailable")
    return "正在等待部署者配置插件执行器。执行器负责启动后端程序；当前尚未运行。";
  if (adminReady.value)
    return "插件已启用。按下方使用说明进入管理界面或用户端功能。";
  return "启用请求已提交，尚未就绪。刷新当前状态查看结果，无需重复启用。";
});
function showApproval() {
  approvalOpen.value = true;
  approvalPanel.value?.scrollIntoView?.({ behavior: "smooth", block: "start" });
}
function restoreApprovalDraft() {
  if (!selected.value || busy.value) return;
  populateApprovalDraft(selected.value);
}
function populateApprovalDraft(item: ConfigEntry) {
  const m = item.release.manifest,
    g = item.grant;
  capabilities.value = (m.consumes || []).map(
    (consume) =>
      g?.consumes?.some((approved: any) =>
        sameConsumption(approved, consume),
      ) || false,
  );
  resourcesApproved.value = !!g?.host_resources;
  limits.value = Object.fromEntries(
    resourceKeys.map((key) => [
      key,
      m.host_resources
        ? exactNumberText(g?.host_resources?.[key] ?? m.host_resources[key])
        : "",
    ]),
  );
  targets.value = [...(g?.host_resources?.network_targets || [])];
  expiry.value = localDateTime(
    g?.expires_at_ms && g.expires_at_ms !== "0"
      ? Number(g.expires_at_ms)
      : Date.now() + 24 * 60 * 60 * 1000,
  );
  reason.value = "";
  draftBaseline.value = draftSnapshot();
}
const archive = ref<File>(),
  releaseFile = ref<File>(),
  operationID = ref(""),
  operationState = ref("");
const profile = ref(getCurrentAdminProfile());
const operationFact = ref<any>(null);
function localDateTime(ms: number) {
  const d = new Date(ms);
  d.setMinutes(d.getMinutes() - d.getTimezoneOffset());
  return d.toISOString().slice(0, 16);
}
const expiry = ref(localDateTime(Date.now() + 24 * 60 * 60 * 1000));
let epoch = 0,
  disposed = false,
  timer: ReturnType<typeof setInterval>;
let statusTimer: ReturnType<typeof setTimeout> | undefined;
function followPendingStatus(c: Coordinate, turn: number, attempt = 0) {
  clearTimeout(statusTimer);
  const state = selected.value?.installation;
  if (
    attempt >= 10 ||
    !state ||
    state.desired !== "enabled" ||
    state.observed !== "pending" ||
    state.error_code
  )
    return;
  statusTimer = setTimeout(async () => {
    if (!current() || turn !== epoch || busy.value || needsReload.value) return;
    try {
      const result = await pluginV5Api.detail(c);
      if (!current() || turn !== epoch) return;
      if (
        result.code !== 0 ||
        coordinateKey(result.data.release) !== coordinateKey(c)
      )
        return;
      selected.value = result.data;
      draftBaseline.value = serverDraftSnapshot(result.data);
      items.value = items.value.map((item) =>
        coordinateKey(item.release) === coordinateKey(c) ? result.data : item,
      );
      followPendingStatus(c, turn, attempt + 1);
    } catch (e: any) {
      if (!current() || turn !== epoch) return;
      if ([401, 403, 404].includes(e.status)) {
        selected.value = null;
        frame.value = null;
        items.value = [];
        error.value = "当前版本的读取权限或可见性已变化，请重新加载。";
      }
    }
  }, 1200);
}
const stamp = () =>
  JSON.stringify([getAdminAccessToken(), getCurrentAdminProfile()]);
let identity = stamp();
const allowed = (kind: string) =>
  profile.value?.permission_codes.includes(`plugin.plugin.${kind}`) === true;
const file = (e: Event) => (e.target as HTMLInputElement).files?.[0];
function current() {
  if (disposed) return false;
  if (identity === stamp()) return true;
  identity = stamp();
  clearTimeout(statusTimer);
  epoch++;
  items.value = [];
  cursor.value = "";
  selected.value = null;
  needsReload.value = false;
  draftBaseline.value = "";
  approvalOpen.value = false;
  frame.value = null;
  archive.value = undefined;
  releaseFile.value = undefined;
  operationID.value = "";
  operationFact.value = null;
  operationState.value = "";
  profile.value = getCurrentAdminProfile();
  busy.value = false;
  message.value = "";
  error.value = "身份或权限已变化，请重新加载。";
  return false;
}
function confirmMFA() {
  if (current() && profile.value?.mfa_required !== false)
    window.dispatchEvent(new Event("campusos:admin-mfa-step-up-required"));
}
async function load(more: boolean) {
  current();
  if (busy.value || !allowed("read")) return;
  const turn = ++epoch;
  clearTimeout(statusTimer);
  busy.value = true;
  selected.value = null;
  needsReload.value = false;
  frame.value = null;
  error.value = "";
  if (!more) {
    items.value = [];
    cursor.value = "";
  }
  try {
    const r = await pluginV5Api.list(cursor.value);
    if (!current() || turn !== epoch) return;
    if (r.code !== 0 || !Array.isArray(r.data?.items))
      throw Error("清单不可用。");
    const seen = new Set(items.value.map((i) => coordinateKey(i.release)));
    items.value.push(
      ...r.data.items.filter((i) => !seen.has(coordinateKey(i.release))),
    );
    cursor.value = r.data.next_cursor || "";
  } catch (e: any) {
    if (current() && turn === epoch) error.value = e.message || "读取失败。";
  } finally {
    if (turn === epoch) busy.value = false;
  }
}
async function choose(c: Coordinate, preserveContext = false) {
  if (!current() || busy.value) return;
  const turn = ++epoch;
  clearTimeout(statusTimer);
  busy.value = true;
  if (!preserveContext) selected.value = null;
  needsReload.value = true;
  frame.value = null;
  error.value = "";
  if (!preserveContext) {
    purge.value = false;
    reason.value = "";
  }
  try {
    const r = await pluginV5Api.detail(c);
    if (!current() || turn !== epoch) return;
    if (r.code !== 0 || coordinateKey(r.data.release) !== coordinateKey(c))
      throw Error("版本已变化，请重新加载。");
    selected.value = r.data;
    purge.value = false;
    reason.value = "";
    needsReload.value = false;
    items.value = items.value.map((item) =>
      coordinateKey(item.release) === coordinateKey(c) ? r.data : item,
    );
    populateApprovalDraft(r.data);
    approvalOpen.value = !approvalState(r.data).ready;
    followPendingStatus(c, turn);
  } catch (e: any) {
    if (current() && turn === epoch) {
      if ([401, 403, 404].includes(e.status)) {
        selected.value = null;
        items.value =
          e.status === 404
            ? items.value.filter(
                (item) => coordinateKey(item.release) !== coordinateKey(c),
              )
            : [];
      }
      error.value = e.message || "详情不可用，请刷新当前状态。";
    }
  } finally {
    if (turn === epoch) busy.value = false;
  }
}
async function write(intent: Record<string, unknown>) {
  if (!current() || busy.value || needsReload.value || !selected.value) return;
  const c = { ...selected.value.release },
    turn = epoch;
  busy.value = true;
  error.value = "";
  message.value = "";
  try {
    const r = await pluginV5Api.command(c, intent);
    if (!current() || turn !== epoch) return;
    if (r.code !== 0 || !r.data?.id) throw Error("操作结果未确认。");
    operationID.value = r.data.id;
    operationFact.value = null;
    operationState.value = r.data.status;
    busy.value = false;
    await choose(c, true);
    if (current())
      message.value = needsReload.value
        ? "操作已提交，当前状态尚未确认，请刷新当前状态。"
        : selected.value?.installation?.error_code ===
            "plugin.runtime_unavailable"
          ? "启用请求正在等待执行器，插件尚未运行。"
          : "操作已提交，状态以服务器读取结果为准。";
  } catch (e: any) {
    if (current() && turn === epoch) {
      frame.value = null;
      needsReload.value = true;
      // A rejected command must not discard the selected version or approval
      // draft. Re-read current facts once; never replay the mutation.
      try {
        const state = await pluginV5Api.detail(c);
        if (!current() || turn !== epoch) return;
        if (
          state.code !== 0 ||
          coordinateKey(state.data.release) !== coordinateKey(c)
        )
          throw Error("当前版本状态尚未确认。");
        selected.value = state.data;
        draftBaseline.value = serverDraftSnapshot(state.data);
        needsReload.value = false;
        items.value = items.value.map((item) =>
          coordinateKey(item.release) === coordinateKey(c) ? state.data : item,
        );
      } catch (readError: any) {
        if (!current() || turn !== epoch) return;
        if ([401, 403, 404].includes(readError.status)) {
          selected.value = null;
          items.value =
            readError.status === 404
              ? items.value.filter(
                  (item) => coordinateKey(item.release) !== coordinateKey(c),
                )
              : [];
        }
      }
      if (!current() || turn !== epoch) return;
      const explanation =
        e.status === 409
          ? "状态已被其他操作修改。"
          : intent.kind === "enable" && e.status === 403
            ? "启用被拒绝，请核对当前审批、配置、权限及管理员身份。"
            : e.message || "操作结果未确认。";
      error.value = `${explanation} ${needsReload.value ? "请重新加载确认当前状态。" : "已重新读取当前状态，请核对后再操作。"} 未自动重试。`;
    }
  } finally {
    if (turn === epoch) busy.value = false;
  }
}
function command(kind: string) {
  if (!selected.value) return;
  if (kind === "enable") {
    const blocked = approvalDraftDirty.value
      ? "请先保存审批草稿，或恢复已保存的设置，再启用。"
      : enableBlockReason(selected.value);
    if (blocked) {
      error.value = blocked;
      return;
    }
  }
  const permission =
    kind === "revoke"
      ? "configure"
      : kind === "install" || kind === "upgrade"
        ? "install"
        : kind === "uninstall"
          ? "uninstall"
          : "lifecycle";
  if (!allowed(permission)) return;
  if (kind === "revoke" && !reason.value.trim()) {
    error.value = "请填写撤销原因。";
    return;
  }
  void write({
    kind,
    ...(kind === "revoke"
      ? {
          expected_revision: selected.value.grant?.revision || "0",
          reason: reason.value.trim(),
        }
      : {
          expected_generation: selected.value.installation?.generation || "0",
        }),
    ...(kind === "uninstall" ? { purge_data: purge.value } : {}),
  });
}
function approve() {
  if (!current() || !selected.value || !allowed("configure")) return;
  if (!reason.value.trim()) {
    error.value = "请填写审批原因。";
    return;
  }
  const m = selected.value.release.manifest;
  const expires = Date.parse(expiry.value);
  if (
    !Number.isSafeInteger(expires) ||
    expires <= Date.now() ||
    expires > Date.now() + 30 * 24 * 60 * 60 * 1000
  ) {
    error.value = "请选择未来 30 天内的授权截止时间。";
    return;
  }
  let resources: Record<string, unknown> | undefined;
  try {
    if (resourcesApproved.value && m.host_resources) {
      resources = { network_targets: [...targets.value] };
      for (const key of resourceKeys) {
        if (
          !/^[1-9][0-9]*$/.test(limits.value[key] || "") ||
          compareExactNumbers(limits.value[key], m.host_resources[key]) > 0
        )
          throw Error(`${resourceLabels[key]}必须为正整数且不超过声明上限。`);
        resources[key] = new ExactJSONNumber(limits.value[key]);
      }
      if (
        targets.value.some((t) => !m.host_resources.network_targets.includes(t))
      )
        throw Error("网络目标不在声明范围。");
    }
  } catch (e: any) {
    error.value = e.message;
    return;
  }
  void write({
    kind: "grant",
    approved: true,
    reason: reason.value.trim(),
    expires_at_ms: String(expires),
    expected_revision: selected.value.grant?.revision || "0",
    consumes: (m.consumes || []).filter((_, i) => capabilities.value[i]),
    ...(resources ? { host_resources: resources } : {}),
  });
}
async function importPackage() {
  if (
    !current() ||
    busy.value ||
    !allowed("install") ||
    !archive.value ||
    !releaseFile.value
  )
    return;
  const turn = ++epoch;
  busy.value = true;
  error.value = "";
  try {
    const r = await pluginV5Api.importPackage(archive.value, releaseFile.value);
    if (!current() || turn !== epoch) return;
    if (r.code !== 0 || !r.data?.id) throw Error("导入结果未确认。");
    operationID.value = r.data.id;
    operationFact.value = null;
    operationState.value = r.data.status;
    archive.value = undefined;
    releaseFile.value = undefined;
    busy.value = false;
    await load(false);
    message.value = "签名包已验证并导入，请单独安装和审批。";
  } catch (e: any) {
    if (current() && turn === epoch) error.value = e.message || "导入失败。";
  } finally {
    if (turn === epoch) busy.value = false;
  }
}
async function openUI() {
  if (!current() || busy.value || needsReload.value || !selected.value) return;
  const ui = managementUIState(selected.value);
  if (!ui.canOpen) {
    error.value = ui.message;
    return;
  }
  const c = { ...selected.value.release },
    turn = epoch;
  busy.value = true;
  frame.value = null;
  error.value = "";
  try {
    const name = c.publisher + "." + c.plugin_key;
    let after = "",
      surface: any;
    for (let page = 0; page < 4; page++) {
      const r = await pluginV5Api.uiManifest(after);
      if (!current() || turn !== epoch) return;
      if (r.code !== 0)
        throw Error("当前插件管理界面清单不可用，请刷新当前状态后再试。");
      installPluginFramePolicy(r.data.frame_src);
      surface = r.data.plugins.find(
        (p) => p.name === name && p.version === c.version,
      )?.ui?.surfaces?.[0];
      if (surface) break;
      after = r.data.next_cursor;
      if (!after) break;
    }
    if (!surface)
      throw Error(
        after
          ? "已达到本次4页界面查找上限，仍未找到此版本的管理界面。"
          : "服务端当前未提供此版本的管理界面。请刷新当前状态，核对审批、启用及界面网关配置。",
      );
    if (!surface.frame || !surface.context_proof)
      throw Error("服务端未返回有效的管理界面地址或授权证明，请刷新当前状态。");
    const url = new URL(surface.frame.src);
    url.searchParams.set("host_origin", window.location.origin);
    frame.value = {
      identity: {
        pluginKey: name,
        pluginVersion: c.version,
        surfaceID: surface.id.slice(name.length + 1),
        audience: "admin",
        origin: surface.frame.origin,
      },
      src: url.toString(),
      proof: surface.context_proof,
      coordinate: c,
    };
  } catch (e: any) {
    if (current() && turn === epoch) error.value = e.message || "界面不可用。";
  } finally {
    if (turn === epoch) busy.value = false;
  }
}
async function bridgeRequest(request: PluginBridgeRequest) {
  const context = frame.value;
  if (
    !current() ||
    !context ||
    request.method !== "config.read" ||
    Object.keys(request.params).length
  )
    throw Error("当前 Bridge 方法不可用。");
  const check = async () => {
    const state = await pluginV5Api.uiState(context.coordinate);
    if (
      !current() ||
      frame.value !== context ||
      state.code !== 0 ||
      state.data.proof !== context.proof
    )
      throw Error("界面权限或配置已变化。");
  };
  await check();
  const result = await pluginV5Api.detail(context.coordinate);
  if (result.code !== 0) throw Error("配置不可用。");
  await check();
  return {
    revision: result.data.configuration?.revision || "0",
    values_json: stringifyExactJSON(result.data.configuration?.values || {}),
  };
}
async function readOperation() {
  if (!current() || busy.value) return;
  const turn = epoch;
  try {
    const r = await pluginV5Api.operation(operationID.value);
    if (current() && turn === epoch && r.code === 0) {
      operationState.value = `${r.data.status} ${r.data.error_code || ""}`;
      operationFact.value = r.data;
    }
  } catch (e: any) {
    if (current() && turn === epoch) error.value = e.message || "操作不可用。";
  }
}
function retryOperation() {
  if (
    !current() ||
    !allowed("lifecycle") ||
    !selected.value ||
    !operationFact.value
  )
    return;
  const op = operationFact.value;
  if (coordinateKey(op) !== coordinateKey(selected.value.release)) {
    error.value = "请先选择此操作对应的插件版本。";
    return;
  }
  void write({
    kind: "retry",
    operation_id: op.id,
    expected_revision: op.revision,
  });
}
onMounted(() => {
  void load(false);
  timer = setInterval(() => {
    current();
    now.value = Math.floor(Date.now() / 1000) * 1000;
  }, 200);
});
onBeforeUnmount(() => {
  disposed = true;
  epoch++;
  clearInterval(timer);
  clearTimeout(statusTimer);
});
</script>
<style scoped>
.v5-management {
  display: grid;
  gap: 16px;
  max-width: 1200px;
}
header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
}
.workflow {
  padding: 12px 16px;
  background: #eef6ff;
  border: 1px solid #cbd5e1;
  border-radius: 6px;
}
.columns {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) minmax(0, 3fr);
  gap: 16px;
}
nav,
section,
form {
  min-width: 0;
}
nav {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
fieldset {
  margin: 12px 0;
  padding: 16px;
  border: 1px solid #cbd5e1;
}
label {
  display: block;
  margin: 12px 0;
}
button,
input {
  font: inherit;
  padding: 8px;
  max-width: 100%;
}
button {
  margin: 4px;
}
small {
  display: block;
  overflow-wrap: anywhere;
}
p {
  overflow-wrap: anywhere;
}
[role="alert"] {
  color: #a61b1b;
}
@media (max-width: 700px) {
  .columns {
    grid-template-columns: minmax(0, 1fr);
  }
}

.page-header h2 {
  margin: 0;
}
.page-header p,
.help-text {
  color: #637083;
}
.toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}
.plugin-list button {
  text-align: left;
  border: 1px solid #dbe3e8;
  background: #fff;
  border-radius: 8px;
  padding: 14px;
}
.plugin-list button[aria-pressed="true"] {
  border-color: #15805d;
  background: #effaf5;
}
.plugin-detail {
  min-width: 0;
}
.plugin-detail > header {
  display: flex;
  align-items: center;
}
.status-chip,
.saved-tag,
.draft-tag {
  display: inline-block;
  font-size: 13px;
  border-radius: 5px;
  padding: 3px 7px;
  margin: 3px;
  background: #eef3f6;
  color: #33465b;
}
.saved-tag {
  color: #236448;
  background: #eff7f1;
}
.draft-tag,
.draft-notice {
  color: #8c5500;
  background: #fff5df;
}
.plugin-steps {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  padding: 0;
  gap: 10px;
  list-style: none;
}
.plugin-steps li {
  padding: 12px;
  border: 1px solid #dbe3e8;
  border-radius: 8px;
  background: #f8fafb;
}
.plugin-steps li.done {
  border-color: #92c9af;
  background: #effaf4;
}
.plugin-steps span {
  display: block;
  font-size: 14px;
  margin-top: 7px;
}
.next-step {
  padding: 14px 18px;
  border-left: 4px solid #15805d;
  background: #eff7f3;
  border-radius: 6px;
}
.next-step p {
  margin: 6px 0;
}
.secondary-panel,
.approval-panel {
  border: 1px solid #dbe3e8;
  border-radius: 8px;
  padding: 12px 16px;
  margin: 10px 0;
}
summary {
  cursor: pointer;
  padding: 6px 0;
  font-weight: 600;
}
.permission-row {
  padding: 8px 0;
  border-bottom: 1px solid #edf1f4;
}
.permission-row > label {
  display: flex;
  flex-wrap: wrap;
  align-items: start;
  gap: 6px;
}
.inline-details {
  margin-left: 26px;
  font-size: 13px;
  color: #637083;
}
.inline-details summary {
  font-weight: 400;
}
.draft-notice {
  padding: 10px 12px;
  border-radius: 5px;
}
dd {
  margin: 4px 0 12px;
  overflow-wrap: anywhere;
  color: #637083;
}
.version {
  color: #637083;
}
@media (max-width: 700px) {
  .plugin-steps {
    grid-template-columns: 1fr;
  }
  .columns {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
