<template>
  <main class="knowledge-admin" aria-labelledby="knowledge-admin-title">
    <h1 id="knowledge-admin-title">知识来源管理</h1>
    <p>
      管理公开集合、来源规则和官方资料。索引状态与投影数量仅供诊断；每次读取仍核验原来源。
    </p>
    <p v-if="!canRead" role="alert">没有知识集合读取权限，请联系管理员。</p>
    <template v-else>
      <button :disabled="busy" @click="reload">刷新工作区</button>
      <p v-if="busy" role="status">正在处理…</p>
      <p v-if="error" role="alert">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <p v-if="!canManage">当前为只读模式。写入需要知识集合管理权限。</p>
      <p v-else-if="!mfa">
        写入前请点击页眉“管理员认证”完成当前 MFA 认证，再刷新工作区。
      </p>
      <template v-if="loaded">
        <section aria-label="公开集合管理">
          <h2>公开集合</h2>
          <p v-if="!collections.length">尚未配置公开知识集合。</p>
          <label
            >管理集合<select
              v-model="selectedID"
              aria-label="管理集合"
              :disabled="busy"
            >
              <option value="">请选择集合</option>
              <option v-for="c in collections" :key="c.id" :value="c.id">
                {{ c.title }}（{{ c.status === "active" ? "启用" : "停用" }}）
              </option>
            </select></label
          >
          <form aria-label="集合表单" @submit.prevent="saveCollection">
            <label
              >集合名称<input
                v-model="collectionForm.title"
                aria-label="集合名称"
                required
                :disabled="!writable"
            /></label>
            <label
              >集合状态<select
                v-model="collectionForm.status"
                aria-label="集合状态"
                :disabled="!writable"
              >
                <option value="active">启用</option>
                <option value="disabled">停用</option>
              </select></label
            >
            <button :disabled="!writable">
              {{ state ? "保存集合" : "创建集合" }}
            </button>
            <button type="button" :disabled="busy" @click="selectedID = ''">
              新建集合
            </button>
          </form>
          <template v-if="state">
            <h3>选择来源</h3>
            <form aria-label="来源选择" @submit.prevent="addSelection">
              <label
                >来源类型<select
                  v-model="selectionForm.kind"
                  aria-label="来源类型"
                  :disabled="!writable"
                >
                  <option value="thread">单篇帖子或文章</option>
                  <option value="board">公开板块</option>
                  <option value="official">官方资料</option>
                </select></label
              >
              <label v-if="selectionForm.kind === 'official'"
                >官方来源<select
                  v-model="selectionForm.source_id"
                  aria-label="官方来源"
                  :disabled="!writable"
                  required
                >
                  <option value="">请选择已发布资料</option>
                  <option
                    v-for="o in officials.filter(
                      (o) => o.status === 'published',
                    )"
                    :key="o.id"
                    :value="o.id"
                  >
                    {{ o.title }}
                  </option>
                </select></label
              >
              <label v-else
                >来源 ID<input
                  v-model="selectionForm.source_id"
                  aria-label="来源 ID"
                  required
                  :disabled="!writable"
                  placeholder="帖子或板块的数字 ID"
              /></label>
              <button :disabled="!writable">添加来源</button>
            </form>
            <p>
              板块规则由常驻索引器分页同步。手动刷新每次最多处理 50
              条，不代表整个索引完成。
            </p>
            <ul class="rules">
              <li v-for="rule in state.selections" :key="rule.id">
                {{ kinds[rule.kind] }} · {{ rule.source_id }} ·
                {{ rule.active ? "已选择" : "已取消" }}
                <button :disabled="!writable" @click="toggle(rule)">
                  {{ rule.active ? "取消选择" : "恢复选择" }}
                </button>
                <button
                  v-if="rule.active"
                  :disabled="!writable"
                  @click="refresh(rule)"
                >
                  {{ nextPages[rule.id] ? "继续刷新来源" : "刷新来源" }}
                </button>
              </li>
            </ul>
            <details>
              <summary>已物化成员引用（{{ state.members.length }}）</summary>
              <ul>
                <li v-for="member in state.members" :key="member.id">
                  {{ member.kind }} · {{ member.source_id }}
                  <button :disabled="!writable" @click="remove(member)">
                    移除引用
                  </button>
                </li>
              </ul>
            </details>
            <section aria-label="索引诊断">
              <h3>索引诊断与指标</h3>
              <button :disabled="busy" @click="refreshDiagnostics">
                刷新诊断
              </button>
              <template v-if="diagnostics">
                <dl>
                  <dt>执行状态</dt>
                  <dd>{{ states[diagnostics.state] || "暂不可用" }}</dd>
                  <dt>选择规则 / 启用规则</dt>
                  <dd>
                    {{ diagnostics.selections }} /
                    {{ diagnostics.active_selections }}
                  </dd>
                  <dt>成员引用 / 存储投影</dt>
                  <dd>
                    {{ diagnostics.members }} /
                    {{ diagnostics.projected_documents }}
                  </dd>
                  <dt>尝试次数</dt>
                  <dd>{{ diagnostics.attempts }}</dd>
                  <dt>运行代次 / 已完成代次</dt>
                  <dd>
                    {{ diagnostics.run_generation }} /
                    {{ diagnostics.ready_generation }}
                  </dd>
                  <dt>安全原因码</dt>
                  <dd>{{ diagnostics.last_error || "无" }}</dd>
                  <dt>观察时间</dt>
                  <dd>{{ time(diagnostics.observed_at_ms) }}</dd>
                  <dt>下次调度时间</dt>
                  <dd>{{ time(diagnostics.available_at_ms) }}</dd>
                </dl>
                <button
                  v-if="diagnostics.retryable"
                  :disabled="!writable"
                  @click="retryIndex"
                >
                  重试索引
                </button>
              </template>
              <p v-else>诊断尚未加载，可点击“刷新诊断”重试。</p>
              <p>
                投影可能含待清理的旧修订，数量不等于当前可读资料数。引用更新或不可读时，请在
                Web 重新搜索。
              </p>
            </section>
          </template>
        </section>
        <section aria-label="官方资料管理">
          <h2>官方资料</h2>
          <p v-if="!officials.length">尚未配置官方资料。</p>
          <ul>
            <li v-for="o in officials" :key="o.id">
              {{ o.title }}（{{ statuses[o.status] }}）
              <button :disabled="busy" @click="editOfficial(o)">
                查看或编辑资料
              </button>
            </li>
          </ul>
          <form aria-label="官方资料表单" @submit.prevent="saveOfficial">
            <label
              >资料标题<input
                v-model="officialForm.title"
                aria-label="资料标题"
                required
                :disabled="!writable"
            /></label>
            <label
              >资料正文<textarea
                v-model="officialForm.content"
                aria-label="资料正文"
                required
                rows="6"
                :disabled="!writable"
              />
            </label>
            <label
              >正文格式<select
                v-model="officialForm.format"
                aria-label="正文格式"
                :disabled="!writable"
              >
                <option value="text">纯文本</option>
                <option value="markdown">Markdown 文本</option>
              </select></label
            >
            <label
              >资料状态<select
                v-model="officialForm.status"
                aria-label="资料状态"
                :disabled="!writable"
              >
                <option value="draft">草稿</option>
                <option value="published">发布</option>
                <option value="withdrawn">撤回</option>
              </select></label
            >
            <button :disabled="!writable">
              {{ editingOfficial ? "保存资料" : "创建资料" }}</button
            ><button type="button" :disabled="busy" @click="newOfficial">
              新建资料
            </button>
          </form>
          <details v-if="history.length" open>
            <summary>不可变资料修订</summary>
            <ul>
              <li v-for="revision in history" :key="revision.official_version">
                修订 {{ revision.official_version }} ·
                {{ statuses[revision.snapshot.status] }} ·
                {{ revision.snapshot.title }}
                <pre>{{ revision.snapshot.content }}</pre>
              </li>
            </ul>
            <button
              v-if="historyBefore !== '0'"
              :disabled="busy"
              @click="moreHistory"
            >
              更早修订
            </button>
          </details>
        </section>
      </template>
    </template>
  </main>
</template>
<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";
import { useAdminStore } from "../../identity/store";
import {
  knowledgeAdminApi as api,
  type Collection,
  type Official,
  type Selection,
  type Member,
  type CollectionState,
  type History,
  type Diagnostics,
} from "../api";
const admin = useAdminStore();
const canRead = computed(
  () =>
    admin.profile?.permission_codes.includes("knowledge.collection.read") ===
    true,
);
const canManage = computed(
  () =>
    admin.profile?.permission_codes.includes("knowledge.collection.manage") ===
    true,
);
const mfa = computed(() => admin.profile?.authentication_strength === "mfa");
const busy = ref(false),
  loaded = ref(false),
  error = ref(""),
  notice = ref(""),
  selectedID = ref("");
const writable = computed(
  () => canManage.value && mfa.value && !busy.value && loaded.value,
);
const collections = ref<Collection[]>([]),
  officials = ref<Official[]>([]),
  state = ref<CollectionState | null>(null),
  diagnostics = ref<Diagnostics | null>(null);
const collectionForm = reactive<{
  title: string;
  status: Collection["status"];
}>({ title: "", status: "active" });
const selectionForm = reactive<{ kind: Selection["kind"]; source_id: string }>({
  kind: "thread",
  source_id: "",
});
const officialForm = reactive<
  Pick<Official, "title" | "content" | "format" | "status">
>({ title: "", content: "", format: "text", status: "draft" });
const editingOfficial = ref<Official>(),
  history = ref<History["items"]>([]),
  historyBefore = ref("0"),
  nextPages = reactive<Record<string, number>>({});
const kinds = { thread: "单篇", board: "板块", official: "官方资料" };
const states: Record<string, string> = {
  unconfigured: "尚未调度",
  pending: "等待处理",
  processing: "处理中",
  retry: "等待重试",
  dead: "重试耗尽，等待复核",
  idle: "本轮已结束",
};
const statuses = { draft: "草稿", published: "已发布", withdrawn: "已撤回" };
let generation = 0,
  disposed = false;
const time = (value: string) =>
  value === "0" ? "待调度" : new Date(Number(value)).toLocaleString();
function clearDetail() {
  state.value = null;
  diagnostics.value = null;
  selectionForm.source_id = "";
  Object.keys(nextPages).forEach((key) => delete nextPages[key]);
}
function failure(e: unknown) {
  const x = e as { status?: number; machineCode?: string };
  return x.machineCode === "knowledge.disabled"
    ? "知识功能未启用，请到“内置功能”启用后刷新。"
    : x.status === 409
      ? "数据已变更，请刷新工作区后核对再提交。"
      : x.status === 403
        ? "权限已变化，请刷新工作区；写入需要当前 MFA 认证。"
        : "操作暂不可用，请刷新工作区或重试当前操作。";
}
async function run(action: () => Promise<void>) {
  if (busy.value || disposed) return;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    await action();
  } catch (e) {
    if (!disposed) {
      error.value = failure(e);
      clearDetail();
      history.value = [];
      editingOfficial.value = undefined;
      officialForm.content = "";
      loaded.value = false;
    }
  } finally {
    busy.value = false;
  }
}
async function inventory() {
  const [cs, os] = await Promise.all([api.collections(), api.officials()]);
  if (disposed) return;
  collections.value = cs;
  officials.value = os;
  loaded.value = true;
}
async function detail(id: string, epoch: number) {
  const s = await api.state(id);
  if (disposed || epoch !== generation) return;
  state.value = s;
  Object.assign(collectionForm, {
    title: s.collection.title,
    status: s.collection.status,
  });
  try {
    const d = await api.diagnostics(id);
    if (!disposed && epoch === generation) diagnostics.value = d;
  } catch {
    if (!disposed && epoch === generation)
      error.value = "索引诊断暂不可用，请点击“刷新诊断”重试。";
  }
}
async function reload() {
  ++generation;
  clearDetail();
  newOfficial();
  collections.value = [];
  officials.value = [];
  loaded.value = false;
  await run(async () => {
    if (!canRead.value) return;
    await inventory();
    if (
      selectedID.value &&
      collections.value.some((c) => c.id === selectedID.value)
    )
      await detail(selectedID.value, generation);
    else selectedID.value = "";
  });
}
watch(
  selectedID,
  async (id) => {
    const epoch = ++generation;
    clearDetail();
    Object.assign(collectionForm, { title: "", status: "active" });
    if (id) await run(() => detail(id, epoch));
  },
  { flush: "sync" },
);
watch(
  () => selectionForm.kind,
  () => {
    selectionForm.source_id = "";
  },
);
async function saveCollection() {
  if (!writable.value) return;
  await run(async () => {
    const c = await api.saveCollection(
      { ...collectionForm },
      state.value?.collection,
    );
    await inventory();
    selectedID.value = c.id;
    await detail(c.id, generation);
    notice.value = "集合已保存。";
  });
}
async function addSelection() {
  if (!writable.value || !state.value) return;
  await run(async () => {
    state.value = await api.select(state.value!, {
      ...selectionForm,
      active: true,
    });
    selectionForm.source_id = "";
    diagnostics.value = null;
    notice.value = "来源规则已保存，常驻索引器将按当前公开性同步。";
  });
}
async function toggle(rule: Selection) {
  if (!writable.value || !state.value) return;
  await run(async () => {
    state.value = await api.select(
      state.value!,
      { kind: rule.kind, source_id: rule.source_id, active: !rule.active },
      rule,
    );
    diagnostics.value = null;
    notice.value = "来源规则已更新。";
  });
}
async function refresh(rule: Selection) {
  if (!writable.value || !state.value) return;
  await run(async () => {
    const r = await api.refresh(state.value!, rule, nextPages[rule.id] || 1);
    state.value = r.state;
    nextPages[rule.id] = r.has_more ? r.next_page : 0;
    diagnostics.value = null;
    notice.value = `本页检查 ${r.examined} 条；${r.has_more ? "仍有后续页。" : "本页刷新结束，完整索引以常驻任务为准。"}`;
  });
}
async function remove(member: Member) {
  if (!writable.value || !state.value) return;
  await run(async () => {
    state.value = await api.remove(state.value!, member);
    diagnostics.value = null;
    notice.value =
      "引用已移除；启用的来源规则可在后续对账重新加入，请取消规则以持续排除。";
  });
}
async function refreshDiagnostics() {
  if (!state.value) return;
  await run(async () => {
    diagnostics.value = null;
    diagnostics.value = await api.diagnostics(state.value!.collection.id);
  });
}
async function retryIndex() {
  if (!writable.value || !diagnostics.value) return;
  const d = diagnostics.value;
  await run(async () => {
    diagnostics.value = null;
    await api.retry(d);
    notice.value = "索引重试已受理，请刷新诊断查看实际执行结果。";
    diagnostics.value = await api.diagnostics(d.collection_id);
  });
}
function newOfficial() {
  editingOfficial.value = undefined;
  history.value = [];
  historyBefore.value = "0";
  Object.assign(officialForm, {
    title: "",
    content: "",
    format: "text",
    status: "draft",
  });
}
async function editOfficial(o: Official) {
  await run(async () => {
    newOfficial();
    const page = await api.history(o.id);
    if (disposed) return;
    const latest = page.items[0]?.snapshot;
    if (!latest) throw Error("Missing official revision");
    editingOfficial.value = latest;
    Object.assign(officialForm, {
      title: latest.title,
      content: latest.content,
      format: latest.format,
      status: latest.status,
    });
    history.value = page.items;
    historyBefore.value = page.next_before;
  });
}
async function moreHistory() {
  if (!editingOfficial.value) return;
  await run(async () => {
    const page = await api.history(
      editingOfficial.value!.id,
      historyBefore.value,
    );
    history.value.push(...page.items);
    historyBefore.value = page.next_before;
  });
}
async function saveOfficial() {
  if (!writable.value) return;
  if (new TextEncoder().encode(officialForm.content).length > 65536) {
    error.value = "正文超过 64 KiB，请缩短后提交。";
    return;
  }
  await run(async () => {
    const saved = await api.saveOfficial(
      { ...officialForm },
      editingOfficial.value,
    );
    await inventory();
    editingOfficial.value = saved;
    const page = await api.history(saved.id);
    history.value = page.items;
    historyBefore.value = page.next_before;
    notice.value = "官方资料已保存并生成不可变修订。";
  });
}
onMounted(() => void reload());
onBeforeUnmount(() => {
  disposed = true;
  generation++;
  clearDetail();
  newOfficial();
});
</script>
<style scoped>
.knowledge-admin {
  max-width: 1100px;
  margin: auto;
  overflow-wrap: anywhere;
}
.knowledge-admin section {
  margin: 20px 0;
  padding: 16px;
  border: 1px solid #dcdfe6;
  border-radius: 8px;
}
.knowledge-admin form {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: end;
  margin: 18px 0;
}
.knowledge-admin label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.knowledge-admin input,
.knowledge-admin select,
.knowledge-admin textarea {
  max-width: 100%;
  min-height: 44px;
  padding: 8px;
  font: inherit;
}
.knowledge-admin textarea {
  width: 100%;
  min-width: 260px;
}
.knowledge-admin button {
  min-height: 44px;
  padding: 8px 14px;
  margin: 4px;
  cursor: pointer;
  font: inherit;
}
.knowledge-admin button:disabled {
  cursor: default;
  opacity: 0.55;
}
.knowledge-admin dl {
  display: grid;
  grid-template-columns: minmax(100px, 1fr) 2fr;
  gap: 8px;
}
.knowledge-admin dd {
  margin: 0;
}
.knowledge-admin li {
  margin: 12px 0;
}
.knowledge-admin pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 280px;
  overflow: auto;
}
@media (max-width: 640px) {
  .knowledge-admin form,
  .knowledge-admin label {
    display: block;
    width: 100%;
  }
  .knowledge-admin input,
  .knowledge-admin select,
  .knowledge-admin textarea {
    box-sizing: border-box;
    width: 100%;
  }
  .knowledge-admin section {
    padding: 10px;
  }
  .knowledge-admin ul {
    padding-left: 18px;
  }
}
</style>
