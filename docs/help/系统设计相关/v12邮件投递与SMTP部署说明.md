# v12 邮件投递与 SMTP 部署说明

> 适用阶段：v0.12 A4 及以后  
> 读者：部署管理员、运维人员和开发者

## 1. 这个模块做什么

`core.email-delivery` 负责把已经提交的邮箱 Challenge 请求投递到 SMTP Provider。它不是普通
插件，也不能在管理端关闭：注册、绑定邮箱和密码找回都会依赖它。

用户请求验证码时，系统先保存 Challenge、限流、审计和一个不透明的可靠任务。随后 Worker 再
异步发邮件。因此“已接受验证码请求”不等于“SMTP 已发送成功”。SMTP 暂时不可用时，任务会重试，
管理员能看到降级状态，但不会看到用户邮箱、验证码或邮件正文。

## 2. 生产配置

生产环境必须使用 SMTP，并通过部署 Secret 管理器提供密码：

```dotenv
CAMPUSOS_ENV=production
EMAIL_PROVIDER=smtp
EMAIL_SMTP_HOST=smtp.example.edu
EMAIL_SMTP_PORT=587
EMAIL_SMTP_USERNAME=campusos-mailer
EMAIL_SMTP_PASSWORD=<由 Secret 管理器注入>
EMAIL_SMTP_FROM=CampusOS <noreply@example.edu>
EMAIL_SMTP_TIMEOUT=10s
EMAIL_SMTP_STARTTLS=true
```

| 配置 | 作用 | 注意事项 |
| --- | --- | --- |
| `EMAIL_PROVIDER` | 当前 Provider | 生产只能是 `smtp`。 |
| `EMAIL_SMTP_HOST` / `PORT` | SMTP 服务地址 | 端口范围必须是 1 到 65535。 |
| `EMAIL_SMTP_USERNAME` / `PASSWORD` | SMTP 认证 | 仅由进程读取，不能放入插件配置、管理 API 或版本库。 |
| `EMAIL_SMTP_FROM` | 发件人地址 | 必须是合法邮件地址。 |
| `EMAIL_SMTP_TIMEOUT` | 建立 SMTP 连接的超时 | 使用 Go duration，例如 `10s`。 |
| `EMAIL_SMTP_STARTTLS` | 是否要求 STARTTLS | 建议保持 `true`；Provider 不支持时应修复配置，不要降级明文。 |

`CAMPUSOS_ENV=production` 或 `staging` 配置 `EMAIL_PROVIDER=fake` 会直接拒绝启动。不要把 SMTP
密码放进 `.env.example`、截图、Issue、平台日志或风格包。

## 3. 本地开发

本地开发默认：

```dotenv
CAMPUSOS_ENV=development
EMAIL_PROVIDER=fake
```

Fake Provider 只确认 Consumer 是否被调用，不打印邮件，也没有可浏览的收件箱。这是有意的安全
限制。需要手工查看真实邮件时，使用隔离的 Mailpit/MailHog 或测试 SMTP 服务，设置
`EMAIL_PROVIDER=smtp`，并使用专门的测试账号。

## 4. 在管理端查看状态

拥有 `platform.reliability.read` 的管理员可在“可靠任务”页面看到邮件 Provider 的名称、健康
状态和通用错误，也可读取：

```text
GET /api/v1/platform/email-delivery/status
```

响应不会包含 SMTP host、用户名、收件人、验证码、Ticket、正文或 Provider 原始错误。发生故障时
显示的 `email provider delivery failed` 只是提示进入 SMTP/网络排查，不是可用于定位用户的日志。

## 5. 排障顺序

1. 查看可靠任务中的 `identity.email.challenge.requested.v1` 是否为 `retry` 或 `dead`。
2. 查看邮件投递状态是否为 `degraded`。
3. 检查部署 Secret 是否存在、发件人是否被 SMTP Provider 允许、网络/TLS 是否可达。
4. 修复配置或网络后重启服务；Worker 会从 Outbox 重试未完成任务。
5. 对已经进入 `dead` 的任务，按可靠任务的受控重放流程操作，避免手工复制或伪造事件 payload。

不要请求用户提供验证码，不要从数据库寻找验证码，也不要把失败原因改为包含用户邮箱的文本。

## 6. 语义与限制

CampusOS 使用 at-least-once 投递。Consumer receipt 和邮件 ID 能降低重复，但 SMTP 服务器已经
接受邮件后、服务进程尚未写入 receipt 前发生崩溃时，用户仍可能收到重复邮件。验证码本身有期限，
用户只能消费一次 Ticket；重复邮件不会使注册或密码重置重复执行。

可执行验证：

```bash
make email-delivery-check
make reliability-check
```
