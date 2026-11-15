// Feed interpretation belongs to this package. The host returns bounded public
// bytes and accepts a generic, untrusted article import; it knows no feed slots.
const tags = new Set(
  "p div section article br hr h1 h2 h3 h4 h5 h6 strong em b i u s span small ul ol li blockquote pre code a img figure figcaption table thead tbody tr td th".split(
    " ",
  ),
);
const forbidden = new Set(
  "script style iframe object svg math form noscript template input textarea button video audio source link meta".split(
    " ",
  ),
);
function absolute(value, base) {
  try {
    const url = new URL(value, base);
    return url.protocol === "https:" &&
      !url.username &&
      !url.password &&
      url.href.length <= 2048
      ? url.href
      : "";
  } catch {
    return "";
  }
}
function children(node, name) {
  return [...node.children].filter((child) => child.localName === name);
}
function child(node, name) {
  return children(node, name)[0];
}
function text(node, names) {
  for (const name of names) {
    const value = child(node, name)?.textContent?.trim();
    if (value) return value;
  }
  return "";
}
function plain(value, length) {
  return String(value || "")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, length);
}
function safeMarkup(value, base, enclosures = []) {
  const template = document.createElement("template");
  template.innerHTML = value;
  const output = document.createElement("div"),
    images = [];
  let count = 0;
  function copy(node, parent, depth) {
    if (++count > 1500 || depth > 48) return;
    if (node.nodeType === Node.TEXT_NODE) {
      parent.append(document.createTextNode(node.textContent));
      return;
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return;
    const name = node.localName.toLowerCase();
    if (forbidden.has(name)) return;
    let destination = parent;
    if (tags.has(name)) {
      const clean = document.createElement(name);
      if (name === "a") {
        const href = absolute(node.getAttribute("href"), base);
        if (href) {
          clean.href = href;
          clean.rel = "noopener noreferrer";
        }
      }
      if (name === "img") {
        const src = absolute(
          node.getAttribute("data-src") ||
            node.getAttribute("data-original") ||
            node.getAttribute("src"),
          base,
        );
        if (!src || images.length >= 8) return;
        images.push(src);
        clean.setAttribute("data-rss-src", src);
        clean.alt = plain(node.getAttribute("alt"), 120);
      }
      if (name === "td" || name === "th")
        for (const key of ["colspan", "rowspan"]) {
          const value = node.getAttribute(key);
          if (/^[1-9][0-9]?$/.test(value || "")) clean.setAttribute(key, value);
        }
      parent.append(clean);
      destination = clean;
    }
    for (const next of node.childNodes) copy(next, destination, depth + 1);
  }
  for (const node of template.content.childNodes) copy(node, output, 0);
  for (const source of enclosures) {
    if (images.length >= 8) break;
    if (!source || images.includes(source)) continue;
    const image = document.createElement("img");
    image.setAttribute("data-rss-src", source);
    image.alt = "订阅图片";
    output.append(image);
    images.push(source);
  }
  const html = output.innerHTML;
  if (new TextEncoder().encode(html).byteLength > 40000)
    throw new Error("订阅正文超出大小限制。");
  return { html, images, summary: plain(output.textContent, 400) };
}
export function articleHTML(entry) {
  const template = document.createElement("template");
  template.innerHTML = entry.html;
  for (const image of template.content.querySelectorAll("img")) {
    const source = image.getAttribute("data-rss-src");
    image.removeAttribute("data-rss-src");
    if (source) image.src = source;
  }
  return template.innerHTML;
}
export function parseFeed(bytes, source) {
  if (!(bytes instanceof ArrayBuffer) || bytes.byteLength > 1024 * 1024)
    throw new Error("订阅文件超出大小限制。");
  const input = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  if (/<!\s*(?:DOCTYPE|ENTITY)/i.test(input))
    throw new Error("不支持带 DTD 或实体声明的订阅。");
  const xml = new DOMParser().parseFromString(input, "application/xml");
  if (xml.querySelector("parsererror")) throw new Error("订阅 XML 无效。");
  let nodes = 0;
  const pending = [[xml.documentElement, 0]];
  while (pending.length) {
    const [node, depth] = pending.pop();
    if (!node || ++nodes > 5000 || depth > 48)
      throw new Error("订阅结构超出限制。");
    for (const next of node.children) pending.push([next, depth + 1]);
  }
  const root = xml.documentElement,
    atom = root.localName === "feed";
  if (!["feed", "rss", "RDF"].includes(root.localName))
    throw new Error("仅支持 RSS 或 Atom。");
  const channel = atom ? root : child(root, "channel") || root;
  const items = atom
    ? children(root, "entry")
    : root.localName === "RDF"
      ? children(root, "item")
      : children(channel, "item");
  const entries = [],
    seen = new Set();
  for (const [index, node] of items.slice(0, 40).entries()) {
    if (entries.length >= 20) break;
    const link = atom
      ? [...children(node, "link")]
          .find(
            (link) =>
              !link.getAttribute("rel") ||
              link.getAttribute("rel") === "alternate",
          )
          ?.getAttribute("href")
      : text(node, ["link"]);
    const url = absolute(link || source, source),
      identity = text(node, ["guid", "id"]) || url + index;
    if (seen.has(identity)) continue;
    seen.add(identity);
    const attachments = [
      ...children(node, "enclosure"),
      ...children(node, "content"),
      ...children(node, "thumbnail"),
      ...children(node, "link"),
    ]
      .filter(
        (item) =>
          /^image\//.test(item.getAttribute("type") || "") ||
          item.localName === "thumbnail" ||
          item.getAttribute("medium") === "image",
      )
      .map((item) =>
        absolute(item.getAttribute("url") || item.getAttribute("href"), source),
      );
    const content =
      child(node, "encoded") ||
      child(node, "content") ||
      child(node, "description") ||
      child(node, "summary");
    const raw = content?.children.length
      ? [...content.childNodes]
          .map((item) => new XMLSerializer().serializeToString(item))
          .join("")
      : content?.textContent || "";
    const markup = safeMarkup(raw, url || source, attachments);
    entries.push({
      id: identity.slice(0, 512),
      title: plain(text(node, ["title"]), 255) || "未命名条目",
      link: url || source,
      author: plain(
        atom
          ? text(child(node, "author") || node, ["name"])
          : text(node, ["creator", "author"]),
        120,
      ),
      published: plain(
        text(node, ["published", "updated", "pubDate", "date"]),
        120,
      ),
      ...markup,
    });
  }
  return { title: plain(text(channel, ["title"]), 255) || "订阅内容", entries };
}
