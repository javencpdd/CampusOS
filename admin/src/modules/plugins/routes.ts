import type { RouteRecordRaw } from "vue-router";

export const pluginRoutes: RouteRecordRaw[] = [
  {
    path: "plugin-config",
    name: "PluginV5Config",
    component: () => import("./pages/PluginV5ConfigView.vue"),
    meta: { title: "插件系统配置" },
  },
  {
    path: "extensions",
    name: "Extensions",
    component: () => import("./pages/ExtensionHubView.vue"),
    meta: { title: "扩展与集成" },
  },
  {
    path: "plugins",
    name: "Plugins",
    component: () => import("./pages/PluginV5ManageView.vue"),
    meta: { title: "插件管理" },
  },
  {
    path: "plugin-center",
    name: "PluginCenter",
    redirect: "/plugins",
    meta: { title: "插件中心" },
  },
];
