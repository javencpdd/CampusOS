// getRandomValues remains available on LAN HTTP where randomUUID is absent.
// Use cryptographic entropy only; never fall back to Math.random.
export function randomRequestID(source: Pick<Crypto, "getRandomValues"> & Partial<Pick<Crypto, "randomUUID">> = globalThis.crypto): string {
  if (source && typeof source.randomUUID === "function") return source.randomUUID();
  if (!source || typeof source.getRandomValues !== "function") throw new Error("安全随机数不可用。");
  const bytes = new Uint8Array(16);
  source.getRandomValues(bytes);
  bytes[6] = (bytes[6] & 15) | 64;
  bytes[8] = (bytes[8] & 63) | 128;
  const hex = Array.from(bytes, value => value.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
