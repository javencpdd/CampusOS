# 集成中心使用与能力边界

管理后台的“集成中心”把当前可配置的低风险集成放在一起展示。它不是插件市场，也不会为未实现的第三方平台提供虚假的“已连接”状态。

## Webhook

**成熟度：持久投递、可配置、可测试。**

Webhook 会把已提交的 CampusOS 事件经持久队列投递到管理员配置的 HTTP 地址。适合由你自己维护的业务服务接收事件，例如同步校园公告、触发内部通知或记录运营数据。它采用至少一次语义，接收方必须按事件 ID 幂等。

使用步骤：

1. 在“Webhook”页填写名称、可访问的 HTTP URL 和事件列表。
2. 保存后点击“测试”，检查目标服务是否收到请求。测试同样经过安全 egress 检查，但不是持久任务。
3. 在投递记录与“可靠任务”中检查状态；确认目标服务已经验证 v1 签名、timestamp、事件 ID 幂等和重试后再长期启用。

Webhook 目标地址由管理员负责。默认拒绝内网、loopback、link-local、危险 redirect 和 DNS rebinding；不要将携带 CampusOS Token 的 URL 或不可信重定向地址作为目标。CampusOS 不会把数据库连接、JWT 私钥或用户 Session 发送给 Webhook。详细排障见 [可靠任务与 Webhook](/operations/reliable-tasks)。

## MCP-like

**成熟度：内部只读验证。**

当前页面中的 MCP 工具用于验证 CampusOS 内部的受控只读工具调用和审计。它不是标准 MCP Server：外部 MCP 客户端不能用此页面或这些 HTTP 路由进行协议协商、发现和远程调用。

- 工具保持只读；不开放删除、发布、授权或任意代码执行。
- 每次调用应在 MCP 审计中可追踪。
- 标准 MCP Server、远程 Provider 和第三方客户端接入需要后续独立 Runtime、认证、传输和兼容性测试。

## Message Local

**成熟度：本地验证。**

Message Local 用来模拟消息进入 CampusOS 并查看本地处理记录。它适合开发和合同测试，例如验证会话、发送者、文本内容和本地响应是否符合预期。

它不是 Discord、OneBot、微信或其他第三方平台生产适配器。接入真实平台时，应通过外部 Adapter 插件实现平台认证、事件确认、速率限制、重试、隐私配置和审计，不能把第三方 Token 写入普通页面配置或资源包。

## 排查顺序

1. 确认当前管理员拥有对应的 `webhook:*`、`mcp:*` 或 `message:*` 权限。
2. 确认 Built-in Feature 未被停用，并检查概览卡片状态。
3. Webhook 先执行测试，再查看投递记录和目标服务日志。
4. MCP-like 只用于当前列出的只读工具；不要把成功调用理解成标准 MCP 已部署。
5. Message Local 仅用于本地合同验证；真实平台需求应选择受审计的外部 Adapter。

相关 API、权限和错误契约以 [当前 API 契约](/api/contracts) 为准。
