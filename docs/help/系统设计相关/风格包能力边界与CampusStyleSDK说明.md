# CampusOS 风格包能力边界与 CampusStyleSDK 说明

> 适用基线：`v0.6.4-dev`
> 更新时间：2026-07-11

## 1. 为什么先定义边界

风格包需要有足够自由度，但不能因此获得用户登录令牌、任意读取私密数据、修改帖子或在主页面执行不受控脚本。CampusOS 将“页面外观”“沙箱特效”和“业务数据调用”拆成三层：

1. CSS 负责字体、字号、字距、页面背景、页边距、布局、卡片、响应式和 CSS 动画。
2. `sandbox-worker.v1` 负责 Canvas 动态特效和指针响应，不进入主页面执行。
3. CampusStyleSDK 负责经过权限检查的只读数据调用，不向风格包提供 JWT、Axios 或任意 URL。

## 2. 三类风格包

| `target` | 提供者与选择者 | 实际范围 | 强制 CSS 根选择器 |
| --- | --- | --- | --- |
| `personal-space` | 管理员提供内置包；个人用户选择自己的包或上传个人包 | `/u/:username` 中的完整个人主页内容，包括资料头部、头像、元信息、自定义模板、帖子列表和空状态 | `.public-space[data-campusos-space]` |
| `homepage` | 管理员通过 `homepage-customizer` 统一配置 | 仅用户前台 `/` 首页；当前只支持安全 HTML/CSS，不加载脚本或 SDK | `.home[data-campusos-home]` |
| `web` | 后端管理员提供，用户从已提供目录中本地选择 | 整个用户前台，包括首页、帖子、课表、登录、注册、个人主页外壳等；不进入 Admin 管理端 | `.app-container[data-campusos-web]` |

个人主页的风格归主页所有者，不归访问者。访问者打开用户 A 的 `/u/A` 时，后端按 A 的用户 ID 读取 `user_spaces.style_manifest`；无论访问者是谁，看到的都是 A 已应用的主页风格。访问用户 B 时不会继承 A 的风格。

系统主题由 `web-theme` 系统级插件提供。管理员把通过筛查的源码包放在：

```text
data/plugin_data/web-theme/style-packs/<theme>/
```

用户只能选择目录中筛查通过的包，不能从用户端安装系统主题。选择按用户 ID 保存在浏览器本地，切换立即生效。`web-theme` 插件本身的启停属于系统插件生命周期，重启 API 后才生效。

管理员当前有两种供给方式：

1. 直接编写或复制 `data/plugin_data/web-theme/style-packs/<theme>/`，由目录 API 每次重新筛查。
2. 下载随系统插件发布的主题提供包，审查后把插件实现部署到 `data/plugins/`、把风格数据部署到对应 `data/plugin_data/`，再重启 API。

当前 Admin 在线导入只接受可热更新的用户级插件，按既有安全规则拒绝系统级插件运行时导入。因此“下载系统主题插件”目前是受控部署操作，不是普通用户或管理员页面中的免重启市场安装。后续可增加专门的系统主题上传、版本快照和回滚流程。

## 3. CSS 能做什么

允许：

- 设置系统字体栈、字号、行高、字距和文本层级。
- 修改目标页面背景、表面颜色、边框、阴影和响应式断点。
- 调整页面最大宽度、内外边距、网格、Flex 布局和内容密度。
- 重设个人主页资料区、头像、标签、帖子卡片和空状态的完整外观。
- 使用 `@keyframes`、`@media`、`@supports`、`@container` 和 `@layer` 中的目标内规则。
- 为 `prefers-reduced-motion` 提供关闭动画的降级规则。

禁止：

- 选择器逃出目标根节点，例如个人主页包修改 `.app-header`。
- `@import`、`url()`、`javascript:`、`data:`、CSS expression 和浏览器绑定行为。
- `position: fixed` 或 `position: sticky` 等可能遮挡系统交互的规则。
- 未声明、未筛查或超出大小限制的文件。

所有普通选择器以及响应式规则内部的选择器都必须从对应根选择器开始。这样个人用户编写的主页包无法修改导航栏、其他用户页面或 Admin。

## 4. JS/TS 特效边界

风格包可以声明：

```yaml
effect:
  runtime: sandbox-worker.v1
  entry: effects/main.js
  source: effects/source.ts
```

`effects/source.ts` 是可选开发源码，浏览器不会直接执行 TypeScript。开发者必须把它编译为 `effects/main.js`，系统只运行筛查后的 JavaScript 入口。

运行隔离：

- 特效代码运行在 sandbox iframe 内的 Worker，不在 CampusOS 主页面执行。
- iframe 没有 `allow-same-origin`，并通过 CSP 禁止任意网络连接。
- Worker 不能读取主页面 DOM、Cookie、JWT、`localStorage`、`sessionStorage` 或 IndexedDB。
- 主页面只提供隔离 Canvas、尺寸、时间、像素比和归一化指针坐标。
- 特效层不接收点击，不得覆盖按钮或伪造登录交互。
- 单个入口最大 32 KiB；检测到网络、存储、DOM、父窗口、动态代码或 WebAssembly 能力时拒绝应用。
- 用户开启“减少动态效果”时，不创建特效运行环境。
- Worker 超过心跳时间会被宿主终止。

最小入口：

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

## 5. CampusStyleSDK 调用方式

风格包不能直接执行 `fetch('/api/v1/...')`。它先在 `style.yaml` 声明能力：

```yaml
capabilities:
  - space.posts.read
  - schedule.me.read
```

脚本通过沙箱 API 请求：

```js
CampusEffect.register({
  start() {
    CampusEffect.request('space.posts.read', { limit: 10 })
      .then((result) => {
        // result 是宿主裁剪后的只读快照
      })
  },
})
```

宿主依次检查：包是否声明能力、`target` 是否允许、当前用户是否登录、页面所有者是否匹配、用户是否授予私有能力，然后才代为调用 CampusOS API。

## 6. 系统级与用户级权限矩阵

| 能力 | `personal-space` 用户主页包 | `web` 管理员提供包 |
| --- | --- | --- |
| `space.profile.read` | 允许，只返回当前路由主页所有者的公开资料 | 不提供 |
| `space.posts.read` | 允许，只返回当前路由主页所有者已公开同步的帖子摘要，最多 20 条 | 不提供 |
| `community.threads.read` | 不提供 | 允许，只返回公开帖子摘要，数量受宿主限制 |
| `categories.read` | 不提供 | 允许，只返回公开版块信息 |
| `schedule.me.read` | 仅主页所有者本人访问自己的主页时允许；其他访问者拒绝 | 仅登录用户明确授权当前主题后允许，返回当前登录用户自己的课表 |
| 任意写操作 | 禁止 | 禁止 |
| 任意 API 路径 | 禁止 | 禁止 |
| JWT/API Key | 不提供 | 不提供 |

系统级包由管理员提供，不等于自动获得用户私有数据。管理员提供包可以声明 `schedule.me.read`，但每个用户仍要单独同意。用户撤销本地选择或切换主题后，该主题不再具有授权。

## 7. 示例包

完整用户前台系统主题：

```text
data/plugin_data/web-theme/style-packs/campus-canvas/
```

完整个人主页和帖子卡片动态示例：

```text
data/plugin_data/personal-space/style-packs/kinetic-journal/
```

`kinetic-journal` 会设计主页资料区、头像、元信息、介绍模板、同步帖子、标签和空状态，并通过 `space.posts.read` 调节沙箱背景节点数量。它不会读取访问者身份数据，也不会跨用户调用接口。

## 8. 验收要求

一个带特效或 SDK 的包只有同时满足以下条件才能应用：

1. `style.yaml`、HTML、CSS、JS、可选 TS 源码、图片和 Schema 路径全部合法。
2. 所有 CSS 选择器都在目标根节点内。
3. HTML 通过安全标签、属性和 URL 检查。
4. JS 入口通过能力静态检查并使用 `CampusEffect.register(...)`。
5. SDK 能力属于目标允许列表。
6. 私有能力得到当前用户授权且运行时对象归属检查通过。
7. 桌面和移动端布局、减少动态效果模式、切换和回退均可用。
