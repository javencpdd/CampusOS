export const grantExpiryDays = [7, 30, 90, 180, 365];
export const maxGrantExtensionDays = 3650;
const dayMS = 24 * 60 * 60 * 1000;
const maxExpiryMS = 253402300799999;

type Grant = { approved?: boolean; expires_at_ms?: string } | undefined;
export type GrantExpiryDraft = {
  choice: string;
  days: number;
  date: string;
};

function localDateTime(ms: number) {
  const d = new Date(ms);
  if (!Number.isSafeInteger(ms) || !Number.isFinite(d.getTime())) return "";
  d.setMinutes(d.getMinutes() - d.getTimezoneOffset());
  const iso = d.toISOString();
  return iso.startsWith("+") || iso.startsWith("-") ? "" : iso.slice(0, 16);
}

export function initialGrantExpiry(
  grant?: Grant,
  now = Date.now(),
): GrantExpiryDraft {
  const expiry = Number(grant?.expires_at_ms || "0");
  return {
    choice: expiry !== 0 ? "date" : grant?.approved ? "permanent" : "30",
    days: 30,
    date: localDateTime(expiry !== 0 ? expiry : now + dayMS),
  };
}

export function grantExpirySnapshot(draft: GrantExpiryDraft) {
  return {
    choice: draft.choice,
    ...(draft.choice === "custom" ? { days: draft.days } : {}),
    ...(draft.choice === "date" ? { date: draft.date } : {}),
  };
}

export function resolveGrantExpiry(
  draft: GrantExpiryDraft,
  grant?: Grant,
  now = Date.now(),
): number {
  if (draft.choice === "permanent") return 0;
  const saved = Number(grant?.expires_at_ms || "0");
  let expiry: number;
  if (draft.choice === "date") {
    expiry =
      draft.date && draft.date === localDateTime(saved)
        ? saved
        : Date.parse(draft.date);
  } else {
    const days = draft.choice === "custom" ? draft.days : Number(draft.choice);
    if (
      !Number.isSafeInteger(days) ||
      days < 1 ||
      days > maxGrantExtensionDays ||
      (draft.choice !== "custom" && !grantExpiryDays.includes(days))
    )
      throw Error(`请输入 1 至 ${maxGrantExtensionDays} 的整数天数。`);
    const base =
      grant?.approved && saved > now && Number.isSafeInteger(saved)
        ? saved
        : now;
    expiry = base + days * dayMS;
  }
  if (!Number.isSafeInteger(expiry) || expiry <= now || expiry > maxExpiryMS)
    throw Error("请选择未来的授权截止时间（不晚于 9999 年）。");
  return expiry;
}

export function formatGrantExpiry(expiry: number) {
  if (expiry === 0) return "永久";
  if (!Number.isSafeInteger(expiry) || expiry < 0 || expiry > maxExpiryMS)
    return "无效日期";
  return new Date(expiry).toLocaleString("zh-CN", { hour12: false });
}
