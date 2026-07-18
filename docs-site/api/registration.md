# 注册邮箱验证

从 v0.12 A5 开始，注册分为三个公开 HTTP 请求。账号只会在最后一步的 Registration Ticket
被一次性消费后创建；没有 Ticket 的旧一步式请求不会创建未验证账号。

## 1. 申请验证码

```http
POST /api/v1/auth/registration/challenge
Content-Type: application/json

{"email":"student@example.edu"}
```

成功时返回 Challenge ID 和过期时间。Challenge ID 不是验证码，也不能用作登录凭据。

```json
{
  "code": 0,
  "data": {
    "challenge_id": "opaque-public-id",
    "purpose": "registration",
    "expires_at": "2026-07-17T10:10:00Z"
  }
}
```

服务会异步投递邮件。请求成功表示 Challenge 已可靠提交，不承诺邮件已经到达。默认每个邮箱任意
连续十分钟最多申请五次，同一 IP 任意连续六十分钟最多十次；管理员可在安全范围内热更新。达到限制时返回 `429` 和
`identity.registration_verification_rate_limited`。

非法 JSON 或邮箱格式返回 `400 request.invalid`，保留地址等语义拒绝返回
`400 identity.registration_verification_invalid`；策略存储、事务或可靠队列暂不可用时返回可重试的
`503 internal.error`。服务端只用 `request_id` 关联内部诊断，不把邮箱、验证码或数据库错误返回给浏览器。

## 2. 校验验证码

```http
POST /api/v1/auth/registration/verify
Content-Type: application/json

{
  "challenge_id":"opaque-public-id",
  "code":"123456"
}
```

成功时返回短生命周期的 Registration Ticket：

```json
{
  "code": 0,
  "data": {
    "challenge_id": "opaque-public-id",
    "purpose": "registration",
    "ticket": "opaque-one-time-value",
    "expires_at": "2026-07-17T10:15:00Z"
  }
}
```

Ticket 只能用于相同邮箱、相同用途的一次注册。客户端只应短暂保存在内存中，不能放入
localStorage、URL、日志或错误上报。

## 3. 创建账号

```http
POST /api/v1/auth/register
Content-Type: application/json

{
  "username":"alice",
  "nickname":"Alice",
  "email":"student@example.edu",
  "password":"a-strong-password",
  "challenge_id":"opaque-public-id",
  "ticket":"opaque-one-time-value"
}
```

服务端在同一个可靠命令中锁定并消费 Ticket、创建用户、写入 `verified` Email Account、记录
required audit 并投递用户创建事件。任一步失败会回滚 Ticket 和账号，不会留下半注册数据。

旧客户端缺少 `challenge_id` 或 `ticket` 时收到 `400` 和稳定错误
`identity.verification_required`。过期、已消费、用途或邮箱不匹配的 Ticket 返回 `400`
`identity.registration_verification_invalid`。用户名或邮箱冲突保持 `409 resource.conflict`。

## 客户端实现

CampusOS 用户前台已在注册页实现上述流程。其他 HTTP 客户端必须按相同顺序调用，且不应绕过
第二步。当前插件 SDK 不提供身份注册写入能力；External Plugin、MCP 和 Agent 不可调用
Identity 内部 Service 或读取 Challenge 数据。

完整字段合同以[当前 OpenAPI](/api/contracts)为准；邮件部署和脱敏排障见
[邮件投递与 SMTP](/deployment/email-delivery)。
