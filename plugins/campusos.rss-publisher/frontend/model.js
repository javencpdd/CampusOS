export const MAX_SOURCES = 6;
export function canonicalFeedURL(value) {
  const url = new URL(value.trim());
  if (
    url.protocol !== "https:" ||
    !url.hostname ||
    url.username ||
    url.password ||
    url.hash ||
    url.href.length > 512
  )
    throw new Error("请填写不含凭据和片段的公开 HTTPS 订阅地址。");
  return url.href;
}
export function subscriptions(values) {
  return Array.from({ length: MAX_SOURCES }, (_, i) => ({
    field: `feed_${i + 1}`,
    url: String(values[`feed_${i + 1}`] || ""),
  })).filter((s) => s.url);
}
export function addSubscription(values, url) {
  const target = canonicalFeedURL(url);
  const current = subscriptions(values);
  if (current.some((s) => canonicalFeedURL(s.url) === target))
    throw new Error("此订阅已存在。");
  const slot = Array.from(
    { length: MAX_SOURCES },
    (_, i) => `feed_${i + 1}`,
  ).find((key) => !values[key]);
  if (!slot) throw new Error("最多保存 6 个订阅，请先移除一个。");
  return { ...values, [slot]: target };
}
// Keep remote src inert before DOM parsing; previews load only brokered blobs.
export function inertImages(html) {
  return html.replace(
    /(<img\b[^>]*?)\bsrc=("[^"]*"|'[^']*')/gi,
    "$1data-rss-src=$2",
  );
}
