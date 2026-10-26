// @vitest-environment jsdom
import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ plugins: vi.fn() }));
vi.mock("../src/modules/plugins/v5-api", () => ({
  pluginV5Api: { list: api.plugins },
}));
vi.mock("../src/modules/community/api", () => ({
  threadApi: {
    list: async () => ({ data: { items: [], pagination: { total: 10000 } } }),
  },
  categoryApi: { list: async () => ({ data: [] }) },
}));
vi.mock("../src/modules/identity/api", () => ({
  userApi: { list: async () => ({ data: {} }) },
}));
vi.mock("../src/modules/operations/api", () => ({
  eventApi: { list: async () => ({ data: {} }) },
}));
import Dashboard from "../src/modules/dashboard/pages/DashboardView.vue";

const wrappers: ReturnType<typeof mount>[] = [];
const slots = { template: '<div><slot /><slot name="header" /></div>' };
function render() {
  const wrapper = mount(Dashboard, {
    global: {
      stubs: Object.fromEntries(
        [
          "ElRow",
          "ElCol",
          "ElCard",
          "ElIcon",
          "ElSpace",
          "ElButton",
          "ElTable",
          "ElTableColumn",
          "ElTag",
        ].map((name) => [name, name === "ElTable" ? true : slots]),
      ),
      directives: { loading: () => {} },
    },
  });
  wrappers.push(wrapper);
  return wrapper;
}
beforeEach(() => vi.resetAllMocks());
afterEach(() => wrappers.splice(0).forEach((wrapper) => wrapper.unmount()));

it("shows the complete current v5 release count across catalog pages", async () => {
  api.plugins.mockResolvedValueOnce({
    data: { items: [{}, {}], next_cursor: "page2" },
  });
  api.plugins.mockResolvedValueOnce({ data: { items: [{}], next_cursor: "" } });
  const wrapper = render();
  await flushPromises();
  expect(wrapper.find(".stat-plugins .stat-title").text()).toBe("插件版本");
  expect(wrapper.find(".stat-plugins .stat-value").text()).toBe("3");
  expect(api.plugins.mock.calls).toEqual([[""], ["page2"]]);
  expect(wrapper.find(".stat-threads .stat-value").text()).toBe("10000");
});

it("keeps an unavailable count distinct from an empty catalog", async () => {
  api.plugins.mockRejectedValue(new Error("catalog unavailable"));
  const wrapper = render();
  await flushPromises();
  expect(wrapper.find(".stat-plugins .stat-value").text()).toBe("—");
  expect(wrapper.find(".stat-threads .stat-value").text()).toBe("10000");
});
