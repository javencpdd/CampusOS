// @vitest-environment jsdom
import { mount, flushPromises } from "@vue/test-utils";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
const mock = vi.hoisted(() => ({
  list: vi.fn(),
  detail: vi.fn(),
  command: vi.fn(),
  importPackage: vi.fn(),
  operation: vi.fn(),
  profile: null as any,
  token: "current",
}));
vi.mock("../src/modules/plugins/v5-api", () => ({ pluginV5Api: mock }));
vi.mock("../src/modules/identity/session", () => ({
  getAdminAccessToken: () => mock.token,
  getCurrentAdminProfile: () => mock.profile,
}));
import View from "../src/modules/plugins/pages/PluginV5ManageView.vue";
const c = { publisher: "campusos", plugin_key: "example", version: "1.0.0" };
const entry = () => ({
  release: {
    ...c,
    manifest: {
      runtime: "none",
      data: { config_scope: "both" },
      consumes: [
        {
          capability: "ui.surface.open",
          scope: { kind: "self" },
          purpose: "Explicit selected resource",
          required: true,
        },
      ],
    },
  },
  installation: {
    installed: true,
    version: c.version,
    desired: "disabled",
    observed: "pending",
    generation: "9007199254740993",
  },
  grant: { approved: false, revision: "9007199254740994", consumes: [] },
});
const wrappers: ReturnType<typeof mount>[] = [];
beforeEach(() => {
  vi.clearAllMocks();
  mock.token = "current";
  mock.profile = {
    domain: "admin",
    account: { id: "opaque" },
    permission_codes: [
      "plugin.plugin.read",
      "plugin.plugin.install",
      "plugin.plugin.configure",
      "plugin.plugin.lifecycle",
      "plugin.plugin.uninstall",
    ],
  };
  mock.list.mockResolvedValue({
    code: 0,
    data: { items: [entry()], next_cursor: "" },
  });
  mock.detail.mockResolvedValue({ code: 0, data: entry() });
  mock.command.mockResolvedValue({
    code: 0,
    data: { id: "op", status: "queued" },
  });
});
afterEach(() => wrappers.splice(0).forEach((w) => w.unmount()));
async function opened() {
  const w = mount(View, { global: { stubs: { RouterLink: true } } });
  wrappers.push(w);
  await flushPromises();
  await w.get('[aria-label="插件版本"] button').trigger("click");
  await flushPromises();
  return w;
}
it("separates capability and runtime approvals and sends exact revision with explicit reason", async () => {
  const w = await opened();
  expect(w.findAll("legend").map((e) => e.text())).toContain("业务能力");
  expect(w.findAll("legend").map((e) => e.text())).toContain("运行资源");
  expect(w.text()).toContain("ui.surface.open");
  await w.get('[aria-label="版本授权审批"]').trigger("submit");
  expect(mock.command).not.toHaveBeenCalled();
  await w.get('[aria-label="审批原因"]').setValue("已核对当前用途");
  await w
    .get('[aria-label="版本授权审批"] input[type="checkbox"]')
    .setValue(true);
  await w.get('[aria-label="版本授权审批"]').trigger("submit");
  await flushPromises();
  expect(mock.command).toHaveBeenCalledWith(
    expect.objectContaining(c),
    expect.objectContaining({
      kind: "grant",
      expected_revision: "9007199254740994",
      reason: "已核对当前用途",
      consumes: entry().release.manifest.consumes,
    }),
  );
  expect(mock.detail).toHaveBeenCalledTimes(2);
});
it("does not replay a conflict and closes old details after current identity changes", async () => {
  mock.command.mockRejectedValue({ status: 409 });
  const w = await opened();
  await w
    .findAll("button")
    .find((b) => b.text() === "启用")!
    .trigger("click");
  await flushPromises();
  expect(mock.command).toHaveBeenCalledOnce();
  expect(mock.command.mock.calls[0][1].expected_generation).toBe(
    "9007199254740993",
  );
  expect(w.find('[aria-label="插件生命周期"]').exists()).toBe(false);
  expect(w.text()).toContain("状态已被其他操作修改");
});
it("renders read-only facts without write or import controls", async () => {
  mock.profile.permission_codes = ["plugin.plugin.read"];
  const w = await opened();
  expect(w.find('[aria-label="导入签名插件包"]').exists()).toBe(false);
  expect(
    w
      .findAll("button")
      .some((b) => ["保存此版本审批", "启用", "卸载"].includes(b.text())),
  ).toBe(false);
  await w.get('[aria-label="版本授权审批"]').trigger("submit");
  expect(mock.command).not.toHaveBeenCalled();
});

it("rejects past approval expiry and retries only a read-back operation revision", async () => {
  mock.operation.mockResolvedValue({
    code: 0,
    data: { ...c, id: "op", status: "waiting", revision: "9007199254740995" },
  });
  const w = await opened();
  await w.get('[aria-label="审批原因"]').setValue("已核对");
  await w.get('[aria-label="授权有效期"]').setValue("2020-01-01T00:00");
  await w.get('[aria-label="版本授权审批"]').trigger("submit");
  expect(mock.command).not.toHaveBeenCalled();
  expect(w.text()).toContain("未来 30 天");
  await w
    .findAll("button")
    .find((b) => b.text() === "启用")!
    .trigger("click");
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "查询操作状态")!
    .trigger("click");
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "重试此操作")!
    .trigger("click");
  await flushPromises();
  expect(mock.command).toHaveBeenLastCalledWith(expect.objectContaining(c), {
    kind: "retry",
    operation_id: "op",
    expected_revision: "9007199254740995",
  });
});

it("requires an explicit enable for a prepared version while the current installation is stopped", async () => {
  const staged = {
    ...entry(),
    release: { ...entry().release, version: "8.2.0" },
    installation: { ...entry().installation, desired_version: "8.2.0" },
  };
  mock.list.mockResolvedValue({
    code: 0,
    data: { items: [staged], next_cursor: "" },
  });
  mock.detail.mockResolvedValue({ code: 0, data: staged });
  const w = await opened();
  expect(w.text()).toContain("当前版本仍为");
  expect(mock.command).not.toHaveBeenCalled();
  await w
    .findAll("button")
    .find((b) => b.text() === "启用此版本")!
    .trigger("click");
  await flushPromises();
  expect(mock.command).toHaveBeenCalledOnce();
  expect(mock.command.mock.calls[0][0].version).toBe("8.2.0");
  expect(mock.command.mock.calls[0][1]).toEqual({
    kind: "enable",
    expected_generation: "9007199254740993",
  });
});
