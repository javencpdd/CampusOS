// @vitest-environment jsdom
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
const mock = vi.hoisted(() => ({
  list: vi.fn(),
  detail: vi.fn(),
  configure: vi.fn(),
  options: vi.fn(),
  configureSecrets: vi.fn(),
  profile: null as any,
  token: "current",
}));
vi.mock("../src/modules/plugins/v5-api", () => ({ pluginV5Api: mock }));
vi.mock("../src/modules/identity/session", () => ({
  getCurrentAdminProfile: () => mock.profile,
  getAdminAccessToken: () => mock.token,
}));
import View from "../src/modules/plugins/pages/PluginV5ConfigView.vue";
it("opens the existing MFA confirmation without replaying any command", async () => {
  const w = await opened();
  const notify = vi.fn();
  window.addEventListener("campusos:admin-mfa-step-up-required", notify);
  await w
    .findAll("header button")
    .find((b) => b.text() === "确认管理员身份")!
    .trigger("click");
  expect(notify).toHaveBeenCalledOnce();
  expect(mock.configure).not.toHaveBeenCalled();
  window.removeEventListener("campusos:admin-mfa-step-up-required", notify);
});
const coordinate = {
  publisher: "test",
  plugin_key: "config",
  version: "1.0.0",
};
function entry(version = "1.0.0") {
  return {
    release: {
      ...coordinate,
      version,
      manifest: {
        api_version: "campusos.plugin/v5",
        display_name: "配置样例",
        data: { config_scope: "system" },
        configuration: {
          contract: "campusos.plugin-config/v1",
          version: "v1",
          schema: {
            type: "object",
            additionalProperties: false,
            properties: {
              label: {
                type: "string",
                title: "名称",
                minLength: 1,
                maxLength: 20,
              },
              enabled: { type: "boolean", title: "启用" },
              mode: { type: "string", title: "模式", enum: ["a", "b"] },
            },
            required: ["label", "enabled"],
          },
          defaults: {},
          selectors: [],
          secret_names: [],
        },
      },
    },
    installation: {
      installed: true,
      observed: "stopped",
      desired: "disabled",
      error_code: "",
    },
    configuration: {
      owner_user_id: "",
      revision: "9007199254740993",
      values: { label: "原始", enabled: false },
      secret_refs: {},
    },
  };
}
const wrappers: ReturnType<typeof mount>[] = [];
beforeEach(() => {
  vi.clearAllMocks();
  mock.token = "current";
  mock.profile = {
    domain: "admin",
    account: { id: "opaque" },
    permission_codes: ["plugin.plugin.read", "plugin.plugin.configure"],
  };
  mock.list.mockResolvedValue({
    code: 0,
    data: { items: [entry()], next_cursor: "" },
  });
  mock.detail.mockResolvedValue({ code: 0, data: entry() });
  mock.options.mockResolvedValue({
    code: 0,
    data: { profiles: [], public_collections: [], secret_refs: {} },
  });
  mock.configure.mockResolvedValue({
    code: 0,
    data: { id: "operation", status: "pending" },
  });
});
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount());
  vi.useRealTimers();
});
async function opened() {
  const w = mount(View);
  wrappers.push(w);
  await flushPromises();
  await w.get('[aria-label="插件版本列表"] button').trigger("click");
  await flushPromises();
  return w;
}
it("saves exact revision/text/false, keeps optional absent, and reads back committed facts", async () => {
  const w = await opened();
  await w.get('[aria-label="名称"]').setValue("新配置");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(mock.configure).toHaveBeenCalledWith(
    expect.objectContaining(coordinate),
    "9007199254740993",
    { label: "新配置", enabled: false },
  );
  expect(mock.detail).toHaveBeenCalledTimes(2);
  expect(w.get('[role="status"]').text()).toContain("普通配置已保存");
  expect(w.text()).toContain("stopped");
  expect(w.text()).not.toContain("已启用");
  expect(
    JSON.stringify(localStorage) + JSON.stringify(sessionStorage),
  ).not.toContain("新配置");
});
it("checks required input before network and includes/removes optional enum explicitly", async () => {
  const w = await opened();
  await w.get('[aria-label="名称"]').setValue("");
  await w.get("form").trigger("submit");
  expect(mock.configure).not.toHaveBeenCalled();
  await w.get('[aria-label="名称"]').setValue("有效");
  await w.get('[aria-label="设置模式"]').setValue(true);
  await w.get('[aria-label="模式"]').setValue("b");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(mock.configure.mock.calls[0][2].mode).toBe("b");
});
it("read-only administrator sees the form without a save action", async () => {
  mock.profile.permission_codes = ["plugin.plugin.read"];
  const w = await opened();
  expect(w.find('button[type="submit"]').exists()).toBe(false);
  await w.get("form").trigger("submit");
  expect(mock.configure).not.toHaveBeenCalled();
});
it("conflict closes stale editing and never silently retries", async () => {
  mock.configure.mockRejectedValue({ status: 409 });
  const w = await opened();
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(w.get('[role="alert"]').text()).toContain("其他操作修改");
  expect(w.find("form").exists()).toBe(false);
  expect(mock.configure).toHaveBeenCalledOnce();
});
it("clears details immediately on selection and ignores the older response", async () => {
  mock.list.mockResolvedValue({
    code: 0,
    data: { items: [entry(), entry("2.0.0")], next_cursor: "" },
  });
  let old!: (r: any) => void;
  mock.detail.mockImplementationOnce(
    () =>
      new Promise((r) => {
        old = r;
      }),
  );
  const w = await opened();
  await w.findAll('[aria-label="插件版本列表"] button')[1].trigger("click");
  await flushPromises();
  expect(w.find("form").exists()).toBe(false); // mismatched server coordinate fails closed
  old({ code: 0, data: entry() });
  await flushPromises();
  expect(w.find("form").exists()).toBe(false);
});
it("discards a late private response after identity changes and clears cached fields", async () => {
  let done!: (r: any) => void;
  mock.detail.mockImplementationOnce(
    () =>
      new Promise((r) => {
        done = r;
      }),
  );
  const w = await opened();
  mock.token = "other";
  mock.profile = { ...mock.profile, account: { id: "other" } };
  done({ code: 0, data: entry() });
  await flushPromises();
  expect(w.find("form").exists()).toBe(false);
  expect(w.text()).not.toContain("原始");
  expect(w.text()).toContain("身份或权限已变化");
});
it("current permission loss rejects a cached form before submit", async () => {
  const w = await opened();
  mock.profile.permission_codes = ["plugin.plugin.read"];
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(mock.configure).not.toHaveBeenCalled();
  expect(w.find("form").exists()).toBe(false);
});
it("bound Secret never enters ordinary fields or plaintext inputs", async () => {
  const e = entry();
  e.release.manifest.configuration.secret_names = ["api_key"] as any;
  e.configuration.secret_refs = { api_key: "secret-ref:private" } as any;
  mock.detail.mockResolvedValue({ code: 0, data: e });
  const w = await opened();
  expect(w.find('form[aria-label="普通配置"]').exists()).toBe(true);
  expect(w.text()).not.toContain("secret-ref:private");
  expect(mock.configureSecrets).not.toHaveBeenCalled();
});
it("renders failed/empty inventories with a usable refresh action", async () => {
  mock.list.mockRejectedValueOnce(new Error("暂不可用"));
  const w = mount(View);
  wrappers.push(w);
  await flushPromises();
  expect(w.get('[role="alert"]').text()).toContain("暂不可用");
  mock.list.mockResolvedValue({
    code: 0,
    data: { items: [], next_cursor: "" },
  });
  await w.get("header button").trigger("click");
  await flushPromises();
  expect(w.text()).toContain("尚无可查看");
});
