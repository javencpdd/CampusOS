# 初始管理员安全

> 更新时间：2026-10-27（Asia/Shanghai）
> 适用范围：当前 v1.2 开发候选；不代表正式发布。

## 管理员与用户是两个身份域

当前两者共用一个 PostgreSQL 数据库，但使用不同的账号表、凭据、会话、角色绑定和 Token 域：

| 身份域 | 主要表 | 登录入口 |
| --- | --- | --- |
| 用户 | `users`、`accounts`、`sessions`、`user_roles` | 用户端 3000，`/api/v1/auth/login` |
| 管理员 | `admin_accounts`、`admin_credentials`、`admin_sessions`、`admin_roles`、`admin_role_bindings` | 管理端 3001，`/api/v1/admin/identity/login` |

管理域账号不外键依赖 `users/accounts`。相同邮箱或相同数字 ID 也不会合并身份；给用户赋予旧 `admin` 角色不会自动创建管理域账号。

新数据库不会播种固定密码的超级用户。迁移只播种 `security_admin` 等角色参考数据；第一位安全管理员由部署者显式初始化。

## 首次初始化

1. 在实际运行配置中临时设置 `AUTH_ADMIN_BOOTSTRAP_SECRET`，使用独立随机值（16–4096 字节，建议至少 32 个随机字节）。Docker 开发配置为 `deploy/docker/.env.dev.local`。
2. 它必须不同于旧 `AUTH_BOOTSTRAP_ADMIN_SECRET`；后者仍用于历史用户域引导/本地恢复，不是管理域默认密码。
3. 执行 `docker-dev.* up` 应用环境配置，不能仅 `docker restart`。
4. 从开发机本地请求 `POST http://127.0.0.1:8080/api/v1/admin/identity/bootstrap`，请求头 `X-CampusOS-Bootstrap-Secret` 必须与配置一致；JSON 提交自行选择的 `username`、`email`、`password`、`reason`。
5. 成功后清空 `AUTH_ADMIN_BOOTSTRAP_SECRET` 并再次运行 `up`，随后使用新管理域账号登录、绑定 MFA。

初始化凭证不是管理员登录密码，不写入账号表、返回正文或日志。请求凭证缺失、错误、重复、配置关闭或已有有效安全管理员时均拒绝；事务内会再次核对首次初始化条件。

不要把初始化凭证写进 URL、Git、截图或聊天，也不要在不可信网络通过明文 HTTP 执行初始化。开发环境使用本机回环地址；生产使用部署秘密管理和受保护的管理通道。

可复制的 PowerShell 操作位于仓库 `docs/help/系统设计相关/v1.2独立管理员初始化与账号隔离.md`。

## 管理员二次认证可配置

当前独立管理端的敏感操作默认要求 TOTP 二次验证。部署者可以在实际运行配置中设置 `AUTH_ADMIN_MFA_REQUIRED=false` 关闭该要求；未配置、空值或非法值保持默认开启。`true` 恢复开启。Docker 开发修改 `deploy/docker/.env.dev.local` 后执行 `docker-dev.* up`，而非仅 `restart` 或重新构建镜像。

后台“管理员 MFA 策略”页展示当前独立 Admin `/admin/identity/me` 的生效状态和配置方法，不再使用历史用户域的登录策略。部署开关不由浏览器请求直接修改。关闭后仍校验密码、活跃会话、账号状态、角色/资源权限、CSRF 与必需审计，不会删除认证器或把密码会话伪装为 MFA；生产和局域网建议保持开启。用户域、受托用户明确要求的 MFA 和更换既有认证器的持有证明不受此开关影响。

开启时，首次登录的未绑定管理员可从页面提示进入分步引导：准备 TOTP 认证器 → 点击开始配置 → 使用添加链接或手动输入密钥 → 输入 6 位动态验证码完成绑定 → 等待下一组验证码确认身份 → 重试原操作。当前验证有效期 15 分钟；检查设备自动同步时间，勿分享添加链接、密钥和验证码。

## 开发兼容账号不是管理端默认账号

`AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN=true` 仅保留历史用户域兼容路径。`admin@campusos.local / Admin@123456` 只可能是用户端的未修改开发凭据，不能用于当前独立管理域。不要在局域网或共享环境继续使用固定开发密码。

忘记已有管理域密码时，应走独立 Admin 的受控离线恢复，不通过重新开启 bootstrap、修改 `accounts` 或创建用户侧角色来绕过。

## 生产配置仍须满足的约束

生产环境仍要求非开发 JWT、启用密码哈希、关闭固定开发兼容账号，以及有效的验证码/会话摘要密钥和 SMTP。现行兼容启动校验仍要求旧 `AUTH_BOOTSTRAP_ADMIN_SECRET` 至少 16 字符；它与新的管理域初始化凭证职责不同，不要在关闭管理域初始化时误删两个配置。新 `AUTH_ADMIN_BOOTSTRAP_SECRET` 可以在首次初始化后清空，已创建管理域账号的登录不依赖它。
