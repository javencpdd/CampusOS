# Agent 扩展安全合同 v0.7

v0.7 只提供合同，不提供 Agent Center、任务执行器或任意代码执行。

| 能力 | 形态 | 权限边界 |
| --- | --- | --- |
| Agent Core | Core Port | 授权和审计，不执行第三方代码 |
| Agent Runner | External Plugin | gRPC、Wasm 或 Remote HTTP；最小权限 |
| Knowledge Provider | External Plugin | 只返回声明范围的查询结果 |
| MCP Provider | External Plugin | 工具枚举和受控调用 |
| Skill/Prompt/Persona | Resource Package | 纯数据，无宿主能力 |

Runner Manifest 不能请求数据库连接、JWT 私钥、CampusOS 用户 Token 或完整宿主文件系统。允许的文件目录必须显式列出且不能是 `/`。

CampusOS Session、Access/Refresh Token、Host Token、JWT/API key、JWT 私钥和数据库凭据永不导出。第三方 Secret 只有在用户明确确认并使用加密 envelope 时合同才允许导出；v0.7 不实现该加密迁移。未知 Secret 类型默认拒绝。

代码合同位于 `internal/agentcontract`，负向测试覆盖宿主能力和凭据导出拒绝。
