// @vitest-environment jsdom
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, expect, it, vi } from "vitest";
const mock = vi.hoisted(() => ({
  list: vi.fn(),
  credentials: vi.fn(),
  create: vi.fn(),
  issue: vi.fn(),
  rotate: vi.fn(),
  scope: vi.fn(),
  revoke: vi.fn(),
  update: vi.fn(),
  codes: ["identity.integration.read", "identity.integration.manage"],
}));
vi.mock("../src/modules/integrations/principals-api", () => ({
  machineApi: mock,
}));
vi.mock("../src/modules/identity/session", () => ({
  getCurrentAdminProfile: () => ({ permission_codes: mock.codes }),
}));
import View from "../src/modules/integrations/pages/IntegrationPrincipalsView.vue";
const principal = {
  id: "ip_one",
  name: "机器甲",
  version: "42",
  status: "active",
};
beforeEach(() => {
  vi.clearAllMocks();
  mock.codes = ["identity.integration.read", "identity.integration.manage"];
  mock.list.mockResolvedValue({
    data: { principals: [principal], credentials: [], next_cursor: "" },
  });
  mock.credentials.mockResolvedValue({
    data: { principals: [principal], credentials: [], next_cursor: "" },
  });
  mock.issue.mockResolvedValue({
    data: { credential: { id: "key" }, token: "one-time-test-secret" },
  });
});
async function selected() {
  const w = mount(View);
  await flushPromises();
  await w.get('[aria-label="集成列表"] button').trigger("click");
  await flushPromises();
  await w.get('[aria-label="帖子 ID"]').setValue("123,124");
  return w;
}
it("issues exact IDs, shows secret once, clears on close and never persists it", async () => {
  const w = await selected();
  await w.get('[aria-label="集成详情"] form').trigger("submit");
  await flushPromises();
  expect(mock.issue).toHaveBeenCalledOnce();
  const request = mock.issue.mock.calls[0][1];
  expect(request.principal_version).toBe("42");
  expect(request.rules[0].resource_ids).toEqual(["123", "124"]);
  expect(request.rules[0].endpoint).toBe("public_thread.read");
  expect(w.get('[data-testid="one-time-token"]').text()).toBe(
    "one-time-test-secret",
  );
  const close = w.findAll("button").find((b) => b.text() === "我已保存，关闭")!;
  await close.trigger("click");
  expect(w.find('[data-testid="one-time-token"]').exists()).toBe(false);
  expect(
    JSON.stringify(localStorage) + JSON.stringify(sessionStorage),
  ).not.toContain("one-time-test-secret");
  w.unmount();
});
it("rejects wildcard/duplicate IDs before issuing and displays service failure", async () => {
  const w = await selected();
  await w.get('[aria-label="帖子 ID"]').setValue("*");
  await w.get('[aria-label="集成详情"] form').trigger("submit");
  expect(mock.issue).not.toHaveBeenCalled();
  await w.get('[aria-label="帖子 ID"]').setValue("123,123");
  await w.get('[aria-label="集成详情"] form').trigger("submit");
  expect(mock.issue).not.toHaveBeenCalled();
  await w.get('[aria-label="帖子 ID"]').setValue("123");
  mock.issue.mockRejectedValue(new Error("近期多因素认证已过期"));
  await w.get('[aria-label="集成详情"] form').trigger("submit");
  await flushPromises();
  expect(w.get('[role="alert"]').text()).toContain("近期多因素认证");
  expect(w.find('[data-testid="one-time-token"]').exists()).toBe(false);
  w.unmount();
});
it("hides scope and lifecycle actions without manage permission", async () => {
  mock.codes = ["identity.integration.read"];
  const w = mount(View);
  await flushPromises();
  await w.get('[aria-label="集成列表"] button').trigger("click");
  await flushPromises();
  expect(w.text()).not.toContain("签发新凭据");
  expect(w.text()).not.toContain("撤销集成");
  expect(mock.list).toHaveBeenCalledOnce();
  w.unmount();
});
it("clears a one-time secret when the document is hidden", async () => {
  const w = await selected();
  await w.get('[aria-label="集成详情"] form').trigger("submit");
  await flushPromises();
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    value: "hidden",
  });
  document.dispatchEvent(new Event("visibilitychange"));
  await flushPromises();
  expect(w.find('[data-testid="one-time-token"]').exists()).toBe(false);
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    value: "visible",
  });
  w.unmount();
});
