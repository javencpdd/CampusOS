# 历史首个插件教程

> 更新时间：2026-09-23
> 状态：历史 v1-v3 教程，不适用于新插件。

本页原先描述在 `data/plugins/` 中创建 Wasm/进程插件的方式。该目录不再是 v4 源码发现入口，也不能代表当前的隔离 UI、三层授权和用户配置目录模型。

新开发者请直接阅读[以 PDF Viewer 学习第一个 v4 外部插件](/plugins/pdf-viewer-tutorial)。该教程以仓库实际的 `plugins/campusos.pdf-viewer` 为例，覆盖目录结构、Manifest、用户/Admin 页面、Bridge、配置、打包、安装及 Docker 验证。

需要维护历史 v1-v3 包时，请先阅读[兼容性与历史边界](/plugins/compatibility)，不要把历史包复制为新插件模板。
