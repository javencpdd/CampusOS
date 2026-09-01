# ADR-0013：验证码、Ticket 与持久限流边界

> 状态：已接受并在 v0.12 A3 实施  
> 日期：2026-07-17  
> 关联：[v0.12 计划书](../../项目计划书v0/项目计划v0.12/00-v0.12版本计划书.md)、[A3 进度](../../进度/v0.12-dev/v0.12.4-dev.md)

## 背景

邮箱验证码常见的错误实现是把六位明文、原始 Ticket、IP 地址、邮件正文或 SMTP 凭据放入
数据库、日志、Outbox 或管理端。这样即使数据库或运维页面的只读权限泄露，也可能直接变成
账号接管能力。

CampusOS 已有 TxKernel 和持久 Outbox，因此 v0.12 需要把 Challenge 的状态、限流和“请求邮件”
事件一起提交，但不能把敏感材料放入事件 payload。

## 决策

1. 验证码不是存储字段。它由 `HMAC(key[key_id], public_id + nonce + purpose)` 截断为六位，
   数据库只保存 `key_id`、随机 nonce 和用途。
2. Challenge 仅接受三种 Purpose：`registration`、`email_binding`、`password_reset`。Code 和
   Ticket 不能跨 Purpose 使用。
3. 验证成功后生成至少 256 bit 的随机 Ticket；服务端只保存 SHA-256 digest。消费操作通过
   `SELECT ... FOR UPDATE` 和同一事务写入 `consumed_at`，保证一次性。
4. 限流窗口保存 keyed digest，不保存原始 IP 或用于限流的邮箱值。默认规则是每邮箱每分钟
   1 次、每天 5 次、每 IP 每小时 10 次；与 Challenge 写入同一命令事务。
5. `identity.email.challenge.requested.v1` Outbox payload 只有 `challenge_id`。A4 的邮件模块
   通过受限 Dispatch Port 获得短生命周期的内存 Code，不能读取 Identity Repository。
6. Core Identity 的公开 AppContext Port 可以被 Core 邮件模块使用，但 External Plugin、MCP、
   Agent、HTTP Handler 和浏览器端不获得 HMAC key、Challenge Store 或 Dispatch Port。

## 后果

- 进程重启后仍可依照 `key_id` 重建未过期 Code；密钥轮换只需在保留期内同时配置旧 key。
- 错误 Code 会持久计数并在上限后失效；无效、过期、已验证或已消费状态返回统一错误，避免
  细分状态成为账号枚举信号。
- 邮件投递可异步重试，而 Challenge/限流事实不会因 SMTP 故障或 Worker 崩溃丢失。
- A3 不公开发送或验证 API，不改变现有注册登录，避免在 Mail Provider 和 UI 尚未准备好时
  半启用验证流程。
- A4 已接入 SMTP/Fake Consumer；A5 之前仍不公开 Challenge 请求、Code 校验或注册 Ticket
  消费 API。

## 被拒绝的方案

- **数据库保存明文验证码**：数据库查询、备份和管理员只读权限都将获得可用凭据。
- **JWT 作为验证 Ticket**：撤销、单次消费和 Purpose 绑定更难保证，且容易进入浏览器日志。
- **仅内存限流**：多重启或多进程可绕过，也没有审计和恢复证据。
- **在请求 API 直接发送 SMTP**：会把网络副作用放在数据库事务内，失败时不具备可靠重试。

## 验证与回退

`make identity-challenge-check` 覆盖 HMAC Code 生命周期、Purpose 隔离、错误次数、每日限流、
一次 Ticket 消费、Outbox 敏感数据负向断言和 `000029` up/down/up。生产回退采用停止新
Challenge 写入并 forward-fix；down migration 仅用于隔离演练，不能恢复已消费 Ticket。
