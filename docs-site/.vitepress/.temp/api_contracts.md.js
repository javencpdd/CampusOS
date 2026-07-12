import { ssrRenderAttrs, ssrRenderStyle } from "vue/server-renderer";
import { useSSRContext } from "vue";
import { _ as _export_sfc } from "./plugin-vue_export-helper.1tPrXgE0.js";
const __pageData = JSON.parse('{"title":"当前 API 契约与兼容规则","description":"","frontmatter":{},"headers":[],"relativePath":"api/contracts.md","filePath":"api/contracts.md","lastUpdated":1783793126000}');
const _sfc_main = { name: "api/contracts.md" };
function _sfc_ssrRender(_ctx, _push, _parent, _attrs, $props, $setup, $data, $options) {
  _push(`<div${ssrRenderAttrs(_attrs)}><h1 id="当前-api-契约与兼容规则" tabindex="-1">当前 API 契约与兼容规则 <a class="header-anchor" href="#当前-api-契约与兼容规则" aria-label="Permalink to &quot;当前 API 契约与兼容规则&quot;">​</a></h1><p>CampusOS v0.6 的当前 HTTP 路由由 <code>internal/server/server.go</code> 注册，并通过 <code>campusos-contracts</code> 生成 OpenAPI、JSON 清单和授权矩阵。合约漂移检查已进入 CI。</p><h2 id="契约产物" tabindex="-1">契约产物 <a class="header-anchor" href="#契约产物" aria-label="Permalink to &quot;契约产物&quot;">​</a></h2><table tabindex="0"><thead><tr><th>产物</th><th>仓库路径</th></tr></thead><tbody><tr><td>Current OpenAPI</td><td><code>docs/api/openapi-v0.6-current.yaml</code></td></tr><tr><td>路由 JSON</td><td><code>docs/api/http-routes-v0.6.json</code></td></tr><tr><td>路由与授权矩阵</td><td><code>docs/api/HTTP路由与授权矩阵-v0.6.md</code></td></tr><tr><td>插件权限目录</td><td><code>docs/api/plugin-permissions-v1.json</code></td></tr></tbody></table><div class="language-bash vp-adaptive-theme line-numbers-mode"><button title="Copy Code" class="copy"></button><span class="lang">bash</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="${ssrRenderStyle({ "--shiki-light": "#6F42C1", "--shiki-dark": "#B392F0" })}">make</span><span style="${ssrRenderStyle({ "--shiki-light": "#032F62", "--shiki-dark": "#9ECBFF" })}"> contracts</span></span>
<span class="line"><span style="${ssrRenderStyle({ "--shiki-light": "#6F42C1", "--shiki-dark": "#B392F0" })}">make</span><span style="${ssrRenderStyle({ "--shiki-light": "#032F62", "--shiki-dark": "#9ECBFF" })}"> contracts-check</span></span></code></pre><div class="line-numbers-wrapper" aria-hidden="true"><span class="line-number">1</span><br><span class="line-number">2</span><br></div></div><h2 id="稳定性" tabindex="-1">稳定性 <a class="header-anchor" href="#稳定性" aria-label="Permalink to &quot;稳定性&quot;">​</a></h2><p>v0.6 当前路由统一标记为 <code>experimental</code>。进入 stable 前，客户端应允许新增可选字段，并同时依据 HTTP 状态、兼容顶层 <code>{ code, msg, data }</code> 和结构化 <code>error.code</code> 处理失败。删除路径、修改字段类型或扩大写入语义必须经过弃用期或新 API 版本，不能静默变化。</p><p>OpenAPI 当前保证 148 条路由的方法、认证、显式权限和请求体声明与代码一致。新增 UI Runtime Manifest、SSE 和五种 Method 的 Extension Gateway；核心身份、社区、空间、课表、富文本、角色/版主和风格包选择请求已有字段级 schema。标记 <code>generic-experimental</code> 的动态配置、声明式 UI 和上传模型仍只提供通用对象，不能视为 stable。</p><p>生成器测试会解析 YAML、检查路径参数与分页参数只能出现一个 <code>parameters</code> 块，并要求核心 request schema、结构化错误和请求体数量达到门槛。<code>make contracts-check</code> 负责阻止生成产物漂移。</p></div>`);
}
const _sfc_setup = _sfc_main.setup;
_sfc_main.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("api/contracts.md");
  return _sfc_setup ? _sfc_setup(props, ctx) : void 0;
};
const contracts = /* @__PURE__ */ _export_sfc(_sfc_main, [["ssrRender", _sfc_ssrRender]]);
export {
  __pageData,
  contracts as default
};
