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

Docker 开发栈使用独立的本地配置，不读取仓库根 `.env`：

```bash
./scripts/docker-dev.sh setup
# 在 deploy/docker/.env.dev.local 填写 EMAIL_*
./scripts/docker-dev.sh setup --start
```

PowerShell 使用 `.\scripts\docker-dev.ps1 setup` 和 `.\scripts\docker-dev.ps1 setup -Start`。向导校验
配置结构并确保密码不出现在摘要中；实际 SMTP 网络和凭据仍在 API 启动并投递新验证码时验证。
执行 `STOP_EXISTING=true make dev-all` 切换到宿主 Go/Node 进程时，也会读取这同一份配置并继续使用
Docker 开发数据库卷，不需要把 SMTP 授权码复制到根 `.env`。

### QQ 邮箱与 Foxmail 地址

CampusOS 当前支持 STARTTLS，因此 QQ/Foxmail 账号使用 `smtp.qq.com:587`，不要使用需要隐式 TLS 的
465 端口。用户名和发件人都填写完整邮箱地址，密码填写新生成的 QQ 邮箱授权码：

```dotenv
EMAIL_PROVIDER=smtp
EMAIL_SMTP_HOST=smtp.qq.com
EMAIL_SMTP_PORT=587
EMAIL_SMTP_USERNAME=your-account@foxmail.com
EMAIL_SMTP_PASSWORD=<仅存本地或Secret管理器的新授权码>
EMAIL_SMTP_FROM=your-account@foxmail.com
EMAIL_SMTP_STARTTLS=true
```

授权码不得进入文档、Git、Issue、聊天记录或截图。配置后重启服务并重新申请验证码；已经由 Fake Provider
标记为 `published` 的旧事件不会自动补发。完整步骤和错误排查见仓库文档
`docs/help/系统设计相关/v0.12邮件投递与SMTP部署说明.md`。

## 运维状态

管理员在“可靠任务”查看 Provider 健康，也可使用受权限保护的接口：

```text
GET /api/v1/platform/email-delivery/status
```

SMTP 失败后任务会以 at-least-once 语义重试。修复网络或 Provider 后重启服务并检查 Outbox；不要
通过日志或数据库恢复验证码。

仓库内的完整排障说明见
`docs/help/系统设计相关/v0.12邮件投递与SMTP部署说明.md`。
验证码是否允许创建由 Identity Core 的独立安全策略决定；默认每邮箱 10 分钟 5 次。管理端配置见
[验证码策略配置](/guide/challenge-policy)。该策略不会替代 SMTP 配置。
