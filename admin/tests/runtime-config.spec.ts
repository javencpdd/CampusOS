// @vitest-environment jsdom
import { shallowMount, flushPromises } from "@vue/test-utils";
import { nextTick } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { read, update, document, stepUpMFA, restore, profile } = vi.hoisted(() => ({ read: vi.fn(), update: vi.fn(), document: vi.fn(), stepUpMFA: vi.fn(), restore: vi.fn(), profile: { mfa_enrolled: false } }));
vi.mock("../src/modules/operations/runtime-config", () => ({ runtimeConfigApi: { read, update, document } }));
vi.mock("../src/modules/identity/api", () => ({ authApi: { stepUpMFA } }));
vi.mock("../src/modules/identity/store", () => ({ useAdminStore: () => ({ profile, restore }) }));
vi.mock("element-plus", () => ({ ElMessage: { success: vi.fn() } }));
import View from "../src/modules/operations/pages/RuntimeConfigView.vue";

const snapshot = () => ({ available: true, revision: "a".repeat(64), generation: "first", pending: false, restarting: false, fields: [
  { key: "AI_ENABLED", label: "AI", group: "AI", kind: "boolean", value: "false", configured: true, pending: false },
  { key: "EMAIL_SMTP_PASSWORD", label: "凭据", group: "邮件", kind: "secret", value: "", configured: true, pending: false },
] });
const mount = () => shallowMount(View, { global: { stubs: ["el-form", "el-card", "el-form-item", "el-input", "el-button", "el-select", "el-option", "el-checkbox", "el-alert", "el-dialog", "el-radio", "el-radio-group"], renderStubDefaultSlot: true } });
describe("runtime configuration editor verification choices", () => {
  beforeEach(() => {
    vi.clearAllMocks(); profile.mfa_enrolled = false;
    read.mockResolvedValue({ code: 0, data: snapshot() });
    update.mockResolvedValue({ code: 0, data: { ...snapshot(), pending: true } });
    document.mockResolvedValue({ data: { revision: "a".repeat(64), content: "# retained\nAI_ENABLED=false\nEMAIL_SMTP_PASSWORD='fixture-hidden-credential'\nDEPLOYMENT_PORT=3000\n" } });
    stepUpMFA.mockResolvedValue({ code: 0 }); restore.mockResolvedValue(true);
  });
  it("opens password confirmation before sending and omits unchanged hidden credentials", async () => {
    const view = mount(); await flushPromises(); const state = view.vm as any;
    state.form.AI_ENABLED = true; state.save(false);
    expect(update).not.toHaveBeenCalled(); expect(state.verification.visible).toBe(true);
    expect(state.verification.method).toBe("password"); expect(state.mfaAvailable).toBe(false);
    state.verification.password = "current-admin-fixture"; await state.submitVerification();
    expect(update).toHaveBeenCalledWith({ revision: "a".repeat(64), changes: { AI_ENABLED: "true" }, restart: false, verification_method: "password", current_password: "current-admin-fixture" });
    expect(stepUpMFA).not.toHaveBeenCalled(); expect(state.verification.password).toBe("");
    expect(state.verification.visible).toBe(false); expect(state.status).toContain("重启 API 后生效"); view.unmount();
  });
  it("requires an explicit selection to clear an SMTP credential", async () => {
    const view = mount(); await flushPromises(); const state = view.vm as any;
    expect(state.changes()).toEqual({}); state.clearSecrets.EMAIL_SMTP_PASSWORD = true;
    expect(state.changes()).toEqual({ EMAIL_SMTP_PASSWORD: "" }); view.unmount();
  });
  it("keeps a rejected form and dialog for retry, clearing submitted passwords", async () => {
    const view = mount(); await flushPromises(); const state = view.vm as any;
    state.form.AI_ENABLED = true; state.form.EMAIL_SMTP_PASSWORD = "fixture-smtp-value";
    state.save(false); state.verification.password = "incorrect-admin-value";
    update.mockRejectedValue({ msg: "管理员密码不正确，请重新输入" }); await state.submitVerification();
    expect(state.failure).toContain("密码不正确"); expect(state.form.AI_ENABLED).toBe(true);
    expect(state.verification.visible).toBe(true); expect(state.verification.password).toBe("");
    expect(state.form.EMAIL_SMTP_PASSWORD).toBe(""); expect(state.snapshot.revision).toBe("a".repeat(64)); view.unmount();
  });
  it("cancels or switches verification without retaining credentials or sending a request", async () => {
    profile.mfa_enrolled = true;
    const view = mount(); await flushPromises(); const state = view.vm as any;
    state.save(true); state.verification.password = "private-value"; state.verification.method = "mfa";
    await nextTick(); expect(state.verification.password).toBe("");
    state.verification.visible = false; state.clearVerification();
    expect(update).not.toHaveBeenCalled(); expect(stepUpMFA).not.toHaveBeenCalled(); view.unmount();
  });
  it("does not submit empty passwords or unavailable MFA", async () => {
    const view = mount(); await flushPromises(); const state = view.vm as any;
    state.save(false); await state.submitVerification(); expect(state.failure).toContain("当前管理员密码");
    state.verification.method = "mfa"; await nextTick(); state.verification.code = "123456";
    await state.submitVerification(); expect(update).not.toHaveBeenCalled(); expect(stepUpMFA).not.toHaveBeenCalled(); view.unmount();
  });
  it("verifies selected MFA before saving without sending an administrator password", async () => {
    profile.mfa_enrolled = true;
    const view = mount(); await flushPromises(); const state = view.vm as any;
    state.form.AI_ENABLED = true; state.save(false); state.verification.method = "mfa"; await nextTick();
    state.verification.code = "123456"; await state.submitVerification();
    expect(stepUpMFA).toHaveBeenCalledWith({ code: "123456" });
    expect(update).toHaveBeenCalledWith({ revision: "a".repeat(64), changes: { AI_ENABLED: "true" }, restart: false, verification_method: "mfa" });
    expect(state.verification.code).toBe(""); view.unmount();
  });
  it("does not write configuration if MFA verification fails", async () => {
    profile.mfa_enrolled = true;
    const view = mount(); await flushPromises(); const state = view.vm as any;
    state.save(false); state.verification.method = "mfa"; await nextTick(); state.verification.code = "123456";
    stepUpMFA.mockRejectedValue({ msg: "验证码不正确" }); await state.submitVerification();
    expect(update).not.toHaveBeenCalled(); expect(state.verification.visible).toBe(true);
    expect(state.verification.code).toBe(""); expect(state.failure).toContain("验证码不正确"); view.unmount();
  });
  it("confirms document disclosure, merges form edits, and stages edits without a write", async () => {
    const view = mount(); await flushPromises(); const state = view.vm as any;
    state.form.AI_ENABLED = true; state.openEnv();
    expect(document).not.toHaveBeenCalled(); expect(state.verification.openDocument).toBe(true);
    state.verification.password = "current-admin-fixture"; await state.submitVerification();
    expect(document).toHaveBeenCalledWith({ revision: "a".repeat(64), verification_method: "password", current_password: "current-admin-fixture" });
    expect(state.editorVisible).toBe(true); expect(state.editorText).toContain("AI_ENABLED='true'");
    state.editorText = state.editorText.replace('DEPLOYMENT_PORT=3000', 'DEPLOYMENT_PORT=3005');
    state.applyEnv();
    expect(state.form.AI_ENABLED).toBe(true); expect(state.deploymentKeys).toEqual(['DEPLOYMENT_PORT']);
    expect(update).not.toHaveBeenCalled();
    state.form.AI_ENABLED = false;
    state.save(false); state.verification.password = "current-admin-fixture"; await state.submitVerification();
    const payload = update.mock.calls[0][0];
    expect(payload.document).toContain("AI_ENABLED='false'");
    expect(payload.document).toContain('DEPLOYMENT_PORT=3005');
    expect(payload.document).toContain("EMAIL_SMTP_PASSWORD='fixture-hidden-credential'");
    expect(payload.changes).toBeUndefined(); expect(state.envDraft).toBe(null); view.unmount();
  });
  it("rejects duplicate raw keys without losing form state and clears document memory on exit", async () => {
    const view = mount(); await flushPromises(); const state = view.vm as any;
    state.editorText = 'AI_ENABLED=true\nAI_ENABLED=false\n'; state.applyEnv();
    expect(state.editorError).toContain('第 2 行'); expect(state.envDraft).toBe(null);
    expect(state.form.AI_ENABLED).toBe(false); expect(update).not.toHaveBeenCalled();
    state.envDraft = 'private document'; state.editorText = 'private document'; view.unmount();
    expect(state.envDraft).toBe(null); expect(state.editorText).toBe('');
  });

});
