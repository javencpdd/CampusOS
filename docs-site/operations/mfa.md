# 管理员 MFA 与恢复

## 启用前检查

在部署环境设置 `AUTH_MFA_ACTIVE_KEY_ID`、`AUTH_MFA_ENCRYPTION_KEYS`、`AUTH_MFA_ISSUER` 和 `AUTH_BOOTSTRAP_ADMIN_SECRET`。生产环境不得使用开发默认值。MFA 密钥不得写入仓库、插件、资源包、数据库普通配置、日志或导出包。

后台“身份与权限 -> MFA 策略”显示聚合覆盖率：不显示个人 TOTP Secret、恢复码或完整身份资料。建议顺序为：

1. `off`：先让管理员自行启用认证器并保存恢复码。
2. `enrollment_grace`：配置截止日期，督促仍未注册的管理员完成配置。
3. `required`：只有覆盖率和本机恢复条件满足时才允许保存；所有管理 API 都要求近期 Step-up。

## 丢失认证器

优先使用仍保存的一次性恢复码。若所有认证器和恢复码都丢失，服务器本机上的受控恢复命令是唯一后门：

```bash
campusosctl identity reset-mfa --user-id <管理员用户ID> --reason "lost device"
```

命令要求交互式终端、精确确认文本和隐藏输入的 `AUTH_BOOTSTRAP_ADMIN_SECRET`。它清除 MFA 和恢复码、撤销全部 Session、写入审计并触发不含敏感内容的安全通知。恢复后必须重新登录、重新注册 MFA 和保存新恢复码。

暂停管理员准入与重置 MFA 是不同流程。前者使用 `campusosctl identity restore-admin-admission`；详细边界见 [管理员准入与恢复](./admin-admission.md)。

## 密钥轮换

先把新旧密钥同时放入 `AUTH_MFA_ENCRYPTION_KEYS`，将 `AUTH_MFA_ACTIVE_KEY_ID` 切到新 key ID，确认旧信封已迁移或失效后才删除旧密钥。直接删除旧密钥会让关联记录无法安全读取。

完整 API 和错误码见 [多因素认证 API](../api/mfa.md)。
