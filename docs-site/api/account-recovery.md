# 账号恢复与邮箱绑定

> 更新时间：2026-10-26 01:44（Asia/Shanghai）

普通 User 的邮件找回、邮箱绑定、管理员辅助恢复 Case，与独立 Admin 的本机恢复分别处理。User 的邮箱、密码、角色或恢复票据不构成 Admin 身份证明。

## 普通用户

1. 在登录页选择“找回密码”，输入已验证的个人邮箱。
2. 从邮件取得六位验证码；页面在内存中关联本次请求，不需要手动填写请求编号。
3. 验证后自行设置新密码。系统消费一次性 Ticket，并使该 User 的全部旧登录设备失效。

公共请求返回统一受理结果，避免泄露邮箱是否注册或是否可恢复。默认限制为每邮箱 10 分钟 5 次、每 IP 60 分钟 10 次；管理员可通过[验证码策略配置](/guide/challenge-policy)调整有界参数，不能关闭限流。

## 更换邮箱

登录后点击头像，打开“账号安全”，验证新的个人邮箱。成功后新邮箱成为该 User 的登录和找回邮箱，全部旧设备退出。

## 管理员辅助恢复普通用户

历史 User 没有可验证邮箱时，管理员先线下核验，再在“账号恢复”中创建 Case。页面只显示脱敏邮箱、状态和时限；管理员不能读取验证码、Ticket 或密码，也不能替用户设置新密码。用户仍须在新邮箱完成验证并自行提交新密码。

## 独立 Admin 的本机恢复

当前独立 Admin 未开放公网恢复票据消费 HTTP。已有 `admin_recovery_tickets` 仓储不表示已有邮件找回或票据消费入口；User 的公网找回与辅助 Case 也不能用于恢复 Admin。

系统 Admin 密码仅可在可信服务器的交互式 TTY 中恢复：

```bash
campusosctl identity reset-password --email admin@campusos.local --reason "approved system Admin password recovery"
```

目标邮箱固定为 `admin@campusos.local`，省略 `--email` 仍是该邮箱。命令要求逐字输入 `RESET admin@campusos.local`，再隐藏输入部署配置中的 `AUTH_BOOTSTRAP_ADMIN_SECRET`，最后两次隐藏输入一致的新密码。部署已配置 Secret 不替代操作者的终端证明；密码和证明均不接受参数、管道或 CI 输入。

命令只更新已有、活跃的独立系统 Admin 凭据。成功后该 Admin 的旧会话、刷新凭据、MFA 票据和恢复票据全部失效；角色绑定保持原样，不修改同邮箱或同号 User 的凭据、角色或会话。吊销的 Admin 不可通过本机恢复重新激活。状态、凭据、票据和会话变更与必需审计、Outbox 同事务提交；任何一步失败都会回滚。

其他本机命令使用独立 Admin 的不透明 `--account-id`，见[管理员准入与紧急恢复](../operations/admin-admission.md)及[管理员 MFA 与恢复](../operations/mfa.md)。完整 User 流程见仓库[账号恢复与邮箱绑定说明](https://github.com/javencpdd/CampusOS/blob/main/docs/help/%E7%B3%BB%E7%BB%9F%E8%AE%BE%E8%AE%A1%E7%9B%B8%E5%85%B3/v0.12%E8%B4%A6%E5%8F%B7%E6%81%A2%E5%A4%8D%E4%B8%8E%E9%82%AE%E7%AE%B1%E7%BB%91%E5%AE%9A%E8%AF%B4%E6%98%8E.md)。
