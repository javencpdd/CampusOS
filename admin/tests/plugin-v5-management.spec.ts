// @vitest-environment jsdom
import { mount, flushPromises } from "@vue/test-utils";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
const mock = vi.hoisted(() => ({
  list: vi.fn(),
  detail: vi.fn(),
  command: vi.fn(),
  importPackage: vi.fn(),
  operation: vi.fn(),
  uiManifest: vi.fn(),
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
const approvedEntry = () => ({
  ...entry(),
  grant: {
    ...entry().grant,
    approved: true,
    consumes: entry().release.manifest.consumes,
  },
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
  mock.uiManifest.mockResolvedValue({
    code: 0,
    data: {
      plugins: [],
      frame_src: "http://*.plugins.example:3003",
      next_cursor: "",
    },
  });
  document.querySelector("[data-campusos-plugin-frames]")?.remove();
});
afterEach(() => wrappers.splice(0).forEach((w) => w.unmount()));
async function opened() {
  const w = mount(View, {
    global: { stubs: { RouterLink: true, PluginV5Frame: true } },
  });
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
it("keeps the selected plugin and approval draft after a conflict, reads current facts and never replays", async () => {
  mock.detail.mockResolvedValue({ code: 0, data: approvedEntry() });
  mock.command.mockRejectedValue({ status: 409 });
  const w = await opened();
  mock.detail.mockResolvedValue({
    code: 0,
    data: {
      ...approvedEntry(),
      installation: {
        ...approvedEntry().installation,
        generation: "9007199254740998",
      },
    },
  });
  await w
    .findAll("button")
    .find((b) => b.text() === "启用")!
    .trigger("click");
  await flushPromises();
  expect(mock.command).toHaveBeenCalledOnce();
  expect(mock.command.mock.calls[0][1].expected_generation).toBe(
    "9007199254740993",
  );
  expect(w.find('[aria-label="插件生命周期"]').exists()).toBe(true);
  expect(w.get('[aria-label="审批原因"]').element.value).toBe("");
  expect(mock.detail).toHaveBeenCalledTimes(2);
  expect(w.text()).toContain("状态已被其他操作修改");
  expect(w.text()).toContain("未自动重试");
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
  mock.detail.mockResolvedValue({ code: 0, data: approvedEntry() });
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
    .find((b) => b.text() === "恢复已保存的设置")!
    .trigger("click");
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
    ...approvedEntry(),
    release: { ...approvedEntry().release, version: "8.2.0" },
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

it("requires switching versions before enabling a release which has not been staged", async () => {
  const candidate = {
    ...approvedEntry(),
    release: { ...approvedEntry().release, version: "8.2.0" },
  };
  mock.list.mockResolvedValue({
    code: 0,
    data: { items: [candidate], next_cursor: "" },
  });
  mock.detail.mockResolvedValue({ code: 0, data: candidate });
  const w = await opened();
  expect(w.get('[aria-label="下一步"]').text()).toContain("切换到此版本");
  expect(
    w
      .findAll("button")
      .find((b) => b.text() === "启用此版本")!
      .attributes("disabled"),
  ).toBeDefined();
  expect(mock.command).not.toHaveBeenCalled();
});

it.each([
  ["none", "stopped"],
  ["wasm", "ready"],
])("does not label a non-current %s release as enabled", async (runtime, observed) => {
  const historical = approvedEntry();
  historical.release.manifest.runtime = runtime;
  historical.installation = {
    ...historical.installation,
    version: "2.1.0",
    desired: "enabled",
    observed,
  };
  mock.list.mockResolvedValue({
    code: 0,
    data: { items: [historical], next_cursor: "" },
  });
  mock.detail.mockResolvedValue({ code: 0, data: historical });
  const w = await opened();
  expect(w.get('[aria-label="插件版本"]').text()).toContain(
    "非当前版本（当前 2.1.0）",
  );
  expect(w.get('[aria-label="当前审批状态"]').text()).toContain(
    "非当前版本（当前 2.1.0）",
  );
  expect(w.get('[aria-label="插件启用步骤"]').findAll("li")[2].classes()).not.toContain("done");
  expect(w.get('[aria-label="下一步"]').text()).toContain("切换到此版本");
  expect(mock.command).not.toHaveBeenCalled();
});

it("requires saved approval before enabling; editing approval checkboxes alone does not enable", async () => {
  const unapproved = { ...entry(), grant: undefined };
  mock.detail.mockResolvedValue({ code: 0, data: unapproved });
  const w = await opened();
  const enable = () => w.findAll("button").find((b) => b.text() === "启用")!;
  expect(enable().attributes("disabled")).toBeDefined();
  expect(w.text()).toContain("请先在下方完成并保存此版本审批");
  await w
    .get('[aria-label="版本授权审批"] input[type="checkbox"]')
    .setValue(true);
  await w.get('[aria-label="审批原因"]').setValue("批准这个版本");
  expect(enable().attributes("disabled")).toBeDefined();
  mock.detail.mockResolvedValue({ code: 0, data: approvedEntry() });
  await w.get('[aria-label="版本授权审批"]').trigger("submit");
  await flushPromises();
  expect(mock.command).toHaveBeenCalledOnce();
  expect(mock.command.mock.calls[0][1].kind).toBe("grant");
  expect(enable().attributes("disabled")).toBeUndefined();
  expect(w.get('[aria-label="当前审批状态"]').text()).toContain("已审批");
});

it("shows the current package while keeping archived metadata out of the runtime list", async () => {
  const current = {
    ...approvedEntry(),
    release: { ...approvedEntry().release, version: "2.1.0", storage: "available" },
    installation: { ...entry().installation, version: "2.1.0", desired: "enabled", observed: "stopped" },
  };
  const archived = {
    ...current,
    release: { ...approvedEntry().release, storage: "purged" },
  };
  mock.list.mockResolvedValue({ code: 0, data: { items: [archived, current], next_cursor: "" } });
  mock.detail.mockResolvedValue({ code: 0, data: current });
  const w = await opened();
  const list = w.get('[aria-label="插件版本"]');
  expect(list.findAll("button[data-coordinate]")).toHaveLength(1);
  expect(list.text()).toContain("版本 2.1.0");
  expect(list.text()).not.toContain("版本 1.0.0");
  expect(w.get('[aria-label="当前审批状态"]').text()).toContain("界面已启用");
  expect(mock.command).not.toHaveBeenCalled();
});

it.each(["expired", "missing-capability", "missing-resources"])(
  "blocks enabling with %s approval",
  async (kind) => {
    const approved: any = approvedEntry();
    if (kind === "expired")
      approved.grant.expires_at_ms = String(Date.now() - 1000);
    if (kind === "missing-capability") approved.grant.consumes = [];
    if (kind === "missing-resources")
      approved.release.manifest.host_resources = {
        cpu_millis: 100,
        memory_mb: 64,
        storage_bytes: 1024,
        max_concurrency: 1,
        timeout_ms: 1000,
      };
    mock.detail.mockResolvedValue({ code: 0, data: approved });
    const w = await opened();
    expect(
      w
        .findAll("button")
        .find((b) => b.text() === "启用")!
        .attributes("disabled"),
    ).toBeDefined();
    expect(mock.command).not.toHaveBeenCalled();
  },
);

it("preserves the selected plugin and draft when approval is revoked after it was read", async () => {
  mock.detail.mockResolvedValue({ code: 0, data: approvedEntry() });
  const w = await opened();
  mock.command.mockRejectedValue({ status: 403 });
  mock.detail.mockResolvedValue({ code: 0, data: entry() });
  await w
    .findAll("button")
    .find((b) => b.text() === "启用")!
    .trigger("click");
  await flushPromises();
  expect(mock.command).toHaveBeenCalledOnce();
  expect(w.find('[aria-label="插件生命周期"]').exists()).toBe(true);
  expect(
    (
      w.get('[aria-label="版本授权审批"] input[type="checkbox"]')
        .element as HTMLInputElement
    ).checked,
  ).toBe(true);
  expect(w.text()).toContain("待保存");
  expect(w.text()).toContain("启用被拒绝");
  expect(
    w
      .findAll("button")
      .find((b) => b.text() === "启用")!
      .attributes("disabled"),
  ).toBeDefined();
});

it("retains context but blocks further writes if current state cannot be confirmed", async () => {
  mock.detail.mockResolvedValue({ code: 0, data: approvedEntry() });
  const w = await opened();
  mock.command.mockRejectedValue({ status: 503, message: "连接中断" });
  mock.detail.mockRejectedValue({ status: 503 });
  await w
    .findAll("button")
    .find((b) => b.text() === "启用")!
    .trigger("click");
  await flushPromises();
  expect(w.find('[aria-label="插件生命周期"]').exists()).toBe(true);
  expect(w.text()).toContain("当前状态尚未确认，请先刷新当前状态。");
  await w
    .findAll("button")
    .find((b) => b.text() === "启用")!
    .trigger("click");
  expect(mock.command).toHaveBeenCalledOnce();
});

it("retains context if an accepted command cannot be read back", async () => {
  mock.detail.mockResolvedValue({ code: 0, data: approvedEntry() });
  const w = await opened();
  await w.get('[aria-label="审批原因"]').setValue("保留读回失败时的草稿");
  mock.detail.mockRejectedValue({ status: 503 });
  await w.get('[aria-label="版本授权审批"]').trigger("submit");
  await flushPromises();
  expect(mock.command).toHaveBeenCalledOnce();
  expect(w.find('[aria-label="插件生命周期"]').exists()).toBe(true);
  expect(w.get('[aria-label="审批原因"]').element.value).toBe(
    "保留读回失败时的草稿",
  );
  expect(w.text()).toContain("操作已提交，当前状态尚未确认");
  expect(w.text()).toContain("当前状态尚未确认，请先刷新当前状态。");
});

it("clears details if the current read is forbidden after a rejected operation", async () => {
  mock.detail.mockResolvedValue({ code: 0, data: approvedEntry() });
  const w = await opened();
  mock.command.mockRejectedValue({ status: 403 });
  mock.detail.mockRejectedValue({ status: 403 });
  await w
    .findAll("button")
    .find((b) => b.text() === "启用")!
    .trigger("click");
  await flushPromises();
  expect(w.find('[aria-label="插件生命周期"]').exists()).toBe(false);
  expect(w.findAll('[aria-label="插件版本"] button')).toHaveLength(0);
});

it("does not restore stale details or approval drafts after the administrator identity changes", async () => {
  mock.detail.mockResolvedValue({ code: 0, data: approvedEntry() });
  const w = await opened();
  let fail: (reason: any) => void = () => {};
  mock.command.mockImplementation(
    () =>
      new Promise((_, reject) => {
        fail = reject;
      }),
  );
  await w
    .findAll("button")
    .find((b) => b.text() === "启用")!
    .trigger("click");
  mock.token = "another-session";
  fail({ status: 403 });
  await flushPromises();
  expect(w.find('[aria-label="插件生命周期"]').exists()).toBe(false);
  expect(w.text()).toContain("身份或权限已变化");
});

it("distinguishes a pending executor request from a running plugin", async () => {
  mock.detail.mockResolvedValue({
    code: 0,
    data: {
      ...approvedEntry(),
      installation: {
        ...approvedEntry().installation,
        desired: "enabled",
        observed: "pending",
        error_code: "plugin.runtime_unavailable",
      },
    },
  });
  const w = await opened();
  expect(w.get('[aria-label="当前审批状态"]').text()).toContain(
    "等待执行器（尚未运行）",
  );
  expect(w.text()).toContain("配置插件执行器");
});

const adminUIEntry = () => ({
  ...approvedEntry(),
  release: {
    ...approvedEntry().release,
    manifest: {
      ...approvedEntry().release.manifest,
      ui: [
        {
          audience: "admin",
          entrypoint: "dist/admin/index.html",
          digest: "sha256:" + "a".repeat(64),
        },
      ],
    },
  },
  installation: {
    ...approvedEntry().installation,
    desired: "enabled",
    desired_version: c.version,
    observed: "stopped",
  },
});
const managementButton = (w: ReturnType<typeof mount>) =>
  w.findAll("button").find((b) => b.text() === "打开插件管理界面");

it("separates saved approval from a local checkbox and lets the operator restore it", async () => {
  mock.detail.mockResolvedValue({ code: 0, data: approvedEntry() });
  const w = await opened();
  const checkbox = w.get('[aria-label="版本授权审批"] input[type="checkbox"]');
  expect((checkbox.element as HTMLInputElement).checked).toBe(true);
  expect(w.text()).toContain("已保存：批准");
  await checkbox.setValue(false);
  expect(w.text()).toContain("有未保存修改");
  expect(w.get('[aria-label="当前审批状态"]').text()).toContain("已审批");
  expect(
    w
      .findAll("button")
      .find((b) => b.text() === "启用")!
      .attributes("disabled"),
  ).toBeDefined();
  await w
    .findAll("button")
    .find((b) => b.text() === "恢复已保存的设置")!
    .trigger("click");
  expect((checkbox.element as HTMLInputElement).checked).toBe(true);
  expect(
    w
      .findAll("button")
      .find((b) => b.text() === "启用")!
      .attributes("disabled"),
  ).toBeUndefined();
  expect(mock.command).not.toHaveBeenCalled();
});

it("keeps technical details and dangerous actions collapsed while showing the next step", async () => {
  const w = await opened();
  expect(w.get('[aria-label="下一步"]').text()).toContain("展开审批设置");
  expect(
    w.get('details[aria-label="技术详情"]').attributes("open"),
  ).toBeUndefined();
  expect(
    w
      .findAll("details")
      .find((d) => d.find("summary").text() === "撤销授权与卸载")!
      .attributes("open"),
  ).toBeUndefined();
});

it("confirms a pending executor result automatically without discarding an approval draft", async () => {
  vi.useFakeTimers();
  let w: ReturnType<typeof mount> | undefined;
  try {
    const pending = {
      ...approvedEntry(),
      installation: {
        ...approvedEntry().installation,
        desired: "enabled",
        observed: "pending",
      },
    };
    mock.detail.mockResolvedValue({ code: 0, data: pending });
    w = await opened();
    await w.get('[aria-label="审批原因"]').setValue("保留草稿");
    mock.detail.mockResolvedValue({
      code: 0,
      data: {
        ...pending,
        installation: {
          ...pending.installation,
          error_code: "plugin.runtime_unavailable",
        },
      },
    });
    await vi.advanceTimersByTimeAsync(1300);
    await flushPromises();
    expect(w.get('[aria-label="当前审批状态"]').text()).toContain("等待执行器");
    expect(w.get('[aria-label="审批原因"]').element.value).toBe("保留草稿");
    await vi.advanceTimersByTimeAsync(5000);
    expect(mock.detail).toHaveBeenCalledTimes(2);
    expect(mock.command).not.toHaveBeenCalled();
  } finally {
    w?.unmount();
    vi.useRealTimers();
  }
});

it("explains plugins without a UI and does not offer a management page", async () => {
  const w = await opened();
  expect(w.get('[aria-label="插件界面说明"]').text()).toContain("没有独立界面");
  expect(managementButton(w)).toBeUndefined();
  expect(mock.uiManifest).not.toHaveBeenCalled();
});

it("directs a user-only plugin to the user plugin center without requesting an admin UI", async () => {
  const userOnly = adminUIEntry();
  userOnly.release.manifest.ui[0].audience = "user";
  mock.detail.mockResolvedValue({ code: 0, data: userOnly });
  const w = await opened();
  expect(w.get('[aria-label="插件界面说明"]').text()).toContain(
    "仅提供用户界面",
  );
  expect(w.text()).toContain("用户端插件中心");
  expect(managementButton(w)).toBeUndefined();
  expect(mock.uiManifest).not.toHaveBeenCalled();
});

it.each(["not-installed", "not-approved", "disabled", "expired"])(
  "disables declared admin UI with a specific %s prerequisite",
  async (kind) => {
    const item: any = adminUIEntry();
    if (kind === "not-installed") item.installation.installed = false;
    if (kind === "not-approved") item.grant.approved = false;
    if (kind === "disabled") item.installation.desired = "disabled";
    if (kind === "expired")
      item.grant.expires_at_ms = String(Date.now() - 1000);
    mock.detail.mockResolvedValue({ code: 0, data: item });
    const w = await opened();
    expect(managementButton(w)!.attributes("disabled")).toBeDefined();
    expect(w.get('[aria-label="插件界面说明"]').text()).toContain(
      kind === "not-installed"
        ? "先安装"
        : kind === "disabled"
          ? "先启用"
          : kind === "expired"
            ? "审批已过期"
            : "未获批准",
    );
    expect(mock.uiManifest).not.toHaveBeenCalled();
  },
);

it("opens the declared admin UI using only the host-issued frame and proof", async () => {
  mock.detail.mockResolvedValue({ code: 0, data: adminUIEntry() });
  mock.uiManifest.mockResolvedValue({
    code: 0,
    data: {
      plugins: [
        {
          name: "campusos.example",
          version: c.version,
          ui: {
            surfaces: [
              {
                id: "campusos.example.manage",
                frame: {
                  src: "http://lease.plugins.example:3003/ui/lease/index.html",
                  origin: "http://lease.plugins.example:3003",
                },
                context_proof: "sha256:" + "b".repeat(64),
              },
            ],
          },
        },
      ],
      frame_src: "http://*.plugins.example:3003",
      next_cursor: "",
    },
  });
  const w = await opened();
  expect(managementButton(w)!.attributes("disabled")).toBeUndefined();
  await managementButton(w)!.trigger("click");
  await flushPromises();
  expect(mock.uiManifest).toHaveBeenCalledOnce();
  const frame = w.findComponent({ name: "PluginV5Frame" });
  expect(frame.exists()).toBe(true);
  expect(frame.props("identity").audience).toBe("admin");
  expect(new URL(frame.props("src")).searchParams.get("host_origin")).toBe(
    window.location.origin,
  );
});

it("reports an unavailable server projection without incorrectly claiming a paging limit", async () => {
  mock.detail.mockResolvedValue({ code: 0, data: adminUIEntry() });
  const w = await opened();
  await managementButton(w)!.trigger("click");
  await flushPromises();
  expect(w.text()).toContain("服务端当前未提供此版本的管理界面");
  expect(w.text()).not.toContain("查找上限");
  expect(w.find('[aria-label="插件生命周期"]').exists()).toBe(true);
});

it("reports the paging limit only when a continuation cursor remains", async () => {
  mock.detail.mockResolvedValue({ code: 0, data: adminUIEntry() });
  mock.uiManifest.mockResolvedValue({
    code: 0,
    data: {
      plugins: [],
      frame_src: "http://*.plugins.example:3003",
      next_cursor: "more",
    },
  });
  const w = await opened();
  await managementButton(w)!.trigger("click");
  await flushPromises();
  expect(mock.uiManifest).toHaveBeenCalledTimes(4);
  expect(w.text()).toContain("已达到本次4页界面查找上限");
});

it("does not open a frame when the host omits the current UI proof", async () => {
  mock.detail.mockResolvedValue({ code: 0, data: adminUIEntry() });
  mock.uiManifest.mockResolvedValue({
    code: 0,
    data: {
      plugins: [
        {
          name: "campusos.example",
          version: c.version,
          ui: {
            surfaces: [
              {
                id: "campusos.example.manage",
                frame: {
                  src: "http://lease.plugins.example:3003/ui/lease/index.html",
                  origin: "http://lease.plugins.example:3003",
                },
              },
            ],
          },
        },
      ],
      frame_src: "http://*.plugins.example:3003",
      next_cursor: "",
    },
  });
  const w = await opened();
  await managementButton(w)!.trigger("click");
  await flushPromises();
  expect(w.text()).toContain("未返回有效的管理界面地址或授权证明");
  expect(w.findComponent({ name: "PluginV5Frame" }).exists()).toBe(false);
});
