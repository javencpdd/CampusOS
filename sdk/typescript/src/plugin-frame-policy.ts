// The source is produced only by the compiled UI host, never by a package.
// Restricting the parent document's frame-src also blocks an untrusted iframe
// navigating itself to an external site to bypass its own connect-src policy.
export function installPluginFramePolicy(
  source: string,
  document: Document = globalThis.document,
): void {
  if (typeof source !== "string") throw new Error("插件隔离域策略无效。");
  const remote = source.startsWith("'self' ") ? source.slice(7) : source;
  if (source !== "'none'" && source !== "'self'") {
    if (
      typeof source !== "string" ||
      !/^https?:\/\/\*\.[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[0-9]{1,5})?$/.test(
        remote,
      )
    )
      throw new Error("插件隔离域策略无效。");
    const parsed = new URL(remote.replace("*", "isolation"));
    if (parsed.port && (+parsed.port < 1 || +parsed.port > 65535))
      throw new Error("插件隔离端口无效。");
  }
  const content = `frame-src ${source}`;
  let meta = document.querySelector<HTMLMetaElement>(
    "meta[data-campusos-plugin-frames]",
  );
  if (meta) {
    if (meta.content !== content)
      throw new Error("插件隔离域已变化，请刷新页面。");
    return;
  }
  meta = document.createElement("meta");
  meta.dataset.campusosPluginFrames = "true";
  meta.httpEquiv = "Content-Security-Policy";
  meta.content = content;
  document.head.append(meta);
}
