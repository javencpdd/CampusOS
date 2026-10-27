import { computed, onScopeDispose, ref } from "vue";
import { defineStore } from "pinia";
import { authApi, type AdminSelfProfile } from "./api";
import {
  adminCSRFToken,
  clearAdminSession,
  getAdminAccessToken,
  registerAdminSessionRestorer,
  setAdminAccessToken,
  setCurrentAdminProfile,
} from "./session";

function currentProfile(value: any): value is AdminSelfProfile {
  return (
    value?.domain === "admin" &&
    typeof value.account?.id === "string" &&
    !!value.account.id &&
    value.account.status === "active" &&
    typeof value.account.username === "string" &&
    ["password", "mfa"].includes(value.authentication_strength) &&
    typeof value.mfa_enrolled === "boolean" &&
    (value.mfa_required === undefined ||
      typeof value.mfa_required === "boolean") &&
    Number.isFinite(Date.parse(value.session_expires_at)) &&
    Array.isArray(value.roles) &&
    value.roles.every(
      (r: any) =>
        typeof r.id === "string" &&
        r.id &&
        typeof r.name === "string" &&
        r.name,
    ) &&
    Array.isArray(value.permission_codes) &&
    value.permission_codes.every((c: any) => typeof c === "string" && c)
  );
}
export const useAdminStore = defineStore("admin", () => {
  const profile = ref<AdminSelfProfile | null>(null);
  const token = ref<string | null>(getAdminAccessToken());
  const user = computed(() => profile.value?.account || null);
  const isLoggedIn = computed(
    () => !!token.value && profile.value?.domain === "admin",
  );
  const isAdmin = computed(() => isLoggedIn.value);
  const primaryRole = computed(() => profile.value?.roles[0]?.name || "管理员");
  let generation = 0;
  let restorePromise: Promise<boolean> | null = null;
  let loginPromise: Promise<any> | null = null;
  let logoutPromise: Promise<void> | null = null;
  let loggingOut = false;

  function clearSession() {
    generation++;
    profile.value = null;
    token.value = null;
    clearAdminSession();
  }
  function setToken(value: string) {
    token.value = value;
    setAdminAccessToken(value);
  }
  async function loadCurrent(epoch: number) {
    const result: any = await authApi.me();
    if (epoch !== generation) return false;
    if (result?.code !== 0 || !currentProfile(result.data))
      throw new Error("当前管理员身份不可用，请重新登录");
    profile.value = result.data;
    setCurrentAdminProfile(result.data);
    return true;
  }
  async function refreshToken(epoch: number) {
    if (!adminCSRFToken()) throw new Error("管理员会话已过期，请重新登录");
    const result: any = await authApi.refresh();
    if (epoch !== generation) return false;
    if (result?.code !== 0 || !result.data?.access_token)
      throw new Error("管理员会话无法恢复");
    setToken(result.data.access_token);
    return true;
  }
  async function performLogin(username: string, password: string) {
    if (logoutPromise) await logoutPromise;
    const previousRestore = restorePromise;
    clearSession();
    const epoch = generation;
    // An older response may set HttpOnly cookies even when its JS result is
    // discarded. Finish that request before issuing a new login credential.
    if (previousRestore) await previousRestore;
    try {
      const result: any = await authApi.login({ username, password });
      if (epoch !== generation)
        return { code: 20004, msg: "登录状态已变化，请重试" };
      if (result?.code !== 0 || !result.data?.access_token)
        throw new Error(result?.msg || "管理员登录失败");
      setToken(result.data.access_token);
      if (!(await loadCurrent(epoch)))
        return { code: 20004, msg: "登录状态已变化，请重试" };
      return result;
    } catch (error) {
      if (epoch === generation) clearSession();
      throw error;
    }
  }
  async function login(username: string, password: string) {
    if (loginPromise) throw new Error("正在登录，请稍候");
    const pending = performLogin(username, password);
    loginPromise = pending;
    try {
      return await pending;
    } finally {
      if (loginPromise === pending) loginPromise = null;
    }
  }
  async function restore() {
    if (loggingOut) return false;
    if (restorePromise) return restorePromise;
    const epoch = generation;
    const pending = (async () => {
      try {
        let refreshed = false;
        if (!token.value) {
          if (!(await refreshToken(epoch))) return false;
          refreshed = true;
        }
        try {
          return await loadCurrent(epoch);
        } catch (error: any) {
          if (refreshed || error?.status !== 401 || epoch !== generation)
            throw error;
          if (!(await refreshToken(epoch))) return false;
          return await loadCurrent(epoch);
        }
      } catch {
        if (epoch === generation) clearSession();
        return false;
      }
    })();
    restorePromise = pending;
    try {
      return await pending;
    } finally {
      if (restorePromise === pending) restorePromise = null;
    }
  }
  async function logout() {
    if (logoutPromise) return logoutPromise;
    loggingOut = true;
    const previousLogin = loginPromise;
    const previousRestore = restorePromise;
    const pending = (async () => {
      try {
        // A refresh in flight must finish before its new session is revoked.
        if (previousLogin) await previousLogin.catch(() => undefined);
        if (previousRestore) await previousRestore;
        generation++;
        if (token.value) {
          const result: any = await authApi.logout();
          if (result?.code !== 0 || result.data?.revoked !== true)
            throw new Error("服务器退出未完成，请重新登录后重试");
        }
      } finally {
        clearSession();
        loggingOut = false;
      }
    })();
    logoutPromise = pending;
    try {
      await pending;
    } finally {
      if (logoutPromise === pending) logoutPromise = null;
    }
  }
  registerAdminSessionRestorer(restore);
  if (typeof window !== "undefined") {
    window.addEventListener("campusos:admin-session-expired", clearSession);
    onScopeDispose(() =>
      window.removeEventListener(
        "campusos:admin-session-expired",
        clearSession,
      ),
    );
  }
  return {
    user,
    profile,
    token,
    isLoggedIn,
    primaryRole,
    isAdmin,
    login,
    restore,
    logout,
    clearSession,
  };
});
