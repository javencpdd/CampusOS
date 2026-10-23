import type { RouteRecordRaw } from "vue-router";
export const knowledgeRoutes: RouteRecordRaw[] = [
  {
    path: "knowledge",
    name: "KnowledgeManage",
    component: () => import("./pages/KnowledgeManageView.vue"),
    meta: { title: "知识来源管理" },
  },
];
