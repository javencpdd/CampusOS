# 管理员 MFA 与恢复

> 更新时间：2026-10-26 01:44（Asia/Shanghai）

独立 Admin 的 TOTP、MFA 票据和会话与普通 User 分开存储和校验。User 的认证器、恢复码、邮件找回或管理员辅助 Case 不能作为独立 Admin 的恢复证明。

## 启用与部署配置

部署环境应设置 `AUTH_MFA_ACTIVE_KEY_ID`、`AUTH_MFA_ENCRYPTION_KEYS`、`AUTH_MFA_ISSUER` 和 `AUTH_BOOTSTRAP_ADMIN_SECRET`。生产环境不能使用开发默认值，密钥不得进入仓库、插件、资源包、日志或配置导出。

在管理端使用本人 MFA 入口完成认证器注册与近期 Step-up。独立 Admin 本人接口为 `/api/v1/admin/identity/mfa/enroll`、`/confirm` 和 `/step-up`，必须使用当前本域会话及 Admin CSRF；确认注册后需用新的 TOTP 时间步完成 Step-up。

`/mfa-policy` 页面保留的是 User 关联管理员策略兼容能力。其 `off`、宽限期或聚合覆盖率不能替代独立 Admin 的 TOTP 状态、当前角色与具体管理端点的权限/MFA 检查。

## 丢失认证器

当前独立 Admin 没有已开放的公网恢复票据消费 HTTP；不能因存在恢复票据仓储而假设有远程找回入口。普通 User 的一次性恢复码只适用于自己的 User 域。

独立 Admin 认证器丢失时，在可信服务器的交互式 TTY 执行：

```bash
campusosctl identity reset-mfa --account-id adm.example --reason "lost independent Admin authenticator"
```

把示例 ID 替换为实际不透明 Admin `account_id`。旧 `--user-id` 已退役，旧准入兼容页的 `user_id` 不能用于本命令。操作者必须逐字确认 `RESET-MFA adm.example`，再隐藏输入已配置的 `AUTH_BOOTSTRAP_ADMIN_SECRET`；Secret 不接受参数、管道或 CI 输入。

目标须是现有、活跃、具备有效本域凭据和待确认或已启用 TOTP 因子的独立 Admin。成功后关闭该 Admin 的 TOTP，消费旧 Admin MFA/恢复票据，并撤销其全部会话及刷新凭据；保留已有角色绑定，不授予新权限，不修改 User 域。所有变更、必需审计和 Outbox 在同一事务提交，失败则整体回滚。重新登录后应立即注册新的认证器；已吊销的 Admin 不可借此恢复。

账号暂停与 TOTP 丢失是不同操作。暂停账号应使用带 `--account-id` 的 `restore-admin-admission`，见[管理员准入与紧急恢复](./admin-admission.md)；只有固定系统 Admin 邮箱可通过[本机密码恢复](../api/account-recovery.md)重置密码。

## 密钥轮换

先保留新旧密钥的兼容读取能力，再将 `AUTH_MFA_ACTIVE_KEY_ID` 切换到新 key ID。确认旧信封已经迁移或失效后再移除旧密钥，不能用删除密钥代替用户或管理员的受控恢复。

普通 User 的 TOTP/恢复码接口见[多因素认证 API](../api/mfa.md)；独立 Admin 的接口应按本页所述本域路径使用。
