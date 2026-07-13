import { ssrRenderAttrs } from "vue/server-renderer";
import { useSSRContext } from "vue";
import { _ as _export_sfc } from "./plugin-vue_export-helper.1tPrXgE0.js";
const __pageData = JSON.parse('{"title":"Host API 与权限","description":"","frontmatter":{},"headers":[],"relativePath":"plugins/host-api.md","filePath":"plugins/host-api.md","lastUpdated":1783793126000}');
const _sfc_main = { name: "plugins/host-api.md" };
function _sfc_ssrRender(_ctx, _push, _parent, _attrs, $props, $setup, $data, $options) {
  _push(`<div${ssrRenderAttrs(_attrs)}><h1 id="host-api-与权限" tabindex="-1">Host API 与权限 <a class="header-anchor" href="#host-api-与权限" aria-label="Permalink to &quot;Host API 与权限&quot;">​</a></h1><p>Host API 只监听配置的内部地址，默认 <code>127.0.0.1:18080</code>。每个调用同时检查运行中插件身份、启动时签发的短期随机令牌和 Manifest 权限。令牌会在重载时轮换、停用时撤销，并通过进程环境变量交给受管插件；插件不应记录它。</p><h2 id="请求身份" tabindex="-1">请求身份 <a class="header-anchor" href="#请求身份" aria-label="Permalink to &quot;请求身份&quot;">​</a></h2><p>SDK 自动读取：</p><div class="language-text vp-adaptive-theme line-numbers-mode"><button title="Copy Code" class="copy"></button><span class="lang">text</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span>CAMPUSOS_PLUGIN_NAME</span></span>
<span class="line"><span>CAMPUSOS_PLUGIN_TOKEN</span></span>
<span class="line"><span>CAMPUSOS_HOST_API_URL</span></span></code></pre><div class="line-numbers-wrapper" aria-hidden="true"><span class="line-number">1</span><br><span class="line-number">2</span><br><span class="line-number">3</span><br></div></div><p>直接调用需要 <code>X-CampusOS-Plugin</code> 和 <code>X-CampusOS-Plugin-Token</code>。只有插件名称没有令牌不能通过生产 Host API。</p><h2 id="权限模型" tabindex="-1">权限模型 <a class="header-anchor" href="#权限模型" aria-label="Permalink to &quot;权限模型&quot;">​</a></h2><p>Manifest 默认没有权限。方法与权限对应关系由 <code>docs/api/plugin-permissions-v1.json</code> 生成，例如：</p><table tabindex="0"><thead><tr><th>方法</th><th>权限</th></tr></thead><tbody><tr><td><code>GetUser</code></td><td><code>user/read</code></td></tr><tr><td><code>QueryThreads</code></td><td><code>thread/read</code></td></tr><tr><td><code>PublishEvent</code></td><td><code>event/publish</code></td></tr><tr><td><code>StorageSet</code></td><td><code>storage/write</code></td></tr><tr><td><code>SendNotification</code></td><td><code>notification/send</code></td></tr></tbody></table><h2 id="host-api-v2-受管数据" tabindex="-1">Host API v2 受管数据 <a class="header-anchor" href="#host-api-v2-受管数据" aria-label="Permalink to &quot;Host API v2 受管数据&quot;">​</a></h2><p><code>RecordCreate</code>、<code>RecordGet</code>、<code>RecordList</code>、<code>RecordUpdate</code> 和 <code>RecordDelete</code> 只提供给 <code>api_version: campusos.plugin/v2</code>、<code>host_api_version: v2</code> 的 External Plugin，并且只能操作其 Manifest 中 <code>owner: system</code> 的集合。</p><p>用户归属记录和文件必须经已登录的 <code>/api/v1/plugin-market/...</code> 接口处理。扩展进程不能提交用户 ID，也不能直接访问数据库、JWT 或个人空间绝对路径。完整合同见仓库的 <code>docs/api/Host-API-v2受管数据合同.md</code>。</p><p>插件权限和系统用户 RBAC 是两个不同层次。插件声明只表示它可请求某类 Host 能力；涉及具体用户、个人空间、课表或版主管理时，Host 仍需检查用户归属、scope 和可见性。</p><p>系统级与用户级插件使用同一最小权限原则。<code>restart</code>、<code>plugin-restart</code> 或 <code>hot</code> 都不会自动扩大权限，也不会绕过权限复核。</p></div>`);
}
const _sfc_setup = _sfc_main.setup;
_sfc_main.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("plugins/host-api.md");
  return _sfc_setup ? _sfc_setup(props, ctx) : void 0;
};
const hostApi = /* @__PURE__ */ _export_sfc(_sfc_main, [["ssrRender", _sfc_ssrRender]]);
export {
  __pageData,
  hostApi as default
};
