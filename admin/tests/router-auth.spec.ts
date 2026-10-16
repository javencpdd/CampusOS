// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import router, { adminAuthGuard } from "../src/router";
import {
  clearAdminSession,
  getAdminAccessToken,
  registerAdminSessionRestorer,
  setAdminAccessToken,
  setCurrentAdminProfile,
} from "../src/modules/identity/session";
const target = {
  meta: { requiresAuth: true, adminOnly: true },
  fullPath: "/features",
  path: "/features",
} as never;
const self = {
  domain: "admin",
  account: {
    id: "adm.opaque",
    username: "ops",
    display_name: "Ops",
    status: "active",
  },
  roles: [{ id: "custom", name: "只读盘点" }],
  permission_codes: [],
  authentication_strength: "password",
  mfa_enrolled: false,
  session_expires_at: new Date(Date.now() + 3600000).toISOString(),
} as const;
describe("Admin navigation current domain gates", () => {
  beforeEach(() => {
    clearAdminSession();
    registerAdminSessionRestorer(async () => false);
  });
  it("rejects unauthenticated navigation and a cached token without current Admin proof", async () => {
    expect(await adminAuthGuard(target, {} as never)).toEqual({
      path: "/login",
      query: { redirect: "/features" },
    });
    setAdminAccessToken("cached-token");
    expect(await adminAuthGuard(target, {} as never)).toEqual({
      path: "/login",
      query: { redirect: "/features" },
    });
    expect(getAdminAccessToken()).toBeNull();
  });
  it("accepts current independent custom roles and rechecks on every navigation", async () => {
    setAdminAccessToken("admin-token");
    setCurrentAdminProfile(self as never);
    const check = vi.fn().mockResolvedValue(true);
    registerAdminSessionRestorer(check);
    expect(await adminAuthGuard(target, {} as never)).toBe(true);
    expect(await adminAuthGuard(target, {} as never)).toBe(true);
    expect(check).toHaveBeenCalledTimes(2);
    check.mockResolvedValueOnce(false);
    expect(await adminAuthGuard(target, {} as never)).toEqual({
      path: "/login",
      query: { redirect: "/features" },
    });
  });
  it("keeps feature and appearance routes behind the inherited current Admin gate", () => {
    for (const path of ["/features", "/appearance"]) {
      const r = router.resolve(path);
      expect(r.meta.requiresAuth).toBe(true);
      expect(r.meta.adminOnly).toBe(true);
    }
  });
});
