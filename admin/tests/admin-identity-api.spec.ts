import { beforeEach, describe, expect, it, vi } from "vitest";
const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("../src/shared/api/client", () => ({ default: { get, post } }));
import { authApi } from "../src/modules/identity/api";
describe("independent Admin SPA endpoint contract", () => {
  beforeEach(() => vi.clearAllMocks());
  it("uses Admin session endpoints for login, current identity, refresh and logout", async () => {
    await authApi.login({ username: "operator", password: "password" });
    await authApi.me();
    await authApi.refresh();
    await authApi.logout();
    expect(get).toHaveBeenCalledWith("/admin/identity/me");
    expect(post.mock.calls).toEqual([
      ["/admin/identity/login", { username: "operator", password: "password" }],
      ["/admin/identity/refresh"],
      ["/admin/identity/logout"],
    ]);
  });
  it("enrolls, confirms and steps up only the own Admin factor", async () => {
    await authApi.enrollMFA();
    await authApi.confirmMFA({ code: "123456" });
    await authApi.stepUpMFA({ code: "234567" });
    expect(post.mock.calls).toEqual([
      ["/admin/identity/mfa/enroll", { issuer: "CampusOS" }],
      ["/admin/identity/mfa/confirm", { code: "123456" }],
      ["/admin/identity/mfa/step-up", { code: "234567" }],
    ]);
  });
});
