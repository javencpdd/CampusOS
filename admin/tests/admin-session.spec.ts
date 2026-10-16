// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
const { login, refresh, me, logout } = vi.hoisted(() => ({
  login: vi.fn(),
  refresh: vi.fn(),
  me: vi.fn(),
  logout: vi.fn(),
}));
vi.mock("../src/modules/identity/api", () => ({
  authApi: { login, refresh, me, logout },
}));
import { useAdminStore } from "../src/modules/identity/store";
import {
  adminCSRFToken,
  clearAdminSession,
  getAdminAccessToken,
  getCurrentAdminProfile,
} from "../src/modules/identity/session";
const self = (id = "adm.opaque") => ({
  domain: "admin",
  account: {
    id,
    username: "operator",
    display_name: "Operator",
    status: "active",
  },
  roles: [{ id: "role.custom", name: "盘点人员" }],
  permission_codes: [],
  authentication_strength: "password",
  mfa_enrolled: false,
  session_expires_at: new Date(Date.now() + 3600000).toISOString(),
});
const ok = (data: any) => ({ code: 0, data });
const nonce = () => {
  document.cookie = "campusos_admin_csrf=admin-nonce; Path=/";
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}
describe("independent Admin current session", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    clearAdminSession();
    localStorage.clear();
    sessionStorage.clear();
    login.mockResolvedValue(ok({ access_token: "admin-jwt" }));
    refresh.mockResolvedValue(ok({ access_token: "rotated-jwt" }));
    me.mockResolvedValue(ok(self()));
    logout.mockResolvedValue(ok({ revoked: true }));
  });
  it("requires current independent metadata after username/email login and accepts opaque custom roles", async () => {
    const s = useAdminStore();
    await s.login("operator@example.test", "password");
    expect(login).toHaveBeenCalledWith({
      username: "operator@example.test",
      password: "password",
    });
    expect(me).toHaveBeenCalledTimes(1);
    expect(s.isLoggedIn).toBe(true);
    expect(s.user?.id).toBe("adm.opaque");
    expect(s.primaryRole).toBe("盘点人员");
    expect(s.profile?.permission_codes).toEqual([]);
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
  });
  it("discards a user-domain payload or denied metadata instead of trusting a JWT", async () => {
    const s = useAdminStore();
    me.mockResolvedValueOnce(ok({ ...self(), domain: "user" }));
    await expect(s.login("operator", "password")).rejects.toThrow();
    expect(s.isLoggedIn).toBe(false);
    expect(getAdminAccessToken()).toBeNull();
    me.mockRejectedValueOnce({ status: 503 });
    await expect(s.login("operator", "password")).rejects.toBeDefined();
    expect(getCurrentAdminProfile()).toBeNull();
  });
  it("restores once for concurrent guards and checks current permission changes on the next navigation", async () => {
    const s = useAdminStore();
    nonce();
    const pending = deferred<any>();
    refresh.mockReturnValueOnce(pending.promise);
    const a = s.restore(),
      b = s.restore();
    pending.resolve(ok({ access_token: "restored-jwt" }));
    expect(await a).toBe(true);
    expect(await b).toBe(true);
    expect(refresh).toHaveBeenCalledTimes(1);
    me.mockResolvedValueOnce(
      ok({ ...self(), permission_codes: ["community.thread.read"] }),
    );
    expect(await s.restore()).toBe(true);
    expect(s.profile?.permission_codes).toEqual(["community.thread.read"]);
    me.mockRejectedValueOnce({ status: 403 });
    expect(await s.restore()).toBe(false);
    expect(s.profile).toBeNull();
    expect(adminCSRFToken()).toBe("");
    expect(refresh).toHaveBeenCalledTimes(1);
  });
  it("refreshes an expired access token once and does not refresh other failures", async () => {
    const s = useAdminStore();
    await s.login("operator", "password");
    nonce();
    me.mockRejectedValueOnce({ status: 401 }).mockResolvedValueOnce(ok(self()));
    expect(await s.restore()).toBe(true);
    expect(refresh).toHaveBeenCalledTimes(1);
    nonce();
    me.mockRejectedValueOnce({ status: 409 });
    expect(await s.restore()).toBe(false);
    expect(refresh).toHaveBeenCalledTimes(1);
  });
  it("does not repeat refresh when a freshly restored token is rejected", async () => {
    const s = useAdminStore();
    nonce();
    me.mockRejectedValueOnce({ status: 401 });
    expect(await s.restore()).toBe(false);
    expect(refresh).toHaveBeenCalledTimes(1);
  });
  it("ignores a late current profile after local invalidation", async () => {
    const s = useAdminStore();
    await s.login("operator", "password");
    const pending = deferred<any>();
    me.mockReturnValueOnce(pending.promise);
    const check = s.restore();
    s.clearSession();
    pending.resolve(ok(self("late-account")));
    expect(await check).toBe(false);
    expect(s.user).toBeNull();
    expect(getCurrentAdminProfile()).toBeNull();
  });
  it("finishes an in-flight restore before revoking its new session", async () => {
    const s = useAdminStore();
    nonce();
    const pending = deferred<any>();
    refresh.mockReturnValueOnce(pending.promise);
    const restoring = s.restore();
    const exiting = s.logout();
    expect(logout).not.toHaveBeenCalled();
    pending.resolve(ok({ access_token: "latest-jwt" }));
    await restoring;
    await exiting;
    expect(logout).toHaveBeenCalledTimes(1);
    expect(s.isLoggedIn).toBe(false);
    expect(getAdminAccessToken()).toBeNull();
    expect(adminCSRFToken()).toBe("");
  });
  it("reports server logout failure while clearing local credentials and only Admin CSRF", async () => {
    const s = useAdminStore();
    await s.login("operator", "password");
    nonce();
    document.cookie = "campusos_csrf=user-nonce; Path=/";
    logout.mockRejectedValueOnce({ status: 503 });
    await expect(s.logout()).rejects.toBeDefined();
    expect(s.user).toBeNull();
    expect(adminCSRFToken()).toBe("");
    expect(document.cookie).toContain("campusos_csrf=user-nonce");
    expect(await s.restore()).toBe(false);
    expect(refresh).not.toHaveBeenCalled();
  });
  it("waits for an older refresh before creating a new login cookie and rejects concurrent logins", async () => {
    const s = useAdminStore();
    nonce();
    const pending = deferred<any>();
    refresh.mockReturnValueOnce(pending.promise);
    const restoring = s.restore();
    const signing = s.login("new-operator", "password");
    await expect(s.login("duplicate", "password")).rejects.toThrow("正在登录");
    expect(login).not.toHaveBeenCalled();
    pending.resolve(ok({ access_token: "old-jwt" }));
    await restoring;
    await signing;
    expect(getAdminAccessToken()).toBe("admin-jwt");
    expect(s.isLoggedIn).toBe(true);
  });
  it("allows a new login to wait for a pending logout without a dependency cycle", async () => {
    const s = useAdminStore();
    await s.login("operator", "password");
    const pending = deferred<any>();
    logout.mockReturnValueOnce(pending.promise);
    const exiting = s.logout();
    const signing = s.login("next-operator", "password");
    expect(login).toHaveBeenCalledTimes(1);
    pending.resolve(ok({ revoked: true }));
    await exiting;
    await signing;
    expect(s.isLoggedIn).toBe(true);
    expect(login).toHaveBeenCalledTimes(2);
  });
});
