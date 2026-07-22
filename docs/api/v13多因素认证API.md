# v13 多因素认证 API

> 当前机器可读契约仍由 `docs/api/openapi-v0.6-current.yaml` 和 `docs/api/http-routes-v0.6.json` 生成；本页解释 v13 MFA 的请求顺序与安全约束。

## 登录顺序

1. 客户端调用既有 `/api/v1/auth/login` 或管理员登录入口。
2. 密码成功且用户已启用 MFA 时，响应 `data` 只返回 `mfa_required`、短期 `mfa_ticket` 与过期时间；此时没有 Access Token、Refresh Token 或持久化登录态。
3. 客户端仅在内存中保存 ticket，调用 `POST /api/v1/auth/mfa/login/complete`。
4. TOTP 成功后，系统签发正常 Session；ticket 再次使用、超时或连续失败会被拒绝。

管理员策略要求但管理员尚未注册 MFA 时，登录返回 `identity.mfa.enrollment_required`，不能先创建低强度管理员 Session。

## 接口清单

| 方法与路径 | Audience | CSRF | 用途 |
| --- | --- | --- | --- |
| `POST /api/v1/auth/mfa/login/complete` | public（仅 MFA Ticket） | 否 | 用 ticket 和 TOTP 完成登录。 |
| `GET /api/v1/auth/mfa` | authenticated | 否 | 读取当前用户的 MFA 状态与剩余恢复码数量。 |
| `POST /api/v1/auth/mfa/totp/enrollment` | authenticated | 是 | 用当前密码创建待确认认证器材料。 |
| `POST /api/v1/auth/mfa/totp/confirm` | authenticated | 是 | 验证认证器并一次性返回恢复码。 |
| `POST /api/v1/auth/mfa/recovery-codes/rotate` | authenticated | 是 | 用 TOTP 或一个恢复码轮换恢复码。 |
| `DELETE /api/v1/auth/mfa/totp` | authenticated | 是 | 用当前密码和一个第二因子关闭 MFA，并撤销全部 Session。 |
| `POST /api/v1/auth/mfa/step-up` | authenticated | 是 | 将当前 Session 提升为 MFA 强度，返回新的 Access Token。 |
| `GET /api/v1/identity/mfa-policy` | admin + `identity.mfa_policy.read` | 否 | 读取聚合覆盖率和策略，不返回个人 Secret。 |
| `PUT /api/v1/identity/mfa-policy` | admin + `identity.mfa_policy.update` | 是 | 版本化更新管理员 MFA 策略。 |

所有 authenticated 请求使用既有 Bearer/Session、CSRF 和错误包络合同；不要在 URL、查询参数、日志或插件调用中传递 MFA Ticket、验证码、手工密钥或恢复码。

## 关键请求示例

完成登录：

```json
POST /api/v1/auth/mfa/login/complete
{
  "mfa_ticket": "<memory-only-ticket>",
  "code": "123456"
}
```

开始 enrollment：

```json
POST /api/v1/auth/mfa/totp/enrollment
{
  "password": "<current-password>"
}
```

成功响应包含短期 enrollment 的 `otpauth_uri` 与 `manual_key`。它们只用于立即配置认证器，客户端不应缓存或再次展示。

确认 enrollment：

```json
POST /api/v1/auth/mfa/totp/confirm
{
  "code": "123456"
}
```

该响应中的 `recovery_codes` 只会出现一次，同时包含一个用于当前浏览器 Session 的新 `access_token`。

更新管理员策略：

```json
PUT /api/v1/identity/mfa-policy
{
  "mode": "enrollment_grace",
  "grace_ends_at": "2026-08-01T00:00:00Z",
  "expected_version": 3
}
```

可选 `mode`：`off`、`enrollment_grace`、`required`。`required` 会执行恢复能力和管理员覆盖率校验；当前管理员的 Session 若没有近期 MFA，则响应 `identity.mfa.step_up_required`，客户端需先调用 Step-up 再重试。

## 稳定错误码

| 机器码 | HTTP | 客户端处理 |
| --- | --- | --- |
| `identity.mfa.invalid` | 400 | 修正请求字段。 |
| `identity.mfa.ticket_invalid` | 401 | 回到密码登录；不要重用 ticket。 |
| `identity.mfa.factor_invalid` | 400 | 提示重新输入，不清除普通登录态。 |
| `identity.mfa.replay` | 409 | 等待认证器下一时间步后重试。 |
| `identity.mfa.enrollment_required` | 403 | 引导管理员完成 enrollment。 |
| `identity.mfa.not_enabled` | 409 | 先启用 MFA。 |
| `identity.mfa.policy_safety` | 409 | 先修复管理员覆盖率或本机恢复配置。 |
| `identity.mfa.step_up_required` | 403 | 以当前认证器验证码调用 Step-up。 |
| `identity.mfa.unavailable` | 503 | 不降级绕过，检查部署密钥和依赖。 |

所有错误继续保留 CampusOS 兼容顶层 `code`、`msg` 和 `request_id`，新增的 `error.code` 适合程序判断。
