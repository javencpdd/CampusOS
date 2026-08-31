# 多因素认证 API

CampusOS v0.13 的 MFA 使用标准 TOTP 和一次性恢复码。它不把邮箱验证码当作第二因子，也不向插件、资源包或前端暴露 TOTP Secret。

## 登录流程

密码验证成功后，已启用 MFA 的用户会收到内存态 `mfa_ticket`，而不是完整 Session。客户端随后调用 `POST /api/v1/auth/mfa/login/complete` 并提交六位认证器验证码。ticket 短期、单用途，不能保存到 URL、Local Storage、日志或工单。

## 用户接口

- `GET /api/v1/auth/mfa`：读取当前 MFA 状态和剩余恢复码数。
- `POST /api/v1/auth/mfa/totp/enrollment`：以当前密码开始配置。
- `POST /api/v1/auth/mfa/totp/confirm`：确认认证器，并只显示一次恢复码。
- `POST /api/v1/auth/mfa/recovery-codes/rotate`：使用 TOTP 或一个恢复码轮换恢复码。
- `DELETE /api/v1/auth/mfa/totp`：当前密码加第二因子关闭 MFA，并撤销全部登录设备。
- `POST /api/v1/auth/mfa/step-up`：在当前 Session 上完成近期 MFA 验证。

这些接口使用既有认证和 CSRF 机制。请求字段、错误码和完整示例见仓库中的 [v0.13 多因素认证 API](https://github.com/javencpdd/CampusOS/blob/main/docs/api/v0.13%E5%A4%9A%E5%9B%A0%E7%B4%A0%E8%AE%A4%E8%AF%81API.md)。

## 管理员接口

`GET`/`PUT /api/v1/identity/mfa-policy` 由 `identity.mfa_policy.read` 和 `identity.mfa_policy.update` 控制。策略可从关闭逐步切换到注册宽限期和强制；强制前系统验证管理员覆盖率和本机恢复条件。

当管理员管理 Session 缺少近期 MFA 时，服务返回 `identity.mfa.step_up_required`。普通用户登录入口取得的管理员 Session 也不能绕过这个服务端门禁。

参见 [管理员 MFA 与恢复运维](../operations/mfa.md)。
