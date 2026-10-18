# v5 外部插件体系

> 更新时间：2026-10-18（Asia/Shanghai）。当前 v1.2 按阶段验收，真实后端 Runner 仍由 V12-05/06 实施。

| 目标 | 类型 | 边界 |
| --- | --- | --- |
| 身份、权限、社区、User Storage | Core Module | 随主程序运行 |
| 课表、个人空间、富文本、内置 AI/Knowledge | Built-in Feature | 编译装配，按各业务生命周期管理 |
| 独立发布/安装/授权的扩展 | External Plugin | `campusos.plugin/v5`，显式签名导入 |
| 主题、主页样式、Prompt、Skill | Resource Package | 数据资源，不执行插件代码 |

源码位于 `plugins/<key>/`，受管签名包和操作状态由宿主维护；不从源码或旧 `.installed` 自动发现、构建和执行。配置保存在宿主 PostgreSQL，按版本和 owner 分域。安装不运行 shell/npm/go 钩子，不允许插件连接平台数据库、读取宿主目录、Token 或环境密钥。

管理员在 `/plugins` 导入包和独立签名文件，分别安装、按版本批准业务能力/运行资源、启用。用户在自己的 `/plugins` 安装已获批版本，确认当前用途并启用。配置页由不可变声明生成；普通值和 Secret 引用分别提交。撤权/到期后本人安装仍可卸载。

PDF Viewer `3.0.0-dev.2` 是 `runtime: none` 的 UI-only 示例：文章附件、个人资源和个人文档的 PDF 通过宿主选择的有限资源句柄预览。PC/移动端提供弹窗、侧边、全屏、新标签页；没有插件仍可下载。Echo 是后端包合同示例，不能把通过包检查视作真实 Runner 已交付。

UI 在每个当前证明的独立子域加载，由有限 MessagePort 读取普通配置和已选择 PDF。授权、原业务 ACL 和资源修订始终由宿主复核。Wasm/Container 的 CPU/内存/跨进程隔离与恢复验收不由 UI-only 运行替代。

继续阅读：[Manifest 与配置](/plugins/manifest)、[隔离 UI 与 Bridge](/plugins/frontend-runtime)、[授权与资源访问](/plugins/authorization-v3)、签名包与管理操作（仓库 `docs/help/插件v5包与签名操作说明.md`）。旧 v4 教程仅供历史参考。
