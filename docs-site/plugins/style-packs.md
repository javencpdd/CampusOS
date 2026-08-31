# 风格包、特效与 CampusStyleSDK

CampusOS 风格包不仅可以换颜色。通过目标内 CSS，它可以调整字体栈、字号、行高、字距、页面背景、页边距、内容宽度、网格、卡片和响应式布局；通过沙箱入口还可以提供 Canvas 动态特效。

## 两个系统主题版本

- `page-style-pack.v1 + target: web` 继续兼容，适合受控 CSS 和 Canvas 特效。
- `campusos.app-style-pack.v2 + target: web` 增加 ThemeRoot 和 App Shell Layout Recipe，可覆盖完整视口、背景、Header、导航、Hero、Footer、侧栏、主内容宽度、滚动和受控动画。

```yaml
schema_version: campusos.app-style-pack.v2
target: web
layout:
  mode: full
  content_width: 1240px
  header_mode: sticky
  scroll_mode: page
  background_asset: assets/campus.png
  animation_preset: reveal
viewport_support:
  desktop: true
  mobile: true
  mobile_breakpoint: 720px
```

背景资源必须在 `assets` 中声明。CSS 仍必须以 `.app-container[data-campusos-web]` 为根，不能使用任意全局 CSS 或同源 JavaScript。v2 包必须同时声明 PC/移动端支持并提供响应式宽度媒体查询。风格包可以排列插件 Surface，但不能修改 Action 的 API、权限、Method、参数、确认和审计。

## 作用范围

| 目标 | 谁提供、谁选择 | 页面范围 | CSS 根节点 |
| --- | --- | --- | --- |
| `personal-space` | 用户选择自己的主页包 | `/u/:username` 的完整主页内容 | `.public-space[data-campusos-space]` |
| `homepage` | 管理员统一设置 | 仅 `/`；当前只支持安全 HTML/CSS | `.home[data-campusos-home]` |
| `web` | 管理员提供，用户本地选择 | 所有用户前台页面，不含 Admin | `.app-container[data-campusos-web]` |

个人主页按主页所有者加载风格。访问用户 A 的主页时，所有访问者看到的都是 A 保存的风格；访问 B 的主页不会继承 A 的配置。

系统主题源码保存在：

```text
data/resources/themes/<theme>/
```

用户只能从管理员提供且筛查通过的目录中选择，不能从用户端安装系统主题。风格包是 Resource Package，图片、模板、CSS、预览和 schema 全部保存在 `data/resources`；`data/plugins` 与 `data/plugin_data` 只服务 External Plugin。

管理员可以直接维护资源目录，或使用 `campusosctl resource adopt/inspect` 生成并检查 `resource.json`。资源包导入不会启动进程，也不会绕过 Appearance 的路径、HTML/CSS、脚本沙箱、对比度和 checksum 审查。

## 完整目录

```text
style.yaml
README.md
preview.png
preview-desktop.png
preview-mobile.png
templates/
  page.html
  card.html
styles/
  theme.css
effects/
  main.js
  source.ts
assets/
  cover.webp
  avatar-frame.png
config.schema.json
```

## v0.13 双端交付合同

新建、重新导入或重新选择的系统主题、首页包和个人主页包必须声明：

```yaml
delivery_contract: campusos.appearance-delivery/v1
viewport_support:
  desktop: true
  mobile: true
  mobile_breakpoint: 720px
preview_images:
  desktop: preview-desktop.png
  mobile: preview-mobile.png
```

后端和 `campusosctl resource inspect/adopt` 会统一检查双端声明、断点响应式规则、两张预览、目标 CSS 作用域、配置绑定、颜色对比度以及动效的 `prefers-reduced-motion` 降级。历史包可以继续读取和导出，但会标为 `legacy-readonly`，不能绕过检查再次应用。

发布前的浏览器矩阵固定检查 `1440 x 1000`、`390 x 844` 与 `768 x 1024`。系统主题和个人主页包在用户端都有 Desktop/Mobile 分段预览；失败包不会显示可应用操作。完整操作说明见仓库中的 `docs/help/系统设计相关/v13风格包双端交付标准.md`。

`effects/` 是可选目录。TypeScript 仅作为开发源码，运行前必须编译为 `main.js`。

## 用户可配置参数

`config.schema.json` 可通过 `x-campusos-binding` 将控件绑定到已声明 token，或绑定到 `layout.background_asset`、`layout.overlay`、`layout.page_padding`、`layout.content_width`。用户在 `/appearance` 打开当前主题的“个性设置”，可以调整文字色、背景图、遮罩和间距；参数按用户与主题保存在本机，不会修改源包。

系统会拒绝 schema 引用未声明的 token 或图片资源。若 manifest 同时声明文字色和页面/卡片背景色，后端要求常规文字对比度至少为 4.5:1；用户端修改颜色时也会再次检查。图片背景应提供遮罩和高不透明内容表面，避免文字直接落在复杂图片上。

## 沙箱特效

```yaml
effect:
  runtime: sandbox-worker.v1
  entry: effects/main.js
  source: effects/source.ts
```

特效代码运行在无同源权限的 sandbox iframe Worker 中。它不能读取主页面 DOM、JWT、Cookie、浏览器存储，也不能发起任意网络请求。特效层只获得隔离 Canvas、尺寸、时间和归一化指针位置，并且不会接收点击。

```js
CampusEffect.register({
  frame(api) {
    api.clear()
    api.ctx.setTransform(api.dpr, 0, 0, api.dpr, 0, 0)
    api.ctx.fillStyle = 'rgba(21, 127, 91, 0.2)'
    api.ctx.fillRect(api.pointer.x * api.width, api.pointer.y * api.height, 4, 4)
  },
})
```

## 只读 SDK

风格包不能直接调用任意 `/api/v1` 地址。先在 manifest 声明能力：

```yaml
capabilities:
  - space.posts.read
```

再通过宿主桥调用：

```js
CampusEffect.register({
  start() {
    CampusEffect.request('space.posts.read', { limit: 10 })
      .then((result) => {
        // 使用宿主裁剪后的只读数据
      })
  },
})
```

## 权限差异

| 能力 | 个人主页包 | 管理员提供的 `web` 系统包 |
| --- | --- | --- |
| `space.profile.read` | 当前路由主页所有者的公开资料 | 不提供 |
| `space.posts.read` | 当前路由主页所有者的公开帖子摘要 | 不提供 |
| `community.threads.read` | 不提供 | 公开帖子摘要 |
| `categories.read` | 不提供 | 公开版块 |
| `schedule.me.read` | 只有主页所有者本人访问自己的主页时允许 | 当前登录用户明确授权后允许 |
| 写操作或任意 API | 禁止 | 禁止 |

管理员提供系统包不代表自动获得用户私有数据。用户仍需要单独同意课表等私有能力，风格包永远不会获得登录令牌。

## 示例

完整用户前台主题：

```text
data/resources/themes/campus-canvas/
```

全视口 v2 参考包：

```text
data/resources/themes/aurora-campus/
```

个人主页和同步帖子动态示例：

```text
data/resources/space-style-packs/kinetic-journal/
```
