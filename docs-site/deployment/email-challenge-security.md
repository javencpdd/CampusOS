# 验证码与 Ticket 安全

v0.12 的 Challenge 是验证邮件的后台安全基础。当前已经有 Code 重建、一次性 Ticket、
持久限流、可靠 Outbox、Core SMTP/Fake Consumer 和验证式注册页面。

## 不会保存什么

CampusOS 不会在数据库、Outbox 或日志保存：

- 六位验证码；
- 原始 Ticket；
- SMTP Secret；
- 原始 IP；
- 用户密码或 password hash。

验证码由密钥环和 Challenge 的随机 nonce 临时重建；Ticket 仅保存摘要。

## 部署配置

生产环境通过 Secret 管理系统设置：

```dotenv
AUTH_CHALLENGE_ACTIVE_KEY_ID=2026-07-v2
AUTH_CHALLENGE_HMAC_KEYS=2026-06-v1:<旧密钥>,2026-07-v2:<新密钥>
AUTH_CHALLENGE_IP_HASH_SECRET=<独立随机 Secret>
```

旧密钥要保留到使用它的验证码和 Ticket 全部过期。不要将这三个值提交到仓库，或复用 JWT、
管理员 Bootstrap、SMTP、第三方平台 Secret。

## 默认限制

- 每邮箱任意连续十分钟最多五次；
- 每 IP 任意连续六十分钟最多十次；
- Code 十分钟、最多五次错误；
- Ticket 十五分钟、一次消费。

邮箱和 IP 窗口可由管理员在“验证码策略”中热更新，限流本身不能关闭。操作步骤和权限见
[验证码策略配置](/guide/challenge-policy)。

完整运维说明见仓库的
`docs/help/系统设计相关/v0.12验证码与Ticket安全设计.md`。

SMTP 配置、Fake Provider 限制和脱敏排障见[邮件投递与 SMTP](./email-delivery.md)。
公开 API 的调用顺序见[注册邮箱验证](../api/registration.md)。
