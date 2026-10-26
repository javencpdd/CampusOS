// @vitest-environment jsdom
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { ref } from "vue";
import { beforeEach, afterEach, describe, it, expect, vi } from "vitest";
import View from "../src/modules/knowledge/pages/KnowledgeManageView.vue";
import {
  knowledgeAdminApi as api,
  type CollectionState,
  type Diagnostics,
  type Official,
} from "../src/modules/knowledge/api";
const profile = ref({
  permission_codes: [
    "knowledge.collection.read",
    "knowledge.collection.manage",
  ],
  authentication_strength: "mfa",
});
vi.mock("../src/modules/identity/store", () => ({
  useAdminStore: () => ({
    get profile() {
      return profile.value;
    },
  }),
}));
vi.mock("../src/modules/knowledge/api", () => ({
  knowledgeAdminApi: {
    collections: vi.fn(),
    officials: vi.fn(),
    state: vi.fn(),
    diagnostics: vi.fn(),
    saveCollection: vi.fn(),
    saveOfficial: vi.fn(),
    history: vi.fn(),
    select: vi.fn(),
    refresh: vi.fn(),
    remove: vi.fn(),
    retry: vi.fn(),
  },
}));
const state: CollectionState = {
  collection: {
    id: "kc_one",
    version: "9007199254740993",
    title: "校园公开",
    status: "active",
  },
  selections: [
    {
      id: "ks_one",
      version: "8",
      kind: "board",
      source_id: "123",
      active: true,
    },
  ],
  members: [
    {
      id: "km_one",
      kind: "community.thread",
      source_id: "124",
      selection_id: "ks_one",
    },
  ],
};
const diagnostic: Diagnostics = {
  collection_id: "kc_one",
  collection_version: state.collection.version,
  job_version: "9007199254740994",
  state: "dead",
  phase: "documents",
  attempts: 8,
  last_error: "page_failed",
  ready_generation: "21",
  run_generation: "0",
  available_at_ms: "0",
  selections: 1,
  active_selections: 1,
  members: 1,
  projected_documents: 1,
  observed_at_ms: "1",
  retryable: true,
};
const official: Official = {
  id: "ko_one",
  version: "3",
  title: "校历",
  content: '<img src="/private">TEXT',
  format: "text",
  status: "published",
};
let w: VueWrapper;
const button = (label: string) =>
  w.findAll("button").find((b) => b.text() === label)!;
async function render() {
  w = mount(View);
  await flushPromises();
  return w;
}
async function choose() {
  await render();
  await w.get('[aria-label="管理集合"]').setValue("kc_one");
  await flushPromises();
}
beforeEach(() => {
  vi.resetAllMocks();
  profile.value = {
    permission_codes: [
      "knowledge.collection.read",
      "knowledge.collection.manage",
    ],
    authentication_strength: "mfa",
  };
  vi.mocked(api.collections).mockResolvedValue([state.collection]);
  vi.mocked(api.officials).mockResolvedValue([official]);
  vi.mocked(api.state).mockResolvedValue(state);
  vi.mocked(api.diagnostics).mockResolvedValue(diagnostic);
  vi.mocked(api.select).mockResolvedValue(state);
  vi.mocked(api.retry).mockResolvedValue({ accepted: true });
  vi.mocked(api.history).mockResolvedValue({
    items: [{ official_version: "3", snapshot: official, changed_at_ms: 1 }],
    next_before: "0",
  });
});
afterEach(() => w?.unmount());
describe("Knowledge independent Admin management", () => {
  it("does not request metadata without the exact read permission", async () => {
    profile.value.permission_codes = [];
    await render();
    expect(w.text()).toContain("没有知识集合读取权限");
    expect(api.collections).not.toHaveBeenCalled();
  });
  it.each(["read-only", "password"])(
    "keeps %s sessions from writing but permits current diagnostics",
    async (mode) => {
      if (mode === "read-only")
        profile.value.permission_codes = ["knowledge.collection.read"];
      else profile.value.authentication_strength = "password";
      await choose();
      expect(w.text()).toContain(
        mode === "read-only" ? "只读模式" : "当前 MFA 认证",
      );
      expect(button("重试索引").attributes("disabled")).toBeDefined();
      await w.get('[aria-label="集合表单"]').trigger("submit");
      expect(api.saveCollection).not.toHaveBeenCalled();
      expect(w.text()).toContain("等待复核");
    },
  );
  it("creates and selects a new collection without losing its current state", async () => {
    vi.mocked(api.collections).mockResolvedValueOnce([]);
    vi.mocked(api.saveCollection).mockResolvedValue(state.collection);
    await render();
    expect(w.text()).toContain("尚未配置公开知识集合");
    await w.get('[aria-label="集合名称"]').setValue("校园公开");
    await w.get('[aria-label="集合表单"]').trigger("submit");
    await flushPromises();
    expect(api.saveCollection).toHaveBeenCalledWith(
      { title: "校园公开", status: "active" },
      undefined,
    );
    expect(w.find('[aria-label="来源选择"]').exists()).toBe(true);
    expect(w.get("[aria-label=管理集合]").element.value).toBe(
      state.collection.id,
    );
  });
  it("saves a precise source rule, cancels it, and refreshes bounded pages explicitly", async () => {
    await choose();
    await w.get('[aria-label="来源类型"]').setValue("official");
    await w.get('[aria-label="官方来源"]').setValue("ko_one");
    await w.get('[aria-label="来源选择"]').trigger("submit");
    await flushPromises();
    expect(api.select).toHaveBeenCalledWith(state, {
      kind: "official",
      source_id: "ko_one",
      active: true,
    });
    await button("取消选择").trigger("click");
    await flushPromises();
    expect(api.select).toHaveBeenLastCalledWith(
      state,
      { kind: "board", source_id: "123", active: false },
      state.selections[0],
    );
    vi.mocked(api.refresh).mockResolvedValue({
      state,
      examined: 50,
      has_more: true,
      next_page: 2,
    });
    await button("刷新来源").trigger("click");
    await flushPromises();
    expect(w.text()).toContain("本页检查 50 条");
    await button("继续刷新来源").trigger("click");
    await flushPromises();
    expect(api.refresh).toHaveBeenLastCalledWith(state, state.selections[0], 2);
  });
  it("retries the exact job snapshot once and displays acceptance separately from actual readiness", async () => {
    await choose();
    await button("重试索引").trigger("click");
    await flushPromises();
    expect(api.retry).toHaveBeenCalledTimes(1);
    expect(api.retry).toHaveBeenCalledWith(diagnostic);
    expect(w.text()).toContain("已受理");
    expect(w.text()).toContain("不等于当前可读资料数");
  });
  it("clears details on stale CAS, never blindly replays a command, and reloads current facts", async () => {
    await choose();
    vi.mocked(api.retry).mockRejectedValueOnce({
      status: 409,
      message: "INTERNAL_SECRET",
    });
    await button("重试索引").trigger("click");
    await flushPromises();
    expect(w.text()).toContain("数据已变更");
    expect(w.text()).not.toContain("INTERNAL_SECRET");
    expect(w.find('[aria-label="索引诊断"]').exists()).toBe(false);
    expect(api.retry).toHaveBeenCalledTimes(1);
    await button("刷新工作区").trigger("click");
    await flushPromises();
    expect(w.find('[aria-label="来源选择"]').exists()).toBe(true);
  });
  it("loads immutable official snapshots as text and preserves the current string version on edit", async () => {
    vi.mocked(api.saveOfficial).mockResolvedValue(official);
    await choose();
    await button("查看或编辑资料").trigger("click");
    await flushPromises();
    expect(w.get("textarea").element.value).toBe(official.content);
    expect(w.find("img").exists()).toBe(false);
    await w.get('[aria-label="资料状态"]').setValue("withdrawn");
    await w.get('[aria-label="官方资料表单"]').trigger("submit");
    await flushPromises();
    expect(api.saveOfficial).toHaveBeenCalledWith(
      {
        title: official.title,
        content: official.content,
        format: "text",
        status: "withdrawn",
      },
      official,
    );
    expect(w.text()).toContain("不可变修订");
  });
  it("exposes an actionable diagnostics failure and retries without fabricating metrics", async () => {
    vi.mocked(api.diagnostics).mockRejectedValueOnce(Error("INTERNAL"));
    await choose();
    expect(w.text()).toContain("索引诊断暂不可用");
    expect(w.text()).not.toContain("等待复核");
    await button("刷新诊断").trigger("click");
    await flushPromises();
    expect(w.text()).toContain("等待复核");
  });
});
