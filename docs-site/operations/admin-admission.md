# 管理员准入与紧急恢复

管理员后台不是“有 admin 角色就永久可进入”。CampusOS 将身份凭据、管理平面准入和具体操作权限分开处理：

```text
users / accounts -> 身份与密码
identity_admin_accounts -> 是否能进入 Admin
roles / permissions -> 进入后能做什么
```

在 `http://localhost:3001/admin-admission`，管理员可以查看、暂停或恢复管理平面准入。暂停会立即撤销目标的
所有 Session，但不会删除普通用户资料、内容或文件。

## 安全操作顺序

1. 先确认目标用户和当前状态。
2. 使用“暂停准入”或“恢复准入”，并填写具体原因。
3. 遇到版本冲突时刷新页面，不要反复提交旧状态。
4. 最后一个有效管理员不能被暂停，服务端会在数据库事务中再次检查。
5. 查看“操作审计”确认状态变更、操作者和时间。

`revoked` 表示全局 `admin` 角色已经撤销，不能通过恢复按钮绕过角色治理；应先走正常角色分配流程。

## 紧急恢复

当所有准入都被暂停时，只能在可信服务器的交互式终端执行：

```bash
campusosctl identity restore-admin-admission --user-id 123 --reason "approved incident recovery"
```

命令需要本地 TTY、逐字确认和隐藏输入 `AUTH_BOOTSTRAP_ADMIN_SECRET`。它不提供远程 HTTP 后门，也不接受
Secret 参数或管道输入。恢复后旧 Session 仍然撤销，应重设密码、核验角色并完成 MFA 注册。

详细的权限、API、迁移和故障处理说明见仓库
[`v13 管理员准入管理与本地恢复`](https://github.com/javencpdd/CampusOS/blob/main/docs/help/%E7%B3%BB%E7%BB%9F%E8%AE%BE%E8%AE%A1%E7%9B%B8%E5%85%B3/v13%E7%AE%A1%E7%90%86%E5%91%98%E5%87%86%E5%85%A5%E7%AE%A1%E7%90%86%E4%B8%8E%E6%9C%AC%E5%9C%B0%E6%81%A2%E5%A4%8D.md)。

