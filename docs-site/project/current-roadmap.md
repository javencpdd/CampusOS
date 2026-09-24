# 当前规划与后续路线

> 更新时间：2026-09-25
> 当前统一版本：`v1.1.0-dev`；尚未宣布正式发布
> 当前代码主线：`v1.1-dev`；不等于 `v1.1 Final`

历史 v0.1–v0.14 计划已归档，v1.0 计划保留为插件三层授权与数据库设计的历史审计材料。当前正在以 v1.1 的插件自包含重构作为实际开发基线。

## v1.1 已落地的代码方向

- 图文文章附件与个人文档入口、个人文件/文章读者的下载权限；
- 根 `plugins/` 下的自包含 v4 插件源码、`.installed` 不可变发布发现；
- `campusos.pdf-viewer` 作为外部 PDF 预览插件，使用隔离 UI 和 Bridge；
- 管理员 Grant、用户 Consent、活动版本和资源 ACL 共同决定访问；
- 可信市场来源与用户按市场插件 ID 的申请流程；
- 用户添加插件时由宿主创建插件配置目录。

## 仍需证据或后续实施

- 完整浏览器/LAN/目标 Linux 验收、撤销和恢复演练、规模性能证据；
- 通用 Wasm/container Runtime、插件升级回滚和跨版本数据迁移；
- 自动下载/安装公共市场插件、Office/音视频预览、多节点生产能力。

完整范围与逐项状态在仓库的[项目计划书 v1.1](https://github.com/javencpdd/CampusOS/tree/main/docs/%E9%A1%B9%E7%9B%AE%E8%AE%A1%E5%88%92%E4%B9%A6v1/%E9%A1%B9%E7%9B%AE%E8%AE%A1%E5%88%92v1.1)和[进度记录](https://github.com/javencpdd/CampusOS/tree/main/docs/%E8%BF%9B%E5%BA%A6/v1.1-dev)中维护。开发者入口见[PDF Viewer 教程](/plugins/pdf-viewer-tutorial)。
