# v0.13 TOTP 多因素认证与恢复说明

> 适用版本：`v0.13-dev` 候选实现
> 关联接口：[v0.13 多因素认证 API](../../api/v13多因素认证API.md)
> 管理员准入与本地恢复：[v0.13 管理员准入管理与本地恢复](v13管理员准入管理与本地恢复.md)

## 1. 这项能力解决什么问题

CampusOS 的 MFA 是 Identity Core 的内置安全能力，不是插件，也不能被资源包、外部插件或 Agent 调用。它在密码之外要求认证器生成的六位 TOTP 验证码，用于：

- 管理员登录后的第二步验证；
- 管理员已登录后访问管理 API 时的近期 Step-up；
- 用户主动保护自己的账号；
- 丢失设备时使用一次性恢复码恢复控制权。

v0.13 只实现 TOTP 和恢复码，不把邮箱验证码当作第二因子，也不提供 Passkey、短信或 OAuth 登录。

## 2. 部署前配置

MFA 密钥只存在部署环境变量中。它不能写入前端、插件 Manifest、资源包、数据库普通字段、日志、备份说明或配置导出包。

```dotenv
# 当前用于写入新 TOTP Secret 的密钥 ID。
AUTH_MFA_ACTIVE_KEY_ID=v1

# key-id:secret 形式，可保留旧密钥以读取历史记录。
AUTH_MFA_ENCRYPTION_KEYS=v1:<new-random-secret>,v0:<previous-random-secret>

# 认证器中显示的签发方名称。
AUTH_MFA_ISSUER=CampusOS

# 本机紧急恢复所需；生产环境也要求设置。
AUTH_BOOTSTRAP_ADMIN_SECRET=<deployment-secret>
```

要求：

1. 每个密钥使用独立、高熵、至少 16 字符的部署 Secret；生产环境不能使用 `.env.example` 中的开发值。
2. `AUTH_MFA_ACTIVE_KEY_ID` 必须在 `AUTH_MFA_ENCRYPTION_KEYS` 中存在。
3. 先部署新旧两把密钥并切换 active ID，再逐步让用户重新写入 MFA Secret；确认没有旧记录后才能删除旧密钥。
4. 未配置可用密钥时，未强制 MFA 的普通登录保持可用；已有 MFA 因子或强制管理员策略会安全失败，而不是降级绕过。

## 3. 用户如何启用

在 PC 或手机端登录后，打开“账号安全 -> 多因素认证”。

1. 点击“启用认证器”，输入当前密码。
2. 在认证器 App 中扫描二维码；不能扫码时复制页面给出的手工密钥。
3. 输入认证器生成的六位验证码并确认。
4. 页面只显示一次恢复码。离线保存它们，不要截图上传、发送到聊天群、写进浏览器笔记或交给他人。
5. 下次登录时，密码验证成功后再输入认证器验证码。

关闭 MFA 需要同时证明当前密码和一个 TOTP 验证码或未使用的恢复码。关闭成功会撤销该账号的全部 Session，必须重新登录。

## 4. 管理员策略与 Step-up

管理员在后台“身份与权限 -> MFA 策略”中只能选择以下顺序：

| 模式 | 行为 |
| --- | --- |
| `off` | MFA 自愿启用，不阻止管理员登录。 |
| `enrollment_grace` | 管理员可在宽限期内完成注册，页面显示覆盖率。宽限期到期后等同强制。 |
| `required` | 只有具备 MFA 的管理员才能完成管理员登录；所有管理 API 还要求当前 Session 的近期 MFA Step-up。 |

系统在切换到 `required` 前检查管理员覆盖率、至少一名有效管理员以及受控本地恢复是否可用。策略写入使用版本号，其他管理员先修改过策略时会返回冲突而不会静默覆盖。

关键边界：即使管理员先通过普通用户登录入口获得 Session，再直接请求管理 API，也不能绕过已启用的管理员 MFA 策略。管理端会提示完成 Step-up；仅依赖板块 scope 的版主不被误当作全局管理员。

## 5. 丢失设备时的恢复顺序

优先使用仍保存的恢复码：在需要验证码的页面输入一个未使用恢复码，然后立即轮换恢复码并检查登录设备。

所有认证器和恢复码都丢失时，只有受控本机恢复可关闭活跃管理员的 MFA：

```bash
campusosctl identity reset-mfa --user-id <管理员用户ID> --reason "lost authenticator and recovery codes"
```

该命令必须在服务器本机的交互式终端执行，并要求：

1. 目标存在有效的管理员准入记录；
2. 输入完全匹配的 `RESET-MFA <用户ID>` 确认文本；
3. 隐藏输入 `AUTH_BOOTSTRAP_ADMIN_SECRET`；
4. 数据库连接与可靠命令服务可用。

它没有 HTTP API，不接受 Secret、恢复码或 Token 作为命令行参数，不回显旧密钥或恢复码。成功后会禁用 MFA、清除恢复码、撤销该用户全部 Session、留下 required audit/可靠事件，并通过可投递邮箱发送不含敏感内容的安全通知。用户必须走批准的密码登录路径重新登录、重新注册 MFA 并保存新的恢复码。

管理员准入被暂停是另一类问题，使用 `campusosctl identity restore-admin-admission`，不要把它与 `reset-mfa` 混用。

## 5. 受控浏览器演练

MFA 的完整浏览器演练会临时启用、轮换并关闭一个认证器，因此不会混入默认的发布检查，也绝不能针对真实管理员账号执行。只可在隔离开发/候选环境中，对一个明确可变更、初始未启用 MFA 的测试管理员运行：

```bash
RUN_MFA_BROWSER_WORKFLOW=true \
CAMPUSOS_MFA_TEST_EMAIL=<disposable-admin-email> \
CAMPUSOS_MFA_TEST_PASSWORD=<disposable-admin-password> \
CAMPUSOS_MFA_TEST_ALLOW_STATE_CHANGE=yes \
scripts/smoke/browser-smoke.sh
```

演练覆盖用户端启用、Web 与 Admin 第二步登录、恢复码轮换、使用新恢复码关闭 MFA、会话撤销，以及关闭后的密码直登。脚本失败时会尝试用尚未使用的恢复码关闭认证器；如果清理也失败，必须按本节的本机 `reset-mfa` 流程处理，不要继续使用该测试账号。

## 6. 安全与排错

- 手机和服务器时间应自动同步。系统只容忍有限时间窗口，重复使用同一 30 秒时间步会被拒绝。
- MFA Ticket 只在密码验证成功后产生，短期、单用途，并且数据库只保存摘要；浏览器不应把它放入 Local Storage、URL 或日志。
- TOTP Secret 只以带 key ID 的加密信封保存；恢复码只保存不可逆摘要和使用状态。
- `identity.mfa.ticket_invalid`、`identity.mfa.factor_invalid`、`identity.mfa.replay` 和 `identity.mfa.step_up_required` 都是稳定机器错误码。不要把用户输入的验证码写入工单或日志。
- `identity.mfa.unavailable` 表示部署侧密钥、策略或依赖无法安全评估，应先检查环境配置和服务健康，而不是关闭权限检查。

当前限制：没有 Passkey、硬件密钥、跨节点密钥托管或自动化身份验证器迁移。密钥轮换和本机恢复须由具备服务器管理权限的运维人员执行。
