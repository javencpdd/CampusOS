# Plugin UI v2：历史兼容说明

> 更新时间：2026-09-23
> 状态：仅供维护旧插件；新插件不得使用。

`campusos.ui/v2` 曾用于声明式 Surface。当前 v4 外部插件统一使用 `campusos.ui/v3` 与 `campusos.bridge/v1`：独立前端产物在 Plugin UI Gateway 下以隔离 iframe 加载，宿主负责路由、会话、授权与资源读取。

已移除的 `builtin.pdf-viewer` 不能安装、展示或授权。唯一的当前 PDF 预览实现是自包含外部插件 `campusos.pdf-viewer`。

新开发请阅读：[PDF Viewer 教程](/plugins/pdf-viewer-tutorial)、[隔离 UI 与 Bridge](/plugins/frontend-runtime)、[v4 Manifest](/plugins/manifest)。
