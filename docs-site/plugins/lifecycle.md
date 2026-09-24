# 插件生命周期：v4 运行约定

> 更新时间：2026-09-23

v4 的发布与运行边界如下：

1. 开发源码位于 `plugins/<key>/`，不得直接作为运行来源。
2. 预检、打包、验签后写入 `plugins/.installed/<key>/<version>/`；切换活动版本必须原子完成。
3. 管理员安装、发布、启用/禁用并治理能力；用户仅能添加已发布插件并同意本人数据访问。
4. API 与 Gateway 每次 Bridge 调用均检查活动版本、管理员 Grant、用户 Consent（需要时）和业务资源 ACL。
5. 禁用、撤销或卸载不会把已授权资源变成公开资源；插件自有配置仍由宿主保留、清理或恢复。

当前 `campusos.pdf-viewer` 为 `runtime: none` 的 UI 插件。Manifest 可枚举 Wasm/container Runtime，但通用生产执行、跨版本迁移与回滚流程仍需后续交付，不应据此假设可直接投产。

参见[打包、安装与更新](/plugins/package-import)和[目录与用户数据](/plugins/market-managed-data)。
