# 插件兼容性与历史边界

> 更新时间：2026-10-19

| 范围 | 当前约定 | 新开发是否应使用 |
| --- | --- | --- |
| v5 签名包 | `campusos.plugin/v5`，当前不可变声明、分域安装和受限 UI Bridge | 是 |
| v4 源码发现/旧控制入口 | 已退役，不自动构建/执行 | 否 |
| 当前示例 | `campusos.pdf-viewer` `3.0.0-dev.2`，`runtime: none` | 是 |
| v1-v3 Manifest/Host API/数据目录 | 兼容读取或历史资料 | 否 |
| 有限 ABI Wasm | V12-05 Linux 固定宿主 worker，显式配置及当前授权 | 遵循当前 ABI |
| 有限管道 Container | 06 Linux实现与目标证据见`.125`，显式启用；Windows目标仍待验收 | 仅Linux当前合同 |
| 任意监听服务/WASI | 不会自动适配Container管道或Wasm有限ABI，作者SDK属08a | 否 |

当前统一版本为 `v1.1.0-dev`，对应 `v1.1-dev` 插件自包含重构。应用版本不等于 Manifest、Host API 或 Bridge 协议版本，也不是 v1.1 Final 声明。

v5 Manifest 声明 `host_contracts` 与版本化贡献/能力/配置。变化能力、用途、风险、资源类型或发布内容时必须发布新版本并重新经历管理员治理；不能在同一已发布版本中静默扩大权限。

旧教程与旧目录仅用于维护既有数据，不能成为新插件的脚手架。请从[PDF Viewer 实战](/plugins/pdf-viewer-tutorial)和[v5 Manifest](/plugins/manifest)开始。
