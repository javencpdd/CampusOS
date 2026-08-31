# 当前规划与后续路线

> 当前发布基线：`v0.13.0`
> 更新日期：2026-08-31
> 状态：项目所有者已确认 v0.14-dev 当前代码验证完成并完成开发收尾；AcademicTerm、对象账本、课表 Object 引用/历史采用命令、个人文档 MVP、共享内容编辑安全核心、版本提交 Outbox 与安全 Converter 降级均已实现。Windows Docker 的全量 Go、隔离 migration/恢复和认证浏览器工作流已有实际证据，Final 发布门禁尚未通过

CampusOS v0.13 已完成模块化单体、可信账号、可靠任务、可观测、响应式、内容治理和 Windows/Linux
单主机 Docker 交付。v0.14 选择“学期治理与个人工作区基础”作为有限主线，不改变当前四类能力和单主机边界。

仓库中的完整计划位于 `docs/项目计划书v0/项目计划v0.14/00-v0.14版本计划书.md`。本文是公开摘要；计划不等于功能已经上线，
实际完成情况必须以 `docs/进度/v0.14-dev/`、运行代码和发布证据为准；最新开发收尾记录为
`v0.14.11-dev.md`。

## v0.14 正式范围

| 主线 | 目标 | 关键保护 |
| --- | --- | --- |
| G0 基线 | 历史 `000043` 事实快照与隔离 down/up 已保留；当前采集器已可分别记录 Schema/OpenAPI/Route/Permission/Module hash 与 bundle 预算 | 早期 `v14-g0.json` 是 v1 历史证据，未含后续补充的拆分 hash/bundle 字段；Final 前须在受控发布工作树重新采集并审阅 v2 快照，不能把当前 v0.14 工作树输出回填成历史事实 |
| AcademicTerm | 已交付管理员 spring/fall、open/closed/default、第一周和版本控制 | `000044` CHECK、单默认、版本冲突、管理 API 与 Admin 控制台 |
| Schedule | 已交付服务端受管学期 Guard、旧 JSON + immutable Object 双写及历史采用命令；关闭学期仍可读取 | `000046`/`000049` 记录用户/学期、当前对象、第一周和偏好；旧 JSON 不重命名 |
| User Storage Object | 已交付 Object ID、owner、metadata、Quota Reservation、Local Provider 原子写与默认只读 reconcile | `000045` 对象/账户/预留账本；对账使用 keyset 批次和可恢复 checkpoint，详细差异有界采样且采样截断时拒绝 apply。低基数指标只输出固定聚合键，受审计 apply 只收敛过期 Reservation 和缺物理文件 metadata，未知文件不自动改写 |
| Personal Documents | 已交付私有文档、版本、回收站、TXT/Markdown/CampusDoc 编辑与服务端安全预览、PDF/DOCX 认证下载 | `000047` 文档/版本/预览状态；每次版本提交与最小化 `document.preview.requested.v1` Outbox 事件同事务完成。未启用受审查 Converter Runner 时，内置安全降级消费者只校验最小 payload 并写 receipt，不读取内容或调用外部转换器。共享 `core.content-editor` 转义 Markdown 原始 HTML、约束 CampusDoc v1 图片 Object ID；受信 ReadPort 不进入插件 Host，跨用户统一 404 |
| Preview | 复杂转换只允许在可选隔离 Converter Runner 执行 | 示例 Compose 强制无网络、非 root、只读根文件系统和资源限制；没有受审查 Runner 时，DOCX/PDF 保持上传/下载，预览状态明确为 `converter_unavailable` |

## 实施顺序

```text
G0 真实基线
  ├─> AcademicTerm -> Admin/Term Guard -> Schedule 历史迁移
  └─> Storage Object -> Documents/Version -> Preview/Converter
                                          └─> 恢复与 Final
```

标准团队按 8 周规划；小团队按 10–12 周。Storage 并发配额、历史课表保护、文档版本和恢复是硬门；
DOCX Preview 可以在隔离条件不足时降级为上传与下载。

## 迁移与兼容

- 当前 migration 为 `000001-000049`；`000048` 无损统一早期对象账本的约束名称，`000049` 增加课表对象绑定和查看偏好。生产回滚应关闭 Feature 并 forward-fix，不能用 down 删除用户对象或文档。
- 文件迁移采用 `shadow -> dual -> enforce`，旧头像、图片、RichText 资产和课表先盘点再登记。
- 未知文件只报告或隔离，不在启动时自动删除。
- 旧 `year + semester` 请求在兼容期只能解析为已存在 AcademicTerm，不能绕过 Guard 创建学期。
- Feature Disable、Converter Disable 和紧急回退均保留对象、课表、文档与版本。

## v0.14 非目标

- 多人协作文档、分享链接、跨用户 ACL、完整 Office/PDF 编辑。
- S3/OSS/WebDAV 生产 Provider、云盘同步、全文/向量搜索和 AI 文档能力。
- 标准 MCP、真实 protobuf gRPC、远程插件市场、Agent Runner。
- 多节点 API/Worker、数据库 HA、自动 TLS。

这些方向仍需独立威胁模型、资源隔离、迁移、回滚和目标环境证据，不能从 v0.14 的接口预留推断为已交付。仓库内的逐项代码审查、验证证据和 Final 未决项见 `docs/项目计划书v0/项目计划v0.14/01-v0.14计划逐项审查与项目回顾.md`。

## v1.1 及以后候选

- 真实 Windows/arm64 发行认证、SBOM、镜像签名和目标环境长稳。
- S3/OSS/WebDAV Provider 与多实例对象/配额一致性。
- OAuth/OIDC、Passkey、实时通知。
- 标准 MCP/进程扩展协议、远程插件目录。
- 隔离 Agent Runner 和多节点高可用。

相关页面：[版本演进](/project/version-evolution)、[文档状态与历史替代](/project/document-lifecycle)、
[完整入门路径](/guide/getting-started)、[构建与发布](/deployment/release)。
