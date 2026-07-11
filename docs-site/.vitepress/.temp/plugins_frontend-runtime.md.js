import { ssrRenderAttrs, ssrRenderStyle } from "vue/server-renderer";
import { useSSRContext } from "vue";
import { _ as _export_sfc } from "./plugin-vue_export-helper.1tPrXgE0.js";
const __pageData = JSON.parse('{"title":"前端运行时与 Extension Gateway","description":"","frontmatter":{},"headers":[],"relativePath":"plugins/frontend-runtime.md","filePath":"plugins/frontend-runtime.md","lastUpdated":null}');
const _sfc_main = { name: "plugins/frontend-runtime.md" };
function _sfc_ssrRender(_ctx, _push, _parent, _attrs, $props, $setup, $data, $options) {
  _push(`<div${ssrRenderAttrs(_attrs)}><h1 id="前端运行时与-extension-gateway" tabindex="-1">前端运行时与 Extension Gateway <a class="header-anchor" href="#前端运行时与-extension-gateway" aria-label="Permalink to &quot;前端运行时与 Extension Gateway&quot;">​</a></h1><p>插件在 <code>plugin.yaml</code> 的 <code>ui</code> 中声明 Route、Navigation、Slot、Surface 和 Action。Core 返回当前用户可见的 Runtime Manifest，Web 收到 SSE revision 后原子重建注册项。</p><div class="language-http vp-adaptive-theme line-numbers-mode"><button title="Copy Code" class="copy"></button><span class="lang">http</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="${ssrRenderStyle({ "--shiki-light": "#D73A49", "--shiki-dark": "#F97583" })}">GET</span><span style="${ssrRenderStyle({ "--shiki-light": "#24292E", "--shiki-dark": "#E1E4E8" })}"> /api/v1/ui/runtime-manifest</span></span>
<span class="line"><span style="${ssrRenderStyle({ "--shiki-light": "#D73A49", "--shiki-dark": "#F97583" })}">GET</span><span style="${ssrRenderStyle({ "--shiki-light": "#24292E", "--shiki-dark": "#E1E4E8" })}"> /api/v1/ui/events</span></span>
<span class="line"><span style="${ssrRenderStyle({ "--shiki-light": "#24292E", "--shiki-dark": "#E1E4E8" })}">ANY /api/v1/extensions/:plugin/*path</span></span></code></pre><div class="line-numbers-wrapper" aria-hidden="true"><span class="line-number">1</span><br><span class="line-number">2</span><br><span class="line-number">3</span><br></div></div><h2 id="默认-ui" tabindex="-1">默认 UI <a class="header-anchor" href="#默认-ui" aria-label="Permalink to &quot;默认 UI&quot;">​</a></h2><p>第三方插件应使用声明式 schema，只能组合 Campus UI 白名单组件。复杂编辑器或图表可以使用 <code>trusted-module</code>，但 module ID 必须由 Core 编译进白名单；不支持远程 Vue 模块或任意同源脚本。</p><p>每个 Surface 必须提供 ID、版本、类型、layout role、默认 renderer/schema、data contract、Action IDs、公开 token 和可调整 region。Action 的 method、path、权限、确认、审计和固定 body 属于插件合同，风格包不能修改。</p><h2 id="gateway-安全" tabindex="-1">Gateway 安全 <a class="header-anchor" href="#gateway-安全" aria-label="Permalink to &quot;Gateway 安全&quot;">​</a></h2><p>Gateway 要求 JWT，并执行插件状态与健康检查、权限、1 MiB 请求上限、5 秒超时、Trace ID、审计和标准错误。插件只信任 Core 注入的 caller，不能信任请求体中的 user ID、角色或管理员状态。</p><p>系统级插件和用户级插件都不会自动取得当前用户没有的权限。风格包使用只读 CampusStyleSDK，与可执行业务 Action 的 Extension Gateway 是两条不同边界。</p><p>可运行示例位于 <code>data/plugins/campus-welcome/</code>。</p></div>`);
}
const _sfc_setup = _sfc_main.setup;
_sfc_main.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("plugins/frontend-runtime.md");
  return _sfc_setup ? _sfc_setup(props, ctx) : void 0;
};
const frontendRuntime = /* @__PURE__ */ _export_sfc(_sfc_main, [["ssrRender", _sfc_ssrRender]]);
export {
  __pageData,
  frontendRuntime as default
};
