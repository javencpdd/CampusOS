import type { AdminSelfProfile } from "./api";

let accessToken: string | null = null;
let profile: AdminSelfProfile | null = null;
let restoreSession: (() => Promise<boolean>) | null = null;

export function getAdminAccessToken() {
  return accessToken;
}
export function setAdminAccessToken(value: string | null) {
  accessToken = value || null;
}
export function getCurrentAdminProfile() {
  return profile;
}
export function setCurrentAdminProfile(value: AdminSelfProfile | null) {
  profile = value;
}
export function clearAdminSession() {
  accessToken = null;
  profile = null;
  // Removing the readable nonce prevents a failed logout/restore from silently
  // resurrecting the HttpOnly session. The server owns refresh-cookie deletion.
  if (typeof document !== "undefined")
    document.cookie = "campusos_admin_csrf=; Path=/; Max-Age=0; SameSite=Lax";
}
export function registerAdminSessionRestorer(restorer: () => Promise<boolean>) {
  restoreSession = restorer;
}
export async function ensureAdminAccessToken() {
  // Navigation always asks the current server; a cached JWT is insufficient.
  return (await restoreSession?.()) || false;
}
export function adminCSRFToken() {
  if (typeof document === "undefined") return "";
  const encoded =
    document.cookie
      .split("; ")
      .find((item) => item.startsWith("campusos_admin_csrf="))
      ?.split("=")
      .slice(1)
      .join("=") || "";
  try {
    return decodeURIComponent(encoded);
  } catch {
    return "";
  }
}
