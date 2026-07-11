# app-style-pack.v2 开发说明

## 1. 版本边界

`page-style-pack.v1` 和 `target: web` 继续兼容。需要控制完整浏览器视口和 App Shell 时，使用：

```yaml
schema_version: campusos.app-style-pack.v2
target: web
```

v2 可以设计 ThemeRoot、Header、导航、Hero、Footer、左右侧栏、主内容区、全宽/限宽、背景、字体、字距、间距、响应式和受控动画。它不能删除 PageOutlet、登录/用户菜单、安全提示和插件健康区域，也不能改变插件 API、权限、Method、参数、确认和审计。

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

## 3. Surface Override

```yaml
surface_overrides:
  - surface_id: plugin.poll.page.list
    variant: glass-reading
    region: hero
    order: 10
```

Override 只表达位置和视觉变体。渲染回退顺序是：兼容 Override、Layout Recipe、插件默认 Renderer、Core Generic Renderer、标准不兼容 Surface。`page-outlet` 和 `safety` 是必要宿主区，不能被 override 替换。

## 4. 示例

`data/plugin_data/web-theme/style-packs/aurora-campus/` 是 v2 示例，包含项目自有校园背景、全视口阅读布局、响应式处理和隔离 Canvas 特效。它只参考沉浸式博客的页面层次与阅读组织，没有复制第三方代码或业务模型。
