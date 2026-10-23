<template>
  <main class="machines">
    <header>
      <h2>机器集成凭据</h2>
      <p>
        仅允许指定通道读取指定的公开帖子。帖子转为私有、待审核或回收站后，凭据也无法读取。
      </p>
    </header>
    <p v-if="!mayRead" role="alert">当前管理员没有查看机器集成的权限。</p>
    <template v-else>
      <p v-if="error" role="alert" class="error">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <p v-if="loading" role="status">正在加载…</p>
      <button :disabled="busy" @click="loadPrincipals()">刷新集成列表</button>
      <form v-if="mayManage" @submit.prevent="create">
        <label
          >集成名称
          <input v-model="name" aria-label="集成名称" maxlength="120" required
        /></label>
        <button :disabled="busy || !name.trim()">创建集成</button>
      </form>
      <p v-if="!loading && !principals.length">
        尚无机器集成。创建后可签发独立凭据。
      </p>
      <ul aria-label="集成列表">
        <li v-for="p in principals" :key="p.id">
          <button
            :disabled="busy"
            :aria-pressed="selected?.id === p.id"
            @click="select(p)"
          >
            {{ p.name }} · {{ statusLabel(p.status) }}
          </button>
        </li>
      </ul>
      <button
        v-if="principalCursor"
        :disabled="busy"
        @click="loadPrincipals(principalCursor)"
      >
        更多集成
      </button>
      <section v-if="selected" aria-label="集成详情">
        <h3>{{ selected.name }} · {{ statusLabel(selected.status) }}</h3>
        <p>集成 ID：{{ selected.id }}</p>
        <div v-if="mayManage" class="actions">
          <button
            v-if="selected.status === 'active'"
            :disabled="busy"
            @click="changeStatus('suspended')"
          >
            暂停集成
          </button>
          <button
            v-if="selected.status === 'suspended'"
            :disabled="busy"
            @click="changeStatus('active')"
          >
            恢复集成
          </button>
          <button
            v-if="selected.status !== 'revoked'"
            :disabled="busy"
            @click="changeStatus('revoked')"
          >
            撤销集成
          </button>
        </div>
        <p v-if="selected.status === 'revoked'">
          该集成已永久撤销，所有关联凭据失效。
        </p>
        <p v-if="selected.status === 'suspended'">
          暂停期间所有关联凭据无法使用，恢复后仍须满足原有效期与范围。
        </p>
        <p v-if="!keys.length && !loading">尚未签发凭据。</p>
        <ul aria-label="凭据列表">
          <li v-for="key in keys" :key="key.id">
            <button
              :disabled="busy"
              :aria-pressed="chosen?.id === key.id"
              @click="choose(key)"
            >
              {{ key.prefix }} · {{ statusLabel(key.status) }}
            </button>
            <span
              >到期：{{ new Date(key.expires_at_ms).toLocaleString() }}；{{
                key.rules.length
              }}
              条白名单</span
            >
          </li>
        </ul>
        <button v-if="keyCursor" :disabled="busy" @click="loadKeys(keyCursor)">
          更多凭据
        </button>
        <form
          v-if="mayManage && selected.status === 'active'"
          @submit.prevent="issue(false)"
        >
          <h3>签发与范围</h3>
          <p>
            填写精确帖子 ID，以逗号分隔；最多 32 个。有效期最长 90
            天。每个通道单独授权。
          </p>
          <label
            >帖子 ID
            <input
              v-model="resources"
              aria-label="帖子 ID"
              required
              placeholder="例如 123,124"
          /></label>
          <label
            ><input v-model="http" type="checkbox" /> HTTP 读取公开帖子</label
          >
          <label
            ><input v-model="mcp" type="checkbox" /> MCP 读取公开帖子</label
          >
          <label
            >有效天数
            <input
              v-model.number="days"
              aria-label="有效天数"
              type="number"
              min="1"
              max="90"
          /></label>
          <button :disabled="busy || !validDraft">签发新凭据</button>
          <template v-if="chosen?.status === 'active'">
            <button
              type="button"
              :disabled="busy || !validDraft"
              @click="issue(true)"
            >
              轮换所选凭据
            </button>
            <button
              type="button"
              :disabled="busy || !validDraft"
              @click="scope"
            >
              更新所选范围
            </button>
          </template>
        </form>
        <button
          v-if="mayManage && chosen?.status === 'active'"
          :disabled="busy"
          @click="revoke"
        >
          撤销所选凭据
        </button>
        <p v-if="mayManage">
          签发、轮换、修改范围与撤销均需当前管理员近期完成多因素验证。轮换成功时旧凭据立即失效。
        </p>
      </section>
    </template>
    <section
      v-if="secret"
      role="dialog"
      aria-modal="true"
      aria-label="一次性凭据"
      class="secret"
    >
      <h3>凭据仅显示这一次</h3>
      <p>
        请保存到集成客户端的秘密配置。关闭、离开页面或切换窗口后无法再次查看。
      </p>
      <code data-testid="one-time-token">{{ secret }}</code>
      <button @click="copy">复制凭据</button
      ><button @click="secret = ''">我已保存，关闭</button>
    </section>
  </main>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { getCurrentAdminProfile } from "../../identity/session";
import {
  machineApi,
  type MachinePrincipal,
  type MachineCredential,
  type MachineRule,
  type MachineInventory,
  type MachineIssueResult,
} from "../principals-api";
let alive = true;
const principals = ref<MachinePrincipal[]>([]),
  keys = ref<MachineCredential[]>([]),
  selected = ref<MachinePrincipal>(),
  chosen = ref<MachineCredential>();
const name = ref(""),
  resources = ref(""),
  http = ref(true),
  mcp = ref(false),
  days = ref(7),
  secret = ref(""),
  error = ref(""),
  notice = ref(""),
  loading = ref(false),
  busy = ref(false),
  principalCursor = ref(""),
  keyCursor = ref("");
const codes = getCurrentAdminProfile()?.permission_codes || [];
const mayRead = codes.includes("identity.integration.read"),
  mayManage = codes.includes("identity.integration.manage");
const ids = computed(() =>
  resources.value
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean),
);
const validDraft = computed(
  () =>
    ids.value.length > 0 &&
    ids.value.length <= 32 &&
    new Set(ids.value).size === ids.value.length &&
    ids.value.every((id) => /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(id)) &&
    (http.value || mcp.value) &&
    Number.isInteger(days.value) &&
    days.value >= 1 &&
    days.value <= 90,
);
function statusLabel(status: string) {
  return (
    (
      { active: "已启用", suspended: "已暂停", revoked: "已撤销" } as Record<
        string,
        string
      >
    )[status] || status
  );
}
function message(err: any) {
  return err?.message || "操作失败，请刷新后重试。";
}
async function run(action: () => Promise<void>) {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  notice.value = "";
  secret.value = "";
  try {
    await action();
  } catch (err) {
    error.value = message(err);
  } finally {
    busy.value = false;
  }
}
async function loadPrincipals(after = "") {
  loading.value = true;
  try {
    const r = await machineApi.list(after);
    const data = r.data as unknown as MachineInventory;
    principals.value = after
      ? [...principals.value, ...data.principals]
      : data.principals;
    principalCursor.value = data.next_cursor;
    if (!after) {
      selected.value = undefined;
      chosen.value = undefined;
      keys.value = [];
      secret.value = "";
    }
  } catch (err) {
    error.value = message(err);
  } finally {
    loading.value = false;
  }
}
async function loadKeys(after = "") {
  if (!selected.value) return;
  const id = selected.value.id;
  loading.value = true;
  try {
    const r = await machineApi.credentials(id, after);
    if (selected.value?.id !== id) return;
    const data = r.data as unknown as MachineInventory;
    selected.value = data.principals[0];
    keys.value = after
      ? [...keys.value, ...data.credentials]
      : data.credentials;
    keyCursor.value = data.next_cursor;
    if (!after) chosen.value = undefined;
  } catch (err) {
    error.value = message(err);
    keys.value = [];
    chosen.value = undefined;
  } finally {
    loading.value = false;
  }
}
async function select(p: MachinePrincipal) {
  secret.value = "";
  selected.value = p;
  keys.value = [];
  chosen.value = undefined;
  await loadKeys();
}
function choose(key: MachineCredential) {
  secret.value = "";
  chosen.value = key;
  resources.value = [...new Set(key.rules.flatMap((r) => r.resource_ids))].join(
    ",",
  );
  http.value = key.rules.some((r) => r.channel === "integration_http");
  mcp.value = key.rules.some((r) => r.channel === "integration_mcp");
}
async function create() {
  await run(async () => {
    const r = await machineApi.create(name.value.trim());
    const p = r.data as unknown as MachinePrincipal;
    name.value = "";
    await loadPrincipals();
    await select(p);
    notice.value = "集成已创建";
  });
}
function rules(start: number, end: number): MachineRule[] {
  return [
    ["integration_http", "public_thread.read", http.value],
    ["integration_mcp", "public_thread_read", mcp.value],
  ]
    .filter((row) => row[2])
    .map((row) => ({
      id: String(row[0]),
      channel: String(row[0]),
      endpoint: String(row[1]),
      action: "community.thread.read",
      resource_kind: "community.thread",
      resource_ids: [...ids.value],
      status: "active",
      not_before_ms: start,
      expires_at_ms: end,
    }));
}
async function issue(rotate: boolean) {
  if (!selected.value || !validDraft.value) return;
  await run(async () => {
    const p = selected.value!;
    const start = Date.now();
    const data = {
      principal_version: p.version,
      not_before_ms: start,
      expires_at_ms: start + days.value * 86400000,
      rules: rules(start, start + days.value * 86400000),
    };
    const r =
      rotate && chosen.value
        ? await machineApi.rotate(p.id, chosen.value, data)
        : await machineApi.issue(p.id, data);
    const result = r.data as unknown as MachineIssueResult;
    await loadKeys();
    if (alive && document.visibilityState === "visible")
      secret.value = result.token;
    notice.value = rotate ? "轮换已提交，旧凭据已失效" : "凭据已签发";
  });
}
async function scope() {
  if (!selected.value || !chosen.value || !validDraft.value) return;
  await run(async () => {
    const k = chosen.value!;
    await machineApi.scope(
      selected.value!.id,
      k,
      rules(k.not_before_ms, k.expires_at_ms),
    );
    await loadKeys();
    notice.value = "范围已更新，原有效期保持不变";
  });
}
async function revoke() {
  if (
    !selected.value ||
    !chosen.value ||
    !window.confirm("撤销此凭据后无法恢复，确认继续？")
  )
    return;
  await run(async () => {
    await machineApi.revoke(selected.value!.id, chosen.value!);
    await loadKeys();
    notice.value = "凭据已撤销";
  });
}
async function changeStatus(status: string) {
  if (
    !selected.value ||
    (status === "revoked" &&
      !window.confirm("永久撤销此集成及其全部凭据，确认继续？"))
  )
    return;
  await run(async () => {
    const r = await machineApi.update(selected.value!, status);
    const p = r.data as unknown as MachinePrincipal;
    const index = principals.value.findIndex((v) => v.id === p.id);
    if (index >= 0) principals.value[index] = p;
    selected.value = p;
    await loadKeys();
    notice.value = "集成状态已更新";
  });
}
async function copy() {
  try {
    await navigator.clipboard.writeText(secret.value);
    notice.value = "已复制，请保存到秘密配置";
  } catch {
    error.value = "复制失败，请手动复制凭据";
  }
}
function hideSecret() {
  if (document.visibilityState !== "visible") secret.value = "";
}
onMounted(() => {
  document.addEventListener("visibilitychange", hideSecret);
  if (mayRead) void loadPrincipals();
});
onBeforeUnmount(() => {
  alive = false;
  secret.value = "";
  document.removeEventListener("visibilitychange", hideSecret);
});
</script>
<style scoped>
.machines {
  max-width: 960px;
  padding: 24px;
  margin: 0 auto;
}
header p {
  color: #526173;
}
form,
section {
  margin-top: 20px;
  padding: 18px;
  border: 1px solid #d6dce3;
  border-radius: 8px;
}
label {
  display: block;
  margin: 12px 0;
}
input:not([type="checkbox"]) {
  padding: 8px;
  max-width: 100%;
  min-width: 180px;
}
button {
  padding: 8px 12px;
  margin: 4px;
  cursor: pointer;
}
button[aria-pressed="true"] {
  outline: 2px solid #2563eb;
}
button:disabled {
  opacity: 0.5;
  cursor: default;
}
li {
  margin: 6px 0;
  overflow-wrap: anywhere;
}
.error {
  color: #b42318;
}
.secret {
  position: fixed;
  inset: 20% max(12px, calc((100vw - 720px) / 2)) auto;
  background: white;
  z-index: 2100;
  box-shadow: 0 8px 50px #0005;
}
code {
  display: block;
  overflow-wrap: anywhere;
  padding: 12px;
  background: #f1f5f9;
  user-select: all;
}
@media (max-width: 600px) {
  .machines {
    padding: 12px;
  }
  input:not([type="checkbox"]) {
    min-width: 0;
    width: 100%;
    box-sizing: border-box;
  }
}
</style>
