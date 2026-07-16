# 校园个人便签（官方 v2 外部插件示例）

这是一个可运行的 `campusos.plugin/v2` 官方示例。它不在生产启动时自动导入、安装或发布；管理员需要显式完成预检、安装和发布，普通用户才会在插件中心看到它。

## 它做什么

- `announcements` 是系统归属集合，只能经 Host API v2 访问。
- `notes` 是用户归属集合，用于保存用户主动创建的个人便签。
- 附件只保存在 `data/personal-space/<user>/plugins/v2-managed-example/`，不会暴露真实文件系统路径给插件。
- `main.go` 提供一个最小受控 Runtime：健康检查和 Extension Gateway 回显端点，用于验证安装、启动、授权和卸载流程。

示例 Runtime 不读取数据库、不接触 CampusOS JWT 私钥，也不接受用户 ID、文件路径或数据库连接作为参数。实际业务数据必须走 Host API 或 `/api/v1/plugin-market/...` 的用户受管接口。

## 本地开发与验证

```bash
go run ./cmd/campusosctl plugin dev examples/plugins/v2-managed-example
```

`plugin dev` 会编译示例 Runtime，并检查 `plugin.yaml`。开发环境中启动后，Runtime 默认监听 `127.0.0.1:19091`：

```bash
go run .
curl -fsS http://127.0.0.1:19091/health
```

管理员验证流程：导入插件包 -> 安全预检 -> 安装 -> 启动 Runtime -> 在插件市场发布。用户验证流程：查看用途和风险 -> 明确授权 -> 保存或导出自己的数据 -> 撤销授权或删除数据。

签名使用离线私钥。私钥不应放入此目录或插件包：

```bash
go run ./cmd/campusosctl plugin sign examples/plugins/v2-managed-example --key-id organization-key --key-file /secure/private-key.txt
go run ./cmd/campusosctl plugin pack examples/plugins/v2-managed-example
```

打包或导出不会包含 CampusOS Session、Access Token、Refresh Token、JWT 私钥或数据库凭据。
