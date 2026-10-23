# 当前规划与后续路线

> 更新时间：2026-10-19
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
- 插件Windows Container目标验收按用户要求延期（Linux范围已验收）；Linux插件升级回滚和跨版本数据迁移继续按依赖实施；
- 自动下载/安装公共市场插件、Office/音视频预览、多节点生产能力。

完整范围与逐项状态在仓库的[项目计划书 v1.1](https://github.com/javencpdd/CampusOS/tree/main/docs/%E9%A1%B9%E7%9B%AE%E8%AE%A1%E5%88%92%E4%B9%A6v1/%E9%A1%B9%E7%9B%AE%E8%AE%A1%E5%88%92v1.1)和[进度记录](https://github.com/javencpdd/CampusOS/tree/main/docs/%E8%BF%9B%E5%BA%A6/v1.1-dev)中维护。开发者入口见[PDF Viewer 教程](/plugins/pdf-viewer-tutorial)。

## v1.2 当前阶段

更新时间：2026-10-19（Asia/Shanghai）。G0/G1、01a/01b、02a/02d/02b/02c、03a/03b/03c与04已按各自范围退出；05提供 Linux 原生宿主的固定 worker Wasm 执行及实际资源/生命周期验收，配置见[Wasm 执行器](/deployment/configuration#v12-05-wasm-执行器)。v4源码自动发现与旧控制入口已退役，以上v1.1段落仅记录历史代码方向。当前权威状态为仓库v1.2主计划、`.123`与`v12-05-runtime-exit-linux.json`；Container/Windows 为06，作者SDK为08a，内建AI/RAG绑定为14c，尚非v1.2 Final。

V12-06当前Linux Container的有限HTTP/gRPC、硬隔离与受管生命周期见[Container执行器](/deployment/configuration#v12-06-container-执行器)、仓库`.127`和`v12-06-runtime-portable-linux.json`。Linux实现/自动化/目标已通过；2026-10-19用户同意Windows相关内容延期（`.128`），Windows仍未验收、完整跨平台06尚未退出。后续Linux阶段按Linux依赖继续推进；本轮仅同步范围，07未实施。以上`.123`段落保留05当时边界。
