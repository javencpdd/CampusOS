# app-style-pack.v2 开发说明

## 1. 版本边界

`page-style-pack.v1` 和 `target: web` 继续兼容。需要控制完整浏览器视口和 App Shell 时，使用：

```yaml
schema_version: campusos.app-style-pack.v2
target: web
```

v2 可以设计 ThemeRoot、Header、导航、Hero、Footer、左右侧栏、主内容区、全宽/限宽、背景、字体、字距、间距、响应式和受控动画。它不能删除 PageOutlet、登录/用户菜单、安全提示和插件健康区域，也不能改变插件 API、权限、Method、参数、确认和审计。

每个 v2 包必须同时声明 PC 与移动端支持，并在 CSS 中提供宽度媒体查询：

```yaml
viewport_support:
  desktop: true
  mobile: true
  mobile_breakpoint: 720px
```

校验器会拒绝缺少该声明、只支持单一端或声明移动端却没有 `@media (max-width: ...)` / `@media (min-width: ...)` 的 v2 包。`page-style-pack.v1` 为兼容旧包只给出缺少声明的警告，但项目内置包均按双端标准验收。

## 2. 目录与布局

```text
data/plugin_data/web-theme/style-packs/<pack>/
├── style.yaml
├── README.md
├── preview.png
├── templates/page.html
├── templates/card.html
├── styles/theme.css
├── effects/main.js          # 可选 sandbox-worker.v1
├── assets/<image>
└── config.schema.json
```

```yaml
layout:
  mode: full                 # full | contained
  content_width: 1240px
  page_padding: 28px
  header_mode: sticky        # sticky | static | overlay
  scroll_mode: page          # page | content
  background_asset: assets/campus.png
  overlay: rgba(4, 22, 29, 0.30)
  animation_preset: reveal   # none | fade | reveal | parallax
  left_sidebar_width: 0px
  right_sidebar_width: 0px
```

背景必须同时在 `assets` 中声明，不能在 CSS 中使用任意 `url()`。CSS 选择器必须以 `.app-container[data-campusos-web]` 开头；该节点现在就是受 Core 控制的 ThemeRoot。

## 3. 可配置参数与可读性

风格包不是写死的 CSS。`config.schema.json` 可以把用户设置绑定到 manifest token 或受控布局字段：

```json
{
  "type": "object",
  "properties": {
    "text_color": {
      "type": "string",
      "title": "主要文字颜色",
      "format": "color",
      "default": "#17242a",
      "x-campusos-binding": "token.text_color"
    },
    "background_asset": {
      "type": "string",
      "title": "页面背景图",
      "enum": ["assets/campus.png", ""],
      "default": "assets/campus.png",
      "x-campusos-binding": "layout.background_asset"
    }
  }
}
```

支持的布局绑定为 `layout.overlay`、`layout.background_asset`、`layout.page_padding` 和 `layout.content_width`。背景图枚举只能引用 `style.yaml` 已声明且已筛查的资源。用户在 `/appearance` 的“个性设置”中修改参数，配置按当前用户与风格包保存在本机；不会改写管理员提供的源包。

若同时声明 `text_color`/`color.text` 与页面、卡片背景 token，后端会执行 WCAG 常规文字 4.5:1 基础对比度检查；用户端修改颜色时也会再次检查，不通过不能应用。图片背景应同时配置遮罩和近不透明内容表面，不能让正文直接叠在复杂图片上。

## 4. Surface Override

```yaml
surface_overrides:
  - surface_id: plugin.poll.page.list
    variant: glass-reading
    region: hero
    order: 10
```

Override 只表达位置和视觉变体。渲染回退顺序是：兼容 Override、Layout Recipe、插件默认 Renderer、Core Generic Renderer、标准不兼容 Surface。`page-outlet` 和 `safety` 是必要宿主区，不能被 override 替换。

## 5. 数据边界与示例

`data/plugins/web-theme/` 只保存插件 manifest、切换/读取实现说明和生命周期配置。所有风格包、图片、预览、模板、CSS、特效和 schema 都是数据，只能放在：

```text
data/plugin_data/web-theme/style-packs/<pack>/
```

`data/plugin_data/web-theme/style-packs/aurora-campus/` 是 v2 示例，包含项目自有校园背景、全视口阅读布局、响应式处理和隔离 Canvas 特效。它只参考沉浸式博客的页面层次与阅读组织，没有复制第三方代码或业务模型。
