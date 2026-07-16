# schedule-helper

这是 `localhost:3002/plugins/schedule-plugin-tutorial` 配套的 CampusOS Manifest v2 外部插件示例。

它演示：

- `terms` 和 `courses` 两个 `owner: user` 受管集合。
- 用户级 `managed_data` 与 `plugin_search` 授权。
- 受管进程的健康和 Extension 端点。
- 插件代码、插件数据与内置个人课表之间的边界。

它不会读取 `internal/modules/features/schedule`、PostgreSQL、CampusOS JWT 或 `data/personal-space` 的物理路径。用户记录只能由已登录用户通过 `/api/v1/plugin-market/schedule-helper/records/*` 访问。

验证：

```bash
go run ./cmd/campusosctl plugin dev examples/plugins/schedule-helper
go run ./cmd/campusosctl plugin pack examples/plugins/schedule-helper
```

当前 `runtime: grpc` 是历史兼容名称，实际使用受限 loopback HTTP Extension 合同，不代表标准 protobuf gRPC。
