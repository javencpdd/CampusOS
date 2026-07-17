# v12 验证码与 Ticket 安全设计

> 适用阶段：v0.12 A3-A4  
> 状态：Challenge、Ticket、持久限流、Core 邮件投递和验证式用户注册已实施。

## 1. 先理解三个对象

| 对象 | 用户何时看到 | 系统保存什么 | 用途 |
| --- | --- | --- | --- |
| Challenge | 申请验证邮件后得到挑战 ID | 用途、过期时间、随机 nonce、key ID、次数 | 标识一次验证请求。 |
| 六位 Code | 邮件发送后用户看到 | 不保存明文；由 HMAC 临时重建 | 证明用户收到邮件。 |
| Ticket | Code 验证成功后短暂返回 | 仅保存 digest | 让下一步注册、绑定或重置只执行一次。 |

Challenge ID 不是验证码。Ticket 也不是登录 Token，不能用于访问 `/api/v1`，只能连同正确用途和
邮箱完成一次后续身份命令。

## 2. 安全规则

1. Code 有 10 分钟有效期，最多尝试 5 次。
2. Ticket 默认 15 分钟有效，消费后立即失效。
3. Code 的用途严格隔离。注册 Code 不能用来重置密码，反过来也不行。
4. 每个邮箱一分钟只能申请一次、每天最多五次；同一个 IP 每小时最多十次。
5. 共享历史邮箱和保留身份标识不能申请 Challenge。
6. 服务日志、数据库、Outbox、管理端页面和导出文件都不应包含 Code、原始 Ticket、SMTP
   Secret、密码 hash 或原始 IP。

## 3. 配置密钥环

本地 `.env` 可使用示例中的 development 值。正式环境必须从 Secret 管理系统注入新的值：

```dotenv
AUTH_CHALLENGE_ACTIVE_KEY_ID=2026-07-v2
AUTH_CHALLENGE_HMAC_KEYS=2026-06-v1:<旧密钥>,2026-07-v2:<新密钥>
AUTH_CHALLENGE_IP_HASH_SECRET=<独立随机 Secret>
```

- `AUTH_CHALLENGE_ACTIVE_KEY_ID` 指向新 Challenge 使用的密钥。
- 旧 key 要至少保留到最后一条使用它的 Challenge 和 Ticket 过期后，才能删除。
- `AUTH_CHALLENGE_IP_HASH_SECRET` 不能与 JWT、Bootstrap Secret、SMTP 密码或第三方 API Key
  复用。
- 生产环境拒绝 development sentinel、空 key 或短 IP 哈希 Secret。

不要把这些值写进插件配置、风格包、版本控制、浏览器变量或数据库。

## 4. 运维排查

当用户反馈“没有收到验证码”时，先检查可靠任务是否存在和邮件 Provider 健康状态。不要让
客服要求用户提供六位 Code 或 Ticket，也不要从数据库中尝试查找它们。A4 已在管理端提供
脱敏 Provider 状态和投递错误；详细 SMTP 配置见[邮件投递与 SMTP 部署说明](v12邮件投递与SMTP部署说明.md)。

可运行：

```bash
make identity-challenge-check
make email-delivery-check
make database-check
```

如果迁移或限流异常，保留 Challenge ID、时间和请求 ID，使用受控日志排查。不要直接增加
`request_count`、清空 Ticket digest 或把 Challenge 改成已验证。

## 5. 给开发者的边界

- 内置邮件模块只使用 `identity.challenge-dispatch-reader`，获取一次内存中的投递投影。
- 任何插件、Agent、MCP、浏览器端代码或普通 Handler 都不能直接读取 Challenge Repository。
- 任何后续注册、绑定、重置操作都必须消费 Ticket，不能只相信前端传来的 `verified=true`。
- A5 的公开注册固定走 Challenge、Code、Ticket 三步。最后一步在一个可靠命令内创建 verified
  Account；旧一步式请求只收到 `identity.verification_required`，不会留下未验证账号。
- 需要增加 Purpose 时先修改 Domain、数据库约束、限流、邮件模板、Ticket 消费测试、API
  Contract 和文档；不能接受任意字符串。
