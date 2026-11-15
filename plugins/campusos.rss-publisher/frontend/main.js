import { Bridge } from "./bridge.js";
import { subscriptions, addSubscription } from "./model.js";
import { parseFeed, articleHTML } from "./parser.js";
const $ = (id) => document.getElementById(id);
let client,
  values = {},
  selectedClient = "",
  localAvailable = false,
  pollClients,
  clientRefreshEpoch = 0,
  revision = "0",
  feed,
  selected,
  activeField = "",
  busy = false,
  generation = 0;
const urls = new Set();
function status(text, bad = false) {
  $("status").textContent = text;
  $("status").className = bad ? "error" : "";
}
function releaseImages() {
  for (const url of urls) URL.revokeObjectURL(url);
  urls.clear();
}
function setBusy(value) {
  busy = value;
  document
    .querySelectorAll("button,input,select")
    .forEach((el) => (el.disabled = value));
  $("pair-reader").disabled = value || !localAvailable;
  document
    .querySelectorAll("[data-platform]")
    .forEach((el) => (el.disabled = value || !localAvailable));
}
async function task(action) {
  if (busy) return;
  setBusy(true);
  try {
    await action();
  } catch (e) {
    status(
      (e?.message || "操作失败") +
        " 请检查插件批准/个人同意，以及最终 HTTPS 订阅地址。",
      true,
    );
  } finally {
    setBusy(false);
  }
}
function clearPreview() {
  selected = undefined;
  releaseImages();
  $("preview").hidden = true;
  $("preview-empty").hidden = false;
  $("entry-body").replaceChildren();
}
function renderSubscriptions() {
  $("sources").replaceChildren();
  for (const source of subscriptions(values)) {
    const option = document.createElement("option");
    option.value = source.field;
    option.textContent = source.url;
    $("sources").append(option);
  }
  activeField = $("sources").value;
  if (!activeField) {
    const option = document.createElement("option");
    option.textContent = "尚无订阅";
    option.value = "";
    $("sources").append(option);
  }
}
async function save(next) {
  await client.request("config.update", {
    revision,
    values_json: JSON.stringify(next),
  });
  status("订阅已保存，正在重新载入插件…");
}
function preview(entry) {
  releaseImages();
  selected = entry;
  $("preview").hidden = false;
  $("preview-empty").hidden = true;
  $("entry-title").textContent = entry.title;
  $("entry-meta").textContent = [
    entry.author,
    entry.published,
    `${entry.images.length} 张图片`,
  ]
    .filter(Boolean)
    .join(" · ");
  $("entry-source").textContent = "原文：" + entry.link;
  $("entry-body").innerHTML = entry.html;
  $("entry-body")
    .querySelectorAll("img")
    .forEach((image) => {
      image.alt ||= "点击“加载预览图片”查看";
      image.removeAttribute("src");
    });
  $("load-images").hidden = !entry.images.length;
  status("已载入图文预览。确认后可导入编辑器。");
}
$("add-button").addEventListener("click", (event) => {
  event.preventDefault();
  void task(() => save(addSubscription(values, $("feed-url").value)));
});
$("remove").addEventListener(
  "click",
  () =>
    void task(async () => {
      if (!activeField) throw new Error("请先选择订阅。");
      await save({ ...values, [activeField]: "" });
    }),
);
$("sources").addEventListener("change", () => {
  generation++;
  activeField = $("sources").value;
  feed = undefined;
  clearPreview();
  $("entries").textContent = "点击“读取最新内容”加载所选订阅。";
});
$("refresh").addEventListener(
  "click",
  () =>
    void task(async () => {
      if (!activeField) throw new Error("请先添加订阅。");
      clearPreview();
      const current = ++generation;
      const result = await client.request("web.read", {
        url: values[activeField],
        kind: "document",
        ...(selectedClient ? { client_id: selectedClient } : {}),
      });
      const data = parseFeed(result.bytes, values[activeField]);
      if (current !== generation) return;
      feed = data;
      $("entries").replaceChildren();
      for (const entry of feed.entries) {
        const card = document.createElement("div");
        card.className = "entry";
        const title = document.createElement("h3");
        title.textContent = entry.title;
        const meta = document.createElement("p");
        meta.className = "hint";
        meta.textContent = entry.published;
        const summary = document.createElement("p");
        summary.textContent = entry.summary;
        const button = document.createElement("button");
        button.textContent = "预览图文";
        button.className = "quiet";
        button.addEventListener("click", () => preview(entry));
        card.append(title, meta, summary, button);
        $("entries").append(card);
      }
      if (!feed.entries.length)
        $("entries").textContent = "订阅中没有可导入的图文内容。";
      status(`${feed.title}：已读取 ${feed.entries.length} 篇内容。`);
    }),
);
$("load-images").addEventListener(
  "click",
  () =>
    void task(async () => {
      if (!selected) return;
      const entry = selected,
        current = generation;
      for (const [index, source] of entry.images.entries()) {
        try {
          const result = await client.request("web.read", {
            url: source,
            kind: "image",
            ...(selectedClient ? { client_id: selectedClient } : {}),
          });
          if (current !== generation || selected?.id !== entry.id) return;
          const url = URL.createObjectURL(
            new Blob([result.bytes], { type: result.mime }),
          );
          urls.add(url);
          $("entry-body")
            .querySelectorAll("img[data-rss-src]")
            .forEach((image) => {
              if (image.dataset.rssSrc === source) {
                image.loading = "eager";
                image.src = url;
              }
            });
        } catch {
          status(
            "部分图片无法读取。导入时会再次尝试，并保留失败位置提示。",
            true,
          );
        }
      }
    }),
);
$("compose").addEventListener(
  "click",
  () =>
    void task(async () => {
      if (!selected) return;
      status("正在准备导入，请在宿主弹窗确认；图片转存可能需要一些时间。");
      await client.request("editor.article.import", {
        title: selected.title,
        summary: selected.summary,
        content_html: articleHTML(selected),
        source: selected.link,
        ...(selectedClient ? { client_id: selectedClient } : {}),
      });
      status("已准备图文，请到编辑器核对并发布。");
    }),
);
$("entry-body").addEventListener("click", (event) => {
  const link = event.target.closest("a");
  if (link) {
    event.preventDefault();
    status("请在导入后的图文编辑器中查看原文链接。");
  }
});
async function refreshClients() {
  const epoch = ++clientRefreshEpoch;
  const state = await client.request("web.clients.status", {});
  if (epoch !== clientRefreshEpoch) return;
  localAvailable = state.local_available;
  selectedClient = state.selected_client_id || "";
  const select = $("reader");
  select.replaceChildren();
  const server = document.createElement("option");
  server.value = "";
  server.textContent = "CampusOS 服务器";
  select.append(server);
  for (const item of state.clients || []) {
    const option = document.createElement("option");
    option.value = item.id;
    option.textContent =
      "本机：" +
      item.name +
      "（" +
      (item.online ? "在线" : "离线") +
      " / " +
      item.proxy_mode +
      "）";
    option.disabled = !item.online;
    select.append(option);
  }
  if (
    selectedClient &&
    !(state.clients || []).some((item) => item.id === selectedClient)
  ) {
    const unavailable = document.createElement("option");
    unavailable.value = selectedClient;
    unavailable.textContent = "所选助手不可用，请重新配对或明确切换服务器";
    unavailable.disabled = true;
    select.append(unavailable);
  }
  select.value = selectedClient;
  $("reader-location").textContent =
    "读取位置：" +
    (selectedClient
      ? select.selectedOptions[0].textContent
      : "CampusOS 服务器");
  $("disconnect-reader").hidden = !(state.clients || []).some(
    (item) => item.id === selectedClient,
  );
  $("pair-reader").disabled = busy || !localAvailable;
  $("reader-hint").textContent = localAvailable
    ? "服务器代理由管理员控制；本机代理仅在你自己的助手中配置。助手失联不会自动切回服务器。"
    : "本机读取能力尚未批准；服务器读取仍可使用。请管理员在插件授权中检查本机读取用途。";
}
$("pair-reader").addEventListener(
  "click",
  () =>
    void task(async () => {
      const pairing = await client.request("web.clients.pair", {});
      $("pairing").hidden = false;
      $("pair-code").value = pairing.pairing_code;
      $("pair-server").textContent = "CampusOS 地址：" + pairing.server;
      $("pair-expiry").textContent =
        "配对码仅可使用一次，有效至 " +
        new Date(pairing.expires_at).toLocaleTimeString() +
        "。服务器重启或当前会话/授权失效后需重新配对。";
      status("请在你自己的电脑运行助手并输入配对码，再选择这台电脑。");
    }),
);
$("refresh-readers").addEventListener("click", () => void task(refreshClients));
$("reader").addEventListener(
  "change",
  () =>
    void task(async () => {
      try {
        const id = $("reader").value;
        await client.request("web.clients.select", id ? { client_id: id } : {});
        generation++;
        feed = undefined;
        clearPreview();
        $("entries").textContent = "读取位置已变化，请重新读取订阅。";
        await refreshClients();
        status("读取位置已切换，旧预览已清空。");
      } finally {
        $("reader").value = selectedClient;
      }
    }),
);
$("disconnect-reader").addEventListener(
  "click",
  () =>
    void task(async () => {
      await client.request("web.clients.disconnect", {
        client_id: selectedClient,
      });
      generation++;
      clearPreview();
      await refreshClients();
      status("助手已断开。重新配对或明确切换服务器后再读取。");
    }),
);
document.querySelectorAll("[data-platform]").forEach((button) =>
  button.addEventListener(
    "click",
    () =>
      void task(async () => {
        await client.request("web.clients.download", {
          platform: button.dataset.platform,
        });
        status("助手已下载。请在自己的电脑启动，并配置本机代理及配对码。");
      }),
  ),
);
async function start() {
  const host = new URLSearchParams(location.search).get("host_origin");
  if (!host || new URL(host).origin !== host || parent === window)
    throw new Error("请从 CampusOS 的用户插件中心打开。");
  client = new Bridge(host);
  await client.connect();
  const config = await client.request("config.read", {});
  revision = config.revision;
  values = JSON.parse(config.values_json);
  renderSubscriptions();
  await refreshClients();
  pollClients = setInterval(() => {
    if (!busy) void refreshClients().catch(() => {});
  }, 3000);
  status("订阅列表已就绪。添加地址或选择订阅后读取内容。");
}
setBusy(true);
start()
  .catch((e) => status(e.message, true))
  .finally(() => setBusy(false));
window.addEventListener("pagehide", () => {
  clearInterval(pollClients);
  releaseImages();
  client?.close();
});

$("feed-url").addEventListener("keydown", (event) => {
  if (event.key === "Enter") {
    event.preventDefault();
    $("add-button").click();
  }
});
