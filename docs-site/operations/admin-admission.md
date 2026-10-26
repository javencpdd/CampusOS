# 管理员准入与紧急恢复

> 更新时间：2026-10-26 01:44（Asia/Shanghai）

当前独立 Admin 使用自己的账号、密码凭据、MFA、会话与角色绑定。普通 User 的账号状态、全局 `admin` 角色、邮箱或同号 ID 都不能代替独立 Admin 身份。

## 区分独立 Admin 与旧准入兼容页

`/admin-admission` 页面及 `/api/v1/identity/admin-accounts` 系列接口保留的是 User 关联准入兼容能力，数据位于 `identity_admin_accounts`。其中的 `user_id` 不是独立 Admin 的 `account_id`，不能用于下述本机恢复。

独立 Admin 的账号生命周期接口为 `/api/v1/admin/identity/accounts/:id/suspend`、`/revoke` 与 `/restore`，由当前独立 Admin 权限、MFA 和服务端事实保护。恢复只适用于 `suspended`；`revoked` 是终态，旧 User 角色重新分配也不能解除独立 Admin 的吊销。恢复账号不会新增或扩张其角色权限。

## 可信本机恢复

在线管理不可用时，部署维护者可在可信服务器的交互式 TTY 中执行：

```bash
campusosctl identity restore-admin-admission --account-id adm.example --reason "approved incident recovery"
```

`adm.example` 只是示例，应替换为受控管理员创建回执或运维记录中的实际不透明 Admin ID。不能从 User ID、旧准入页或相同邮箱推导该 ID；旧 `--user-id` 参数已经退役。

命令要求：

1. 部署配置中已设置 `AUTH_BOOTSTRAP_ADMIN_SECRET`，且操作者使用真实本机 TTY。
2. 逐字输入 `RESTORE-ADMIN adm.example`，并隐藏输入该部署 Secret。
3. 目标是现有、已暂停的独立 Admin，且具备有效本域凭据；`--reason` 为非空、单行且不超过 500 字符的操作原因。
4. 数据库及必需事务审计可用。

Secret 不接受命令行参数、管道或自动化环境输入代替终端证明。成功只把目标从 `suspended` 恢复为 `active`，保持已有角色绑定；旧 Admin 会话、刷新凭据、MFA 票据和恢复票据全部失效，必须重新登录。操作不修改任何 User 的密码、准入、角色或会话，也不能恢复已吊销的 Admin。

状态、旧票据消费、会话撤销与必需审计、Outbox 必须同事务成功，失败不会留下部分恢复。这个本机入口不是公网恢复 API；当前独立 Admin 的公网恢复票据消费 HTTP 尚未开放。

系统 Admin 密码恢复见[账号恢复](../api/account-recovery.md)，TOTP 丢失见[MFA 本机恢复](./mfa.md)。详细边界见仓库[管理员准入管理与本地恢复](https://github.com/javencpdd/CampusOS/blob/main/docs/help/%E7%B3%BB%E7%BB%9F%E8%AE%BE%E8%AE%A1%E7%9B%B8%E5%85%B3/v0.13%E7%AE%A1%E7%90%86%E5%91%98%E5%87%86%E5%85%A5%E7%AE%A1%E7%90%86%E4%B8%8E%E6%9C%AC%E5%9C%B0%E6%81%A2%E5%A4%8D.md)。
