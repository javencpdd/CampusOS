<template>
  <section class="runtime-config" aria-labelledby="config-title">
    <div class="config-heading">
      <div>
        <h2 id="config-title">运行配置</h2>
        <p>修改常用运行参数，保存到当前部署配置文件。重启 API 后生效。</p>
      </div>
      <div class="config-actions"><el-button v-if="snapshot?.available" :disabled="busy" @click="openEnv">打开配置文件</el-button><el-button :disabled="busy" @click="load">重新加载</el-button></div>
    </div>
    <el-alert v-if="failure" :title="failure" type="error" :closable="false" role="alert" />
    <el-alert v-if="status" :title="status" type="info" :closable="false" role="status" />
    <el-alert v-if="snapshot && !snapshot.available" title="此部署尚未开放配置编辑。请由部署者配置受管文件路径，并确认 API 有写入权限。" type="warning" :closable="false" />
    <template v-if="snapshot?.available">
      <el-alert v-if="snapshot.pending && !busy" title="文件中有尚未生效的配置，可保存并重启 API 使其生效。" type="warning" :closable="false" />
      <el-alert v-if="envDraft !== null" title="env 修改已同步到页面草稿，尚未写入文件；请核对后保存。" type="info" :closable="false" />
      <el-alert v-if="deploymentKeys.length" :title="`以下配置保存后仍需部署工具应用：${deploymentKeys.join('、')}`" type="warning" :closable="false" />
      <el-form label-position="top" :disabled="busy" @submit.prevent>
        <el-card v-for="group in groups" :key="group" class="config-group" shadow="never">
          <template #header><h3>{{ group }}</h3></template>
          <div class="config-fields">
            <el-form-item v-for="field in displayFields.filter(f => f.group === group)" :key="field.key" :label="field.label" :data-config-key="field.key">
              <template v-if="field.kind === 'boolean'">
                <el-checkbox v-model="form[field.key]">启用</el-checkbox>
              </template>
              <el-select v-else-if="field.kind === 'select'" v-model="form[field.key]" :aria-label="field.label">
                <el-option v-for="option in field.options" :key="option" :label="option" :value="option" />
              </el-select>
              <el-input v-else v-model="form[field.key]" :type="field.kind === 'secret' ? 'password' : 'text'" :inputmode="field.kind === 'number' ? 'numeric' : 'text'" :autocomplete="field.kind === 'secret' ? 'new-password' : 'off'" :show-password="field.kind === 'secret'" :disabled="field.kind === 'secret' && clearSecrets[field.key]" :placeholder="field.kind === 'secret' ? (field.configured ? '已配置，留空保留' : '尚未配置') : ''" :aria-label="field.label" />
              <el-checkbox v-if="field.kind === 'secret'" v-model="clearSecrets[field.key]">清除已保存凭据</el-checkbox>
              <p v-if="field.help" class="field-help">{{ field.help }}</p>
              <span v-if="field.pending" class="pending-field">待重启生效</span>
            </el-form-item>
          </div>
        </el-card>
      </el-form>
      <div class="config-actions">
        <el-button :disabled="busy || !dirty" @click="save(false)">仅保存</el-button>
        <el-button type="primary" :loading="busy" :disabled="busy || snapshot.restarting" @click="save(true)">保存并重启 API</el-button>
      </div>
      <p class="config-note">保存时保留原有注释，并生成一份私有备份。重启期间 API 会短暂不可用，页面会等待恢复。完整 env 编辑支持修改其他配置项；端口、数据库、Docker 网络及密钥等仍需部署工具应用，API 重启只加载常用运行项。</p>
    </template>
    <el-dialog v-model="editorVisible" title="编辑 env 配置文件" width="min(1000px, calc(100vw - 24px))" :close-on-click-modal="false" @closed="editorText = ''; editorError = ''">
      <p class="field-help">文件包含部署凭据，请私下编辑。应用后先同步到页面草稿，核对后再保存并重启 API。端口等部署配置须另行通过 docker-dev up 应用。</p>
      <el-input v-model="editorText" type="textarea" :rows="22" aria-label="env 配置文件内容" class="env-editor" spellcheck="false" autocomplete="off" />
      <el-alert v-if="editorError" :title="editorError" type="error" :closable="false" />
      <template #footer><el-button @click="editorVisible = false">取消</el-button><el-button type="primary" @click="applyEnv">应用到运行配置</el-button></template>
    </el-dialog>
    <el-dialog v-model="verification.visible" :title="verification.openDocument ? '验证并打开配置文件' : verification.restart ? '保存并重启 API' : '验证并保存配置'" width="min(460px, calc(100vw - 32px))" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy" @closed="clearVerification">
      <p class="verification-note">{{ verification.openDocument ? "完整 env 包含部署凭据，读取前请验证当前管理员身份。" : "请选择一种方式确认本次操作。" }}</p>
      <p v-if="verification.restart" class="verification-note">重启期间 API 会短暂不可用，页面将等待恢复。</p>
      <el-form label-position="top" :disabled="busy" @submit.prevent="submitVerification">
        <el-form-item label="验证方式">
          <el-radio-group v-model="verification.method" aria-label="验证方式">
            <el-radio value="password">管理员密码</el-radio>
            <el-radio value="mfa" :disabled="!mfaAvailable">MFA 验证码</el-radio>
          </el-radio-group>
          <p v-if="!mfaAvailable" class="field-help">尚未绑定验证器，可直接使用管理员密码验证。</p>
        </el-form-item>
        <el-form-item v-if="verification.method === 'password'" label="当前管理员密码">
          <el-input v-model="verification.password" type="password" autocomplete="current-password" placeholder="请再次输入当前管理员密码" aria-label="当前管理员密码" show-password @keyup.enter="submitVerification" />
          <p class="field-help">仅验证当前管理员，只对本次配置操作有效。</p>
        </el-form-item>
        <el-form-item v-else label="MFA 验证码">
          <el-input v-model="verification.code" inputmode="numeric" autocomplete="one-time-code" maxlength="6" placeholder="输入验证器中的6位验证码" aria-label="MFA 验证码" @keyup.enter="submitVerification" />
        </el-form-item>
        <el-alert v-if="failure" :title="failure" type="error" :closable="false" />
      </el-form>
      <template #footer>
        <el-button :disabled="busy" @click="verification.visible = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="submitVerification">{{ verification.openDocument ? '验证并打开' : verification.restart ? '验证并保存重启' : '验证并保存' }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { authApi } from "@/modules/identity/api";
import { useAdminStore } from "@/modules/identity/store";
import { runtimeConfigApi, type RuntimeConfigSnapshot } from "../runtime-config";
import { parseEnvDocument, patchEnvDocument, booleanEnvValue } from "../env-document";

const snapshot = ref<RuntimeConfigSnapshot | null>(null);
const form = reactive<Record<string, string | boolean>>({});
const envBase = ref<string | null>(null), envDraft = ref<string | null>(null);
const editorVisible = ref(false), editorText = ref(""), editorError = ref("");
const draftBaseline = ref<Record<string, string> | null>(null);
const draftValues = ref<Record<string, string> | null>(null);
const clearSecrets = reactive<Record<string, boolean>>({});
const busy = ref(false), failure = ref(""), status = ref("");
const admin = useAdminStore();
const mfaAvailable = computed(() => admin.profile?.mfa_enrolled === true);
const verification = reactive({ visible: false, openDocument: false, restart: false, method: "password" as "password" | "mfa", password: "", code: "" });
function clearVerification() { verification.password = ""; verification.code = ""; }
watch(() => verification.method, clearVerification);
let disposed = false;
onBeforeUnmount(() => { disposed = true; clearVerification(); envBase.value = envDraft.value = null; editorText.value = ""; draftValues.value = null; });
const groups = computed(() => [...new Set(snapshot.value?.fields.map(f => f.group) || [])]);
function changes() {
  const out: Record<string, string> = {};
  for (const field of snapshot.value?.fields || []) {
    const value = String(form[field.key] ?? "");
    if (field.kind === "secret") {
      if (clearSecrets[field.key]) out[field.key] = "";
      else if (value) out[field.key] = value;
    } else if (value !== (draftBaseline.value?.[field.key] ?? field.value)) out[field.key] = value;
  }
  return out;
}
const dirty = computed(() => Object.keys(changes()).length > 0 || envDraft.value !== null && envDraft.value !== envBase.value);
const displayFields = computed(() => (snapshot.value?.fields || []).map(f => draftValues.value && f.kind === "secret" ? { ...f, configured: Boolean(draftValues.value[f.key]) } : f));
const deploymentKeys = computed(() => {
 if (envBase.value === null || envDraft.value === null) return [];
 const original = parseEnvDocument(envBase.value), next = parseEnvDocument(envDraft.value);
 const known = new Set(snapshot.value?.fields.map(f => f.key));
 return [...new Set([...Object.keys(original), ...Object.keys(next)])].filter(k => !known.has(k) && (original[k] !== next[k] || Object.prototype.hasOwnProperty.call(original, k) !== Object.prototype.hasOwnProperty.call(next, k))).sort();
});
function openEnv() {
 if (busy.value || verification.visible) return;
 if (envDraft.value !== null) { editorText.value = patchEnvDocument(envDraft.value, changes()); editorVisible.value = true; return; }
 clearVerification(); verification.restart = false; verification.openDocument = true; verification.method = "password"; verification.visible = true;
}
function applyEnv() {
 try {
  const values = parseEnvDocument(editorText.value);
  const next: Record<string, string> = {};
  for (const field of snapshot.value?.fields || []) {
   const value = values[field.key] ?? field.active_value ?? field.value;
   if (field.kind === "boolean" && !["true", "false"].includes(booleanEnvValue(value))) throw new Error(`${field.label}必须为 true 或 false`);
   next[field.key] = field.kind === "secret" ? "" : field.kind === "boolean" ? booleanEnvValue(value) : value;
  }
  envDraft.value = editorText.value; draftValues.value = values; draftBaseline.value = next;
  for (const field of snapshot.value?.fields || []) { form[field.key] = field.kind === "boolean" ? next[field.key] === "true" : next[field.key]; clearSecrets[field.key] = false; }
  editorVisible.value = false; failure.value = ""; status.value = "env 修改已同步到运行配置，请核对后保存";
 } catch (e: any) { editorError.value = e?.message || "env 格式不正确"; }
}
function accept(value: RuntimeConfigSnapshot) {
  snapshot.value = value;
  envBase.value = envDraft.value = null; draftBaseline.value = draftValues.value = null; editorText.value = "";
  for (const field of value.fields) {
    form[field.key] = field.kind === "boolean" ? field.value === "true" : field.value;
    clearSecrets[field.key] = false;
  }
}
async function load() {
  if (busy.value) return;
  busy.value = true; failure.value = "";
  try { const result: any = await runtimeConfigApi.read(); accept(result.data); }
  catch (e: any) { failure.value = e?.msg || e?.message || "配置加载失败"; }
  finally { busy.value = false; }
}
async function waitForRestart(generation: string, deployment: string[] = []) {
  const deadline = Date.now() + 60000;
  while (!disposed && Date.now() < deadline) {
    await new Promise(resolve => setTimeout(resolve, 1000));
    try {
      const result: any = await runtimeConfigApi.read();
      if (disposed) return;
      const value: RuntimeConfigSnapshot = result.data;
      if (value.generation !== generation && !value.restarting) {
        accept(value);
        if (value.pending) throw new Error("API 已恢复，部分配置仍未生效，请重新加载检查");
        status.value = deployment.length ? `API 已重启，常用运行项已生效；以下配置仍需部署工具应用：${deployment.join("、")}` : "API 已重启，常用运行配置已生效；其他配置需部署工具应用";
        ElMessage.success(status.value);
        return;
      }
    } catch (e: any) {
      if (e?.message?.includes("部分配置")) throw e;
    }
  }
  if (!disposed) throw new Error("配置已保存，但未能确认 API 重启完成。请重新加载或查看平台日志，勿反复保存。");
}
function save(restart: boolean) {
  if (busy.value || verification.visible || !snapshot.value?.available) return;
  failure.value = "";
  clearVerification();
  verification.openDocument = false;
  verification.restart = restart;
  verification.method = "password";
  verification.visible = true;
}
async function submitVerification() {
  if (busy.value || !verification.visible || !snapshot.value?.available) return;
  if (verification.method === "password" && !verification.password) { failure.value = "请输入当前管理员密码"; return; }
  if (verification.method === "mfa" && (!mfaAvailable.value || !/^\d{6}$/.test(verification.code))) { failure.value = "请输入验证器中的6位验证码"; return; }
  const opening = verification.openDocument;
  const restart = verification.restart;
  const method = verification.method;
  const password = verification.password;
  const code = verification.code;
  clearVerification();
  busy.value = true; failure.value = ""; status.value = "";
  try {
    if (method === "mfa") {
      await authApi.stepUpMFA({ code });
      if (!await admin.restore()) throw new Error("管理员会话已变化，请重新登录");
    }
    if (opening) {
      const result: any = await runtimeConfigApi.document({ revision: snapshot.value.revision, verification_method: method, ...(method === "password" ? { current_password: password } : {}) });
      if (disposed) return;
      envBase.value = result.data.content;
      editorText.value = patchEnvDocument(result.data.content, changes());
      editorVisible.value = true; verification.visible = false;
      return;
    }
    const generation = snapshot.value.generation;
    const result: any = await runtimeConfigApi.update({ revision: snapshot.value.revision, ...(envDraft.value === null ? { changes: changes() } : { document: patchEnvDocument(envDraft.value, changes()) }), restart, verification_method: method, ...(method === "password" ? { current_password: password } : {}) });
    const deployment: string[] = result.data.deployment_keys || [];
    accept(result.data);
    verification.visible = false;
    if (restart) { status.value = "配置已保存，正在等待 API 重启…"; await waitForRestart(generation, deployment); }
    else { status.value = deployment.length ? `配置已保存；常用运行项重启 API 后生效，其他修改需部署工具应用：${deployment.join("、")}` : "配置已保存，重启 API 后生效"; ElMessage.success(status.value); }
  } catch (e: any) { failure.value = e?.msg || e?.message || "保存结果未确认，请重新加载检查配置"; }
  finally {
    clearVerification();
    if (!opening) for (const f of snapshot.value?.fields || []) if (f.kind === "secret") form[f.key] = "";
    busy.value = false;
  }
}
onMounted(load);
</script>

<style scoped>
.runtime-config { max-width: 1000px; }
.config-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 20px; }
.config-heading p, .config-note, .field-help { color: #606266; line-height: 1.7; }
.verification-note { color: #606266; line-height: 1.7; margin-bottom: 16px; }
.config-heading p { margin-top: 12px; }
.config-group, .el-alert { margin-bottom: 20px; }
.config-group h3 { margin: 0; }
.config-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 24px; }
.config-fields :deep(.el-form-item__content) { display: block; }
.config-fields .el-select { width: 100%; }
.field-help { font-size: 12px; margin: 6px 0 0; }
.pending-field { color: #b36b00; font-size: 12px; }
.config-actions { display: flex; gap: 12px; flex-wrap: wrap; }
.env-editor :deep(textarea) { font-family: ui-monospace, monospace; white-space: pre; overflow-wrap: normal; tab-size: 2; }
.config-note { font-size: 13px; margin-top: 16px; }
@media (max-width: 600px) { .config-fields { grid-template-columns: 1fr; } .config-heading { align-items: flex-start; } }
</style>
