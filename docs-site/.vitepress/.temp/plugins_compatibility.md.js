import { ssrRenderAttrs, ssrRenderStyle } from "vue/server-renderer";
import { useSSRContext } from "vue";
import { _ as _export_sfc } from "./plugin-vue_export-helper.1tPrXgE0.js";
const __pageData = JSON.parse('{"title":"插件兼容矩阵","description":"","frontmatter":{},"headers":[],"relativePath":"plugins/compatibility.md","filePath":"plugins/compatibility.md","lastUpdated":1783761653000}');
const _sfc_main = { name: "plugins/compatibility.md" };
function _sfc_ssrRender(_ctx, _push, _parent, _attrs, $props, $setup, $data, $options) {
  _push(`<div${ssrRenderAttrs(_attrs)}><h1 id="插件兼容矩阵" tabindex="-1">插件兼容矩阵 <a class="header-anchor" href="#插件兼容矩阵" aria-label="Permalink to &quot;插件兼容矩阵&quot;">​</a></h1><table tabindex="0"><thead><tr><th>CampusOS</th><th>Manifest API</th><th>Host API</th><th>Go SDK</th><th>Runtime 模板</th></tr></thead><tbody><tr><td><code>0.6.x</code></td><td><code>campusos.plugin/v1</code></td><td><code>v1</code></td><td><code>v0.6</code></td><td>Built-in/gRPC/Wasm <code>0.1.x</code></td></tr></tbody></table><p>Manifest 应显式声明：</p><div class="language-yaml vp-adaptive-theme line-numbers-mode"><button title="Copy Code" class="copy"></button><span class="lang">yaml</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="${ssrRenderStyle({ "--shiki-light": "#22863A", "--shiki-dark": "#85E89D" })}">api_version</span><span style="${ssrRenderStyle({ "--shiki-light": "#24292E", "--shiki-dark": "#E1E4E8" })}">: </span><span style="${ssrRenderStyle({ "--shiki-light": "#032F62", "--shiki-dark": "#9ECBFF" })}">campusos.plugin/v1</span></span>
<span class="line"><span style="${ssrRenderStyle({ "--shiki-light": "#22863A", "--shiki-dark": "#85E89D" })}">host_api_version</span><span style="${ssrRenderStyle({ "--shiki-light": "#24292E", "--shiki-dark": "#E1E4E8" })}">: </span><span style="${ssrRenderStyle({ "--shiki-light": "#032F62", "--shiki-dark": "#9ECBFF" })}">v1</span></span>
<span class="line"><span style="${ssrRenderStyle({ "--shiki-light": "#22863A", "--shiki-dark": "#85E89D" })}">compatibility</span><span style="${ssrRenderStyle({ "--shiki-light": "#24292E", "--shiki-dark": "#E1E4E8" })}">:</span></span>
<span class="line"><span style="${ssrRenderStyle({ "--shiki-light": "#22863A", "--shiki-dark": "#85E89D" })}">  campusos</span><span style="${ssrRenderStyle({ "--shiki-light": "#24292E", "--shiki-dark": "#E1E4E8" })}">: </span><span style="${ssrRenderStyle({ "--shiki-light": "#032F62", "--shiki-dark": "#9ECBFF" })}">&quot;&gt;=0.6.0 &lt;0.7.0&quot;</span></span>
<span class="line"><span style="${ssrRenderStyle({ "--shiki-light": "#22863A", "--shiki-dark": "#85E89D" })}">  host_api</span><span style="${ssrRenderStyle({ "--shiki-light": "#24292E", "--shiki-dark": "#E1E4E8" })}">: </span><span style="${ssrRenderStyle({ "--shiki-light": "#032F62", "--shiki-dark": "#9ECBFF" })}">&quot;v1&quot;</span></span>
<span class="line"><span style="${ssrRenderStyle({ "--shiki-light": "#22863A", "--shiki-dark": "#85E89D" })}">  sdk_go</span><span style="${ssrRenderStyle({ "--shiki-light": "#24292E", "--shiki-dark": "#E1E4E8" })}">: </span><span style="${ssrRenderStyle({ "--shiki-light": "#032F62", "--shiki-dark": "#9ECBFF" })}">&quot;v0.6&quot;</span></span></code></pre><div class="line-numbers-wrapper" aria-hidden="true"><span class="line-number">1</span><br><span class="line-number">2</span><br><span class="line-number">3</span><br><span class="line-number">4</span><br><span class="line-number">5</span><br><span class="line-number">6</span><br></div></div><p>旧包未写版本字段时按 v1 读取，便于 v0.3-v0.5 包迁移；写入未知版本会被拒绝。新增权限会提高预检风险并要求管理员重新确认。破坏 Host API 请求/响应或权限语义必须发布新版本，不在 v1 中静默替换。</p><p>TypeScript SDK 暂不复制 Go Host API。浏览器插件不能持有 Host token，前端调用继续使用 Public HTTP API；待字段级 OpenAPI schema 稳定后，从 OpenAPI 生成只覆盖 Public API 的 TypeScript client。</p></div>`);
}
const _sfc_setup = _sfc_main.setup;
_sfc_main.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("plugins/compatibility.md");
  return _sfc_setup ? _sfc_setup(props, ctx) : void 0;
};
const compatibility = /* @__PURE__ */ _export_sfc(_sfc_main, [["ssrRender", _sfc_ssrRender]]);
export {
  __pageData,
  compatibility as default
};
