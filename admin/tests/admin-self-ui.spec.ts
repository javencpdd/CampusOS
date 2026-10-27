// @vitest-environment jsdom
import { flushPromises, shallowMount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { reactive } from "vue";
const {
  login,
  enrollMFA,
  confirmMFA,
  stepUpMFA,
  restore,
  success,
  error,
  warning,
  push,
} = vi.hoisted(() => ({
  login: vi.fn(),
  enrollMFA: vi.fn(),
  confirmMFA: vi.fn(),
  stepUpMFA: vi.fn(),
  restore: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
  warning: vi.fn(),
  push: vi.fn(),
}));
let admin: any;
vi.mock("../src/modules/identity/store", () => ({
  useAdminStore: () => admin,
}));
vi.mock("../src/modules/identity/api", () => ({
  authApi: { enrollMFA, confirmMFA, stepUpMFA },
}));
vi.mock("element-plus", () => ({ ElMessage: { success, error, warning } }));
vi.mock("vue-router", () => ({
  useRouter: () => ({ push }),
  useRoute: () => ({ query: {} }),
}));
import AdminSelfMFA from "../src/components/AdminSelfMFA.vue";
import LoginView from "../src/modules/identity/pages/LoginView.vue";
const stubs = Object.fromEntries(
  [
    "el-dialog",
    "el-button",
    "el-input",
    "el-form",
    "el-form-item",
    "el-alert",
    "el-card",
  ].map((name) => [name, true]),
);
const mountMFA = () =>
  shallowMount(AdminSelfMFA, {
    props: { modelValue: true },
    global: { stubs, renderStubDefaultSlot: true },
  });
const uri =
  "otpauth://totp/CampusOS:operator?secret=JBSWY3DPEHPK3PXP&issuer=CampusOS";
describe("Admin self MFA and independent login UI", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    admin = reactive({
      user: { id: "adm.opaque" },
      token: "admin-jwt",
      isLoggedIn: true,
      profile: { mfa_enrolled: false, authentication_strength: "password" },
      login,
      restore,
    });
    enrollMFA.mockResolvedValue({ code: 0, data: { otpauth_url: uri } });
    confirmMFA.mockResolvedValue({ code: 0, data: { status: "active" } });
    stepUpMFA.mockResolvedValue({
      code: 0,
      data: { authentication_strength: "mfa" },
    });
    restore.mockResolvedValue(true);
  });
  it("logs in with a username and clears the password without a User MFA ticket", async () => {
    login.mockResolvedValue({ code: 0 });
    const w = shallowMount(LoginView, {
      global: { stubs, renderStubDefaultSlot: true },
    });
    const v = w.vm as any;
    v.form.username = "operator";
    v.form.password = "password";
    await v.handleLogin();
    expect(login).toHaveBeenCalledWith("operator", "password");
    expect(push).toHaveBeenCalledWith("/");
    expect(v.form.password).toBe("");
    expect(
      w.findAll("el-form-item-stub").map((item) => item.attributes("label")),
    ).toContain("管理员用户名或邮箱");
    expect(w.text()).not.toContain("普通用户端");
    w.unmount();
  });
  it("explains enrollment steps and does not require an email code or password", () => {
    const w = mountMFA();
    expect(w.text()).toContain("支持 TOTP");
    expect(w.text()).toContain("6 位动态验证码");
    expect(w.text()).toContain("15 分钟");
    expect(w.text()).toContain("不是邮箱验证码");
    w.unmount();
  });
  it("does not enroll or step up when the current deployment disables MFA", async () => {
    admin.profile.mfa_required = false;
    const w = mountMFA(),
      v = w.vm as any;
    await v.enroll();
    v.code = "123456";
    await v.submit();
    expect(enrollMFA).not.toHaveBeenCalled();
    expect(confirmMFA).not.toHaveBeenCalled();
    expect(stepUpMFA).not.toHaveBeenCalled();
    expect(w.text()).not.toContain("开始配置管理员认证器");
    w.unmount();
  });
  it("clears enrollment secrets when the deployment requirement changes", async () => {
    const w = mountMFA(),
      v = w.vm as any;
    await v.enroll();
    expect(v.enrollmentSecret).toBeTruthy();
    admin.profile.mfa_required = false;
    await flushPromises();
    expect(v.enrollmentSecret).toBe("");
    expect(v.enrollmentURL).toBe("");
    w.unmount();
  });
  it("configures the own Admin factor, then requests a new code instead of replaying confirmation", async () => {
    const w = mountMFA(),
      v = w.vm as any;
    await v.enroll();
    expect(v.enrollmentSecret).toBe("JBSWY3DPEHPK3PXP");
    restore.mockImplementationOnce(async () => {
      admin.profile.mfa_enrolled = true;
      return true;
    });
    v.code = "123456";
    await v.submit();
    expect(confirmMFA).toHaveBeenCalledWith({ code: "123456" });
    expect(stepUpMFA).not.toHaveBeenCalled();
    expect(v.enrollmentURL).toBe("");
    expect(v.code).toBe("");
    expect(v.notice).toContain("下一组验证码");
    w.unmount();
  });
  it("accepts an MFA strength ACK only after current self confirms it without a new access token", async () => {
    admin.profile.mfa_enrolled = true;
    const w = mountMFA(),
      v = w.vm as any;
    v.code = "234567";
    restore.mockImplementationOnce(async () => {
      admin.profile.authentication_strength = "mfa";
      return true;
    });
    await v.submit();
    expect(restore).toHaveBeenCalled();
    expect(w.emitted("confirmed")).toHaveLength(1);
    expect(w.emitted("update:modelValue")?.[0]).toEqual([false]);
    w.unmount();
  });
  it("rejects a strength ACK when current self has expired or changed", async () => {
    admin.profile.mfa_enrolled = true;
    const w = mountMFA(),
      v = w.vm as any;
    v.code = "234567";
    await v.submit();
    expect(w.emitted("confirmed")).toBeUndefined();
    expect(error).toHaveBeenCalled();
    expect(v.code).toBe("");
    w.unmount();
  });
  it("clears secret material on close and ignores late enrollment after account changes", async () => {
    const w = mountMFA(),
      v = w.vm as any;
    await v.enroll();
    await w.setProps({ modelValue: false });
    expect(v.enrollmentURL).toBe("");
    expect(v.enrollmentSecret).toBe("");
    await w.setProps({ modelValue: true });
    let release!: (value: any) => void;
    enrollMFA.mockReturnValueOnce(
      new Promise((resolve) => {
        release = resolve;
      }),
    );
    const pending = v.enroll();
    admin.user = { id: "adm.other" };
    await flushPromises();
    release({ code: 0, data: { otpauth_url: uri } });
    await pending;
    expect(v.enrollmentURL).toBe("");
    expect(v.loading).toBe(false);
    w.unmount();
  });
});
