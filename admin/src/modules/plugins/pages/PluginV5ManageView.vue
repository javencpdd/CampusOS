<template>
  <main class="v5-management">
    <header>
      <h2>插件管理</h2>
      <button :disabled="busy" @click="load(false)">重新加载</button
      ><button @click="confirmMFA">确认管理员身份</button
      ><router-link to="/plugin-config">系统配置与密钥引用</router-link>
    </header>
    <p role="alert" v-if="error">{{ error }}</p>
    <p role="status" v-if="message">{{ message }}</p>
    <form
      v-if="allowed('install')"
      aria-label="导入签名插件包"
      @submit.prevent="importPackage"
    >
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
    <div class="columns">
      <nav aria-label="插件版本">
        <button
          v-for="item in items"
          :key="coordinateKey(item.release)"
          :disabled="busy"
          @click="choose(item.release)"
        >
          {{ item.release.manifest.display_name || item.release.plugin_key
          }}<small>{{ coordinateKey(item.release) }}</small></button
        ><button v-if="cursor" :disabled="busy" @click="load(true)">
          加载更多
        </button>
        <p v-if="!items.length">暂无可见的插件包。</p>
      </nav>
      <section v-if="selected" aria-label="插件生命周期">
        <h3>{{ coordinateKey(selected.release) }}</h3>
        <p>
          期望：{{ selected.installation?.desired || "未安装" }}；后端观测：{{
            selected.installation?.observed || "stopped"
          }}；{{ selected.installation?.error_code }}
        </p>
        <p>
          安装代次：{{ selected.installation?.generation || "0" }}；授权修订：{{
            selected.grant?.revision || "0"
          }}
        </p>
        <button :disabled="busy" @click="openUI">打开隔离界面</button
        ><button v-if="frame" @click="frame = null">关闭隔离界面</button
        ><PluginV5Frame
          v-if="frame"
          :identity="frame.identity"
          :src="frame.src"
          title="插件隔离界面"
          :request="bridgeRequest"
        />
        <fieldset :disabled="busy">
          <legend>生命周期</legend>
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
          <template
            v-if="
              selected.installation?.installed &&
              selected.installation.version === selected.release.version
            "
            ><button v-if="allowed('lifecycle')" @click="command('enable')">
              启用</button
            ><button v-if="allowed('lifecycle')" @click="command('disable')">
              停用</button
            ><label v-if="allowed('uninstall')"
              ><input
                type="checkbox"
                v-model="purge"
              />卸载时清理插件自身数据</label
            ><button v-if="allowed('uninstall')" @click="command('uninstall')">
              卸载
            </button></template
          >
        </fieldset>
        <form aria-label="版本授权审批" @submit.prevent="approve">
          <fieldset :disabled="busy || !allowed('configure')">
            <legend>业务能力</legend>
            <p>仅审批此不可变版本声明的能力；用户仍需同意自己的数据用途。</p>
            <label
              v-for="(consume, index) in selected.release.manifest.consumes ||
              []"
              :key="index"
              ><input type="checkbox" v-model="capabilities[index]" />{{
                consume.capability
              }}
              · {{ consume.scope.kind }} ·
              {{ consume.required ? "必需" : "可选"
              }}<small>{{ consume.purpose }}</small
              ><small v-if="consume.scope.collection_ids?.length"
                >公开集合 ID：{{
                  consume.scope.collection_ids.join("、")
                }}</small
              ></label
            >
            <p v-if="!selected.release.manifest.consumes?.length">
              此版本未声明业务能力。
            </p>
          </fieldset>
          <fieldset :disabled="busy || !allowed('configure')">
            <legend>运行资源</legend>
            <template v-if="selected.release.manifest.host_resources"
              ><label
                ><input
                  type="checkbox"
                  v-model="resourcesApproved"
                />批准运行资源（不得超过声明上限）</label
              ><label v-for="key in resourceKeys" :key="key"
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
              ></template
            >
            <p v-else>此版本未申请后端运行资源。</p>
            <label
              >授权有效期（最长 30 天，适用于业务能力与运行资源）<input
                aria-label="授权有效期"
                type="datetime-local"
                required
                v-model="expiry"
            /></label>
          </fieldset>
          <label v-if="allowed('configure')"
            >审批或撤销原因<input
              aria-label="审批原因"
              required
              maxlength="512"
              v-model="reason"
          /></label>
          <p v-if="selected.grant?.approved_by">
            审批人：{{ selected.grant.approved_by }}；原因：{{
              selected.grant.reason
            }}
          </p>
          <button v-if="allowed('configure')" :disabled="busy" type="submit">
            保存此版本审批</button
          ><button
            v-if="allowed('configure')"
            :disabled="busy"
            type="button"
            @click="command('revoke')"
          >
            撤销此版本授权
          </button>
        </form>
      </section>
      <p v-else>请选择一个版本。</p>
    </div>
    <aside v-if="operationID">
      <p>操作编号：{{ operationID }}</p>
      <button :disabled="busy" @click="readOperation">查询操作状态</button>
      <p>{{ operationState }}</p>
      <button
        v-if="
          operationFact &&
          ['waiting', 'failed'].includes(operationFact.status) &&
          allowed('lifecycle')
        "
        :disabled="busy"
        @click="retryOperation"
      >
        重试此操作
      </button>
    </aside>
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
import { onBeforeUnmount, onMounted, ref } from "vue";
import {
  getAdminAccessToken,
  getCurrentAdminProfile,
} from "../../identity/session";
import { pluginV5Api } from "../v5-api";
import { coordinateKey, type ConfigEntry, type Coordinate } from "../v5-config";
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
  epoch++;
  items.value = [];
  cursor.value = "";
  selected.value = null;
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
  if (current())
    window.dispatchEvent(new Event("campusos:admin-mfa-step-up-required"));
}
async function load(more: boolean) {
  current();
  if (busy.value || !allowed("read")) return;
  const turn = ++epoch;
  busy.value = true;
  selected.value = null;
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
async function choose(c: Coordinate) {
  if (!current() || busy.value) return;
  const turn = ++epoch;
  busy.value = true;
  selected.value = null;
  frame.value = null;
  error.value = "";
  purge.value = false;
  reason.value = "";
  try {
    const r = await pluginV5Api.detail(c);
    if (!current() || turn !== epoch) return;
    if (r.code !== 0 || coordinateKey(r.data.release) !== coordinateKey(c))
      throw Error("版本已变化，请重新加载。");
    selected.value = r.data;
    expiry.value = localDateTime(
      r.data.grant?.expires_at_ms && r.data.grant.expires_at_ms !== "0"
        ? Number(r.data.grant.expires_at_ms)
        : Date.now() + 24 * 60 * 60 * 1000,
    );
    const m = r.data.release.manifest,
      g = r.data.grant;
    capabilities.value = (m.consumes || []).map(
      (d) =>
        g?.consumes?.some(
          (a: any) => JSON.stringify(a) === JSON.stringify(d),
        ) || false,
    );
    resourcesApproved.value = !!g?.host_resources;
    limits.value = Object.fromEntries(
      resourceKeys.map((k) => [
        k,
        m.host_resources
          ? exactNumberText(g?.host_resources?.[k] ?? m.host_resources[k])
          : "",
      ]),
    );
    targets.value = [...(g?.host_resources?.network_targets || [])];
  } catch (e: any) {
    if (current() && turn === epoch) error.value = e.message || "详情不可用。";
  } finally {
    if (turn === epoch) busy.value = false;
  }
}
async function write(intent: Record<string, unknown>) {
  if (!current() || busy.value || !selected.value) return;
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
    await choose(c);
    if (current()) message.value = "操作已提交，状态以服务器读取结果为准。";
  } catch (e: any) {
    if (current() && turn === epoch) {
      selected.value = null;
      frame.value = null;
      error.value =
        e.status === 409
          ? "状态已被其他操作修改，请重新加载。"
          : `${e.message || "操作结果未确认。"} 请重新加载确认后再操作。`;
    }
  } finally {
    if (turn === epoch) busy.value = false;
  }
}
function command(kind: string) {
  if (!selected.value) return;
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
  if (!current() || busy.value || !selected.value) return;
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
      if (r.code !== 0) throw Error("此版本的管理界面尚未启用或不可用。");
      installPluginFramePolicy(r.data.frame_src);
      surface = r.data.plugins.find(
        (p) => p.name === name && p.version === c.version,
      )?.ui?.surfaces?.[0];
      if (surface) break;
      after = r.data.next_cursor;
      if (!after) break;
    }
    if (!surface?.frame || !surface.context_proof)
      throw Error("此版本的管理界面尚未启用、不可用或超出本次加载上限。");
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
  timer = setInterval(current, 200);
});
onBeforeUnmount(() => {
  disposed = true;
  epoch++;
  clearInterval(timer);
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
</style>
