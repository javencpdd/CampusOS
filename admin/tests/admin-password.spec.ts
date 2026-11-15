// @vitest-environment jsdom
import { shallowMount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { changePassword, clearSession, replace, success, error, warning } =
  vi.hoisted(() => ({
    changePassword: vi.fn(),
    clearSession: vi.fn(),
    replace: vi.fn(),
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
  }));
vi.mock("../src/modules/identity/api", () => ({ authApi: { changePassword } }));
vi.mock("../src/modules/identity/store", () => ({
  useAdminStore: () => ({ clearSession }),
}));
vi.mock("vue-router", () => ({ useRouter: () => ({ replace }) }));
vi.mock("element-plus", () => ({ ElMessage: { success, error, warning } }));
import View from "../src/modules/identity/pages/AdminAccountSecurityView.vue";

const mount = () =>
  shallowMount(View, {
    global: {
      stubs: ["el-form", "el-form-item", "el-input", "el-button"],
      renderStubDefaultSlot: true,
    },
  });
describe("current administrator password change", () => {
  beforeEach(() => vi.clearAllMocks());
  it("rejects mismatched confirmation before sending and preserves the session", async () => {
    const wrapper = mount(),
      state = wrapper.vm as any;
    state.form.current = "current-fixture";
    state.form.next = "next-fixture";
    state.form.repeat = "different";
    await state.submit();
    expect(changePassword).not.toHaveBeenCalled();
    expect(clearSession).not.toHaveBeenCalled();
    wrapper.unmount();
  });
  it("clears only local session after success and sends no target account or policy fields", async () => {
    changePassword.mockResolvedValue({ code: 0, data: { changed: true } });
    const wrapper = mount(),
      state = wrapper.vm as any;
    Object.assign(state.form, {
      current: "current-fixture",
      next: "next-fixture",
      repeat: "next-fixture",
    });
    await state.submit();
    expect(changePassword).toHaveBeenCalledWith({
      current_password: "current-fixture",
      new_password: "next-fixture",
    });
    expect(clearSession).toHaveBeenCalledOnce();
    expect(replace).toHaveBeenCalledWith("/login");
    expect(state.form).toEqual({ current: "", next: "", repeat: "" });
    wrapper.unmount();
  });
  it("keeps the authenticated session on a rejected change and clears form secrets", async () => {
    changePassword.mockRejectedValue({ msg: "当前密码不正确" });
    const wrapper = mount(),
      state = wrapper.vm as any;
    Object.assign(state.form, {
      current: "current-fixture",
      next: "next-fixture",
      repeat: "next-fixture",
    });
    await state.submit();
    expect(clearSession).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
    expect(state.form).toEqual({ current: "", next: "", repeat: "" });
    wrapper.unmount();
  });
});
