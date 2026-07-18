# 邮件投递与 SMTP

CampusOS 的验证邮件由始终启用的 `core.email-delivery` 模块处理。它使用可靠任务异步投递：
请求验证码成功只表示 Challenge 已安全提交，不能表示邮件已经到达。

## 生产配置

```dotenv
CAMPUSOS_ENV=production
EMAIL_PROVIDER=smtp
EMAIL_SMTP_HOST=smtp.example.edu
EMAIL_SMTP_PORT=587
EMAIL_SMTP_USERNAME=campusos-mailer
EMAIL_SMTP_PASSWORD=<Secret 管理器注入>
EMAIL_SMTP_FROM=CampusOS <noreply@example.edu>
EMAIL_SMTP_TIMEOUT=10s
EMAIL_SMTP_STARTTLS=true
```

生产和 staging 环境拒绝 `EMAIL_PROVIDER=fake`。不要提交 SMTP 密码；管理端也不会显示 host、
用户名、收件人、验证码或完整 Provider 错误。

## 本地开发

`development` 和 `test` 可以使用 `EMAIL_PROVIDER=fake`。Fake Provider 不会打印验证码或提供
可公开访问的测试邮箱；需要手工验收时请使用隔离的 SMTP 测试服务。

## 运维状态

管理员在“可靠任务”查看 Provider 健康，也可使用受权限保护的接口：

```text
GET /api/v1/platform/email-delivery/status
```

SMTP 失败后任务会以 at-least-once 语义重试。修复网络或 Provider 后重启服务并检查 Outbox；不要
通过日志或数据库恢复验证码。

仓库内的完整排障说明见
`docs/help/系统设计相关/v12邮件投递与SMTP部署说明.md`。
验证码是否允许创建由 Identity Core 的独立安全策略决定；默认每邮箱 10 分钟 5 次。管理端配置见
[验证码策略配置](/guide/challenge-policy)。该策略不会替代 SMTP 配置。
