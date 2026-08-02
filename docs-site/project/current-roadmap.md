# 当前规划与后续路线

> 基线：`v0.13.0`
> 状态：版本间候选路线，不是已经立项的 v14 承诺

v13 已完成当前计划内的业务、架构和可移植交付范围。下一版不应继续横向堆叠功能，而应先从有限主线中
选择范围、冻结基线，再建立可独立验收的阶段。

## P0 候选

### 生产交付认证

- 在真实 Windows Docker Desktop 和 Linux `arm64` Runner 上复验镜像、PowerShell、备份与恢复。
- 提供反向代理/TLS 部署示例，并保持证书生命周期在专用网关层。
- 增加镜像 SBOM、漏洞扫描、签名、来源证明和依赖更新规则。
- 在目标环境完成 SMTP、Prometheus 告警、备份保留和长稳测试。

### 文档与发布治理

- 为公开文档增加版本化和 release notes，避免历史页面被当作当前操作说明。
- 持续同步 OpenAPI、路由授权矩阵、Admin 只读架构和资源 checksum。
- 将 Windows/Linux/arm64 真实运行证据接入发行 Runner。

当前文档有效性和替代关系见 [文档状态与历史替代](/project/document-lifecycle)。该页面解决当前版本的
入口治理；版本选择器和 release notes 仍是下一阶段候选。

### 安全与运行

- 为 TOTP、SMTP、插件签名和第三方 Secret 提供可替换 Secret Provider。
- 为可靠 Worker、邮件、Webhook、数据库和 User Storage 建立目标环境 SLO。
- 保持最后管理员保护、Step-up、板块作用域、required audit 和事务 Outbox 的默认拒绝语义。

## P1 候选

| 方向 | 目标 | 前置边界 |
| --- | --- | --- |
| 标准 MCP | 正式协议 Server 和版本化 Tool/Resource | 权限、确认、审计、限流和敏感字段投影 |
| 进程扩展协议 | 真实 protobuf gRPC 或明确的新协议名 | 双版本兼容、超时、健康、签名和迁移 |
| 远程插件目录 | 下载、版本、审核和签名吊销 | 供应链扫描、透明审计和失败回滚 |
| 共享 User Storage | S3/OSS/WebDAV Provider | 所有权、配额、幂等、加密和多实例一致性 |
| 身份增强 | OAuth/OIDC、Passkey、更多邮件 Provider | 账号合并、恢复、Step-up 和历史兼容 |
| 通知增强 | SSE/WebSocket 或外部邮件通知 | 轮询兼容、离线状态、偏好和限流 |
| 第三方 Adapter | Discord、OneBot 等生产适配器 | 独立插件、最小权限、重试和审计 |

## P2 候选

- 多节点 API/Worker、高可用数据库和分布式 Session/限流。
- 隔离 Agent Runner、Knowledge Provider、Prompt/Persona 管理和代码执行审批。
- 公共插件市场的社区审核、评分、支付和恶意包分析。
- 个人云盘、跨插件统一搜索和向量检索。

这些能力在缺少威胁模型、隔离、资源配额、审计和恢复方案时不得提前开放。

## 下一版如何立项

1. 冻结当前代码、42 个 migration、路由/OpenAPI、Module/Runtime Manifest 和数据目录。
2. 在目标平台运行完整发布门禁并记录环境。
3. 只选择一条主要产品或架构主线，明确非目标。
4. 为每阶段定义代码目录、数据迁移、兼容、回滚、负向测试和进度证据。
5. 创建正式计划后才形成版本承诺；本页只提供候选优先级。

## 不应回退的边界

- 不把模块化单体拆成无必要的微服务。
- 不把 Built-in Feature 或 Resource Package 放回普通插件生命周期。
- External Plugin 和 Agent Runner 不得取得核心数据库、JWT 私钥、用户 Token 或完整宿主文件系统。
- Feature 停用、Plugin 卸载和资源切换不得隐式删除用户数据。
- 不为了通过测试删除权限、审计、清洗、路径、配额、事务或数据库约束。

相关页面：[版本演进](/project/version-evolution)、[文档状态与历史替代](/project/document-lifecycle)、
[完整入门路径](/guide/getting-started)、[构建与发布](/deployment/release)。
