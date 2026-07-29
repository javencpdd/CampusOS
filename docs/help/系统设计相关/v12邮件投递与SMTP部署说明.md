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

### 3.1 QQ 邮箱 / Foxmail 地址示例

腾讯的 SMTP 配置中，QQ 邮箱使用 `smtp.qq.com`，支持 465 隐式 TLS 和 587 STARTTLS。CampusOS
当前 SMTP Adapter 实现的是 STARTTLS，不是 465 隐式 TLS，因此应使用 **587 + STARTTLS**。参考：
[腾讯云官方 SMTP 配置表](https://intl.cloud.tencent.com/zh/document/product/1266/71700)、
[Foxmail 官方帮助中心](https://service.foxmail.com/)。

先在 QQ 邮箱设置中开启 IMAP/SMTP 或 POP3/SMTP 服务并生成一枚新的授权码。Foxmail 地址也使用完整
邮箱地址作为 SMTP 用户名。授权码相当于第三方客户端密码，不是邮箱网页登录密码。

完整 Docker 与共享数据的本机 Go/Node 模式统一读取 `deploy/docker/.env.dev.local`。先运行
`./scripts/docker-dev.sh setup` 生成该 Git 忽略文件，再按下面形式填写；尖括号必须替换为自己的值，但
不得把真实授权码写入本文档、模板、Issue、聊天记录、截图或 Git：

```dotenv
CAMPUSOS_ENV=development
EMAIL_PROVIDER=smtp
EMAIL_SMTP_HOST=smtp.qq.com
EMAIL_SMTP_PORT=587
EMAIL_SMTP_USERNAME=your-account@foxmail.com
EMAIL_SMTP_PASSWORD=<新生成的QQ邮箱授权码，仅存本机或Secret管理器>
EMAIL_SMTP_FROM=your-account@foxmail.com
EMAIL_SMTP_TIMEOUT=10s
EMAIL_SMTP_STARTTLS=true
```

这里的 `EMAIL_SMTP_FROM` 使用裸邮箱地址，不写成 `CampusOS <...>`，以兼容当前 SMTP envelope sender
和 QQ 邮箱的发件人校验。`EMAIL_SMTP_USERNAME` 与 `EMAIL_SMTP_FROM` 首次配置时应保持一致。

配置后限制本地文件权限。启动完整 Docker 模式：

```bash
chmod 600 deploy/docker/.env.dev.local
./scripts/docker-dev.sh setup --start
```

已安装 Go、Node.js 和 pnpm 时，可以在同一数据库和配置上切换为本机应用进程：

```bash
STOP_EXISTING=true make dev-all
```

PowerShell 使用 `.\scripts\docker-dev.ps1 setup` 和 `.\scripts\docker-dev.ps1 setup -Start`。脚本会验证
SMTP 必填字段并通过 Compose 传入 API，但不会显示授权码，也不会主动连接 SMTP 测试凭据。后续修改配置后
运行 `docker-dev.* up` 重启开发栈。只有显式使用 `CAMPUSOS_DEV_INFRA_MODE=legacy` 时，本机 API 才以根
`.env` 为配置源；Legacy 数据不与 `campusos-dev` 卷共享。

然后重新申请一枚验证码。此前由 Fake Provider 处理并已标记为 `published` 的可靠事件不会在切换 SMTP
后自动重发；不要重放旧验证码事件，应使用新的 Challenge。若触发邮箱/IP 频率限制，应等待当前窗口结束，
不要删除 `identity_challenge_rate_limits` 中的计数。

### 3.2 QQ 邮箱常见问题

| 现象 | 优先检查 |
| --- | --- |
| 接口返回成功但没有真实邮件 | 确认 `EMAIL_PROVIDER=smtp` 已被重启后的进程读取；旧 Fake 事件不会补发。 |
| SMTP 返回 `535` | 使用的是授权码而非网页登录密码；用户名是完整邮箱地址；授权码未撤销或过期。 |
| 提示不支持 STARTTLS | 端口应为 `587`，且 `EMAIL_SMTP_STARTTLS=true`；CampusOS 当前不使用 465 隐式 TLS。 |
| 返回 `501 Bad address syntax` | `EMAIL_SMTP_FROM` 使用裸邮箱地址，并与被授权账号一致。 |
| 事件持续 `retry` | 检查网络能否访问 `smtp.qq.com:587`、Provider 服务状态和授权码状态。 |
| 事件是 `published` 但收件箱没有邮件 | 检查垃圾邮件、QQ 邮箱发信限制和收件地址；`published` 表示 SMTP Consumer 已完成，不保证最终进入收件箱。 |

授权码一旦出现在聊天、终端历史、日志或截图中，应立即在 QQ 邮箱中撤销，重新生成后只写入本地 Secret。

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
