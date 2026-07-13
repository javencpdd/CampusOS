// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import router, { adminAuthGuard } from "../src/router";

describe("Admin navigation and permission gates", () => {
  beforeEach(() => localStorage.clear());

  it("redirects unauthenticated and non-admin users", () => {
    const next = vi.fn();
    adminAuthGuard(
      { meta: { requiresAuth: true, adminOnly: true }, fullPath: "/features", path: "/features" } as never,
      {} as never,
      next,
    );
    expect(next).toHaveBeenCalledWith({ path: "/login", query: { redirect: "/features" } });

    localStorage.setItem("admin_token", "test-token");
    localStorage.setItem("admin_user", JSON.stringify({ roles: [{ name: "user" }] }));
    next.mockClear();
    adminAuthGuard(
      { meta: { requiresAuth: true, adminOnly: true }, fullPath: "/features", path: "/features" } as never,
      {} as never,
      next,
    );
    expect(localStorage.getItem("admin_token")).toBeNull();
    expect(next).toHaveBeenCalledWith({ path: "/login", query: { redirect: "/features" } });
  });

  it("keeps built-in feature management behind the inherited admin gate", () => {
    const resolved = router.resolve("/features");
    expect(resolved.name).toBe("Features");
    expect(resolved.meta.requiresAuth).toBe(true);
    expect(resolved.meta.adminOnly).toBe(true);
  });
});
