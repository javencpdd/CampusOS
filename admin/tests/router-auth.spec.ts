// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import router, { adminAuthGuard } from "../src/router";
import { clearAdminSession, getAdminAccessToken, setAdminAccessToken, setAdminRoleNames } from "../src/modules/identity/session";

describe("Admin navigation and permission gates", () => {
  beforeEach(() => {
    localStorage.clear();
    clearAdminSession();
  });

  it("redirects unauthenticated and non-admin users", async () => {
    const unauthenticated = await adminAuthGuard(
      { meta: { requiresAuth: true, adminOnly: true }, fullPath: "/features", path: "/features" } as never,
      {} as never,
    );
    expect(unauthenticated).toEqual({ path: "/login", query: { redirect: "/features" } });

    setAdminAccessToken("test-token");
    setAdminRoleNames(["user"]);
    const nonAdmin = await adminAuthGuard(
      { meta: { requiresAuth: true, adminOnly: true }, fullPath: "/features", path: "/features" } as never,
      {} as never,
    );
    expect(getAdminAccessToken()).toBeNull();
    expect(nonAdmin).toEqual({ path: "/login", query: { redirect: "/features" } });
  });

  it("keeps built-in feature management behind the inherited admin gate", () => {
    const resolved = router.resolve("/features");
    expect(resolved.name).toBe("Features");
    expect(resolved.meta.requiresAuth).toBe(true);
    expect(resolved.meta.adminOnly).toBe(true);

    const appearance = router.resolve("/appearance");
    expect(appearance.name).toBe("Appearance");
    expect(appearance.meta.requiresAuth).toBe(true);
    expect(appearance.meta.adminOnly).toBe(true);
  });
});
