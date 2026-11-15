// @vitest-environment jsdom
import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
const mock = vi.hoisted(() => ({
  list: vi.fn(),
  detail: vi.fn(),
  command: vi.fn(),
}));
vi.mock("../src/modules/plugins/v5-api", () => ({ pluginV5Api: mock }));
vi.mock("../src/modules/identity/session", () => ({
  getAdminAccessToken: () => "current",
  getCurrentAdminProfile: () => profile,
}));
const profile = {
  domain: "admin",
  account: { id: "expiry-test" },
  permission_codes: [
    "plugin.plugin.read",
    "plugin.plugin.configure",
    "plugin.plugin.lifecycle",
  ],
};
import View from "../src/modules/plugins/pages/PluginV5ManageView.vue";
const day = 24 * 60 * 60 * 1000;
const coordinate = {
  publisher: "campusos",
  plugin_key: "example",
  version: "1.0.0",
};
const entry = (expiry: number) => ({
  release: {
    ...coordinate,
    manifest: { runtime: "none", data: { config_scope: "both" }, consumes: [] },
  },
  installation: {
    installed: true,
    version: "1.0.0",
    desired_version: "1.0.0",
    desired: "enabled",
    observed: "stopped",
    generation: "9",
  },
  grant: {
    approved: true,
    consumes: [],
    revision: "10",
    expires_at_ms: String(expiry),
  },
});
const wrappers: ReturnType<typeof mount>[] = [];
beforeEach(() => vi.clearAllMocks());
afterEach(() => wrappers.splice(0).forEach((w) => w.unmount()));
async function open(expiry: number) {
  const item = entry(expiry);
  mock.list.mockResolvedValue({
    code: 0,
    data: { items: [item], next_cursor: "" },
  });
  mock.detail.mockResolvedValue({ code: 0, data: item });
  mock.command.mockImplementation(async (_c, intent) => {
    mock.detail.mockResolvedValue({
      code: 0,
      data: {
        ...item,
        grant: {
          ...item.grant,
          revision: "11",
          expires_at_ms: intent.expires_at_ms,
        },
      },
    });
    return { code: 0, data: { id: "expiry-op", status: "done" } };
  });
  const w = mount(View, {
    global: { stubs: { RouterLink: true, PluginV5Frame: true } },
  });
  wrappers.push(w);
  await flushPromises();
  await w.get('[aria-label="插件版本"] button').trigger("click");
  await flushPromises();
  await w.get('[aria-label="审批原因"]').setValue("调整授权期限");
  return w;
}
async function save(w: ReturnType<typeof mount>) {
  await w.get('[aria-label="版本授权审批"]').trigger("submit");
  await flushPromises();
}

it("saves a permanent grant as zero and reads it back as permanent without a required date", async () => {
  const w = await open(Date.now() + day);
  await w.get('[aria-label="授权期限"]').setValue("permanent");
  expect(w.find('[aria-label="授权有效期"]').exists()).toBe(false);
  await save(w);
  expect(mock.command).toHaveBeenCalledOnce();
  expect(mock.command.mock.calls[0][1]).toMatchObject({
    kind: "grant",
    expires_at_ms: "0",
    expected_revision: "10",
  });
  expect(w.get('[aria-label="授权期限"]').element.value).toBe("permanent");
  expect(w.text()).toContain("已保存：永久");
});

it("extends the existing future deadline, preserves exact milliseconds and resets the draft after save", async () => {
  const expiry = Date.now() + 12 * day + 123;
  const w = await open(expiry);
  await w.get('[aria-label="授权期限"]').setValue("30");
  expect(mock.command).not.toHaveBeenCalled();
  await save(w);
  expect(mock.command.mock.calls[0][1].expires_at_ms).toBe(
    String(expiry + 30 * day),
  );
  expect(w.get('[aria-label="授权期限"]').element.value).toBe("date");
  expect(w.text()).not.toContain("有未保存修改");
});

it.each(["expired", "permanent"])(
  "starts the %s grant's new finite duration at save time",
  async (mode) => {
    const w = await open(mode === "expired" ? Date.now() - day : 0);
    await w.get('[aria-label="授权期限"]').setValue("90");
    const before = Date.now();
    await save(w);
    const after = Date.now();
    const expiry = Number(mock.command.mock.calls[0][1].expires_at_ms);
    expect(expiry).toBeGreaterThanOrEqual(before + 90 * day);
    expect(expiry).toBeLessThanOrEqual(after + 90 * day);
  },
);

it.each(["0", "-1", "1.5", "3651", ""])(
  "rejects an invalid custom extension of %s days",
  async (days) => {
    const w = await open(0);
    await w.get('[aria-label="授权期限"]').setValue("custom");
    await w.get('[aria-label="延长天数"]').setValue(days);
    await save(w);
    expect(mock.command).not.toHaveBeenCalled();
    expect(w.text()).toContain("1 至 3650 的整数天数");
  },
);

it("accepts custom days and preserves a saved deadline when only the approval reason is edited", async () => {
  const expiry = Date.now() + 3 * day + 789;
  const w = await open(expiry);
  await save(w);
  expect(mock.command.mock.calls[0][1].expires_at_ms).toBe(String(expiry));
  await w.get('[aria-label="审批原因"]').setValue("再次延期");
  await w.get('[aria-label="授权期限"]').setValue("custom");
  await w.get('[aria-label="延长天数"]').setValue("45");
  await save(w);
  expect(mock.command.mock.calls[1][1]).toMatchObject({
    expires_at_ms: String(expiry + 45 * day),
    expected_revision: "11",
  });
});

it("restores the saved permanent setting without submitting the extension draft", async () => {
  const w = await open(0);
  await w.get('[aria-label="授权期限"]').setValue("365");
  await w
    .findAll("button")
    .find((b) => b.text() === "恢复已保存的设置")!
    .trigger("click");
  expect(w.get('[aria-label="授权期限"]').element.value).toBe("permanent");
  expect(mock.command).not.toHaveBeenCalled();
});
