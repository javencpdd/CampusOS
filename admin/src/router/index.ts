import { createRouter, createWebHistory } from "vue-router";
import { appearanceRoutes } from "../modules/appearance/routes";
import { academicTermRoutes } from "../modules/academicterm/routes";
import { communityRoutes } from "../modules/community/routes";
import { featureRoutes } from "../modules/features/routes";
import { identityRoutes } from "../modules/identity/routes";
import { integrationRoutes } from "../modules/integrations/routes";
import { operationRoutes } from "../modules/operations/routes";
import { pluginRoutes } from "../modules/plugins/routes";
import { adminAuthGuard } from "../modules/identity/navigation";
import { knowledgeRoutes } from "../modules/knowledge/routes";
import { legacyAdminRedirects } from "./legacyRedirects";
const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/login",
      name: "Login",
      component: () => import("@/modules/identity/pages/LoginView.vue"),
    },
    ...legacyAdminRedirects.map((path) => ({
      path,
      redirect: path.replace("/admin", "") || "/",
    })),
    {
      path: "/",
      component: () => import("@/components/AdminLayout.vue"),
      meta: { requiresAuth: true, adminOnly: true },
      children: [
        {
          path: "",
          name: "Dashboard",
          component: () =>
            import("@/modules/dashboard/pages/DashboardView.vue"),
          meta: { title: "仪表盘" },
        },
        ...identityRoutes,
        ...academicTermRoutes,
        ...communityRoutes,
        ...featureRoutes,
        ...knowledgeRoutes,
        ...appearanceRoutes,
        ...pluginRoutes,
        ...integrationRoutes,
        ...operationRoutes,
      ],
    },
  ],
});

router.beforeEach(adminAuthGuard);

export { adminAuthGuard };
export default router;
