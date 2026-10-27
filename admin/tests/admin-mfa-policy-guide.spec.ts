// @vitest-environment jsdom
import { shallowMount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { reactive } from "vue";
const { restore, error } = vi.hoisted(() => ({
  restore: vi.fn(),
  error: vi.fn(),
}));
let admin: any;
vi.mock("../src/modules/identity/store", () => ({
  useAdminStore: () => admin,
}));
vi.mock("element-plus", () => ({ ElMessage: { error } }));
import View from "../src/modules/identity/pages/MFAPolicyView.vue";
const mountView = () =>
  shallowMount(View, {
    global: {
      stubs: { "el-button": true, "el-alert": true, AdminSelfMFA: true },
      renderStubDefaultSlot: true,
    },
  });
describe("Independent Admin MFA deployment status and guide", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    admin = reactive({
      profile: { mfa_required: true, mfa_enrolled: false },
      restore,
    });
    restore.mockResolvedValue(true);
  });
  it("shows effective deployment configuration and a usable enrollment guide", () => {
    const w = mountView();
    expect(w.text()).toContain("已开启二次验证");
    expect(w.text()).toContain("AUTH_ADMIN_MFA_REQUIRED=true");
    expect(w.text()).toContain("TOTP");
    expect(w.text()).toContain("6 位验证码");
    expect(w.text()).toContain("下一组验证码");
    expect(restore).not.toHaveBeenCalled();
    w.unmount();
  });
  it("shows explicit opt-out without prompting for enrollment", () => {
    admin.profile.mfa_required = false;
    const w = mountView();
    expect(w.text()).toContain("已关闭二次验证");
    expect(w.text()).toContain("AUTH_ADMIN_MFA_REQUIRED=false");
    expect(w.text()).not.toContain("开始配置认证器");
    w.unmount();
  });
  it("refreshes current server facts, never calls the legacy user-domain policy API", async () => {
    const w = mountView();
    await (w.vm as any).refresh();
    expect(restore).toHaveBeenCalledTimes(1);
    expect(error).not.toHaveBeenCalled();
    w.unmount();
  });
});
