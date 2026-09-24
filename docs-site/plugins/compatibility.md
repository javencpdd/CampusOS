# 插件兼容性与历史边界

> 更新时间：2026-09-25

| 范围 | 当前约定 | 新开发是否应使用 |
| --- | --- | --- |
| v4 自包含插件 | `campusos.plugin/v4` + `campusos.ui/v3` + `campusos.bridge/v1` | 是 |
| 当前示例 | `campusos.pdf-viewer` `2.0.0-dev.2`，`runtime: none` | 是 |
| v1-v3 Manifest/Host API/数据目录 | 兼容读取或历史资料 | 否 |
| 任意 Wasm/container Runtime | Manifest 枚举值存在，通用生产执行链未作为本期交付 | 否 |

当前统一版本为 `v1.1.0-dev`，对应 `v1.1-dev` 插件自包含重构。应用版本不等于 Manifest、Host API 或 Bridge 协议版本，也不是 v1.1 Final 声明。

v4 Manifest 应声明 `compatibility.host`、UI 协议和 Bridge 协议。变化能力、用途、风险、资源类型或发布内容时必须发布新版本并重新经历管理员治理；不能在同一已发布版本中静默扩大权限。

旧教程与旧目录仅用于维护既有数据，不能成为新插件的脚手架。请从[PDF Viewer 实战](/plugins/pdf-viewer-tutorial)和[v4 Manifest](/plugins/manifest)开始。
