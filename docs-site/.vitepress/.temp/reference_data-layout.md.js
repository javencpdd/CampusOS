import { ssrRenderAttrs } from "vue/server-renderer";
import { useSSRContext } from "vue";
import { _ as _export_sfc } from "./plugin-vue_export-helper.1tPrXgE0.js";
const __pageData = JSON.parse('{"title":"数据目录","description":"","frontmatter":{},"headers":[],"relativePath":"reference/data-layout.md","filePath":"reference/data-layout.md","lastUpdated":1783761653000}');
const _sfc_main = { name: "reference/data-layout.md" };
function _sfc_ssrRender(_ctx, _push, _parent, _attrs, $props, $setup, $data, $options) {
  _push(`<div${ssrRenderAttrs(_attrs)}><h1 id="数据目录" tabindex="-1">数据目录 <a class="header-anchor" href="#数据目录" aria-label="Permalink to &quot;数据目录&quot;">​</a></h1><h2 id="总体结构" tabindex="-1">总体结构 <a class="header-anchor" href="#总体结构" aria-label="Permalink to &quot;总体结构&quot;">​</a></h2><div class="language-text vp-adaptive-theme line-numbers-mode"><button title="Copy Code" class="copy"></button><span class="lang">text</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span>data/</span></span>
<span class="line"><span>├── plugins/                       插件实现和 manifest</span></span>
<span class="line"><span>├── plugin_data/                   插件运行数据和源码风格包</span></span>
<span class="line"><span>├── personal-space/&lt;user_id&gt;/      用户个人文件</span></span>
<span class="line"><span>│   ├── file/</span></span>
<span class="line"><span>│   ├── img/</span></span>
<span class="line"><span>│   ├── excel/</span></span>
<span class="line"><span>│   ├── word/</span></span>
<span class="line"><span>│   └── pdf/</span></span>
<span class="line"><span>├── images/                        非用户归属的全局图片</span></span>
<span class="line"><span>├── config/                        本地配置预留</span></span>
<span class="line"><span>├── dist/                          本地发布产物预留</span></span>
<span class="line"><span>└── skills/                        本地 Skill 数据预留</span></span></code></pre><div class="line-numbers-wrapper" aria-hidden="true"><span class="line-number">1</span><br><span class="line-number">2</span><br><span class="line-number">3</span><br><span class="line-number">4</span><br><span class="line-number">5</span><br><span class="line-number">6</span><br><span class="line-number">7</span><br><span class="line-number">8</span><br><span class="line-number">9</span><br><span class="line-number">10</span><br><span class="line-number">11</span><br><span class="line-number">12</span><br><span class="line-number">13</span><br></div></div><h2 id="插件代码与数据分离" tabindex="-1">插件代码与数据分离 <a class="header-anchor" href="#插件代码与数据分离" aria-label="Permalink to &quot;插件代码与数据分离&quot;">​</a></h2><p><code>data/plugins/&lt;plugin&gt;/</code> 只保存插件实现：</p><ul><li><code>plugin.yaml</code></li><li>插件 README</li><li><code>plugin.wasm</code> 或 gRPC 可执行入口</li><li>插件运行需要的静态资源</li></ul><p><code>data/plugin_data/&lt;plugin&gt;/</code> 保存运行后产生或可编辑的数据：</p><ul><li>SQLite/KV 文件</li><li>缓存和生成文件</li><li>页面风格包源码目录</li><li>插件自己的可恢复状态</li></ul><p>插件包导出不会自动包含 <code>plugin_data</code>。迁移插件时必须分别考虑代码包和运行数据。</p><h2 id="用户个人空间" tabindex="-1">用户个人空间 <a class="header-anchor" href="#用户个人空间" aria-label="Permalink to &quot;用户个人空间&quot;">​</a></h2><p>默认根目录为：</p><div class="language-text vp-adaptive-theme line-numbers-mode"><button title="Copy Code" class="copy"></button><span class="lang">text</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span>data/personal-space/&lt;user_id&gt;/</span></span></code></pre><div class="line-numbers-wrapper" aria-hidden="true"><span class="line-number">1</span><br></div></div><p>常见子目录：</p><table tabindex="0"><thead><tr><th>路径</th><th>内容</th></tr></thead><tbody><tr><td><code>img/avatars/</code></td><td>头像源文件，默认保留最近 3 个。</td></tr><tr><td><code>img/richtext/</code></td><td>富文本文章图片。</td></tr><tr><td><code>file/schedule/</code></td><td>个人课表索引和学期 JSON。</td></tr><tr><td><code>excel/</code>、<code>word/</code>、<code>pdf/</code></td><td>按文件类型分类的上传文件。</td></tr></tbody></table><p>默认个人空间配额为 10MB，由 <code>personal-space</code> 插件配置控制。头像、富文本图片和课表文件共同计入配额。</p><h2 id="页面风格包" tabindex="-1">页面风格包 <a class="header-anchor" href="#页面风格包" aria-label="Permalink to &quot;页面风格包&quot;">​</a></h2><p>可编辑源码风格包放在：</p><div class="language-text vp-adaptive-theme line-numbers-mode"><button title="Copy Code" class="copy"></button><span class="lang">text</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span>data/plugin_data/&lt;plugin&gt;/style-packs/&lt;pack&gt;/</span></span></code></pre><div class="line-numbers-wrapper" aria-hidden="true"><span class="line-number">1</span><br></div></div><p>当前三个目标目录：</p><div class="language-text vp-adaptive-theme line-numbers-mode"><button title="Copy Code" class="copy"></button><span class="lang">text</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span>data/plugin_data/personal-space/style-packs/      个人主页所有者风格</span></span>
<span class="line"><span>data/plugin_data/homepage-customizer/style-packs/ 管理员统一首页风格</span></span>
<span class="line"><span>data/plugin_data/web-theme/style-packs/           管理员提供的完整用户前台主题</span></span></code></pre><div class="line-numbers-wrapper" aria-hidden="true"><span class="line-number">1</span><br><span class="line-number">2</span><br><span class="line-number">3</span><br></div></div><p>典型结构：</p><div class="language-text vp-adaptive-theme line-numbers-mode"><button title="Copy Code" class="copy"></button><span class="lang">text</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span>style.yaml</span></span>
<span class="line"><span>README.md</span></span>
<span class="line"><span>preview.png</span></span>
<span class="line"><span>templates/</span></span>
<span class="line"><span>assets/</span></span>
<span class="line"><span>styles/</span></span>
<span class="line"><span>config.schema.json</span></span></code></pre><div class="line-numbers-wrapper" aria-hidden="true"><span class="line-number">1</span><br><span class="line-number">2</span><br><span class="line-number">3</span><br><span class="line-number">4</span><br><span class="line-number">5</span><br><span class="line-number">6</span><br><span class="line-number">7</span><br></div></div><p>风格包不应放入 <code>data/plugins</code>，因为它不是插件 Runtime 的实现代码。可选的 <code>effects/main.js</code> 仍属于风格包数据，但只能通过沙箱运行时执行；<code>effects/source.ts</code> 是不直接运行的开发源码。</p><h2 id="备份边界" tabindex="-1">备份边界 <a class="header-anchor" href="#备份边界" aria-label="Permalink to &quot;备份边界&quot;">​</a></h2><p>完整恢复至少需要同一时间点的：</p><ol><li>PostgreSQL 数据。</li><li><code>data/</code> 持久文件。</li><li><code>.env</code> 或等价的安全配置备份。</li></ol><p>只备份数据库会丢失用户头像、富文本图片和插件文件；只备份 <code>data/</code> 会丢失用户、帖子、授权和插件状态。</p></div>`);
}
const _sfc_setup = _sfc_main.setup;
_sfc_main.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("reference/data-layout.md");
  return _sfc_setup ? _sfc_setup(props, ctx) : void 0;
};
const dataLayout = /* @__PURE__ */ _export_sfc(_sfc_main, [["ssrRender", _sfc_ssrRender]]);
export {
  __pageData,
  dataLayout as default
};
