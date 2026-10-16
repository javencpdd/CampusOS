import type { NavigationGuard } from "vue-router";
import {
  clearAdminSession,
  ensureAdminAccessToken,
  getAdminAccessToken,
  getCurrentAdminProfile,
} from "./session";

export const adminAuthGuard: NavigationGuard = async (to) => {
  if (to.meta.requiresAuth || to.meta.adminOnly) {
    if (
      !(await ensureAdminAccessToken()) ||
      getCurrentAdminProfile()?.domain !== "admin"
    ) {
      clearAdminSession();
      return { path: "/login", query: { redirect: to.fullPath } };
    }
  }
  if (
    to.path === "/login" &&
    getAdminAccessToken() &&
    (await ensureAdminAccessToken())
  )
    return { path: "/" };
  return true;
};
