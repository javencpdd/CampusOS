<template>
  <main class="plugin-config">
    <header>
      <div>
        <h2>插件系统配置</h2>
        <p>选择已安装的版本，分别管理普通配置和密钥引用。</p>
      </div>
      <button type="button" :disabled="loading || saving" @click="refresh">
        重新加载
      </button>
      <button
        v-if="canConfigure"
        type="button"
        :disabled="saving"
        @click="confirmMFA"
      >
        确认管理员身份
      </button>
    </header>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="message" role="status">{{ message }}</p>
    <p v-if="loading" role="status">正在加载插件…</p>
    <p v-else-if="!items.length && !error">尚无可查看的插件包。</p>
    <div class="columns">
      <section aria-label="插件版本列表">
        <button
          v-for="item in items"
          :key="coordinateKey(item.release)"
          type="button"
          :disabled="saving"
          :aria-pressed="
            !!selected &&
            coordinateKey(selected.release) === coordinateKey(item.release)
          "
          @click="choose(item.release)"
        >
          {{ item.release.manifest.display_name || item.release.plugin_key }}
          <small>{{ coordinateKey(item.release) }}</small>
        </button>
        <button
          v-if="cursor"
          type="button"
          :disabled="loading || saving"
          @click="loadMore"
        >
          加载更多版本
        </button>
      </section>
      <section aria-label="系统配置详情" :aria-busy="detailLoading || saving">
        <p v-if="detailLoading" role="status">正在读取所选版本…</p>
        <template v-else-if="selected">
          <h3>
            {{
              selected.release.manifest.display_name ||
              selected.release.plugin_key
            }}
          </h3>
          <p>{{ coordinateKey(selected.release) }}</p>
          <p v-if="selected.installation">
            运行状态：{{ selected.installation.observed }}；期望状态：{{
              selected.installation.desired
            }}
          </p>
          <p v-if="!canConfigure">当前管理员没有修改插件配置的权限。</p>
          <form v-if="form" aria-label="普通配置" @submit.prevent="save">
            <fieldset :disabled="saving || !canConfigure">
              <legend>普通配置</legend>
              <p>配置修订：{{ form.revision }}</p>
              <div
                v-for="(field, key) in form.fields"
                :key="key"
                class="config-field"
              >
                <label
                  v-if="!form.required.includes(String(key))"
                  class="optional"
                  ><input
                    type="checkbox"
                    :checked="hasField(String(key))"
                    :aria-label="`设置${field.title}`"
                    @change="
                      toggle(
                        String(key),
                        ($event.target as HTMLInputElement).checked,
                      )
                    "
                  />设置{{ field.title }}</label
                >
                <template v-if="hasField(String(key))">
                  <label :for="`config-${key}`"
                    >{{ field.title
                    }}{{
                      form.required.includes(String(key)) ? "（必填）" : ""
                    }}</label
                  >
                  <select
                    v-if="field.selector"
                    :id="`config-${key}`"
                    v-model="draft[key]"
                    :aria-label="field.title"
                  >
                    <option
                      v-if="
                        !selectorChoices(field.selector).some(
                          (o) => o.id === draft[key],
                        )
                      "
                      :value="draft[key]"
                      disabled
                    >
                      当前选项已不可用，请重新选择
                    </option>
                    <option
                      v-for="o in selectorChoices(field.selector)"
                      :key="o.id"
                      :value="o.id"
                    >
                      {{ o.title }}
                    </option>
                  </select>
                  <select
                    v-else-if="field.enum"
                    :id="`config-${key}`"
                    v-model="draft[key]"
                    :aria-label="field.title"
                  >
                    <option
                      v-for="(value, index) in field.enum"
                      :key="index"
                      :value="value"
                    >
                      {{
                        value === true ? "是" : value === false ? "否" : value
                      }}
                    </option>
                  </select>
                  <input
                    v-else-if="field.type === 'boolean'"
                    :id="`config-${key}`"
                    v-model="draft[key]"
                    type="checkbox"
                    :aria-label="field.title"
                  />
                  <input
                    v-else
                    :id="`config-${key}`"
                    :value="
                      field.type === 'number' || field.type === 'integer'
                        ? numericText(draft[key])
                        : draft[key]
                    "
                    @input="
                      draft[key] = ($event.target as HTMLInputElement).value
                    "
                    type="text"
                    :inputmode="
                      field.type === 'number' || field.type === 'integer'
                        ? 'decimal'
                        : 'text'
                    "
                    :aria-label="field.title"
                    autocomplete="off"
                  />
                </template>
              </div>
              <p v-if="!Object.keys(form.fields).length">
                此版本没有普通配置字段。
              </p>
              <button
                v-if="canConfigure && Object.keys(form.fields).length"
                type="submit"
              >
                {{ saving ? "正在保存…" : "保存普通配置" }}
              </button>
            </fieldset>
          </form>
          <aside aria-label="密钥配置">
            <h4>密钥配置</h4>
            <p>
              仅选择宿主允许的引用，不接收或显示密钥原值。普通配置保存不会修改这些绑定。
            </p>
            <p>此版本声明 {{ secretCount }} 项密钥。</p>
            <form
              v-if="form && form.secretNames.length"
              aria-label="密钥引用"
              @submit.prevent="saveSecrets"
            >
              <fieldset :disabled="saving || !canConfigure">
                <div
                  v-for="name in form.secretNames"
                  :key="name"
                  class="config-field"
                >
                  <label :for="`secret-${name}`">{{ name }}</label>
                  <select
                    :id="`secret-${name}`"
                    v-model="secretDraft[name]"
                    :aria-label="`密钥引用 ${name}`"
                  >
                    <option value="">不绑定</option>
                    <option
                      v-if="
                        secretDraft[name] &&
                        !form.options.secret_refs[name]?.some(
                          (o) => o.id === secretDraft[name],
                        )
                      "
                      :value="secretDraft[name]"
                      disabled
                    >
                      已有绑定，当前不可选择；可解除
                    </option>
                    <option
                      v-for="o in form.options.secret_refs[name] || []"
                      :key="o.id"
                      :value="o.id"
                    >
                      {{ o.title }}
                    </option>
                  </select>
                </div>
                <button v-if="canConfigure" type="submit">保存密钥引用</button>
              </fieldset>
            </form>
          </aside>
        </template>
        <p v-else-if="!detailLoading">请选择一个插件版本。</p>
      </section>
    </div>
  </main>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  getAdminAccessToken,
  getCurrentAdminProfile,
} from "../../identity/session";
import { pluginV5Api } from "../v5-api";
import {
  configurationForm,
  coordinateKey,
  validateValues,
  normalizeValues,
  type ConfigEntry,
  type ConfigForm,
  type ConfigValue,
  type Coordinate,
} from "../v5-config";
import { exactNumberText } from "../../../../../sdk/typescript/src/exact-json";

const items = ref<ConfigEntry[]>([]),
  cursor = ref("");
const selected = ref<ConfigEntry | null>(null),
  form = ref<ConfigForm | null>(null);
const draft = ref<Record<string, ConfigValue>>({});
const secretDraft = ref<Record<string, string>>({});
const selectorChoices = (kind: string) =>
  kind === "profile"
    ? form.value?.options.profiles || []
    : form.value?.options.public_collections || [];
const numericText = (value: unknown) => {
  try {
    return exactNumberText(value);
  } catch {
    return String(value ?? "");
  }
};
const loading = ref(false),
  detailLoading = ref(false),
  saving = ref(false);
const error = ref(""),
  message = ref("");
const profile = ref(getCurrentAdminProfile());
const canConfigure = computed(
  () =>
    profile.value?.permission_codes.includes("plugin.plugin.configure") ===
    true,
);
const secretCount = computed(() => {
  const d = selected.value?.release.manifest.configuration as any;
  return Array.isArray(d?.secret_names) ? d.secret_names.length : 0;
});
let listEpoch = 0,
  detailEpoch = 0,
  disposed = false;
let timer: ReturnType<typeof setInterval>;
const stamp = () =>
  JSON.stringify([getAdminAccessToken(), getCurrentAdminProfile()]);
let identity = stamp();
const hasField = (key: string) =>
  Object.prototype.hasOwnProperty.call(draft.value, key);
function confirmMFA() {
  if (identityCurrent() && canConfigure.value)
    window.dispatchEvent(new Event("campusos:admin-mfa-step-up-required"));
}
function clear() {
  listEpoch++;
  detailEpoch++;
  items.value = [];
  cursor.value = "";
  selected.value = null;
  form.value = null;
  draft.value = {};
  secretDraft.value = {};
  loading.value = false;
  detailLoading.value = false;
  saving.value = false;
  message.value = "";
}
function identityCurrent() {
  if (disposed) return false;
  if (identity === stamp()) return true;
  identity = stamp();
  profile.value = getCurrentAdminProfile();
  clear();
  error.value = "管理员身份或权限已变化，请重新加载。";
  return false;
}
async function refresh() {
  identityCurrent();
  clear();
  error.value = "";
  profile.value = getCurrentAdminProfile();
  if (!profile.value?.permission_codes.includes("plugin.plugin.read")) {
    error.value = "当前管理员没有查看插件的权限。";
    return;
  }
  await loadMore();
}
async function loadMore() {
  if (!identityCurrent() || loading.value || saving.value) return;
  const epoch = ++listEpoch;
  loading.value = true;
  error.value = "";
  try {
    const response = await pluginV5Api.list(cursor.value);
    if (!identityCurrent() || epoch !== listEpoch) return;
    if (response.code !== 0 || !Array.isArray(response.data?.items))
      throw new Error("插件清单不可用，请重新加载。 ");
    const existing = new Set(items.value.map((i) => coordinateKey(i.release)));
    items.value.push(
      ...response.data.items.filter(
        (i) => !existing.has(coordinateKey(i.release)),
      ),
    );
    cursor.value = response.data.next_cursor || "";
  } catch (e: any) {
    if (identityCurrent() && epoch === listEpoch)
      error.value = e.message || "读取插件失败，请重试。";
  } finally {
    if (epoch === listEpoch) loading.value = false;
  }
}
async function choose(c: Coordinate, afterSave = false) {
  if (!identityCurrent() || (saving.value && !afterSave)) return;
  const epoch = ++detailEpoch;
  selected.value = null;
  form.value = null;
  draft.value = {};
  secretDraft.value = {};
  error.value = "";
  message.value = "";
  detailLoading.value = true;
  try {
    const [response, choices] = await Promise.all([
      pluginV5Api.detail(c),
      pluginV5Api.options(c),
    ]);
    if (!identityCurrent() || epoch !== detailEpoch) return;
    if (
      response.code !== 0 ||
      coordinateKey(response.data?.release) !== coordinateKey(c)
    )
      throw new Error("所选版本已变化，请重新加载。 ");
    selected.value = response.data;
    if (choices.code !== 0) throw new Error("配置选项不可用，请重新加载。");
    form.value = configurationForm(response.data, choices.data);
    draft.value = { ...form.value.values };
    secretDraft.value = Object.fromEntries(
      form.value.secretNames.map((name) => [
        name,
        form.value!.secretRefs[name] || "",
      ]),
    );
    if (afterSave)
      message.value = "普通配置已保存。运行状态以服务器当前状态为准。";
  } catch (e: any) {
    if (identityCurrent() && epoch === detailEpoch)
      error.value = e.message || "读取配置失败，请重新加载。";
  } finally {
    if (epoch === detailEpoch) detailLoading.value = false;
  }
}
function toggle(key: string, present: boolean) {
  if (!form.value || saving.value || !canConfigure.value) return;
  if (!present) {
    const values = { ...draft.value };
    delete values[key];
    draft.value = values;
  } else {
    const f = form.value.fields[key];
    draft.value = {
      ...draft.value,
      [key]: f.enum?.[0] ?? (f.type === "boolean" ? false : ""),
    };
  }
}
async function save() {
  if (
    !identityCurrent() ||
    saving.value ||
    !canConfigure.value ||
    !selected.value ||
    !form.value
  )
    return;
  error.value = validateValues(form.value, draft.value);
  message.value = "";
  if (error.value) return;
  const epoch = detailEpoch,
    c = { ...selected.value.release },
    revision = form.value.revision;
  saving.value = true;
  try {
    const response = await pluginV5Api.configure(
      c,
      revision,
      normalizeValues(form.value, draft.value),
    );
    if (!identityCurrent() || epoch !== detailEpoch) return;
    if (response.code !== 0 || !response.data?.id)
      throw new Error("未能确认保存结果。 ");
    await choose(c, true);
  } catch (e: any) {
    if (identityCurrent() && epoch === detailEpoch) {
      form.value = null;
      draft.value = {};
      secretDraft.value = {};
      error.value =
        e.status === 409
          ? "配置已被其他操作修改，请重新加载后再编辑。"
          : `${e.message || "保存结果未确认。"} 请重新加载确认服务器结果，再决定是否重试。`;
    }
  } finally {
    saving.value = false;
  }
}
async function saveSecrets() {
  if (
    !identityCurrent() ||
    saving.value ||
    !canConfigure.value ||
    !selected.value ||
    !form.value
  )
    return;
  const refs = Object.fromEntries(
    Object.entries(secretDraft.value).filter(([, ref]) => ref),
  );
  if (new Set(Object.values(refs)).size !== Object.keys(refs).length) {
    error.value = "一个密钥引用不能重复绑定。";
    return;
  }
  for (const [name, ref] of Object.entries(refs))
    if (!form.value.options.secret_refs[name]?.some((o) => o.id === ref)) {
      error.value = "密钥引用已不可选，请重新加载或解除绑定。";
      return;
    }
  const epoch = detailEpoch,
    c = { ...selected.value.release },
    revision = form.value.revision;
  saving.value = true;
  error.value = "";
  message.value = "";
  try {
    const response = await pluginV5Api.configureSecrets(c, revision, refs);
    if (!identityCurrent() || epoch !== detailEpoch) return;
    if (response.code !== 0 || !response.data?.id)
      throw new Error("未能确认密钥引用保存结果。");
    await choose(c, true);
    if (identityCurrent()) message.value = "密钥引用已更新，普通配置未变。";
  } catch (e: any) {
    if (identityCurrent() && epoch === detailEpoch) {
      form.value = null;
      draft.value = {};
      secretDraft.value = {};
      error.value =
        e.status === 409
          ? "配置已被其他操作修改，请重新加载后再编辑。"
          : `${e.message || "保存结果未确认。"} 请重新加载确认服务器结果。`;
    }
  } finally {
    saving.value = false;
  }
}
onMounted(() => {
  void refresh();
  timer = setInterval(identityCurrent, 200);
});
onBeforeUnmount(() => {
  disposed = true;
  clearInterval(timer);
  clear();
});
</script>

<style scoped>
.plugin-config {
  display: grid;
  gap: 16px;
  max-width: 1100px;
}
header {
  display: flex;
  flex-wrap: wrap;
  align-items: start;
  justify-content: space-between;
  gap: 12px;
}
h2,
h3 {
  margin: 0;
}
p {
  overflow-wrap: anywhere;
  line-height: 1.6;
}
.columns {
  display: grid;
  grid-template-columns: minmax(200px, 1fr) minmax(0, 2fr);
  gap: 18px;
}
section,
aside,
fieldset {
  min-width: 0;
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  padding: 16px;
  background: #fff;
}
section[aria-label="插件版本列表"] {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 10px;
}
button,
input,
select {
  font: inherit;
  max-width: 100%;
}
button {
  padding: 9px 14px;
  border: 1px solid #b5becb;
  border-radius: 4px;
  background: #fff;
  color: #1d4ed8;
  cursor: pointer;
  overflow-wrap: anywhere;
}
button:disabled {
  cursor: default;
  opacity: 0.6;
}
button[aria-pressed="true"] {
  background: #edf4ff;
}
small {
  display: block;
  margin-top: 4px;
  overflow-wrap: anywhere;
}
.config-field {
  display: grid;
  gap: 8px;
  margin: 14px 0;
}
.optional {
  color: #606266;
}
input[type="text"],
select {
  box-sizing: border-box;
  width: 100%;
  padding: 8px;
  border: 1px solid #b5becb;
  border-radius: 4px;
}
input[type="checkbox"] {
  justify-self: start;
}
aside {
  margin-top: 16px;
}
[role="alert"] {
  color: #a61b1b;
}
[role="status"] {
  color: #25603b;
}
@media (max-width: 700px) {
  .columns {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
