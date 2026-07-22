# v13 身份 MFA 与管理员恢复数据流

## 正常登录

```text
Password login
  -> Identity verifies account and administrator admission
  -> MFA policy/factor check
     -> no factor and policy not required: Session(password)
     -> factor required: digest-only MFA ticket
  -> TOTP completion
  -> Session(mfa, mfa_authenticated_at)
```

MFA Ticket 是短期、单用途、数据库摘要存储的登录中间态。第二因子成功前不能签发 Access Token 或 Refresh Cookie。

## 管理 API 门禁

```text
JWT/session -> active administrator admission -> permission -> MFA policy
                                                   -> SessionService.HasRecentMFA
```

管理 API 的 MFA 判定读取服务端 Session 状态，不相信浏览器时间戳或 JWT 中旧声明。这样管理员从普通用户登录入口取得的 Session 也不能绕过 `required` 策略。板块版主的 scoped permission 路径不被错误升级为全局管理员 MFA 门禁。

## 受控恢复

```text
local TTY + bootstrap secret + explicit confirmation
  -> active administrator admission check
  -> reliable command transaction
      -> disable active TOTP
      -> delete recovery-code digests
      -> revoke all sessions
      -> authorization audit + outbox security notification
```

恢复功能只位于 `campusosctl identity reset-mfa`，没有 HTTP 或 Plugin Host API。数据库保存加密 TOTP 信封、恢复码摘要、策略、ticket 摘要、Session MFA 强度和审计证据，不保存明文 TOTP Secret 或恢复码。

## 依赖边界

- `core.identity` 拥有 MFA Repository、Secret Protector、Session strength 和策略。
- `core.email-delivery` 只消费无敏感 payload 的本地恢复通知事件。
- Reliability 只负责可靠命令、审计和 Outbox；不解释 MFA Secret。
- 外部插件、Resource Package、Agent Runner、前端和 Admin UI 只使用受控 API，不读取 Repository、数据库或部署密钥。
