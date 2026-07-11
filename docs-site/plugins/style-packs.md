# 风格包、特效与 CampusStyleSDK

CampusOS 风格包不仅可以换颜色。通过目标内 CSS，它可以调整字体栈、字号、行高、字距、页面背景、页边距、内容宽度、网格、卡片和响应式布局；通过沙箱入口还可以提供 Canvas 动态特效。

## 作用范围

| 目标 | 谁提供、谁选择 | 页面范围 | CSS 根节点 |
| --- | --- | --- | --- |
| `personal-space` | 用户选择自己的主页包 | `/u/:username` 的完整主页内容 | `.public-space[data-campusos-space]` |
| `homepage` | 管理员统一设置 | 仅 `/`；当前只支持安全 HTML/CSS | `.home[data-campusos-home]` |
| `web` | 管理员提供，用户本地选择 | 所有用户前台页面，不含 Admin | `.app-container[data-campusos-web]` |

个人主页按主页所有者加载风格。访问用户 A 的主页时，所有访问者看到的都是 A 保存的风格；访问 B 的主页不会继承 A 的配置。

系统主题源码保存在：

```text
data/plugin_data/web-theme/style-packs/<theme>/
```

用户只能从管理员提供且筛查通过的目录中选择，不能从用户端安装系统主题。

管理员可以直接维护该目录，也可以把下载的系统主题提供插件按部署包拆分到 `data/plugins/` 和对应 `data/plugin_data/` 后重启 API。当前在线插件导入只支持可热更新的用户级插件，不会绕过系统插件的重启和代码审查边界。

## 完整目录

```text
style.yaml
README.md
preview.png
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

`effects/` 是可选目录。TypeScript 仅作为开发源码，运行前必须编译为 `main.js`。

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
data/plugin_data/web-theme/style-packs/campus-canvas/
```

个人主页和同步帖子动态示例：

```text
data/plugin_data/personal-space/style-packs/kinetic-journal/
```
