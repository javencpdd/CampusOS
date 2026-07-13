// @vitest-environment jsdom
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PluginCenterView from "../src/modules/plugins/pages/PluginCenterView.vue";

const { api } = vi.hoisted(() => ({
  api: {
    marketOverview: vi.fn(),
    marketRequests: vi.fn(),
    marketAudits: vi.fn(),
    setMarketVisibility: vi.fn(),
    reviewMarketRequest: vi.fn(),
    marketReleases: vi.fn(),
    saveMarketRelease: vi.fn(),
  },
}));

vi.mock("../src/modules/plugins/api", () => ({ pluginApi: api }));
vi.mock("element-plus", () => ({
  ElMessage: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
}));

const stubs = {
  "el-icon": true,
  "el-alert": true,
  "el-tag": { template: "<span><slot /></span>" },
  "el-empty": true,
  "el-table": { template: "<div><slot /></div>", props: ["data"] },
  "el-table-column": true,
  "el-button": {
    template: "<button @click=\"$emit('click')\"><slot /></button>",
  },
  "el-dialog": {
    template:
      '<section v-if="modelValue"><slot /><slot name="footer" /></section>',
    props: ["modelValue"],
  },
  "el-form": { template: "<form><slot /></form>" },
  "el-form-item": { template: "<label><slot /></label>" },
  "el-input": true,
  "el-select": true,
  "el-option": true,
};

describe("PluginCenterView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.marketOverview.mockResolvedValue({
      data: {
        items: [
          {
            catalog: {
              plugin_name: "notes-v2",
              display_name: "课堂笔记",
              description: "",
              version: "1.0.0",
              runtime: "wasm",
              visibility: "published",
            },
            metrics: {
              user_count: 3,
              record_count: 8,
              file_count: 2,
              file_bytes: 2048,
            },
            runtime_state: { status: "running", health: "healthy" },
            system_permissions: [
              { resource: "managed_data", actions: ["read"] },
            ],
          },
        ],
      },
    });
    api.marketRequests.mockResolvedValue({ data: { items: [] } });
    api.marketAudits.mockResolvedValue({
      data: {
        items: [
          {
            id: 1,
            plugin_name: "notes-v2",
            actor_id: "admin",
            action: "catalog.visibility",
            outcome: "success",
            created_at: "2026-07-13T00:00:00Z",
          },
        ],
      },
    });
  });

  it("shows host-owned usage metrics instead of exposing user records", async () => {
    const wrapper = mount(PluginCenterView, {
      global: { stubs, directives: { loading: {} } },
    });
    await flushPromises();

    expect(api.marketOverview).toHaveBeenCalledOnce();
    expect(wrapper.text()).toContain("目录插件");
    expect(wrapper.text()).toContain("用户授权");
    expect(wrapper.text()).toContain("受管记录");
    expect(wrapper.text()).toContain("受管文件");
    expect(wrapper.text()).toContain("3");
    expect(wrapper.text()).toContain("8");
    expect(wrapper.text()).toContain("running / healthy");
    expect(wrapper.text()).toContain("近期治理审计");
  });
});
