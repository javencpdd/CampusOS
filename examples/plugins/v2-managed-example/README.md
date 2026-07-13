# V2 受管数据示例

此目录展示 `campusos.plugin/v2` 的最小声明，不包含可执行 gRPC 二进制。

- `announcements` 是系统归属集合。扩展进程只能通过 Host API v2 的 `Record*` 方法读写它。
- `notes` 是用户归属集合。前端使用带 CampusOS 用户 JWT 的 `/api/v1/plugin-market/...` 接口访问，扩展不能指定或伪造用户 ID。
- 附件只保存在 `data/personal-space/<user>/plugins/v2-managed-example/`，并且只通过受管文件 API 访问。

在真实插件中添加 `go.mod`、可执行 `plugin` 文件、测试及迁移说明后，运行：

```bash
go run ./cmd/campusosctl plugin dev examples/plugins/v2-managed-example
```

签名使用离线私钥。私钥不应放入此目录或插件包：

```bash
go run ./cmd/campusosctl plugin sign examples/plugins/v2-managed-example --key-id organization-key --key-file /secure/private-key.txt
go run ./cmd/campusosctl plugin pack examples/plugins/v2-managed-example
```
